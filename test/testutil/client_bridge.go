package testutil

import (
	"context"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClientBridge syncs resources between kubernetes clientset and controller-runtime client
type ClientBridge struct {
	kubeClient kubernetes.Interface
	ctrlClient client.Client
}

func NewClientBridge(kubeClient kubernetes.Interface, ctrlClient client.Client) *ClientBridge {
	return &ClientBridge{
		kubeClient: kubeClient,
		ctrlClient: ctrlClient,
	}
}

// SyncFromKubeToCtrl syncs resources from kubernetes clientset to controller-runtime client
func (b *ClientBridge) SyncFromKubeToCtrl(ctx context.Context, namespace string) error {
	// Sync ConfigMaps
	cmList, err := b.kubeClient.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, cm := range cmList.Items {
			existing := &corev1.ConfigMap{}
			err := b.ctrlClient.Get(ctx, client.ObjectKey{Name: cm.Name, Namespace: cm.Namespace}, existing)
			if err != nil {
				if err := b.ctrlClient.Create(ctx, &cm); err != nil {
					continue
				}
			}
		}
	}

	// Sync StatefulSets
	stsList, err := b.kubeClient.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, sts := range stsList.Items {
			existing := &appv1.StatefulSet{}
			err := b.ctrlClient.Get(ctx, client.ObjectKey{Name: sts.Name, Namespace: sts.Namespace}, existing)
			if err != nil {
				if err := b.ctrlClient.Create(ctx, &sts); err != nil {
					continue
				}
			} else {
				existing.Spec = sts.Spec
				existing.Status = sts.Status
				b.ctrlClient.Update(ctx, existing)
			}
		}
	}

	// Sync Services
	svcList, err := b.kubeClient.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, svc := range svcList.Items {
			existing := &corev1.Service{}
			err := b.ctrlClient.Get(ctx, client.ObjectKey{Name: svc.Name, Namespace: svc.Namespace}, existing)
			if err != nil {
				if err := b.ctrlClient.Create(ctx, &svc); err != nil {
					continue
				}
			}
		}
	}

	return nil
}
