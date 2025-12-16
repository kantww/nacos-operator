package testutil

import (
	"context"
	"fmt"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// K8sSimulator simulates Kubernetes behavior for testing
type K8sSimulator struct {
	client     client.Client
	kubeClient kubernetes.Interface
}

func NewK8sSimulator(client client.Client) *K8sSimulator {
	return &K8sSimulator{client: client}
}

func (s *K8sSimulator) SetKubeClient(kubeClient kubernetes.Interface) {
	s.kubeClient = kubeClient
}

// SimulateStatefulSetPods creates pods based on StatefulSet spec
func (s *K8sSimulator) SimulateStatefulSetPods(ctx context.Context, namespace, stsName string) error {
	return s.SimulateStatefulSetPodsWithIP(ctx, namespace, stsName, "")
}

// SimulateStatefulSetPodsWithIP creates pods based on StatefulSet spec with custom IP
func (s *K8sSimulator) SimulateStatefulSetPodsWithIP(ctx context.Context, namespace, stsName, customIP string) error {
	sts := &appv1.StatefulSet{}
	if err := s.client.Get(ctx, types.NamespacedName{Name: stsName, Namespace: namespace}, sts); err != nil {
		return err
	}

	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	for i := int32(0); i < replicas; i++ {
		podName := fmt.Sprintf("%s-%d", stsName, i)
		var podIP string
		if customIP != "" {
			podIP = customIP
		} else {
			podIP = fmt.Sprintf("10.244.0.%d", i+10)
		}

		pod := &corev1.Pod{}
		err := s.client.Get(ctx, types.NamespacedName{Name: podName, Namespace: namespace}, pod)
		if err == nil {
			// Pod exists, update IP if needed
			if pod.Status.PodIP != podIP {
				pod.Status.PodIP = podIP
				s.client.Status().Update(ctx, pod)
				if s.kubeClient != nil {
					s.kubeClient.CoreV1().Pods(namespace).Update(ctx, pod, metav1.UpdateOptions{})
				}
			}
			continue
		}

		newPod := &corev1.Pod{
			ObjectMeta: sts.Spec.Template.ObjectMeta,
			Spec:       sts.Spec.Template.Spec,
		}
		newPod.Name = podName
		newPod.Namespace = namespace
		newPod.Status.Phase = corev1.PodRunning
		newPod.Status.PodIP = podIP
		// Kubernetes pods have 4 standard conditions
		newPod.Status.Conditions = []corev1.PodCondition{
			{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
			{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			{Type: corev1.PodInitialized, Status: corev1.ConditionTrue},
			{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
		}

		// Create in controller-runtime client
		if err := s.client.Create(ctx, newPod); err != nil {
			return err
		}

		// Also create in kubernetes clientset for operator to find
		if s.kubeClient != nil {
			s.kubeClient.CoreV1().Pods(namespace).Create(ctx, newPod, metav1.CreateOptions{})
		}
	}

	sts.Status.Replicas = replicas
	sts.Status.ReadyReplicas = replicas
	sts.Status.CurrentReplicas = replicas
	sts.Status.UpdatedReplicas = replicas
	return s.client.Status().Update(ctx, sts)
}

// UpdateStatefulSetStatus updates StatefulSet status to reflect current state
func (s *K8sSimulator) UpdateStatefulSetStatus(ctx context.Context, namespace, stsName string, readyReplicas int32) error {
	sts := &appv1.StatefulSet{}
	if err := s.client.Get(ctx, types.NamespacedName{Name: stsName, Namespace: namespace}, sts); err != nil {
		return err
	}

	sts.Status.ReadyReplicas = readyReplicas
	sts.Status.Replicas = readyReplicas
	sts.Status.CurrentReplicas = readyReplicas
	sts.Status.UpdatedReplicas = readyReplicas
	return s.client.Status().Update(ctx, sts)
}

// SimulatePodsReady marks pods as ready
func (s *K8sSimulator) SimulatePodsReady(ctx context.Context, namespace, stsName string, count int) error {
	for i := 0; i < count; i++ {
		podName := fmt.Sprintf("%s-%d", stsName, i)
		pod := &corev1.Pod{}
		if err := s.client.Get(ctx, types.NamespacedName{Name: podName, Namespace: namespace}, pod); err != nil {
			continue
		}

		pod.Status.Phase = corev1.PodRunning
		// Kubernetes pods have 4 standard conditions
		pod.Status.Conditions = []corev1.PodCondition{
			{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
			{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			{Type: corev1.PodInitialized, Status: corev1.ConditionTrue},
			{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
		}
		if err := s.client.Status().Update(ctx, pod); err != nil {
			return err
		}
	}
	return nil
}
