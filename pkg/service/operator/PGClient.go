package operator

import (
    "context"
    "fmt"
    "time"
    "os"
    "strings"

    log "github.com/go-logr/logr"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    nacosgroupv1alpha1 "nacos.io/nacos-operator/api/v1alpha1"
    myErrors "nacos.io/nacos-operator/pkg/errors"
    "sigs.k8s.io/controller-runtime/pkg/client"

    "github.com/jackc/pgx/v5"
    "crypto/sha256"
    "encoding/hex"
)

type PGClient struct {
    logger    log.Logger
    k8sClient client.Client
}

func NewPGClient(logger log.Logger, c client.Client) *PGClient {
    return &PGClient{logger: logger, k8sClient: c}
}

const initSQLPath = "config/sql/nacos-pg.sql"

// PingAndInit performs Postgres connectivity check and optional initialization (idempotent script execution).
func (p *PGClient) PingAndInit(nacos *nacosgroupv1alpha1.Nacos) {
    // Build DSN from spec.postgres + secret
    user, pass := p.readDBCredentials(nacos)
    host := nacos.Spec.Postgres.Host
    port := nacos.Spec.Postgres.Port
    if port == "" {
        port = "5432"
    }
    database := nacos.Spec.Postgres.Database
    if host == "" || database == "" || user == "" {
        panic(myErrors.New(myErrors.CODE_PARAMETER_ERROR, "postgres config invalid: host/user/database must be set"))
    }

    dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", urlQueryEscape(user), urlQueryEscape(pass), host, port, database)

    // Connect with simple protocol to allow multi-statement execution
    cfg, err := pgx.ParseConfig(dsn)
    if err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "pgx parse dsn failed: %v", err))
    }
    cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

    timeout := time.Duration(0)
    if nacos.Spec.PGInit.TimeoutSeconds > 0 {
        timeout = time.Duration(nacos.Spec.PGInit.TimeoutSeconds) * time.Second
    } else {
        timeout = 10 * time.Second
    }
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()

    conn, err := pgx.ConnectConfig(ctx, cfg)
    if err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "postgres connect failed: %v", err))
    }
    defer conn.Close(context.Background())

    // Ping
    if _, err := conn.Exec(ctx, "select 1"); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "postgres ping failed: %v", err))
    }

    // Read-only checks
    var inRecovery bool
    if err := conn.QueryRow(ctx, "select pg_is_in_recovery()").Scan(&inRecovery); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "check pg_is_in_recovery failed: %v", err))
    }
    var ro string
    if err := conn.QueryRow(ctx, "show transaction_read_only").Scan(&ro); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "check transaction_read_only failed: %v", err))
    }
    if inRecovery || ro == "on" {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "postgres is read-only (pg_is_in_recovery=%v, transaction_read_only=%s)", inRecovery, ro))
    }

    // Initialization disabled
    if !nacos.Spec.PGInit.Enabled {
        return
    }

    // Simplified: Only run once when not initialized
    if nacos.Status.PG.Initialized {
        p.logger.V(0).Info("postgres already initialized; skipping")
        return
    }

    // Read SQL from fixed path inside the image
    data, err := os.ReadFile(initSQLPath)
    if err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "read init sql failed from %s: %v", initSQLPath, err))
    }
    sql := string(data)
    if sql == "" {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "init sql at %s is empty", initSQLPath))
    }

    // Optional: Only as a guard, not the main decision (status-driven)
    // If sentinel exists and policy=IfNotPresent with no changes (shouldInit would be false), code won’t reach here.

    // Execute multi-statement script via simple protocol
    if _, err := conn.Exec(ctx, sql); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "execute init sql failed: %v", err))
    }

    // Determine desired schema version (default 1)
    desiredVer := nacos.Spec.PGInit.SchemaVersion
    if desiredVer == 0 { desiredVer = 1 }

    // Ensure/Upsert sentinel version table
    if _, err := conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS \"nacos_schema_version\" (version int NOT NULL PRIMARY KEY, updated_at timestamptz NOT NULL DEFAULT now())"); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "create sentinel table failed: %v", err))
    }
    // Upsert version (simple: delete+insert to avoid ON CONFLICT requirement)
    if _, err := conn.Exec(ctx, "DELETE FROM \"nacos_schema_version\";"); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "clear sentinel version failed: %v", err))
    }
    if _, err := conn.Exec(ctx, fmt.Sprintf("INSERT INTO \"nacos_schema_version\"(version) VALUES (%d)", desiredVer)); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "write sentinel version failed: %v", err))
    }

    p.logger.V(0).Info("postgres init finished")

    // Update status fields
    nacos.Status.PG.Initialized = true
    nacos.Status.PG.InitVersion = desiredVer
    nacos.Status.PG.LastInitTime = metav1.Now()
    nacos.Status.PG.LastResult = "Success"
    nacos.Status.PG.LastMessage = ""
    // Persist status
    if err := p.k8sClient.Status().Update(context.Background(), nacos); err != nil {
        p.logger.V(0).Info("update status.pg failed", "error", err.Error())
    }
}

// RotateAdminPassword updates the admin user's bcrypt hash in DB if inputs changed.
func (p *PGClient) RotateAdminPassword(nacos *nacosgroupv1alpha1.Nacos) {
    // No admin secret configured → skip
    if nacos.Spec.AdminCredentialsSecretRef.Name == "" {
        return
    }

    // Read admin secret (username + passwordHash)
    adminUser, passwordHash, adminSecRV, adminSecChecksum := p.readAdminSecret(nacos)

    // Decide if rotation is needed
    // 1) If spec.AdminSecretChecksum is provided, use it as the primary trigger
    if nacos.Spec.AdminSecretChecksum != "" && nacos.Spec.AdminSecretChecksum == nacos.Status.Admin.LastSecretChecksum {
        return
    }
    // 2) Otherwise, compare secret RV or checksum
    if nacos.Spec.AdminSecretChecksum == "" {
        if nacos.Status.Admin.LastSecretResourceVersion == adminSecRV || nacos.Status.Admin.LastSecretChecksum == adminSecChecksum {
            // nothing changed
            // still allow rotation if first time (no last result)
            if nacos.Status.Admin.LastResult == "Success" {
                return
            }
        }
    }

    // Build DSN and connect (reuse PG creds)
    user, pass := p.readDBCredentials(nacos)
    host := nacos.Spec.Postgres.Host
    port := nacos.Spec.Postgres.Port
    if port == "" { port = "5432" }
    database := nacos.Spec.Postgres.Database
    if host == "" || database == "" || user == "" {
        panic(myErrors.New(myErrors.CODE_PARAMETER_ERROR, "postgres config invalid: host/user/database must be set"))
    }
    dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", urlQueryEscape(user), urlQueryEscape(pass), host, port, database)
    cfg, err := pgx.ParseConfig(dsn)
    if err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "pgx parse dsn failed: %v", err))
    }
    cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
    timeout := 10 * time.Second
    if nacos.Spec.PGInit.TimeoutSeconds > 0 { timeout = time.Duration(nacos.Spec.PGInit.TimeoutSeconds) * time.Second }
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    conn, err := pgx.ConnectConfig(ctx, cfg)
    if err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "postgres connect failed: %v", err))
    }
    defer conn.Close(context.Background())
    if _, err := conn.Exec(ctx, "select 1"); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "postgres ping failed: %v", err))
    }
    // Ensure not read-only
    var inRecovery bool
    if err := conn.QueryRow(ctx, "select pg_is_in_recovery()").Scan(&inRecovery); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "check pg_is_in_recovery failed: %v", err))
    }
    var ro string
    if err := conn.QueryRow(ctx, "show transaction_read_only").Scan(&ro); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "check transaction_read_only failed: %v", err))
    }
    if inRecovery || ro == "on" {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "postgres is read-only (pg_is_in_recovery=%v, transaction_read_only=%s)", inRecovery, ro))
    }

    // Upsert admin user with new bcrypt hash
    // Try update; if no row, insert
    tag, err := conn.Exec(ctx, "UPDATE \"users\" SET \"password\"=$1, \"enabled\"=TRUE WHERE \"username\"=$2", passwordHash, adminUser)
    if err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "update users failed: %v", err))
    }
    if tag.RowsAffected() == 0 {
        if _, err := conn.Exec(ctx, "INSERT INTO \"users\"(\"username\",\"password\",\"enabled\") VALUES ($1,$2,TRUE)", adminUser, passwordHash); err != nil {
            panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "insert users failed: %v", err))
        }
    }
    // Ensure role
    if _, err := conn.Exec(ctx, "INSERT INTO \"roles\"(\"username\",\"role\") SELECT $1,'ROLE_ADMIN' WHERE NOT EXISTS (SELECT 1 FROM \"roles\" WHERE \"username\"=$1 AND \"role\"='ROLE_ADMIN')", adminUser); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "ensure role failed: %v", err))
    }

    p.logger.V(0).Info("admin password rotation finished")

    // Update status
    nacos.Status.Admin.LastRotateTime = metav1.Now()
    nacos.Status.Admin.LastResult = "Success"
    nacos.Status.Admin.LastMessage = ""
    nacos.Status.Admin.LastSecretResourceVersion = adminSecRV
    if nacos.Spec.AdminSecretChecksum != "" {
        nacos.Status.Admin.LastSecretChecksum = nacos.Spec.AdminSecretChecksum
    } else {
        nacos.Status.Admin.LastSecretChecksum = adminSecChecksum
    }
    _ = p.k8sClient.Status().Update(context.Background(), nacos)
}

func (p *PGClient) readAdminSecret(nacos *nacosgroupv1alpha1.Nacos) (username, passwordHash, rv, checksum string) {
    ref := nacos.Spec.AdminCredentialsSecretRef
    if ref.UsernameKey == "" { ref.UsernameKey = "username" }
    if ref.PasswordHashKey == "" { ref.PasswordHashKey = "passwordHash" }
    var sec corev1.Secret
    if err := p.k8sClient.Get(context.Background(), types.NamespacedName{Namespace: nacos.Namespace, Name: ref.Name}, &sec); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "get admin secret %s/%s failed: %v", nacos.Namespace, ref.Name, err))
    }
    u, ok := sec.Data[ref.UsernameKey]
    if !ok { panic(myErrors.New(myErrors.CODE_PARAMETER_ERROR, "admin secret missing key %s", ref.UsernameKey)) }
    ph, ok := sec.Data[ref.PasswordHashKey]
    if !ok { panic(myErrors.New(myErrors.CODE_PARAMETER_ERROR, "admin secret missing key %s", ref.PasswordHashKey)) }
    rv = sec.ResourceVersion
    // checksum over username + ':' + passwordHash
    c := shortSHA256(string(u) + ":" + string(ph))
    return string(u), string(ph), rv, c
}

func (p *PGClient) readDBCredentials(nacos *nacosgroupv1alpha1.Nacos) (string, string) {
    ref := nacos.Spec.Postgres.CredentialsSecretRef
    if ref.Name == "" {
        panic(myErrors.New(myErrors.CODE_PARAMETER_ERROR, "postgres.credentialsSecretRef.name is required"))
    }
    userKey := ref.UsernameKey
    passKey := ref.PasswordKey
    if userKey == "" {
        userKey = "username"
    }
    if passKey == "" {
        passKey = "password"
    }
    var sec corev1.Secret
    if err := p.k8sClient.Get(context.Background(), types.NamespacedName{Namespace: nacos.Namespace, Name: ref.Name}, &sec); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "get secret %s/%s failed: %v", nacos.Namespace, ref.Name, err))
    }
    userBytes, ok := sec.Data[userKey]
    if !ok {
        panic(myErrors.New(myErrors.CODE_PARAMETER_ERROR, "secret %s missing key %s", ref.Name, userKey))
    }
    passBytes, ok := sec.Data[passKey]
    if !ok {
        panic(myErrors.New(myErrors.CODE_PARAMETER_ERROR, "secret %s missing key %s", ref.Name, passKey))
    }
    return string(userBytes), string(passBytes)
}

func (p *PGClient) readDBSecretRV(nacos *nacosgroupv1alpha1.Nacos) string {
    ref := nacos.Spec.Postgres.CredentialsSecretRef
    if ref.Name == "" { return "" }
    var sec corev1.Secret
    if err := p.k8sClient.Get(context.Background(), types.NamespacedName{Namespace: nacos.Namespace, Name: ref.Name}, &sec); err != nil {
        return ""
    }
    return sec.ResourceVersion
}

// legacy loadInitSQL removed: init SQL now read from fixed image path

func shortSHA256(s string) string {
    h := sha256.Sum256([]byte(s))
    // return first 16 hex chars for brevity
    return hex.EncodeToString(h[:])[:16]
}

// urlQueryEscape performs minimal escaping for DSN components.
func urlQueryEscape(s string) string {
    // Avoid pulling net/url just for user:pass escaping; replace only critical characters.
    r := s
    r = strings.ReplaceAll(r, "@", "%40")
    r = strings.ReplaceAll(r, ":", "%3A")
    r = strings.ReplaceAll(r, "/", "%2F")
    r = strings.ReplaceAll(r, "?", "%3F")
    r = strings.ReplaceAll(r, "#", "%23")
    return r
}
