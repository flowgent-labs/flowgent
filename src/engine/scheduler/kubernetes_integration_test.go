//go:build integration
// +build integration

package scheduler

import (
	"context"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testDeployName = "flowgent-tm-test"

func realK8sClient(t *testing.T) kubernetes.Interface {
	t.Helper()
	kubeconfig := homedir.HomeDir() + "/.kube/config"
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Skipf("no kubeconfig: %v", err)
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	return client
}

func TestKubernetesScheduler_RealCluster_CreateAndScale(t *testing.T) {
	if testing.Short() { t.Skip("skipping integration test in short mode") }
	client := realK8sClient(t)
	namespace := "default"
	ctx := context.Background()

	// Clean up any leftover test deployment
	_ = client.AppsV1().Deployments(namespace).Delete(ctx, testDeployName, metav1.DeleteOptions{})

	rm := &KubernetesResourceManager{
		kubeClient:  client,
		namespace:   namespace,
		deployName:  testDeployName,
		slotsPerTM:  4,
		minTMs:      1,
		maxTMs:      3,
		currentTMs:  1,
		idleTimeout: 5 * time.Minute,
		planTimeout: 5 * time.Minute,
	}
	rctx, cancel := context.WithCancel(ctx)
	rm.ctx = rctx
	rm.cancel = cancel
	defer rm.Shutdown(ctx)

	// Step 1: Create the TM Deployment
	t.Log("Creating TM Deployment...")
	if err := rm.ensureDeployment(ctx); err != nil {
		t.Fatalf("ensureDeployment: %v", err)
	}

	// Step 2: Verify Deployment exists with 1 replica
	deploy, err := client.AppsV1().Deployments(namespace).Get(ctx, testDeployName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if *deploy.Spec.Replicas != 1 {
		t.Errorf("expected 1 replica after create, got %d", *deploy.Spec.Replicas)
	}
	t.Logf("Deployment created: replicas=%d", *deploy.Spec.Replicas)

	// Step 3: Scale up to 3
	t.Log("Scaling up to 3 replicas...")
	if err := rm.scaleDeployment(ctx, 3); err != nil {
		t.Fatalf("scale up to 3: %v", err)
	}
	time.Sleep(3 * time.Second) // let controller reconcile

	scale, err := client.AppsV1().Deployments(namespace).GetScale(ctx, testDeployName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get scale: %v", err)
	}
	if scale.Spec.Replicas != 3 {
		t.Errorf("expected 3 replicas after scale up, got %d", scale.Spec.Replicas)
	}
	pods, _ := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "flowgent/role=worker",
	})
	t.Logf("Scale up done: replicas=%d, running pods=%d", scale.Spec.Replicas, len(pods.Items))

	// Step 4: Scale down to 1
	t.Log("Scaling down to 1 replica...")
	if err := rm.scaleDeployment(ctx, 1); err != nil {
		t.Fatalf("scale down to 1: %v", err)
	}
	time.Sleep(2 * time.Second)

	deploy, err = client.AppsV1().Deployments(namespace).Get(ctx, testDeployName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment after scale down: %v", err)
	}
	if *deploy.Spec.Replicas != 1 {
		t.Errorf("expected 1 replica after scale down, got %d", *deploy.Spec.Replicas)
	}

	// Step 5: Clean up
	_ = client.AppsV1().Deployments(namespace).Delete(ctx, testDeployName, metav1.DeleteOptions{})
	t.Log("Done: deployment deleted")
}
