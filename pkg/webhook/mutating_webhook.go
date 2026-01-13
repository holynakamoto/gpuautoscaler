package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/gpuautoscaler/gpuautoscaler/pkg/sharing"
)

// GPUOptimizationWebhook is a mutating webhook that automatically optimizes GPU requests
type GPUOptimizationWebhook struct {
	client    client.Client
	decoder   *admission.Decoder
	migMgr    *sharing.MIGManager
	mpsMgr    *sharing.MPSManager
	tsMgr     *sharing.TimeSlicingManager
	enableMIG bool
	enableMPS bool
	enableTS  bool
}

// NewGPUOptimizationWebhook creates a new GPU optimization webhook
func NewGPUOptimizationWebhook(client client.Client, enableMIG, enableMPS, enableTS bool) *GPUOptimizationWebhook {
	return &GPUOptimizationWebhook{
		client:    client,
		migMgr:    sharing.NewMIGManager(client),
		mpsMgr:    sharing.NewMPSManager(client),
		tsMgr:     sharing.NewTimeSlicingManager(client),
		enableMIG: enableMIG,
		enableMPS: enableMPS,
		enableTS:  enableTS,
	}
}

// Handle processes admission requests for pod creation/updates
func (w *GPUOptimizationWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := log.FromContext(ctx)
	log.Info("Processing admission request",
		"namespace", req.Namespace,
		"name", req.Name,
		"operation", req.Operation)

	pod := &corev1.Pod{}
	if err := w.decoder.Decode(req, pod); err != nil {
		log.Error(err, "Failed to decode pod")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Check if pod requests GPUs
	if !hasGPURequest(pod) {
		log.V(1).Info("Pod does not request GPUs, skipping optimization")
		return admission.Allowed("no GPU requests")
	}

	// Check if optimization is disabled for this pod
	if pod.Annotations["gpu-autoscaler.io/optimize"] == "false" {
		log.Info("GPU optimization disabled for pod via annotation")
		return admission.Allowed("optimization disabled")
	}

	// Create a copy for mutations
	optimizedPod := pod.DeepCopy()

	// Apply optimizations
	modified := false
	var err error

	// Determine the best sharing strategy
	strategy, err := w.selectOptimizationStrategy(ctx, optimizedPod)
	if err != nil {
		log.Error(err, "Failed to select optimization strategy")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	log.Info("Selected optimization strategy",
		"pod", pod.Name,
		"strategy", strategy)

	switch strategy {
	case "mig":
		if w.enableMIG {
			if err := w.applyMIGOptimization(ctx, optimizedPod); err != nil {
				log.Error(err, "Failed to apply MIG optimization")
			} else {
				modified = true
			}
		}
	case "mps":
		if w.enableMPS {
			if err := w.applyMPSOptimization(ctx, optimizedPod); err != nil {
				log.Error(err, "Failed to apply MPS optimization")
			} else {
				modified = true
			}
		}
	case "timeslicing":
		if w.enableTS {
			if err := w.applyTimeSlicingOptimization(ctx, optimizedPod); err != nil {
				log.Error(err, "Failed to apply time-slicing optimization")
			} else {
				modified = true
			}
		}
	case "exclusive":
		log.Info("Pod requires exclusive GPU access, no optimization applied")
	default:
		log.Info("No optimization strategy selected")
	}

	if !modified {
		return admission.Allowed("no optimizations applied")
	}

	// Marshal the modified pod
	marshaledPod, err := json.Marshal(optimizedPod)
	if err != nil {
		log.Error(err, "Failed to marshal optimized pod")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	log.Info("Pod optimized successfully",
		"pod", pod.Name,
		"strategy", strategy)

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

// selectOptimizationStrategy selects the best GPU sharing strategy for a pod
func (w *GPUOptimizationWebhook) selectOptimizationStrategy(ctx context.Context, pod *corev1.Pod) (string, error) {
	log := log.FromContext(ctx)

	// Check for explicit strategy in annotations
	if strategy, ok := pod.Annotations["gpu-autoscaler.io/sharing-mode"]; ok {
		log.Info("Using explicit sharing mode from annotation", "strategy", strategy)
		return strategy, nil
	}

	// Extract workload characteristics
	gpuRequest := getTotalGPURequest(pod)
	memoryRequest := getTotalMemoryRequest(pod)
	workloadType := pod.Labels["gpu-autoscaler.io/workload-type"]

	log.Info("Analyzing workload characteristics",
		"gpuRequest", gpuRequest,
		"memoryMB", memoryRequest/(1024*1024),
		"workloadType", workloadType)

	// Decision logic for strategy selection:
	// 1. MIG: Small workloads (<20GB memory, 1 GPU) - hardware isolation
	// 2. MPS: Inference workloads with low utilization - process-level sharing
	// 3. Time-slicing: Development/batch workloads - temporal sharing
	// 4. Exclusive: Training or high-performance workloads

	// Check for training workloads - typically need exclusive access
	if workloadType == "training" {
		return "exclusive", nil
	}

	// Small workloads are good candidates for MIG
	if w.enableMIG && gpuRequest == 1 && memoryRequest < 20*1024*1024*1024 {
		return "mig", nil
	}

	// Inference workloads benefit from MPS
	if w.enableMPS && workloadType == "inference" {
		return "mps", nil
	}

	// Development and batch workloads work well with time-slicing
	if w.enableTS && (workloadType == "development" || workloadType == "batch") {
		return "timeslicing", nil
	}

	// Check if sharing is explicitly requested
	if pod.Annotations["gpu-autoscaler.io/sharing"] == "enabled" {
		// Default to MPS for inference-like workloads
		if w.enableMPS {
			return "mps", nil
		}
		// Fall back to time-slicing
		if w.enableTS {
			return "timeslicing", nil
		}
	}

	// Default to exclusive access
	return "exclusive", nil
}

// applyMIGOptimization applies MIG-based optimization to a pod
func (w *GPUOptimizationWebhook) applyMIGOptimization(ctx context.Context, pod *corev1.Pod) error {
	log := log.FromContext(ctx)
	log.Info("Applying MIG optimization", "pod", pod.Name)

	gpuRequest := getTotalGPURequest(pod)
	memoryRequest := getTotalMemoryRequest(pod)

	// Select appropriate MIG profile
	profile, err := w.migMgr.GetMIGProfile(gpuRequest, memoryRequest)
	if err != nil {
		return fmt.Errorf("failed to get MIG profile: %w", err)
	}

	log.Info("Selected MIG profile",
		"pod", pod.Name,
		"profile", profile.Name,
		"memory", profile.Memory)

	// Convert pod to use MIG
	if err := w.migMgr.ConvertPodToMIG(ctx, pod, *profile); err != nil {
		return fmt.Errorf("failed to convert pod to MIG: %w", err)
	}

	// Add optimization metadata
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations["gpu-autoscaler.io/optimized"] = "true"
	pod.Annotations["gpu-autoscaler.io/optimization-strategy"] = "mig"
	pod.Annotations["gpu-autoscaler.io/optimization-timestamp"] = ctx.Value("timestamp").(string)

	return nil
}

// applyMPSOptimization applies MPS-based optimization to a pod
func (w *GPUOptimizationWebhook) applyMPSOptimization(ctx context.Context, pod *corev1.Pod) error {
	log := log.FromContext(ctx)
	log.Info("Applying MPS optimization", "pod", pod.Name)

	config := sharing.DefaultMPSConfig()

	// Convert pod to use MPS
	if err := w.mpsMgr.ConvertPodToMPS(ctx, pod, config); err != nil {
		return fmt.Errorf("failed to convert pod to MPS: %w", err)
	}

	// Add optimization metadata
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations["gpu-autoscaler.io/optimized"] = "true"
	pod.Annotations["gpu-autoscaler.io/optimization-strategy"] = "mps"
	pod.Annotations["gpu-autoscaler.io/optimization-timestamp"] = ctx.Value("timestamp").(string)

	return nil
}

// applyTimeSlicingOptimization applies time-slicing optimization to a pod
func (w *GPUOptimizationWebhook) applyTimeSlicingOptimization(ctx context.Context, pod *corev1.Pod) error {
	log := log.FromContext(ctx)
	log.Info("Applying time-slicing optimization", "pod", pod.Name)

	replicasPerGPU := 4 // Default replicas

	// Convert pod to use time-slicing
	if err := w.tsMgr.ConvertPodToTimeSlicing(ctx, pod, replicasPerGPU); err != nil {
		return fmt.Errorf("failed to convert pod to time-slicing: %w", err)
	}

	// Add optimization metadata
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations["gpu-autoscaler.io/optimized"] = "true"
	pod.Annotations["gpu-autoscaler.io/optimization-strategy"] = "timeslicing"
	pod.Annotations["gpu-autoscaler.io/optimization-timestamp"] = ctx.Value("timestamp").(string)

	return nil
}

// InjectDecoder injects the decoder
func (w *GPUOptimizationWebhook) InjectDecoder(d *admission.Decoder) error {
	w.decoder = d
	return nil
}

// hasGPURequest checks if a pod requests GPUs
func hasGPURequest(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if _, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
			return true
		}
		if _, ok := container.Resources.Limits["nvidia.com/gpu"]; ok {
			return true
		}
	}
	return false
}

// getTotalGPURequest calculates total GPU request for a pod
func getTotalGPURequest(pod *corev1.Pod) int {
	total := 0
	for _, container := range pod.Spec.Containers {
		if gpuReq, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
			total += int(gpuReq.Value())
		}
	}
	return total
}

// getTotalMemoryRequest calculates total memory request for a pod
func getTotalMemoryRequest(pod *corev1.Pod) int64 {
	total := int64(0)
	for _, container := range pod.Spec.Containers {
		if memReq, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
			total += memReq.Value()
		}
	}
	return total
}

// OptimizationStats tracks webhook optimization statistics
type OptimizationStats struct {
	TotalRequests      int64
	OptimizedPods      int64
	MIGOptimizations   int64
	MPSOptimizations   int64
	TSOptimizations    int64
	FailedOptimizations int64
}

// Global stats for monitoring
var WebhookStats = &OptimizationStats{}

// RecordOptimization records an optimization event
func RecordOptimization(strategy string, success bool) {
	WebhookStats.TotalRequests++
	if success {
		WebhookStats.OptimizedPods++
		switch strategy {
		case "mig":
			WebhookStats.MIGOptimizations++
		case "mps":
			WebhookStats.MPSOptimizations++
		case "timeslicing":
			WebhookStats.TSOptimizations++
		}
	} else {
		WebhookStats.FailedOptimizations++
	}
}
