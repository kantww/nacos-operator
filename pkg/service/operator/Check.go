package operator

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	log "github.com/go-logr/logr"
	nacosgroupv1alpha1 "nacos.io/nacos-operator/api/v1alpha1"
	myErrors "nacos.io/nacos-operator/pkg/errors"
	"nacos.io/nacos-operator/pkg/service/k8s"
	nacosClient "nacos.io/nacos-operator/pkg/service/nacos"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type ICheckClient interface {
	Check(nacos *nacosgroupv1alpha1.Nacos)
}

type CheckClient struct {
	k8sService  k8s.Services
	logger      log.Logger
	nacosClient nacosClient.NacosClient
	k8sClient   crclient.Client
}

func NewCheckClient(logger log.Logger, k8sService k8s.Services, k8sClient crclient.Client) *CheckClient {
	return &CheckClient{
		k8sService: k8sService,
		logger:     logger,
		k8sClient:  k8sClient,
	}
}

func (c *CheckClient) CheckKind(nacos *nacosgroupv1alpha1.Nacos) []corev1.Pod {
	// 保证ss数量和cr副本数匹配
	ss, err := c.k8sService.GetStatefulSet(nacos.Namespace, nacos.Name)
	myErrors.EnsureNormal(err)

	if *ss.Spec.Replicas != *nacos.Spec.Replicas {
		panic(myErrors.New(myErrors.CODE_ERR_UNKNOW, "cr replicas is not equal ss replicas"))

	}

	// 检查正常的pod数量，根据实际情况。如果单实例，必须要有1个;集群要1/2以上
	pods, err := c.k8sService.GetStatefulSetReadPod(nacos.Namespace, nacos.Name)
	if len(pods) < (int(*nacos.Spec.Replicas)+1)/2 {
		panic(myErrors.New(myErrors.CODE_ERR_UNKNOW, "The number of ready pods is too less"))
	} else if len(pods) != int(*nacos.Spec.Replicas) {
		c.logger.V(0).Info("pod num is not right")
	}
	return pods
}

func (c *CheckClient) CheckNacos(nacos *nacosgroupv1alpha1.Nacos, pods []corev1.Pod) {
	leader := ""
    nacos.Status.Conditions = []nacosgroupv1alpha1.Condition{}
    identityKey, identityValue := c.resolveIdentityHeader(nacos)
	// 检查nacos是否访问通
	for _, pod := range pods {
        servers, err := c.nacosClient.GetClusterNodes(pod.Status.PodIP, identityKey, identityValue)
		myErrors.EnsureNormalMyError(err, myErrors.CODE_CLUSTER_FAILE)
		// 确保cr中实例个数和server数量相同
		myErrors.EnsureEqual(len(servers.Data), int(*nacos.Spec.Replicas), myErrors.CODE_CLUSTER_FAILE, "server num is not equal")
		for _, svc := range servers.Data {
			myErrors.EnsureEqual(svc.State, "UP", myErrors.CODE_CLUSTER_FAILE, "node is not up")
			if leader != "" {
				// 确保每个节点leader相同
				myErrors.EnsureEqual(leader, svc.ExtendInfo.RaftMetaData.MetaDataMap.NamingPersistentServiceV2.Leader,
					myErrors.CODE_CLUSTER_FAILE, "leader not equal")
			} else {
				leader = svc.ExtendInfo.RaftMetaData.MetaDataMap.NamingPersistentServiceV2.Leader
			}
			nacos.Status.Version = svc.ExtendInfo.Version
		}

		condition := nacosgroupv1alpha1.Condition{
			Status:   "true",
			Instance: pod.Status.PodIP,
			PodName:  pod.Name,
			NodeName: pod.Spec.NodeName,
		}
		leaderSplit := []string{}
		if strings.Index(leader, ".") > 0 {
			leaderSplit = strings.Split(leader, ".")
		} else {
			leaderSplit = strings.Split(leader, ":")
		}
		if len(leaderSplit) > 0 {
			if leaderSplit[0] == pod.Name {
				condition.Type = "leader"
			} else {
				condition.Type = "follower"
			}
		}
		nacos.Status.Conditions = append(nacos.Status.Conditions, condition)
	}

}

// 解析身份头（从 Secret 中读取；如未配置或读取失败，则回退到 spec.certification）
func (c *CheckClient) resolveIdentityHeader(nacos *nacosgroupv1alpha1.Nacos) (string, string) {
    ref := nacos.Spec.IdentitySecretRef
    if ref == nil || ref.Name == "" || c.k8sClient == nil {
        // No identity configured; return empty (no header)
        return "", ""
    }
    keyKey := ref.KeyKey
    if keyKey == "" {
        keyKey = "identity_key"
    }
    valKey := ref.ValueKey
    if valKey == "" {
        valKey = "identity_value"
    }

    var sec corev1.Secret
    if err := c.k8sClient.Get(context.TODO(), k8stypes.NamespacedName{Namespace: nacos.Namespace, Name: ref.Name}, &sec); err != nil {
        c.logger.V(0).Info("failed to read identity secret; skipping identity header", "name", ref.Name, "err", err)
        return "", ""
    }
    bKey, ok1 := sec.Data[keyKey]
    bVal, ok2 := sec.Data[valKey]
    if !ok1 || !ok2 {
        c.logger.V(0).Info("identity secret missing keys; skipping identity header", "keyKey", keyKey, "valueKey", valKey)
        return "", ""
    }
    return string(bKey), string(bVal)
}
