package testcase

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nacosgroupv1alpha1 "nacos.io/nacos-operator/api/v1alpha1"
	"nacos.io/nacos-operator/test/testutil"
)

// TestConfigMerge tests the configuration merging functionality
// This test verifies that user-config and internal-config are properly merged into final-config
func TestConfigMerge(t *testing.T) {
	ctx := context.Background()
	replicas := int32(3)
	reconciler, fakeClient, mockServer, _, simulator, bridge := setupTestWithName(t, int(replicas), "test-config-merge")
	defer mockServer.Close()

	// Create user-config ConfigMap
	userConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-user-config",
			Namespace: "default",
		},
		Data: map[string]string{
			"user.properties": `# User configurable parameters
nacos.console.ui.enabled=true
nacos.core.param.check.enabled=true
management.endpoints.web.exposure.include=health,info,prometheus`,
		},
	}
	if err := fakeClient.Create(ctx, userConfig); err != nil {
		t.Fatalf("Failed to create user-config ConfigMap: %v", err)
	}
	t.Log("✓ Created user-config ConfigMap")

	// Create internal-config ConfigMap
	internalConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-internal-config",
			Namespace: "default",
		},
		Data: map[string]string{
			"internal.properties": `# Internal core parameters
server.port=8848
server.servlet.contextPath=/nacos
nacos.core.auth.enabled=true
nacos.naming.distro.taskDispatchThreadCount=10`,
		},
	}
	if err := fakeClient.Create(ctx, internalConfig); err != nil {
		t.Fatalf("Failed to create internal-config ConfigMap: %v", err)
	}
	t.Log("✓ Created internal-config ConfigMap")

	// Sync user-config and internal-config to kubernetes clientset
	// This is needed because buildMergedConfigMap reads from kubernetes clientset
	bridge.SyncFromCtrlToKube(ctx, "default")
	t.Log("✓ Synced config ConfigMaps to kubernetes clientset")

	// Create Nacos CR with config management fields
	nacos := testutil.NewNacosCluster("test-config-merge", "default", replicas)
	nacos.UID = "test-uid-config-merge"
	nacos.Spec.UserConfigRef = &nacosgroupv1alpha1.ConfigMapRef{
		Name: "test-user-config",
		Key:  "user.properties",
	}
	nacos.Spec.InternalConfigRef = &nacosgroupv1alpha1.ConfigMapRef{
		Name: "test-internal-config",
		Key:  "internal.properties",
	}
	nacos.Spec.FinalConfigName = "test-config-merge-final"

	if err := fakeClient.Create(ctx, nacos); err != nil {
		t.Fatalf("Failed to create Nacos CR: %v", err)
	}
	t.Log("✓ Created Nacos CR with config management fields")

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-config-merge", Namespace: "default"},
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

	// Round 2: MakeEnsure - Should merge configs and create final-config
	t.Log("=== Round 2: MakeEnsure - Merging configs ===")
	result, err = reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Logf("Round 2 error: %v", err)
	}

	// Sync resources from kubernetes clientset to controller-runtime client
	bridge.SyncFromKubeToCtrl(ctx, "default")

	// Verify final-config ConfigMap was created in k8s cluster
	finalConfig := &corev1.ConfigMap{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-config-merge-final", Namespace: "default"}, finalConfig); err != nil {
		t.Fatalf("Final-config ConfigMap not created in k8s cluster: %v", err)
	}
	t.Logf("✓ Final-config ConfigMap created in k8s cluster: %s", finalConfig.Name)

	// Verify final-config contains merged content
	mergedContent, ok := finalConfig.Data["application.properties"]
	if !ok {
		t.Fatalf("Final-config does not contain 'application.properties' key")
	}
	t.Logf("✓ Final-config contains merged content (%d bytes)", len(mergedContent))

	// Verify merged content contains both user and internal config
	if !strings.Contains(mergedContent, "nacos.console.ui.enabled=true") {
		t.Errorf("Merged config missing user parameter: nacos.console.ui.enabled")
	}
	if !strings.Contains(mergedContent, "server.port=8848") {
		t.Errorf("Merged config missing internal parameter: server.port")
	}
	if !strings.Contains(mergedContent, "nacos.core.auth.enabled=true") {
		t.Errorf("Merged config missing internal parameter: nacos.core.auth.enabled")
	}
	t.Log("✓ Merged config contains both user and internal parameters")

	// Verify StatefulSet was created
	sts := &appv1.StatefulSet{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "test-config-merge", Namespace: "default"}, sts); err != nil {
		t.Fatalf("StatefulSet not created: %v", err)
	}
	t.Logf("✓ StatefulSet created: %s", sts.Name)

	// Verify StatefulSet mounts the final-config ConfigMap
	foundConfigVolume := false
	for _, vol := range sts.Spec.Template.Spec.Volumes {
		if vol.ConfigMap != nil && vol.ConfigMap.Name == "test-config-merge-final" {
			foundConfigVolume = true
			t.Logf("✓ StatefulSet has volume referencing final-config: %s", vol.Name)
			break
		}
	}
	if !foundConfigVolume {
		t.Errorf("StatefulSet does not have a volume referencing final-config ConfigMap")
	}

	// Verify container has volumeMount for the config
	foundVolumeMount := false
	if len(sts.Spec.Template.Spec.Containers) > 0 {
		for _, mount := range sts.Spec.Template.Spec.Containers[0].VolumeMounts {
			// Check if mount path is for nacos config
			if strings.Contains(mount.MountPath, "/home/nacos") && strings.Contains(mount.MountPath, "application.properties") {
				foundVolumeMount = true
				t.Logf("✓ Container has volumeMount for config at: %s", mount.MountPath)
				break
			}
		}
	}
	if !foundVolumeMount {
		t.Errorf("Container does not have volumeMount for final-config")
	}

	// Simulate K8s creating pods
	t.Log("=== Round 3: Simulating K8s pod creation ===")
	if err := simulator.SimulateStatefulSetPodsWithIP(ctx, "default", "test-config-merge", "127.0.0.1"); err != nil {
		t.Fatalf("Failed to simulate pods: %v", err)
	}

	// Verify all pods created and check if they mount the final-config
	for i := int32(0); i < replicas; i++ {
		podName := fmt.Sprintf("test-config-merge-%d", i)
		pod := &corev1.Pod{}
		if err := fakeClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: "default"}, pod); err != nil {
			t.Fatalf("Pod %s not created: %v", podName, err)
		}

		// Verify pod has the config volume
		foundPodConfigVolume := false
		for _, vol := range pod.Spec.Volumes {
			if vol.ConfigMap != nil && vol.ConfigMap.Name == "test-config-merge-final" {
				foundPodConfigVolume = true
				break
			}
		}
		if !foundPodConfigVolume {
			t.Errorf("Pod %s does not have volume referencing final-config ConfigMap", podName)
		}

		if pod.Spec.SchedulerName != "hostpath-scheduler" {
			t.Errorf("Pod %s does not use hostpath-scheduler", podName)
		}
	}
	t.Logf("✓ Created %d pods, all mounting final-config ConfigMap", replicas)

	// Continue reconciling until Running
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

		t.Logf("Round %d: Phase=%s", i, updatedNacos.Status.Phase)

		if updatedNacos.Status.Phase == nacosgroupv1alpha1.PhaseRunning {
			t.Logf("✓ Status reached Running at round %d", i)
			break
		}

		if i == maxRounds {
			t.Errorf("Status did not reach Running after %d rounds, final phase: %s", maxRounds, updatedNacos.Status.Phase)
		}
	}

	// Verify config digest was set in status (optional - will be implemented in requirement 2)
	if err := fakeClient.Get(ctx, req.NamespacedName, updatedNacos); err != nil {
		t.Fatalf("Failed to get final Nacos CR: %v", err)
	}

	if updatedNacos.Status.ConfigDigest == "" {
		t.Logf("Note: ConfigDigest not yet set in status (will be implemented in requirement 2 - rolling update)")
	} else {
		t.Logf("✓ ConfigDigest set in status: %s", updatedNacos.Status.ConfigDigest)
	}

	t.Log("✓✓✓ Config merge test PASSED ✓✓✓")
}
