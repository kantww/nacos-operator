package testcase

import (
	"context"
	"fmt"
	"testing"

	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nacosgroupv1alpha1 "nacos.io/nacos-operator/api/v1alpha1"
	"nacos.io/nacos-operator/test/testutil"
)

func TestClusterNacosDeletion(t *testing.T) {
	ctx := context.Background()
	replicas := int32(3)
	reconciler, fakeClient, mockServer, _, simulator, bridge := setupTestWithName(t, int(replicas), "test-delete")
	defer mockServer.Close()

	// Create cluster
	nacos := testutil.NewNacosCluster("test-delete", "default", replicas)
	nacos.UID = "test-uid-delete"
	if err := fakeClient.Create(ctx, nacos); err != nil {
		t.Fatalf("Failed to create Nacos CR: %v", err)
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-delete", Namespace: "default"},
	}

	// Setup cluster to Running state
	t.Log("=== Setup: Creating cluster ===")
	reconciler.Reconcile(ctx, req)
	reconciler.Reconcile(ctx, req)
	bridge.SyncFromKubeToCtrl(ctx, "default")
	simulator.SimulateStatefulSetPodsWithIP(ctx, "default", "test-delete", "127.0.0.1")

	// Reconcile until Running
	for i := 0; i < 10; i++ {
		reconciler.Reconcile(ctx, req)
		updatedNacos := &nacosgroupv1alpha1.Nacos{}
		fakeClient.Get(ctx, req.NamespacedName, updatedNacos)
		if updatedNacos.Status.Phase == nacosgroupv1alpha1.PhaseRunning {
			t.Logf("✓ Cluster Running")
			break
		}
	}

	// Verify initial state
	updatedNacos := &nacosgroupv1alpha1.Nacos{}
	if err := fakeClient.Get(ctx, req.NamespacedName, updatedNacos); err != nil {
		t.Fatalf("Failed to get Nacos CR: %v", err)
	}
	if updatedNacos.Status.Phase != nacosgroupv1alpha1.PhaseRunning {
		t.Fatalf("Cluster not Running before deletion, phase: %s", updatedNacos.Status.Phase)
	}

	// Verify resources exist before deletion
	sts := &appv1.StatefulSet{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-delete", Namespace: "default"}, sts); err != nil {
		t.Fatalf("StatefulSet should exist before deletion: %v", err)
	}
	t.Logf("✓ StatefulSet exists before deletion")

	// Verify pods exist
	for i := int32(0); i < replicas; i++ {
		podName := fmt.Sprintf("test-delete-%d", i)
		pod := &corev1.Pod{}
		if err := fakeClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: "default"}, pod); err != nil {
			t.Errorf("Pod %s should exist before deletion: %v", podName, err)
		}
	}
	t.Logf("✓ All %d pods exist before deletion", replicas)

	// Delete Nacos CR
	t.Log("=== Deleting Nacos CR ===")
	if err := fakeClient.Delete(ctx, updatedNacos); err != nil {
		t.Fatalf("Failed to delete Nacos CR: %v", err)
	}
	t.Logf("✓ Nacos CR deleted")

	// Reconcile should return without error (CR not found)
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Reconcile should not error on deleted CR: %v", err)
	}
	if result.Requeue || result.RequeueAfter > 0 {
		t.Errorf("Should not requeue after CR deletion, got: Requeue=%v, RequeueAfter=%v", result.Requeue, result.RequeueAfter)
	}
	t.Logf("✓ Reconcile handled deletion correctly")

	// Verify CR is gone
	deletedNacos := &nacosgroupv1alpha1.Nacos{}
	err = fakeClient.Get(ctx, req.NamespacedName, deletedNacos)
	if err == nil {
		t.Errorf("Nacos CR should not exist after deletion")
	}
	t.Logf("✓ Nacos CR no longer exists")

	// Note: In a real Kubernetes environment, the StatefulSet and Pods would be deleted
	// by the garbage collector due to owner references. In our fake client test,
	// we verify that the reconciler doesn't error when the CR is deleted.

	t.Log("✓✓✓ Cluster deletion test PASSED ✓✓✓")
}
