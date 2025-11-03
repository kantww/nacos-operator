package operator

import (
    "context"
    "fmt"
    "time"

    log "github.com/go-logr/logr"
    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/types"
    nacosgroupv1alpha1 "nacos.io/nacos-operator/api/v1alpha1"
    myErrors "nacos.io/nacos-operator/pkg/errors"
    "sigs.k8s.io/controller-runtime/pkg/client"

    "github.com/jackc/pgx/v5"
    "strings"
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

    // Initialization disabled
    if !nacos.Spec.PGInit.Enabled {
        return
    }

    // Check sentinel table existence
    var reg string
    if err := conn.QueryRow(ctx, "select coalesce(to_regclass('public.nacos_schema_version')::text,'')").Scan(&reg); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "check sentinel table failed: %v", err))
    }
    if reg != "" {
        // already initialized
        p.logger.V(0).Info("postgres already initialized", "table", reg)
        return
    }

    // Load SQL script (ConfigMap only)
    sql := p.loadInitSQL(nacos)
    if sql == "" {
        panic(myErrors.New(myErrors.CODE_PARAMETER_ERROR, "init sql is empty from ConfigMap"))
    }

    // Execute multi-statement script via simple protocol
    if _, err := conn.Exec(ctx, sql); err != nil {
        panic(myErrors.New(myErrors.CODE_ERR_SYSTEM, "execute init sql failed: %v", err))
    }

    p.logger.V(0).Info("postgres init finished")
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
