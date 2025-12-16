package operator

import (
	"testing"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	nacosgroupv1alpha1 "nacos.io/nacos-operator/api/v1alpha1"
	"nacos.io/nacos-operator/pkg/service/k8s"
)

func TestBuildMergedConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = nacosgroupv1alpha1.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)

	tests := []struct {
		name               string
		nacos              *nacosgroupv1alpha1.Nacos
		internalConfigMap  *v1.ConfigMap
		userConfigMap      *v1.ConfigMap
		expectedContent    string
		expectPanic        bool
	}{
		{
			name: "merge internal and user config",
			nacos: &nacosgroupv1alpha1.Nacos{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nacos",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: nacosgroupv1alpha1.NacosSpec{
					InternalConfigRef: &nacosgroupv1alpha1.ConfigMapRef{
						Name: "internal-config",
						Key:  "internal.properties",
					},
					UserConfigRef: &nacosgroupv1alpha1.ConfigMapRef{
						Name: "user-config",
						Key:  "user.properties",
					},
				},
			},
			internalConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "internal-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"internal.properties": "server.port=8848\ndb.type=embedded",
				},
			},
			userConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "user-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"user.properties": "custom.property=value",
				},
			},
			expectedContent: "# ===== Internal Configuration =====\nserver.port=8848\ndb.type=embedded\n\n# ===== User Configuration =====\ncustom.property=value\n",
			expectPanic:     false,
		},
		{
			name: "only internal config",
			nacos: &nacosgroupv1alpha1.Nacos{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nacos",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: nacosgroupv1alpha1.NacosSpec{
					InternalConfigRef: &nacosgroupv1alpha1.ConfigMapRef{
						Name: "internal-config",
					},
				},
			},
			internalConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "internal-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"internal.properties": "server.port=8848",
				},
			},
			expectedContent: "# ===== Internal Configuration =====\nserver.port=8848\n\n",
			expectPanic:     false,
		},
		{
			name: "only user config",
			nacos: &nacosgroupv1alpha1.Nacos{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nacos",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: nacosgroupv1alpha1.NacosSpec{
					UserConfigRef: &nacosgroupv1alpha1.ConfigMapRef{
						Name: "user-config",
					},
				},
			},
			userConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "user-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"user.properties": "custom.property=value",
				},
			},
			expectedContent: "# ===== User Configuration =====\ncustom.property=value\n",
			expectPanic:     false,
		},
		{
			name: "custom final config name",
			nacos: &nacosgroupv1alpha1.Nacos{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nacos",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: nacosgroupv1alpha1.NacosSpec{
					FinalConfigName: "custom-final-config",
					UserConfigRef: &nacosgroupv1alpha1.ConfigMapRef{
						Name: "user-config",
					},
				},
			},
			userConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "user-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"user.properties": "test=value",
				},
			},
			expectedContent: "# ===== User Configuration =====\ntest=value\n",
			expectPanic:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset()

			if tt.internalConfigMap != nil {
				_, err := fakeClient.CoreV1().ConfigMaps(tt.nacos.Namespace).Create(nil, tt.internalConfigMap, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("Failed to create internal ConfigMap: %v", err)
				}
			}

			if tt.userConfigMap != nil {
				_, err := fakeClient.CoreV1().ConfigMaps(tt.nacos.Namespace).Create(nil, tt.userConfigMap, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("Failed to create user ConfigMap: %v", err)
				}
			}

			k8sService := k8s.NewK8sService(fakeClient, logr.Discard())
			kindClient := &KindClient{
				k8sService: k8sService,
				scheme:     scheme,
				logger:     logr.Discard(),
			}

			if tt.expectPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("Expected panic but did not panic")
					}
				}()
			}

			result := kindClient.buildMergedConfigMap(tt.nacos)

			if tt.expectPanic {
				return
			}

			expectedName := tt.nacos.Spec.FinalConfigName
			if expectedName == "" {
				expectedName = tt.nacos.Name + "-final-config"
			}

			if result.Name != expectedName {
				t.Errorf("Expected ConfigMap name %s, got %s", expectedName, result.Name)
			}

			if result.Namespace != tt.nacos.Namespace {
				t.Errorf("Expected namespace %s, got %s", tt.nacos.Namespace, result.Namespace)
			}

			if result.Data["application.properties"] != tt.expectedContent {
				t.Errorf("Expected content:\n%s\nGot:\n%s", tt.expectedContent, result.Data["application.properties"])
			}
		})
	}
}
