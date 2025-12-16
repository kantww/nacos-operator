package testcase

import (
	"context"
	"fmt"
	"testing"
	"time"

	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nacosgroupv1alpha1 "nacos.io/nacos-operator/api/v1alpha1"
	"nacos.io/nacos-operator/test/testutil"
)

// TestStandaloneNacosCreation tests the complete lifecycle of creating a standalone Nacos instance
func TestStandaloneNacosCreation(t *testing.T) {
	ctx := context.Background()
	reconciler, fakeClient, mockServer, simulator, bridge := setupTest(t, 1)
	defer mockServer.Close()

	// Create Nacos CR
	nacos := testutil.NewNacosStandalone("test-standalone", "default")
	nacos.UID = "test-uid-standalone"
	if err := fakeClient.Create(ctx, nacos); err != nil {
		t.Fatalf("Failed to create Nacos CR: %v", err)
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-standalone", Namespace: "default"},
	}

	// Round 1: PreCheck - Phase should change to Creating
	t.Log("=== Round 1: PreCheck ===")
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Logf("Round 1 error (expected): %v", err)
	}
	if result.RequeueAfter != 5*time.Second {
		t.Errorf("Expected RequeueAfter 5s, got %v", result.RequeueAfter)
	}

	// Verify phase changed to Creating
	updatedNacos := &nacosgroupv1alpha1.Nacos{}
	if err := fakeClient.Get(ctx, req.NamespacedName, updatedNacos); err != nil {
		t.Fatalf("Failed to get Nacos CR: %v", err)
	}
	if updatedNacos.Status.Phase != nacosgroupv1alpha1.PhaseCreating {
		t.Errorf("Expected phase Creating, got %s", updatedNacos.Status.Phase)
	}
	t.Logf("✓ Phase: %s", updatedNacos.Status.Phase)

	// Round 2: MakeEnsure - Create K8s resources
	t.Log("=== Round 2: MakeEnsure - Creating K8s resources ===")
	result, err = reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Logf("Round 2 error: %v", err)
	}

	// Sync resources from kubernetes clientset to controller-runtime client
	bridge.SyncFromKubeToCtrl(ctx, "default")

	// Verify StatefulSet created
	sts := &appv1.StatefulSet{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-standalone", Namespace: "default"}, sts); err != nil {
		t.Fatalf("StatefulSet not created: %v", err)
	}
	if *sts.Spec.Replicas != 1 {
		t.Errorf("Expected 1 replica, got %d", *sts.Spec.Replicas)
	}
	t.Logf("✓ StatefulSet created with %d replica", *sts.Spec.Replicas)

	// Verify Service created
	svc := &corev1.Service{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-standalone", Namespace: "default"}, svc); err != nil {
		t.Fatalf("Service not created: %v", err)
	}
	t.Logf("✓ Service created")

	// Round 3: Simulate K8s creating pods with IP 127.0.0.1
	t.Log("=== Round 3: Simulating K8s pod creation ===")
	if err := simulator.SimulateStatefulSetPodsWithIP(ctx, "default", "test-standalone", "127.0.0.1"); err != nil {
		t.Fatalf("Failed to simulate pods: %v", err)
	}

	// Verify pod created with correct IP
	pod := &corev1.Pod{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-standalone-0", Namespace: "default"}, pod); err != nil {
		t.Fatalf("Pod not created: %v", err)
	}
	t.Logf("✓ Pod created: %s (IP: %s)", pod.Name, pod.Status.PodIP)

	// Round 4-10: Continue reconciling until Running
	t.Log("=== Rounds 4-10: Waiting for Running status ===")
	maxRounds := 10
	for i := 4; i <= maxRounds; i++ {
		result, err = reconciler.Reconcile(ctx, req)
		if err != nil {
			t.Logf("Round %d error: %v", i, err)
		}

		if err := fakeClient.Get(ctx, req.NamespacedName, updatedNacos); err != nil {
			t.Fatalf("Failed to get Nacos CR: %v", err)
		}

		t.Logf("Round %d: Phase=%s, Events=%d", i, updatedNacos.Status.Phase, len(updatedNacos.Status.Event))

		// Print events for debugging
		if len(updatedNacos.Status.Event) > 0 {
			lastEvent := updatedNacos.Status.Event[len(updatedNacos.Status.Event)-1]
			t.Logf("  Last Event: Code=%d, Message=%s", lastEvent.Code, lastEvent.Message)
		}

		if updatedNacos.Status.Phase == nacosgroupv1alpha1.PhaseRunning {
			t.Logf("✓ Status reached Running at round %d", i)
			break
		}

		if i == maxRounds {
			t.Errorf("Status did not reach Running after %d rounds, final phase: %s", maxRounds, updatedNacos.Status.Phase)
			// Print all events for debugging
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

	t.Log("✓✓✓ Standalone Nacos creation test PASSED ✓✓✓")
}

// TestClusterNacosCreation tests the complete lifecycle of creating a cluster Nacos instance
func TestClusterNacosCreation(t *testing.T) {
	ctx := context.Background()
	replicas := int32(3)
	reconciler, fakeClient, mockServer, _, simulator, bridge := setupTestWithName(t, int(replicas), "test-cluster")
	defer mockServer.Close()

	// Create Nacos CR
	nacos := testutil.NewNacosCluster("test-cluster", "default", replicas)
	nacos.UID = "test-uid-cluster"
	if err := fakeClient.Create(ctx, nacos); err != nil {
		t.Fatalf("Failed to create Nacos CR: %v", err)
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-cluster", Namespace: "default"},
	}

	// Round 1: PreCheck - Phase should change to Creating
	t.Log("=== Round 1: PreCheck ===")
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Logf("Round 1 error (expected): %v", err)
	}
	if result.RequeueAfter != 5*time.Second {
		t.Errorf("Expected RequeueAfter 5s, got %v", result.RequeueAfter)
	}

	// Verify phase changed to Creating
	updatedNacos := &nacosgroupv1alpha1.Nacos{}
	if err := fakeClient.Get(ctx, req.NamespacedName, updatedNacos); err != nil {
		t.Fatalf("Failed to get Nacos CR: %v", err)
	}
	if updatedNacos.Status.Phase != nacosgroupv1alpha1.PhaseCreating {
		t.Errorf("Expected phase Creating, got %s", updatedNacos.Status.Phase)
	}
	t.Logf("✓ Phase: %s", updatedNacos.Status.Phase)

	// Round 2: MakeEnsure - Create K8s resources
	t.Log("=== Round 2: MakeEnsure - Creating K8s resources ===")
	result, err = reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Logf("Round 2 error: %v", err)
	}

	// Sync resources from kubernetes clientset to controller-runtime client
	bridge.SyncFromKubeToCtrl(ctx, "default")

	// Verify StatefulSet created
	sts := &appv1.StatefulSet{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-cluster", Namespace: "default"}, sts); err != nil {
		t.Fatalf("StatefulSet not created: %v", err)
	}
	if *sts.Spec.Replicas != replicas {
		t.Errorf("Expected %d replicas, got %d", replicas, *sts.Spec.Replicas)
	}
	t.Logf("✓ StatefulSet created with %d replicas", *sts.Spec.Replicas)

	// Verify no preStop lifecycle on containers to avoid deleting raft data
	if sts.Spec.Template.Spec.Containers[0].Lifecycle != nil {
		t.Errorf("Expected container has no lifecycle, but found one")
	}
	t.Logf("✓ StatefulSet created with no lifecycle")

	// Verify container command does not include ping all cluster
	if len(sts.Spec.Template.Spec.Containers[0].Command) != 3 ||
		sts.Spec.Template.Spec.Containers[0].Command[0] != "/bin/bash" ||
		sts.Spec.Template.Spec.Containers[0].Command[1] != "-c" ||
		sts.Spec.Template.Spec.Containers[0].Command[2] != "bin/docker-startup.sh" {
		t.Errorf("Expected container command to be default /bin/bash -c bin/docker-startup.sh,"+
			"but got %v", sts.Spec.Template.Spec.Containers[0].Command)
	}
	t.Logf("✓ StatefulSet created with commond no ping all cluster")

	// Verify Services created (cluster mode creates headless and client services)
	svcList := &corev1.ServiceList{}
	if err := fakeClient.List(ctx, svcList, client.InNamespace("default")); err != nil {
		t.Logf("Warning: Could not list services: %v", err)
	} else {
		t.Logf("✓ Created %d service(s)", len(svcList.Items))
		for _, svc := range svcList.Items {
			t.Logf("  - Service: %s", svc.Name)
		}
	}

	// Round 3: Simulate K8s creating pods with IP 127.0.0.1
	t.Log("=== Round 3: Simulating K8s pod creation ===")
	if err := simulator.SimulateStatefulSetPodsWithIP(ctx, "default", "test-cluster", "127.0.0.1"); err != nil {
		t.Fatalf("Failed to simulate pods: %v", err)
	}

	// Verify all pods created with correct IP
	for i := int32(0); i < replicas; i++ {
		podName := fmt.Sprintf("test-cluster-%d", i)
		pod := &corev1.Pod{}
		if err := fakeClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: "default"}, pod); err != nil {
			t.Fatalf("Pod %s not created: %v", podName, err)
		}
		if pod.Status.PodIP != "127.0.0.1" {
			t.Errorf("Pod %s has wrong IP: expected 127.0.0.1, got %s", podName, pod.Status.PodIP)
		}
	}
	t.Logf("✓ Created %d pods with IP 127.0.0.1", replicas)

	// Round 4-10: Continue reconciling until Running
	t.Log("=== Rounds 4-10: Waiting for Running status ===")
	maxRounds := 10
	for i := 4; i <= maxRounds; i++ {
		result, err = reconciler.Reconcile(ctx, req)
		if err != nil {
			t.Logf("Round %d error: %v", i, err)
		}

		if err := fakeClient.Get(ctx, req.NamespacedName, updatedNacos); err != nil {
			t.Fatalf("Failed to get Nacos CR: %v", err)
		}

		t.Logf("Round %d: Phase=%s, Conditions=%d, Events=%d", i, updatedNacos.Status.Phase, len(updatedNacos.Status.Conditions), len(updatedNacos.Status.Event))

		// Print events for debugging
		if len(updatedNacos.Status.Event) > 0 {
			lastEvent := updatedNacos.Status.Event[len(updatedNacos.Status.Event)-1]
			t.Logf("  Last Event: Code=%d, Message=%s", lastEvent.Code, lastEvent.Message)
		}

		if updatedNacos.Status.Phase == nacosgroupv1alpha1.PhaseRunning {
			t.Logf("✓ Status reached Running at round %d", i)
			break
		}

		if i == maxRounds {
			t.Errorf("Status did not reach Running after %d rounds, final phase: %s", maxRounds, updatedNacos.Status.Phase)
			// Print all events for debugging
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

	// Verify all nodes are in conditions
	if len(updatedNacos.Status.Conditions) != int(replicas) {
		t.Errorf("Expected %d conditions (one per replica), got %d", replicas, len(updatedNacos.Status.Conditions))
	} else {
		t.Logf("✓ All %d nodes reported in conditions", replicas)
		// Verify leader election
		leaderCount := 0
		followerCount := 0
		for _, cond := range updatedNacos.Status.Conditions {
			if cond.Type == "leader" {
				leaderCount++
				t.Logf("  - Leader: %s (IP: %s)", cond.PodName, cond.Instance)
			} else if cond.Type == "follower" {
				followerCount++
				t.Logf("  - Follower: %s (IP: %s)", cond.PodName, cond.Instance)
			}
		}
		if leaderCount != 1 {
			t.Errorf("Expected 1 leader, got %d", leaderCount)
		}
		if followerCount != int(replicas)-1 {
			t.Errorf("Expected %d followers, got %d", int(replicas)-1, followerCount)
		}
	}

	t.Log("✓✓✓ Cluster Nacos creation test PASSED ✓✓✓")
}
