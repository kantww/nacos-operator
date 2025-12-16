package operator

import (
	log "github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	nacosgroupv1alpha1 "nacos.io/nacos-operator/api/v1alpha1"
	myErrors "nacos.io/nacos-operator/pkg/errors"
	"nacos.io/nacos-operator/pkg/service/k8s"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type IOperatorClient interface {
	IKindClient
	ICheckClient
	IHealClient
	IStatusClient
}

type OperatorClient struct {
    KindClient   *KindClient
    CheckClient  *CheckClient
    HealClient   *HealClient
    StatusClient *StatusClient
    PGClient     *PGClient
}

func NewOperatorClient(logger log.Logger, clientset kubernetes.Interface, s *runtime.Scheme, client client.Client) *OperatorClient {
	service := k8s.NewK8sService(clientset, logger)
    return &OperatorClient{
		// 资源客户端
		KindClient: NewKindClient(logger, service, s),
        // 检测客户端
        CheckClient: NewCheckClient(logger, service, client),
		// 状态客户端
		StatusClient: NewStatusClient(logger, service, client),
		// 维护客户端
        HealClient: NewHealClient(logger, service),
        PGClient:   NewPGClient(logger, client),
    }
}

func (c *OperatorClient) MakeEnsure(nacos *nacosgroupv1alpha1.Nacos) {
	// 验证CR字段
	c.KindClient.ValidationField(nacos)

	switch nacos.Spec.Type {
	case TYPE_STAND_ALONE:
		c.KindClient.EnsureConfigmap(nacos)
		c.KindClient.EnsureStatefulset(nacos)
		c.KindClient.EnsureService(nacos)
		// also expose client ports via NodePort service in standalone mode
		c.KindClient.EnsureClientService(nacos)
		if nacos.Spec.Database.TypeDatabase == "mysql" && nacos.Spec.MysqlInitImage != "" {
			c.KindClient.EnsureMysqlConfigMap(nacos)
			c.KindClient.EnsureJob(nacos)
		}
	case TYPE_CLUSTER:
		c.KindClient.EnsureConfigmap(nacos)
		c.KindClient.EnsureStatefulsetCluster(nacos)
		c.KindClient.EnsureHeadlessServiceCluster(nacos)
		c.KindClient.EnsureClientService(nacos)
		if nacos.Spec.Database.TypeDatabase == "mysql" && nacos.Spec.MysqlInitImage != "" {
			c.KindClient.EnsureMysqlConfigMap(nacos)
			c.KindClient.EnsureJob(nacos)
		}
	default:
		panic(myErrors.New(myErrors.CODE_PARAMETER_ERROR, myErrors.MSG_PARAMETER_ERROT, "nacos.Spec.Type", nacos.Spec.Type))
	}
}

func (c *OperatorClient) PreCheck(nacos *nacosgroupv1alpha1.Nacos) {
	switch nacos.Status.Phase {
	case nacosgroupv1alpha1.PhaseFailed:
		// 失败，需要修复
		c.HealClient.MakeHeal(nacos)
	case nacosgroupv1alpha1.PhaseNone:
		// 初始化
		nacos.Status.Phase = nacosgroupv1alpha1.PhaseCreating
		nacos.Status.Healthy = false
		panic(myErrors.New(myErrors.CODE_NORMAL, ""))
	case nacosgroupv1alpha1.PhaseScale:
	default:
		// TODO
	}
}

// PGEnsure: 在确保 K8s 资源前进行 PG 连通性校验与初始化
func (c *OperatorClient) PGEnsure(nacos *nacosgroupv1alpha1.Nacos) {
    // 未配置 Postgres 则跳过
    if nacos.Spec.Postgres.Host == "" {
        return
    }
    // 若显式关闭初始化，直接跳过
    if !nacos.Spec.PGInit.Enabled {
        return
    }
    c.PGClient.PingAndInit(nacos)
}

func (c *OperatorClient) CheckAndMakeHeal(nacos *nacosgroupv1alpha1.Nacos) {
	// 检查kind
	pods := c.CheckClient.CheckKind(nacos)
	// 检查nacos
	c.CheckClient.CheckNacos(nacos, pods)
}

func (c *OperatorClient) UpdateStatus(nacos *nacosgroupv1alpha1.Nacos) {
    c.StatusClient.UpdateStatusRunning(nacos)
}

// RotateAdmin: rotate admin password via direct DB if needed
func (c *OperatorClient) RotateAdmin(nacos *nacosgroupv1alpha1.Nacos) {
    if c.PGClient == nil {
        return
    }
    if nacos.Spec.Postgres.Host == "" {
        return
    }
    if nacos.Spec.AdminCredentialsSecretRef.Name == "" {
        return
    }
    c.PGClient.RotateAdminPassword(nacos)
}
