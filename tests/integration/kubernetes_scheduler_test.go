//go:build integration
// +build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/src/engine/resourcemanager"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const testDeployName = "flowgent-int-test"

func realK8sClient(t *testing.T) kubernetes.Interface {
	t.Helper()
	kubeconfig := homedir.HomeDir() + "/.kube/config"
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil { t.Skipf("no kubeconfig: %v", err) }
	c, err := kubernetes.NewForConfig(cfg)
	if err != nil { t.Fatalf("create client: %v", err) }
	return c
}

func TestKubernetesScheduler_RealCluster_CreateAndScale(t *testing.T) {
	if testing.Short() { t.Skip("skipping integration test in short mode") }
	client := realK8sClient(t)
	ns := "default"
	ctx := context.Background()

	_ = client.AppsV1().Deployments(ns).Delete(ctx, testDeployName, metav1.DeleteOptions{})

	rm := &resourcemanager.KubernetesResourceManager{}
	rm.InitForTest(client, ns, testDeployName)
	rctx, cancel := context.WithCancel(ctx)
	rm.SetCtx(rctx, cancel)
	defer rm.Shutdown(ctx)

	t.Log("Creating TM Deployment...")
	if err := rm.EnsureDeployment(ctx); err != nil {
		t.Fatalf("ensureDeployment: %v", err)
	}

	deploy, err := client.AppsV1().Deployments(ns).Get(ctx, testDeployName, metav1.GetOptions{})
	if err != nil { t.Fatalf("get: %v", err) }
	if *deploy.Spec.Replicas != 1 { t.Errorf("expected 1, got %d", *deploy.Spec.Replicas) }
	t.Logf("Created: replicas=%d", *deploy.Spec.Replicas)

	t.Log("Scale up to 3...")
	if err := rm.ScaleDeployment(ctx, 3); err != nil { t.Fatalf("scale up: %v", err) }
	time.Sleep(3 * time.Second)
	scale, _ := client.AppsV1().Deployments(ns).GetScale(ctx, testDeployName, metav1.GetOptions{})
	if scale.Spec.Replicas != 3 { t.Errorf("expected 3, got %d", scale.Spec.Replicas) }
	t.Logf("Scaled up: replicas=%d", scale.Spec.Replicas)

	t.Log("Scale down to 1...")
	if err := rm.ScaleDeployment(ctx, 1); err != nil { t.Fatalf("scale down: %v", err) }
	time.Sleep(2 * time.Second)
	deploy, _ = client.AppsV1().Deployments(ns).Get(ctx, testDeployName, metav1.GetOptions{})
	if *deploy.Spec.Replicas != 1 { t.Errorf("expected 1, got %d", *deploy.Spec.Replicas) }

	_ = client.AppsV1().Deployments(ns).Delete(ctx, testDeployName, metav1.DeleteOptions{})
	t.Log("Done")
}
