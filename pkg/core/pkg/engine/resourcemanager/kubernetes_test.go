package resourcemanager

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestKubernetesResourceManager_Creation(t *testing.T) {
	// Use fake clientset to avoid needing a real K8s cluster
	fakeClient := fake.NewSimpleClientset()
	rm := &KubernetesResourceManager{
		kubeClient:  fakeClient,
		namespace:   "default",
		deployName:  "flowgent-taskmanager",
		slotsPerTM:  4,
		minTMs:      2,
		maxTMs:      10,
		currentTMs:  2,
		idleTimeout: 5 * time.Minute,
		planTimeout: 5 * time.Minute,
	}
	ctx, cancel := context.WithCancel(context.Background())
	rm.ctx = ctx
	rm.cancel = cancel

	if rm.Provider() != engine.ProviderKubernetes {
		t.Error("expected kubernetes provider type")
	}
	_ = rm.Shutdown(context.Background())
}

func TestKubernetesResourceManager_Validate_NoQueue(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	rm := &KubernetesResourceManager{
		kubeClient: fakeClient, namespace: "default",
		deployName: "flowgent-taskmanager",
	}
	// Queue is validated lazily in Schedule() — Validate() should pass with nil queue.
	if err := rm.Validate(context.Background()); err != nil {
		t.Fatalf("unexpected Validate error (queue is lazy-validated): %v", err)
	}
}

func TestKubernetesResourceManager_Validate_WithQueue(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	q := messager.NewLocalMessager(10)
	rm := &KubernetesResourceManager{
		q: q, kubeClient: fakeClient, namespace: "default",
		deployName: "flowgent-taskmanager",
	}
	err := rm.Validate(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestKubernetesResourceManagerWaitsForScopedRuntimeReady(t *testing.T) {
	q := messager.NewLocalMessager(10)
	rm := &KubernetesResourceManager{
		q: q, ownerNamespaceID: "default", runtimeClusterID: "cluster-a",
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rm.ensureRuntimeReadySubscription(ctx, "taskmanager"); err != nil {
		t.Fatalf("subscribe readiness: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- rm.waitForRuntimeReady(ctx, "taskmanager") }()

	ready := messager.RuntimeReady{
		WorkerID: "tm-1", Role: "taskmanager", Namespace: "default",
		ClusterID: "cluster-a", Timestamp: time.Now(),
	}
	payload, _ := json.Marshal(ready)
	if err := q.Publish(ctx, messager.RuntimeReadyTopic("default", "cluster-a", "taskmanager", "tm-1"), &messager.InterMessage{Payload: payload}); err != nil {
		t.Fatalf("publish readiness: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("wait readiness: %v", err)
	}
}

func TestKubernetesResourceManager_ScaleDeployment(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	// Pre-create a Deployment to scale
	replicas := int32(1)
	_ = fakeClient.Tracker().Add(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "flowgent-taskmanager", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"flowgent/role": "worker"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"flowgent/role": "worker"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "tm", Image: "flowgent/tm:latest"}},
				},
			},
		},
	})

	rm := &KubernetesResourceManager{
		kubeClient: fakeClient, namespace: "default",
		deployName: "flowgent-taskmanager", currentTMs: 1,
		minTMs: 1, maxTMs: 5,
	}

	// Scale up to 3
	if err := rm.scaleDeployment(context.Background(), 3); err != nil {
		t.Fatalf("scale up failed: %v", err)
	}

	// Verify the scale sub-resource was updated (fake client records it)
	scale, err := fakeClient.AppsV1().Deployments("default").
		GetScale(context.Background(), "flowgent-taskmanager", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get scale: %v", err)
	}
	if scale.Spec.Replicas != 3 {
		t.Errorf("expected replicas=3 after scale, got %d", scale.Spec.Replicas)
	}

	// Scale down to 1
	if err := rm.scaleDeployment(context.Background(), 1); err != nil {
		t.Fatalf("scale down failed: %v", err)
	}
	scale, _ = fakeClient.AppsV1().Deployments("default").
		GetScale(context.Background(), "flowgent-taskmanager", metav1.GetOptions{})
	if scale.Spec.Replicas != 1 {
		t.Errorf("expected replicas=1 after scale down, got %d", scale.Spec.Replicas)
	}
}

func TestKubernetesResourceManager_EnsureDeploymentOwnerLabels(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	rm := &KubernetesResourceManager{
		kubeClient:               fakeClient,
		namespace:                "default",
		deployName:               "flowgent-taskmanager-default-critical",
		tmImage:                  "flowgent:test",
		slotsPerTM:               4,
		minTMs:                   2,
		maxTMs:                   10,
		currentTMs:               2,
		idleTimeout:              5 * time.Minute,
		planTimeout:              5 * time.Minute,
		ownerNamespaceID:         "default",
		runtimeClusterID:         "app-run-1",
		ownerRunID:               "run-1",
		runtimeMode:              "application",
		ownerFlowID:              "sec-fix",
		ownerJobManagerName:      "flowgent-jobmanager-default-sec-fix-run-1",
		ownerJobManagerNamespace: "flowgent-default",
		credentialEnvSecret:      "flowgent-runtime-env",
		internalAuthSecret:       "flowgent-runtime-auth",
		taskManagerAuthKey:       "tm-token",
	}

	if err := rm.ensureDeployment(context.Background()); err != nil {
		t.Fatalf("ensureDeployment: %v", err)
	}

	dep, err := fakeClient.AppsV1().Deployments("default").
		Get(context.Background(), "flowgent-taskmanager-default-critical", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if got := dep.Labels[LabelManagedBy]; got != LabelValueRuntimeCluster {
		t.Fatalf("LabelManagedBy = %q, want %q", got, LabelValueRuntimeCluster)
	}
	if got := dep.Spec.Selector.MatchLabels[LabelRuntimeClusterID]; got != "app-run-1" {
		t.Fatalf("selector runtime cluster label = %q, want app-run-1", got)
	}
	if got := dep.Spec.Selector.MatchLabels[LabelRunID]; got != "run-1" {
		t.Fatalf("selector run label = %q, want run-1", got)
	}
	container := dep.Spec.Template.Spec.Containers[0]
	if len(container.EnvFrom) != 1 || container.EnvFrom[0].SecretRef == nil {
		t.Fatalf("expected only namespace worker credential ref, got %#v", container.EnvFrom)
	}
	if got := container.EnvFrom[0].SecretRef.Name; got != "flowgent-runtime-env" {
		t.Fatalf("TM credential secret = %q, want flowgent-runtime-env", got)
	}
	if container.EnvFrom[0].SecretRef.Optional == nil || !*container.EnvFrom[0].SecretRef.Optional {
		t.Fatal("TM credential secret ref should be optional")
	}
	var workloadToken *corev1.EnvVar
	for i := range container.Env {
		if container.Env[i].Name == "FLOWGENT_INTERNAL_TOKEN" {
			workloadToken = &container.Env[i]
		}
	}
	if workloadToken == nil || workloadToken.ValueFrom == nil || workloadToken.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("TM workload token secret ref missing: %#v", workloadToken)
	}
	if got := workloadToken.ValueFrom.SecretKeyRef.Name; got != "flowgent-runtime-auth" {
		t.Fatalf("TM workload Secret = %q", got)
	}
	if got := workloadToken.ValueFrom.SecretKeyRef.Key; got != "tm-token" {
		t.Fatalf("TM workload Secret key = %q", got)
	}
	var slots string
	for i := range container.Env {
		if container.Env[i].Name == "FLOWGENT__RUNTIME__TM_SLOTS" {
			slots = container.Env[i].Value
		}
	}
	if slots != "4" {
		t.Fatalf("TM slots env = %q, want 4", slots)
	}

}

func TestKubernetesResourceManager_ReconcilesRuntimeDeploymentTemplate(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	rm := &KubernetesResourceManager{
		kubeClient: fakeClient, namespace: "runtime", deployName: "cluster-a",
		tmImage: "flowgent:v1", slotsPerTM: 2, minTMs: 1, maxTMs: 1,
		ownerNamespaceID: "team-a", runtimeClusterID: "cluster-a", runtimeMode: "session",
	}
	if err := rm.ensureDeployment(context.Background()); err != nil {
		t.Fatalf("initial ensureDeployment: %v", err)
	}
	rm.tmImage = "flowgent:v2"
	rm.slotsPerTM = 8
	rm.nodeSelector = map[string]string{"workload": "critical"}
	if err := rm.ensureDeployment(context.Background()); err != nil {
		t.Fatalf("reconcile ensureDeployment: %v", err)
	}
	dep, err := fakeClient.AppsV1().Deployments("runtime").Get(context.Background(), "cluster-a", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if dep.Spec.Template.Spec.Containers[0].Image != "flowgent:v2" {
		t.Fatalf("image was not reconciled: %q", dep.Spec.Template.Spec.Containers[0].Image)
	}
	if dep.Spec.Template.Spec.NodeSelector["workload"] != "critical" {
		t.Fatalf("node selector was not reconciled: %#v", dep.Spec.Template.Spec.NodeSelector)
	}
}

func TestKubernetesResourceManager_DesiredTMCountUsesSlots(t *testing.T) {
	rm := &KubernetesResourceManager{slotsPerTM: 4, minTMs: 0, maxTMs: 10}

	cases := []struct {
		pending int64
		want    int
	}{
		{pending: 0, want: 0},
		{pending: 1, want: 1},
		{pending: 4, want: 1},
		{pending: 5, want: 2},
		{pending: 40, want: 10},
		{pending: 41, want: 10},
	}
	for _, tc := range cases {
		if got := rm.desiredTMCount(tc.pending); got != tc.want {
			t.Fatalf("desiredTMCount(%d) = %d, want %d", tc.pending, got, tc.want)
		}
	}
}

func TestKubernetesResourceManager_Shutdown(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	replicas := int32(2)
	_ = fakeClient.Tracker().Add(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "flowgent-taskmanager", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"flowgent/role": "worker"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"flowgent/role": "worker"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "tm", Image: "flowgent/tm:latest"}}},
			},
		},
	})

	rm := &KubernetesResourceManager{
		kubeClient: fakeClient, namespace: "default",
		deployName: "flowgent-taskmanager", currentTMs: 2,
		minTMs: 0, maxTMs: 5,
	}
	ctx, cancel := context.WithCancel(context.Background())
	rm.ctx = ctx
	rm.cancel = cancel

	if err := rm.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	scale, _ := fakeClient.AppsV1().Deployments("default").
		GetScale(context.Background(), "flowgent-taskmanager", metav1.GetOptions{})
	if scale.Spec.Replicas != int32(rm.minTMs) {
		t.Errorf("expected replicas=%d after shutdown, got %d", rm.minTMs, scale.Spec.Replicas)
	}
}
