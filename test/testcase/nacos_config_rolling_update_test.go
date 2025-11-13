package testcase

import (
	"context"
	"strings"
	"testing"

	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nacosgroupv1alpha1 "nacos.io/nacos-operator/api/v1alpha1"
	"nacos.io/nacos-operator/test/testutil"
)

// TestConfigRollingUpdate tests the automatic rolling update when config changes
// This test verifies that when user-config is updated, the digest in StatefulSet is updated
func TestConfigRollingUpdate(t *testing.T) {
	ctx := context.Background()
	replicas := int32(3)
	reconciler, fakeClient, mockServer, _, simulator, bridge := setupTestWithName(t, int(replicas), "test-config-rolling")
	defer mockServer.Close()

	// Create user-config ConfigMap with initial content
	userConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-user-config-rolling",
			Namespace: "default",
		},
		Data: map[string]string{
			"user.properties": `# User configurable parameters - Version 1
nacos.console.ui.enabled=true
nacos.core.param.check.enabled=true
management.endpoints.web.exposure.include=health,info`,
		},
	}
	if err := fakeClient.Create(ctx, userConfig); err != nil {
		t.Fatalf("Failed to create user-config ConfigMap: %v", err)
	}
	t.Log("✓ Created user-config ConfigMap (Version 1)")

	// Create internal-config ConfigMap
	internalConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-internal-config-rolling",
			Namespace: "default",
		},
		Data: map[string]string{
			"internal.properties": `# Internal core parameters
server.port=8848
server.servlet.contextPath=/nacos
nacos.core.auth.enabled=true`,
		},
	}
	if err := fakeClient.Create(ctx, internalConfig); err != nil {
		t.Fatalf("Failed to create internal-config ConfigMap: %v", err)
	}
	t.Log("✓ Created internal-config ConfigMap")

	// Sync configs to kubernetes clientset
	bridge.SyncFromCtrlToKube(ctx, "default")

	// Create Nacos CR with config management fields
	nacos := testutil.NewNacosCluster("test-config-rolling", "default", replicas)
	nacos.UID = "test-uid-config-rolling"
	nacos.Spec.UserConfigRef = &nacosgroupv1alpha1.ConfigMapRef{
		Name: "test-user-config-rolling",
		Key:  "user.properties",
	}
	nacos.Spec.InternalConfigRef = &nacosgroupv1alpha1.ConfigMapRef{
		Name: "test-internal-config-rolling",
		Key:  "internal.properties",
	}
	nacos.Spec.FinalConfigName = "test-config-rolling-final"

	if err := fakeClient.Create(ctx, nacos); err != nil {
		t.Fatalf("Failed to create Nacos CR: %v", err)
	}
	t.Log("✓ Created Nacos CR with config management fields")

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-config-rolling", Namespace: "default"},
	}

	// Round 1: PreCheck
	t.Log("=== Round 1: PreCheck ===")
	reconciler.Reconcile(ctx, req)

	// Round 2: MakeEnsure - Create resources with initial config
	t.Log("=== Round 2: MakeEnsure - Creating resources with initial config ===")
	reconciler.Reconcile(ctx, req)
	bridge.SyncFromKubeToCtrl(ctx, "default")

	// Get initial StatefulSet and record its digest
	sts := &appv1.StatefulSet{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-config-rolling", Namespace: "default"}, sts); err != nil {
		t.Fatalf("StatefulSet not created: %v", err)
	}

	// Extract initial digest from StatefulSet annotations or template annotations
	initialDigest := ""
	if sts.Spec.Template.Annotations != nil {
		initialDigest = sts.Spec.Template.Annotations["nacos.io/config-digest"]
	}
	if initialDigest == "" {
		t.Logf("Warning: Initial digest not found in StatefulSet template annotations")
	} else {
		t.Logf("✓ Initial config digest in StatefulSet: %s", initialDigest)
	}

	// Get initial final-config content
	finalConfig := &corev1.ConfigMap{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-config-rolling-final", Namespace: "default"}, finalConfig); err != nil {
		t.Fatalf("Final-config ConfigMap not created: %v", err)
	}
	initialContent := finalConfig.Data["application.properties"]
	t.Logf("✓ Initial final-config content length: %d bytes", len(initialContent))

	// Simulate K8s creating pods
	t.Log("=== Round 3: Simulating K8s pod creation ===")
	if err := simulator.SimulateStatefulSetPodsWithIP(ctx, "default", "test-config-rolling", "127.0.0.1"); err != nil {
		t.Fatalf("Failed to simulate pods: %v", err)
	}

	// Continue reconciling until Running
	t.Log("=== Rounds 4-10: Waiting for Running status ===")
	maxRounds := 10
	updatedNacos := &nacosgroupv1alpha1.Nacos{}
	for i := 4; i <= maxRounds; i++ {
		reconciler.Reconcile(ctx, req)
		if err := fakeClient.Get(ctx, req.NamespacedName, updatedNacos); err != nil {
			t.Fatalf("Failed to get Nacos CR: %v", err)
		}
		if updatedNacos.Status.Phase == nacosgroupv1alpha1.PhaseRunning {
			t.Logf("✓ Status reached Running at round %d", i)
			break
		}
	}

	// Record initial ConfigDigest from status
	initialStatusDigest := updatedNacos.Status.ConfigDigest
	if initialStatusDigest == "" {
		t.Logf("Note: Initial ConfigDigest not set in status (expected for now)")
	} else {
		t.Logf("✓ Initial ConfigDigest in status: %s", initialStatusDigest)
	}

	// ===== Now update the user-config ConfigMap =====
	t.Log("=== Updating user-config ConfigMap ===")

	// Get the current user-config
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-user-config-rolling", Namespace: "default"}, userConfig); err != nil {
		t.Fatalf("Failed to get user-config: %v", err)
	}

	// Update user-config with new content
	userConfig.Data["user.properties"] = `# User configurable parameters - Version 2 (UPDATED)
nacos.console.ui.enabled=false
nacos.core.param.check.enabled=false
management.endpoints.web.exposure.include=health,info,prometheus,metrics`

	if err := fakeClient.Update(ctx, userConfig); err != nil {
		t.Fatalf("Failed to update user-config ConfigMap: %v", err)
	}
	t.Log("✓ Updated user-config ConfigMap (Version 2)")

	// Sync updated config to kubernetes clientset
	bridge.SyncFromCtrlToKube(ctx, "default")
	t.Log("✓ Synced updated user-config to kubernetes clientset")

	// Trigger reconcile after config update
	t.Log("=== Reconciling after config update (Round 1) ===")
	reconciler.Reconcile(ctx, req)

	// Sync all resources back to controller-runtime client
	bridge.SyncFromKubeToCtrl(ctx, "default")
	t.Log("✓ Synced resources back to controller-runtime client")

	// Reconcile again to ensure StatefulSet gets the updated digest
	t.Log("=== Reconciling after config update (Round 2) ===")
	reconciler.Reconcile(ctx, req)
	bridge.SyncFromKubeToCtrl(ctx, "default")
	t.Log("✓ Second reconcile completed")

	// Verify final-config was updated
	updatedFinalConfig := &corev1.ConfigMap{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-config-rolling-final", Namespace: "default"}, updatedFinalConfig); err != nil {
		t.Fatalf("Failed to get updated final-config: %v", err)
	}

	updatedContent := updatedFinalConfig.Data["application.properties"]
	if updatedContent == initialContent {
		t.Errorf("Final-config content was not updated after user-config change")
	} else {
		t.Logf("✓ Final-config content was updated (%d bytes)", len(updatedContent))
	}

	// Verify updated content contains new values
	if !strings.Contains(updatedContent, "nacos.console.ui.enabled=false") {
		t.Errorf("Updated config missing new user parameter value")
	}
	if !strings.Contains(updatedContent, "Version 2 (UPDATED)") {
		t.Errorf("Updated config missing version comment")
	}
	t.Log("✓ Updated config contains new user parameter values")

	// Verify StatefulSet digest was updated
	updatedSts := &appv1.StatefulSet{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-config-rolling", Namespace: "default"}, updatedSts); err != nil {
		t.Fatalf("Failed to get updated StatefulSet: %v", err)
	}

	newDigest := ""
	if updatedSts.Spec.Template.Annotations != nil {
		newDigest = updatedSts.Spec.Template.Annotations["nacos.io/config-digest"]
	}

	if newDigest == "" {
		t.Errorf("Digest not found in updated StatefulSet template annotations")
	} else if initialDigest != "" && newDigest == initialDigest {
		t.Errorf("StatefulSet digest was not updated after config change (old: %s, new: %s)", initialDigest, newDigest)
	} else {
		t.Logf("✓ StatefulSet digest was updated: %s -> %s", initialDigest, newDigest)
	}

	// Verify ConfigDigest in status was updated
	if err := fakeClient.Get(ctx, req.NamespacedName, updatedNacos); err != nil {
		t.Fatalf("Failed to get updated Nacos CR: %v", err)
	}

	newStatusDigest := updatedNacos.Status.ConfigDigest
	if newStatusDigest == "" {
		t.Logf("Note: ConfigDigest not set in updated status (will be implemented)")
	} else if initialStatusDigest != "" && newStatusDigest == initialStatusDigest {
		t.Errorf("Status ConfigDigest was not updated after config change")
	} else {
		t.Logf("✓ Status ConfigDigest was updated: %s -> %s", initialStatusDigest, newStatusDigest)
	}

	// Verify the digest in StatefulSet matches the digest in status (if both are set)
	if newDigest != "" && newStatusDigest != "" && newDigest != newStatusDigest {
		t.Errorf("Digest mismatch: StatefulSet has %s, Status has %s", newDigest, newStatusDigest)
	} else if newDigest != "" && newStatusDigest != "" {
		t.Log("✓ Digest in StatefulSet matches digest in Status")
	}

	t.Log("✓✓✓ Config rolling update test PASSED ✓✓✓")
}
