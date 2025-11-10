package k8s

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes/fake"
	"nacos.io/nacos-operator/test/testutil"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("ServiceService", func() {
	var (
		fakeClient *fake.Clientset
		service    Service
		namespace  string
		logger     = ctrl.Log.WithName("test")
	)

	BeforeEach(func() {
		fakeClient = fake.NewSimpleClientset()
		service = NewServiceService(fakeClient, logger)
		namespace = "default"
	})

	Describe("CreateService", func() {
		It("should create a new Service successfully", func() {
			labels := map[string]string{"app": "nacos"}
			svc := testutil.NewService("test-svc", namespace, labels, corev1.ServiceTypeClusterIP)

			err := service.CreateService(namespace, svc)
			Expect(err).NotTo(HaveOccurred())

			retrieved, err := service.GetService(namespace, "test-svc")
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.Name).To(Equal("test-svc"))
		})
	})

	Describe("GetService", func() {
		It("should return error when Service does not exist", func() {
			_, err := service.GetService(namespace, "non-existent")
			Expect(err).To(HaveOccurred())
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})
})
