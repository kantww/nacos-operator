package k8s

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"nacos.io/nacos-operator/test/testutil"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("StatefulSetService", func() {
	var (
		fakeClient *fake.Clientset
		service    StatefulSet
		namespace  string
		logger     = ctrl.Log.WithName("test")
	)

	BeforeEach(func() {
		fakeClient = fake.NewSimpleClientset()
		service = NewStatefulSetService(fakeClient, logger)
		namespace = "default"
	})

	Describe("CreateStatefulSet", func() {
		It("should create a new StatefulSet successfully", func() {
			labels := map[string]string{"app": "nacos", "middleware": "nacos"}
			ss := testutil.NewStatefulSet("test-nacos", namespace, 3, labels)

			err := service.CreateStatefulSet(namespace, ss)
			Expect(err).NotTo(HaveOccurred())

			retrieved, err := service.GetStatefulSet(namespace, "test-nacos")
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.Name).To(Equal("test-nacos"))
			Expect(*retrieved.Spec.Replicas).To(Equal(int32(3)))
		})
	})

	Describe("GetStatefulSet", func() {
		It("should return error when StatefulSet does not exist", func() {
			_, err := service.GetStatefulSet(namespace, "non-existent")
			Expect(err).To(HaveOccurred())
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("GetStatefulSetPods", func() {
		It("should get pods belonging to a StatefulSet", func() {
			labels := map[string]string{"app": "test-nacos", "middleware": "nacos"}
			ss := testutil.NewStatefulSet("test-nacos", namespace, 3, labels)
			err := service.CreateStatefulSet(namespace, ss)
			Expect(err).NotTo(HaveOccurred())

			for i := 0; i < 3; i++ {
				podName := fmt.Sprintf("test-nacos-%d", i)
				podIP := fmt.Sprintf("10.244.0.%d", i+1)
				pod := testutil.NewReadyPod(podName, namespace, "test-nacos", podIP)
				_, err := fakeClient.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
			}

			pods, err := service.GetStatefulSetPods(namespace, "test-nacos")
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(3))
		})
	})

	Describe("GetStatefulSetReadPod", func() {
		It("should get only ready pods", func() {
			labels := map[string]string{"app": "test-nacos", "middleware": "nacos"}
			ss := testutil.NewStatefulSet("test-nacos", namespace, 3, labels)
			err := service.CreateStatefulSet(namespace, ss)
			Expect(err).NotTo(HaveOccurred())

			for i := 0; i < 2; i++ {
				podName := fmt.Sprintf("test-nacos-%d", i)
				podIP := fmt.Sprintf("10.244.0.%d", i+1)
				pod := testutil.NewReadyPod(podName, namespace, "test-nacos", podIP)
				pod.Status.Conditions = []corev1.PodCondition{
					{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					{Type: corev1.PodInitialized, Status: corev1.ConditionTrue},
					{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
				}
				_, err := fakeClient.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
			}

			notReadyPod := testutil.NewReadyPod("test-nacos-2", namespace, "test-nacos", "10.244.0.3")
			notReadyPod.Status.Conditions = []corev1.PodCondition{
				{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
				{Type: corev1.PodReady, Status: corev1.ConditionFalse},
				{Type: corev1.PodInitialized, Status: corev1.ConditionTrue},
				{Type: corev1.ContainersReady, Status: corev1.ConditionFalse},
			}
			_, err = fakeClient.CoreV1().Pods(namespace).Create(context.TODO(), notReadyPod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			readyPods, err := service.GetStatefulSetReadPod(namespace, "test-nacos")
			Expect(err).NotTo(HaveOccurred())
			Expect(len(readyPods)).To(Equal(2))
		})
	})
})
