package testcase

import (
	"testing"

	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nacosgroupv1alpha1 "nacos.io/nacos-operator/api/v1alpha1"
	"nacos.io/nacos-operator/controllers"
	"nacos.io/nacos-operator/pkg/service/operator"
	"nacos.io/nacos-operator/test/testutil"
)

// setupTest creates a test environment with reconciler, fake clients, and mock server
func setupTest(t *testing.T, replicas int) (*controllers.NacosReconciler, client.Client, *testutil.MockNacosServer, *testutil.K8sSimulator, *testutil.ClientBridge) {
	reconciler, fakeClient, mockServer, mockServer8848, simulator, bridge := setupTestWithName(t, replicas, "nacos")
	_ = mockServer8848 // Not used in simple tests
	return reconciler, fakeClient, mockServer, simulator, bridge
}

// setupTestWithName creates a test environment with custom StatefulSet name for mock server
// Returns both mockServer (random port) and mockServer8848 (port 8848) for dynamic updates
func setupTestWithName(t *testing.T, replicas int, stsName string) (*controllers.NacosReconciler, client.Client, *testutil.MockNacosServer, *testutil.MockNacosServer8848, *testutil.K8sSimulator, *testutil.ClientBridge) {
	logger := ctrl.Log.WithName("test")

	// Create scheme
	testScheme := runtime.NewScheme()
	_ = scheme.AddToScheme(testScheme)
	_ = nacosgroupv1alpha1.AddToScheme(testScheme)
	_ = corev1.AddToScheme(testScheme)
	_ = appv1.AddToScheme(testScheme)

	// Create controller-runtime fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		Build()

	// Create mock Nacos server with custom StatefulSet name
	mockServers := testutil.CreateMockClusterServersWithName(replicas, 0, "2.1.0", stsName)
	mockServer8848, err := testutil.NewMockNacosServer8848(mockServers)
	if err != nil {
		t.Logf("Warning: Could not start mock server on port 8848: %v. Health checks may fail.", err)
	} else {
		t.Cleanup(func() { mockServer8848.Close() })
	}
	mockServer := testutil.NewMockNacosServer(mockServers)

	// Create kubernetes fake clientset
	fakeKubeClient := kubefake.NewSimpleClientset()

	// Create operator client
	operatorClient := operator.NewOperatorClient(logger, fakeKubeClient, testScheme, fakeClient)

	// Create reconciler
	reconciler := &controllers.NacosReconciler{
		Client:         fakeClient,
		Log:            logger,
		Scheme:         testScheme,
		OperaterClient: operatorClient,
	}

	// Create simulator and bridge
	simulator := testutil.NewK8sSimulator(fakeClient)
	simulator.SetKubeClient(fakeKubeClient)
	bridge := testutil.NewClientBridge(fakeKubeClient, fakeClient)

	return reconciler, fakeClient, mockServer, mockServer8848, simulator, bridge
}
