package testutil

import (
	"fmt"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewStatefulSet(name, namespace string, replicas int32, labels map[string]string) *appv1.StatefulSet {
	return &appv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nacos",
							Image: "nacos/nacos-server:v2.1.0",
						},
					},
				},
			},
		},
		Status: appv1.StatefulSetStatus{
			Replicas:        replicas,
			ReadyReplicas:   replicas,
			CurrentReplicas: replicas,
			UpdatedReplicas: replicas,
		},
	}
}

func NewConfigMap(name, namespace string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	}
}

func NewService(name, namespace string, labels map[string]string, serviceType corev1.ServiceType) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     serviceType,
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name: "http",
					Port: 8848,
				},
			},
		},
	}
}

func NewPodList(namespace, nacosName string, count int) []corev1.Pod {
	pods := make([]corev1.Pod, count)
	for i := 0; i < count; i++ {
		podName := fmt.Sprintf("%s-%d", nacosName, i)
		podIP := fmt.Sprintf("10.244.0.%d", i+1)
		pods[i] = *NewReadyPod(podName, namespace, nacosName, podIP)
	}
	return pods
}
