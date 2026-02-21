package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubernetesScheduler launches a Kubernetes Job per task execution.
// Each Job runs a container with the tasklet image that executes the
// node and persists the result back to the shared store.
//
// Resource management is delegated to Kubernetes (ResourceQuota,
// LimitRange, node selectors), keeping the scheduler itself thin.
type KubernetesScheduler struct {
	client    kubernetes.Interface
	namespace string
	taskImage string // container image for the tasklet binary
	pullPolicy corev1.PullPolicy

	ttlSecondsAfterFinish int32
	backoffLimit          int32
}

// KubernetesSchedulerConfig configures the Kubernetes scheduler.
type KubernetesSchedulerConfig struct {
	// MasterURL is the Kubernetes API server endpoint.
	// Leave empty to use in-cluster config or ~/.kube/config.
	MasterURL string

	// KubeConfigPath is an optional explicit kubeconfig path.
	// Leave empty to use in-cluster config or default search.
	KubeConfigPath string

	// Namespace where task Jobs are created (default: "default").
	Namespace string

	// TaskImage is the container image for task execution pods.
	// Each pod runs the flowgent-tasklet binary with TaskSubmit
	// passed via environment. Example: "flowgent/tasklet:latest".
	TaskImage string

	// PullPolicy for the task container (default: IfNotPresent).
	PullPolicy corev1.PullPolicy

	// TTLSecondsAfterFinish controls automatic Job cleanup after completion.
	TTLSecondsAfterFinish int32

	// BackoffLimit for failed task attempts (default: 0 = no retry by K8s).
	BackoffLimit int32
}

// NewKubernetesScheduler creates a KubernetesScheduler connected to the cluster.
// Config resolution order:
//  1. Explicit KubeConfigPath (if non-empty)
//  2. In-cluster config (pod running inside K8s)
//  3. Default kubeconfig file (~/.kube/config)
func NewKubernetesScheduler(cfg *KubernetesSchedulerConfig) (*KubernetesScheduler, error) {
	if cfg == nil {
		cfg = &KubernetesSchedulerConfig{}
	}

	// Build REST config
	var restConfig *rest.Config
	var err error

	switch {
	case cfg.KubeConfigPath != "":
		restConfig, err = clientcmd.BuildConfigFromFlags(cfg.MasterURL, cfg.KubeConfigPath)
	default:
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			// Fall back to default kubeconfig
			kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
			restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("kubernetes scheduler: build config: %w", err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("kubernetes scheduler: create client: %w", err)
	}

	namespace := cfg.Namespace
	if namespace == "" {
		// Try in-cluster namespace
		if ns := os.Getenv("KUBERNETES_NAMESPACE"); ns != "" {
			namespace = ns
		} else {
			namespace = "default"
		}
	}

	image := cfg.TaskImage
	if image == "" {
		image = "flowgent/tasklet:latest"
	}

	pullPolicy := cfg.PullPolicy
	if pullPolicy == "" {
		pullPolicy = corev1.PullIfNotPresent
	}

	ttl := cfg.TTLSecondsAfterFinish
	if ttl <= 0 {
		ttl = 60
	}

	backoff := cfg.BackoffLimit
	if backoff == 0 {
		backoff = 0 // no K8s-level retry — JobManager handles retry
	}

	return &KubernetesScheduler{
		client:                client,
		namespace:             namespace,
		taskImage:             image,
		pullPolicy:            pullPolicy,
		ttlSecondsAfterFinish: ttl,
		backoffLimit:          backoff,
	}, nil
}

func (s *KubernetesScheduler) Type() SchedulerType { return SchedulerTypeKubernetes }

// SubmitTask creates a Kubernetes Job that executes the given task.
// The Job runs the configured tasklet image with task parameters
// passed as environment variables. The method blocks until the Job
// completes or fails.
func (s *KubernetesScheduler) SubmitTask(ctx context.Context, submit *TaskSubmit) (*TaskResult, error) {
	jobName := fmt.Sprintf("flowgent-task-%s-%s", submit.RunID[:8], submit.NodeID)
	// Sanitize: K8s names must be lowercase alphanumeric + hyphens
	jobName = sanitizeK8sName(jobName)

	slog.Info("kubernetes scheduler submitting task",
		"cluster_id", submit.ClusterID,
		"run_id", submit.RunID,
		"node_id", submit.NodeID,
		"job", jobName,
		"image", s.taskImage,
	)

	// Serialize TaskSubmit for the pod
	submitJSON, err := json.Marshal(submit)
	if err != nil {
		return nil, fmt.Errorf("kubernetes scheduler: marshal submit: %w", err)
	}

	job := s.buildJob(jobName, string(submitJSON))

	created, err := s.client.BatchV1().Jobs(s.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			slog.Debug("kubernetes scheduler job already exists, reusing", "job", jobName)
			created, err = s.client.BatchV1().Jobs(s.namespace).Get(ctx, jobName, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("kubernetes scheduler: get existing job: %w", err)
			}
		} else {
			return nil, fmt.Errorf("kubernetes scheduler: create job: %w", err)
		}
	}

	slog.Debug("kubernetes scheduler job created",
		"job", created.Name,
		"uid", created.UID,
	)

	// Wait for Job completion
	result, err := s.watchJob(ctx, jobName)
	if err != nil {
		return nil, fmt.Errorf("kubernetes scheduler: watch job %s: %w", jobName, err)
	}

	// Clean up — delete the Job (pods are cleaned up via TTL or owner reference)
	if err := s.client.BatchV1().Jobs(s.namespace).Delete(ctx, jobName, metav1.DeleteOptions{}); err != nil {
		slog.Warn("kubernetes scheduler: delete job failed", "job", jobName, "error", err)
	}

	return result, nil
}

func (s *KubernetesScheduler) buildJob(name, submitJSON string) *batchv1.Job {
	backoff := s.backoffLimit
	ttl := s.ttlSecondsAfterFinish
	labels := map[string]string{
		"app.kubernetes.io/component": "tasklet",
		"flowgent/task-type":         "node-execution",
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoff,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "tasklet",
							Image:           s.taskImage,
							ImagePullPolicy: s.pullPolicy,
							Env: []corev1.EnvVar{
								{
									Name:  "FLOWGENT_TASK_SUBMIT",
									Value: submitJSON,
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func (s *KubernetesScheduler) watchJob(ctx context.Context, jobName string) (*TaskResult, error) {
	watcher, err := s.client.BatchV1().Jobs(s.namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector:  fields.OneTermEqualSelector("metadata.name", jobName).String(),
		TimeoutSeconds: ptrToInt64(300),
	})
	if err != nil {
		return nil, fmt.Errorf("create watch: %w", err)
	}
	defer watcher.Stop()

	timeout := time.After(10 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("job watch timeout after 10m")
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return nil, fmt.Errorf("job watch channel closed")
			}

			job, ok := event.Object.(*batchv1.Job)
			if !ok {
				continue
			}

			switch event.Type {
			case watch.Modified, watch.Added:
				for _, c := range job.Status.Conditions {
					switch c.Type {
					case batchv1.JobComplete:
						slog.Info("kubernetes scheduler job completed", "job", jobName)
						// Result is stored in the shared DB by the tasklet.
						// The JobManager will re-read the TaskRun from store.
						return &TaskResult{Output: map[string]any{
							"status":  "completed",
							"message": c.Message,
						}}, nil

					case batchv1.JobFailed:
						slog.Error("kubernetes scheduler job failed",
							"job", jobName,
							"reason", c.Reason,
							"message", c.Message,
						)
						return &TaskResult{
							Error: fmt.Sprintf("job failed: %s: %s", c.Reason, c.Message),
						}, nil
					}
				}

			case watch.Deleted:
				return nil, fmt.Errorf("job deleted before completion")
			case watch.Error:
				return nil, fmt.Errorf("job watch error event")
			}
		}
	}
}

func (s *KubernetesScheduler) Close() error { return nil }

// ─── Helpers ─────────────────────────────────────────

func sanitizeK8sName(name string) string {
	b := make([]byte, 0, len(name))
	for _, c := range []byte(name) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b = append(b, c)
		} else if c >= 'A' && c <= 'Z' {
			b = append(b, c+32)
		} else {
			b = append(b, '-')
		}
	}
	if len(b) > 63 {
		b = b[:63]
	}
	return string(b)
}

func ptrToInt64(v int64) *int64 { return &v }
