package k8s

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes/fake"
	"nacos.io/nacos-operator/test/testutil"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestK8sServices(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "K8s Services Suite")
}

var _ = Describe("ConfigMapService", func() {
	var (
		fakeClient *fake.Clientset
		service    ConfigMap
		namespace  string
		logger     = ctrl.Log.WithName("test")
	)

	BeforeEach(func() {
		fakeClient = fake.NewSimpleClientset()
		service = NewConfigMapService(fakeClient, logger)
		namespace = "default"
	})

	Describe("CreateConfigMap", func() {
		It("should create a new ConfigMap successfully", func() {
			cm := testutil.NewConfigMap("test-cm", namespace, map[string]string{
				"key1": "value1",
			})

			err := service.CreateConfigMap(namespace, cm)
			Expect(err).NotTo(HaveOccurred())

			retrieved, err := service.GetConfigMap(namespace, "test-cm")
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.Name).To(Equal("test-cm"))
			Expect(retrieved.Data["key1"]).To(Equal("value1"))
		})
	})

	Describe("GetConfigMap", func() {
		It("should return error when ConfigMap does not exist", func() {
			_, err := service.GetConfigMap(namespace, "non-existent")
			Expect(err).To(HaveOccurred())
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("CreateOrUpdateConfigMap", func() {
		It("should create ConfigMap if it does not exist", func() {
			cm := testutil.NewConfigMap("test-cm", namespace, map[string]string{
				"key1": "value1",
			})

			err := service.CreateOrUpdateConfigMap(namespace, cm)
			Expect(err).NotTo(HaveOccurred())

			retrieved, err := service.GetConfigMap(namespace, "test-cm")
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.Data["key1"]).To(Equal("value1"))
		})
	})
})
