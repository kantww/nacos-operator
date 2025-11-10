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

// TestClusterNacosScaling tests scaling a Nacos cluster from 3 to 5 replicas
func TestClusterNacosScaling(t *testing.T) {
	ctx := context.Background()
	initialReplicas := int32(3)
	reconciler, fakeClient, mockServer, mockServer8848, simulator, bridge := setupTestWithName(t, int(initialReplicas), "test-scale")
	defer mockServer.Close()

	// Create initial cluster
	nacos := testutil.NewNacosCluster("test-scale", "default", initialReplicas)
	nacos.UID = "test-uid-scale"
	if err := fakeClient.Create(ctx, nacos); err != nil {
		t.Fatalf("Failed to create Nacos CR: %v", err)
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-scale", Namespace: "default"},
	}

	// Initial setup - get to Running state
	t.Log("=== Initial Setup: Creating cluster with 3 replicas ===")
	reconciler.Reconcile(ctx, req)
	reconciler.Reconcile(ctx, req)
	bridge.SyncFromKubeToCtrl(ctx, "default")
	simulator.SimulateStatefulSetPodsWithIP(ctx, "default", "test-scale", "127.0.0.1")

	// Reconcile until Running
	for i := 0; i < 10; i++ {
		reconciler.Reconcile(ctx, req)
		updatedNacos := &nacosgroupv1alpha1.Nacos{}
		fakeClient.Get(ctx, req.NamespacedName, updatedNacos)
		if updatedNacos.Status.Phase == nacosgroupv1alpha1.PhaseRunning {
			t.Logf("✓ Initial cluster Running with %d replicas", initialReplicas)
			break
		}
	}

	// Verify initial state
	updatedNacos := &nacosgroupv1alpha1.Nacos{}
	if err := fakeClient.Get(ctx, req.NamespacedName, updatedNacos); err != nil {
		t.Fatalf("Failed to get Nacos CR: %v", err)
	}
	if updatedNacos.Status.Phase != nacosgroupv1alpha1.PhaseRunning {
		t.Fatalf("Initial cluster not Running, phase: %s", updatedNacos.Status.Phase)
	}
	t.Logf("✓ Initial state verified: Phase=%s, Replicas=%d", updatedNacos.Status.Phase, *updatedNacos.Spec.Replicas)

	// Scale up to 5 replicas
	t.Log("=== Scaling: 3 -> 5 replicas ===")
	newReplicas := int32(5)
	updatedNacos.Spec.Replicas = &newReplicas
	if err := fakeClient.Update(ctx, updatedNacos); err != nil {
		t.Fatalf("Failed to update Nacos CR: %v", err)
	}
	t.Logf("✓ Updated CR replicas to %d", newReplicas)

	// Update mock server to return 5 nodes (dynamically update without closing)
	newMockServers := testutil.CreateMockClusterServersWithName(int(newReplicas), 0, "2.1.0", "test-scale")
	mockServer.UpdateServers(newMockServers)
	if mockServer8848 != nil {
		mockServer8848.UpdateServers(newMockServers)
	}
	t.Logf("✓ Mock server updated for %d nodes", newReplicas)

	// Reconcile to apply scaling
	t.Log("=== Applying scale operation ===")
	reconciler.Reconcile(ctx, req)
	bridge.SyncFromKubeToCtrl(ctx, "default")

	// Simulate new pods
	simulator.SimulateStatefulSetPodsWithIP(ctx, "default", "test-scale", "127.0.0.1")
	t.Logf("✓ Simulated %d pods", newReplicas)

	// Verify StatefulSet scaled
	sts := &appv1.StatefulSet{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-scale", Namespace: "default"}, sts); err != nil {
		t.Fatalf("StatefulSet not found: %v", err)
	}
	if *sts.Spec.Replicas != newReplicas {
		t.Errorf("Expected StatefulSet replicas %d, got %d", newReplicas, *sts.Spec.Replicas)
	}
	t.Logf("✓ StatefulSet scaled to %d replicas", *sts.Spec.Replicas)

	// Verify all pods exist
	for i := int32(0); i < newReplicas; i++ {
		podName := fmt.Sprintf("test-scale-%d", i)
		pod := &corev1.Pod{}
		if err := fakeClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: "default"}, pod); err != nil {
			t.Errorf("Pod %s not found: %v", podName, err)
		}
	}
	t.Logf("✓ All %d pods exist", newReplicas)

	// Continue reconciling until Running
	t.Log("=== Waiting for Running status after scaling ===")
	maxRounds := 10
	for i := 1; i <= maxRounds; i++ {
		reconciler.Reconcile(ctx, req)

		if err := fakeClient.Get(ctx, req.NamespacedName, updatedNacos); err != nil {
			t.Fatalf("Failed to get Nacos CR: %v", err)
		}

		t.Logf("Round %d: Phase=%s, Conditions=%d", i, updatedNacos.Status.Phase, len(updatedNacos.Status.Conditions))

		if updatedNacos.Status.Phase == nacosgroupv1alpha1.PhaseRunning {
			t.Logf("✓ Status reached Running after scaling at round %d", i)
			break
		}

		if i == maxRounds {
			t.Errorf("Status did not reach Running after scaling, final phase: %s", updatedNacos.Status.Phase)
			// Print events for debugging
			for idx, event := range updatedNacos.Status.Event {
				t.Logf("  Event %d: Code=%d, Message=%s", idx, event.Code, event.Message)
			}
		}
	}

	// Final verification
	if err := fakeClient.Get(ctx, req.NamespacedName, updatedNacos); err != nil {
		t.Fatalf("Failed to get final Nacos CR: %v", err)
	}

	if updatedNacos.Status.Phase != nacosgroupv1alpha1.PhaseRunning {
		t.Errorf("Expected final phase Running, got %s", updatedNacos.Status.Phase)
	}

	if *updatedNacos.Spec.Replicas != newReplicas {
		t.Errorf("Expected final replicas %d, got %d", newReplicas, *updatedNacos.Spec.Replicas)
	}

	// Verify all nodes are in conditions
	if len(updatedNacos.Status.Conditions) != int(newReplicas) {
		t.Errorf("Expected %d conditions after scaling, got %d", newReplicas, len(updatedNacos.Status.Conditions))
	} else {
		t.Logf("✓ All %d nodes reported in conditions after scaling", newReplicas)
		// Count leader and followers
		leaderCount := 0
		followerCount := 0
		for _, cond := range updatedNacos.Status.Conditions {
			if cond.Type == "leader" {
				leaderCount++
			} else if cond.Type == "follower" {
				followerCount++
			}
		}
		t.Logf("  - Leaders: %d, Followers: %d", leaderCount, followerCount)
		if leaderCount != 1 {
			t.Errorf("Expected 1 leader after scaling, got %d", leaderCount)
		}
		if followerCount != int(newReplicas)-1 {
			t.Errorf("Expected %d followers after scaling, got %d", int(newReplicas)-1, followerCount)
		}
	}

	t.Logf("✓✓✓ Cluster scaling test PASSED: %d -> %d replicas ✓✓✓", initialReplicas, newReplicas)
}
