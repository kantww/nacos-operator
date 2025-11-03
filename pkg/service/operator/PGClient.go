package operator

import (
    "context"
    "fmt"
    "time"

    log "github.com/go-logr/logr"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    nacosgroupv1alpha1 "nacos.io/nacos-operator/api/v1alpha1"
    myErrors "nacos.io/nacos-operator/pkg/errors"
    "sigs.k8s.io/controller-runtime/pkg/client"

    "github.com/jackc/pgx/v5"
    "strings"
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

    // Decide whether to run init based on status + policy
    // Read ConfigMap SQL (also used to get RV and checksum) and Secret RV first
    cmName := ""
    if nacos.Spec.PGInit.ConfigMapRef != nil {
        cmName = nacos.Spec.PGInit.ConfigMapRef.Name
    }
    sqlKey := nacos.Spec.PGInit.SQLKey
    if sqlKey == "" { sqlKey = "nacos-pg.sql" }
    cmRV := ""
    sql := ""
    if cmName != "" {
        var cm corev1.ConfigMap
        if err := p.k8sClient.Get(context.Background(), types.NamespacedName{Namespace: nacos.Namespace, Name: cmName}, &cm); err == nil {
            cmRV = cm.ResourceVersion
            if s, ok := cm.Data[sqlKey]; ok { sql = s }
        } else {
            panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "get ConfigMap %s/%s failed: %v", nacos.Namespace, cmName, err))
        }
    } else {
        panic(myErrors.New(myErrors.CODE_PARAMETER_ERROR, "pgInit.configMapRef.name is required when pgInit.enabled = true"))
    }
    if sql == "" {
        panic(myErrors.New(myErrors.CODE_PARAMETER_ERROR, "ConfigMap %s key %s is empty or missing", cmName, sqlKey))
    }

    // Secret RV
    secRV := p.readDBSecretRV(nacos)
    sqlChecksum := shortSHA256(sql)

    desiredVer := nacos.Spec.PGInit.SchemaVersion
    if desiredVer == 0 { desiredVer = 1 }
    policy := nacos.Spec.PGInit.Policy
    if policy == "" { policy = "IfNotPresent" }

    shouldInit := false
    if !nacos.Status.PG.Initialized {
        shouldInit = true
    } else {
        switch policy {
        case "Never":
            shouldInit = false
        case "Always":
            shouldInit = true
        case "BumpVersion":
            shouldInit = nacos.Status.PG.InitVersion < desiredVer
        default: // IfNotPresent
            // Run only if version behind or inputs changed
            if nacos.Status.PG.InitVersion < desiredVer ||
               nacos.Status.PG.LastInitCMResourceVersion != cmRV ||
               nacos.Status.PG.LastInitSecretResourceVersion != secRV ||
               nacos.Status.PG.LastInitSQLChecksum != sqlChecksum {
                shouldInit = true
            }
        }
    }
    if !shouldInit {
        p.logger.V(0).Info("postgres already initialized; skipping", "version", nacos.Status.PG.InitVersion)
        return
    }

    // Optional: Only as a guard, not the main decision (status-driven)
    // If sentinel exists and policy=IfNotPresent with no changes (shouldInit would be false), code won’t reach here.

    // Execute multi-statement script via simple protocol
    if _, err := conn.Exec(ctx, sql); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "execute init sql failed: %v", err))
    }

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
    nacos.Status.PG.LastInitConfigMap = cmName
    nacos.Status.PG.LastInitSQLKey = sqlKey
    nacos.Status.PG.LastInitCMResourceVersion = cmRV
    nacos.Status.PG.LastInitSecretResourceVersion = secRV
    nacos.Status.PG.LastInitSQLChecksum = sqlChecksum
    nacos.Status.PG.LastResult = "Success"
    nacos.Status.PG.LastMessage = ""
    // Persist status
    if err := p.k8sClient.Status().Update(context.Background(), nacos); err != nil {
        p.logger.V(0).Info("update status.pg failed", "error", err.Error())
    }
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

func (p *PGClient) loadInitSQL(nacos *nacosgroupv1alpha1.Nacos) string {
    // Require ConfigMap per user requirement
    if nacos.Spec.PGInit.ConfigMapRef == nil || nacos.Spec.PGInit.ConfigMapRef.Name == "" {
        panic(myErrors.New(myErrors.CODE_PARAMETER_ERROR, "pgInit.configMapRef.name is required when pgInit.enabled = true"))
    }
    key := nacos.Spec.PGInit.SQLKey
    if key == "" {
        key = "nacos-pg.sql"
    }
    var cm corev1.ConfigMap
    if err := p.k8sClient.Get(context.Background(), types.NamespacedName{Namespace: nacos.Namespace, Name: nacos.Spec.PGInit.ConfigMapRef.Name}, &cm); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "get ConfigMap %s/%s failed: %v", nacos.Namespace, nacos.Spec.PGInit.ConfigMapRef.Name, err))
    }
    if s, ok := cm.Data[key]; ok && s != "" {
        return s
    }
    panic(myErrors.New(myErrors.CODE_PARAMETER_ERROR, "ConfigMap %s key %s is empty or missing", nacos.Spec.PGInit.ConfigMapRef.Name, key))
}

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
