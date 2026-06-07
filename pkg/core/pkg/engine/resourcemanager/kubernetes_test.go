package resourcemanager

import (
	"context"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/tests/testutil"
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
	q := testutil.NewTestQueue()
	rm := &KubernetesResourceManager{
		q: q, kubeClient: fakeClient, namespace: "default",
		deployName: "flowgent-taskmanager",
	}
	err := rm.Validate(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
		minTMs: 1, maxTMs: 5,
	}
	ctx, cancel := context.WithCancel(context.Background())
	rm.ctx = ctx
	rm.cancel = cancel

	if err := rm.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	scale, _ := fakeClient.AppsV1().Deployments("default").
		GetScale(context.Background(), "flowgent-taskmanager", metav1.GetOptions{})
	if scale.Spec.Replicas != 1 {
		t.Errorf("expected replicas=%d after shutdown, got %d", rm.minTMs, scale.Spec.Replicas)
	}
}
