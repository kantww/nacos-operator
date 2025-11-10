package k8s

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("JobService", func() {
	var (
		fakeClient *fake.Clientset
		service    Job
		namespace  string
		logger     = ctrl.Log.WithName("test")
	)

	BeforeEach(func() {
		fakeClient = fake.NewSimpleClientset()
		service = NewJobService(fakeClient, logger)
		namespace = "default"
	})

	newJob := func(name, namespace string) *batchv1.Job {
		return &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					"app": "nacos-init",
				},
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "mysql-init",
								Image: "mysql:8.0",
							},
						},
						RestartPolicy: corev1.RestartPolicyNever,
					},
				},
			},
		}
	}

	Describe("CreateJob", func() {
		It("should create a new Job successfully", func() {
			job := newJob("test-job", namespace)

			err := service.CreateJob(namespace, job)
			Expect(err).NotTo(HaveOccurred())

			retrieved, err := service.GetJob(namespace, "test-job")
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.Name).To(Equal("test-job"))
		})
	})

	Describe("GetJob", func() {
		It("should return error when Job does not exist", func() {
			_, err := service.GetJob(namespace, "non-existent")
			Expect(err).To(HaveOccurred())
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})
})
