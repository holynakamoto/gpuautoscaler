package sharing

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TimeSlicingManager handles GPU time-slicing configuration
// Time-slicing allows multiple workloads to share a GPU through temporal multiplexing
type TimeSlicingManager struct {
	client client.Client
}

// TimeSlicingConfig represents time-slicing configuration
type TimeSlicingConfig struct {
	Enabled         bool
	ReplicasPerGPU  int     // Number of replicas per physical GPU
	DefaultSliceMs  int     // Default time slice in milliseconds
	MinSliceMs      int     // Minimum time slice in milliseconds
	MaxSliceMs      int     // Maximum time slice in milliseconds
	OversubscribeOk bool    // Allow oversubscription
	FairnessMode    string  // "roundrobin", "priority", "weighted"
}

// DefaultTimeSlicingConfig returns default time-slicing configuration
func DefaultTimeSlicingConfig() TimeSlicingConfig {
	return TimeSlicingConfig{
		Enabled:         true,
		ReplicasPerGPU:  4, // 4 virtual GPUs per physical GPU
		DefaultSliceMs:  100,
		MinSliceMs:      10,
		MaxSliceMs:      1000,
		OversubscribeOk: false,
		FairnessMode:    "roundrobin",
	}
}

// NewTimeSlicingManager creates a new time-slicing manager
func NewTimeSlicingManager(client client.Client) *TimeSlicingManager {
	return &TimeSlicingManager{
		client: client,
	}
}

// IsTimeSlicingCapable checks if a node supports time-slicing
func (t *TimeSlicingManager) IsTimeSlicingCapable(ctx context.Context, nodeName string) (bool, error) {
	node := &corev1.Node{}
	if err := t.client.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
		return false, err
	}

	// Time-slicing is supported on most NVIDIA GPUs through device plugin
	capable, ok := node.Labels["nvidia.com/time-slicing.capable"]
	if !ok {
		// If not explicitly marked, check if node has NVIDIA GPUs
		_, hasGPU := node.Status.Capacity["nvidia.com/gpu"]
		return hasGPU, nil
	}

	return capable == "true", nil
}

// EnableTimeSlicingOnNode enables time-slicing on a specific node
func (t *TimeSlicingManager) EnableTimeSlicingOnNode(ctx context.Context, nodeName string, config TimeSlicingConfig) error {
	log := log.FromContext(ctx)
	log.Info("Enabling time-slicing on node",
		"node", nodeName,
		"replicasPerGPU", config.ReplicasPerGPU)

	node := &corev1.Node{}
	if err := t.client.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	// Add time-slicing configuration annotations
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations["nvidia.com/time-slicing.enabled"] = "true"
	node.Annotations["nvidia.com/time-slicing.replicas"] = fmt.Sprintf("%d", config.ReplicasPerGPU)
	node.Annotations["nvidia.com/time-slicing.slice-ms"] = fmt.Sprintf("%d", config.DefaultSliceMs)
	node.Annotations["nvidia.com/time-slicing.fairness"] = config.FairnessMode

	// Update node labels
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	node.Labels["nvidia.com/time-slicing.enabled"] = "true"
	node.Labels["nvidia.com/time-slicing.replicas"] = fmt.Sprintf("%d", config.ReplicasPerGPU)

	// Update GPU capacity to reflect virtual GPUs
	// This is typically handled by the NVIDIA device plugin, but we annotate for reference
	originalGPUCount := node.Status.Capacity["nvidia.com/gpu"]
	node.Annotations["nvidia.com/gpu.original-capacity"] = originalGPUCount.String()

	if err := t.client.Update(ctx, node); err != nil {
		return fmt.Errorf("failed to update node: %w", err)
	}

	log.Info("Time-slicing enabled successfully on node",
		"node", nodeName,
		"virtualGPUs", int(originalGPUCount.Value())*config.ReplicasPerGPU)

	return nil
}

// ConvertPodToTimeSlicing converts a pod to use time-sliced GPU
func (t *TimeSlicingManager) ConvertPodToTimeSlicing(ctx context.Context, pod *corev1.Pod, replicasPerGPU int) error {
	log := log.FromContext(ctx)
	log.Info("Converting pod to time-slicing",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"replicasPerGPU", replicasPerGPU)

	// Store original GPU request in annotation
	originalGPURequest := int64(0)
	for _, container := range pod.Spec.Containers {
		if gpuReq, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
			originalGPURequest += gpuReq.Value()
		}
	}

	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations["gpu-autoscaler.io/original-gpu-request"] = fmt.Sprintf("%d", originalGPURequest)
	pod.Annotations["gpu-autoscaler.io/time-slicing-enabled"] = "true"
	pod.Annotations["nvidia.com/time-slicing"] = "enabled"

	// Update GPU resource requests for time-sliced access
	// With time-slicing, pods still request nvidia.com/gpu but multiple pods
	// can share the same physical GPU
	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		if gpuReq, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
			// Keep the GPU request - the device plugin handles the virtual allocation
			container.Resources.Requests["nvidia.com/gpu"] = gpuReq
			container.Resources.Limits["nvidia.com/gpu"] = gpuReq
		}
	}

	// Add node selector for time-slicing enabled nodes
	if pod.Spec.NodeSelector == nil {
		pod.Spec.NodeSelector = make(map[string]string)
	}
	pod.Spec.NodeSelector["nvidia.com/time-slicing.enabled"] = "true"

	// Add toleration for time-sliced GPUs if needed
	hasTimeSlicingToleration := false
	for _, toleration := range pod.Spec.Tolerations {
		if toleration.Key == "nvidia.com/time-slicing" {
			hasTimeSlicingToleration = true
			break
		}
	}

	if !hasTimeSlicingToleration {
		pod.Spec.Tolerations = append(pod.Spec.Tolerations, corev1.Toleration{
			Key:      "nvidia.com/time-slicing",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		})
	}

	log.Info("Pod converted to time-slicing successfully",
		"pod", pod.Name,
		"namespace", pod.Namespace)

	return nil
}

// IsWorkloadSuitableForTimeSlicing determines if a workload is suitable for time-slicing
func (t *TimeSlicingManager) IsWorkloadSuitableForTimeSlicing(ctx context.Context, pod *corev1.Pod, avgUtilization float64) bool {
	// Time-slicing is suitable for:
	// 1. Bursty workloads with low average utilization
	// 2. Development and testing workloads
	// 3. Interactive workloads
	// 4. Batch processing with short GPU operations

	// Check if workload is marked as training (usually not suitable)
	workloadType, ok := pod.Labels["gpu-autoscaler.io/workload-type"]
	if ok && workloadType == "training" {
		return false
	}

	// Check if utilization is low (time-slicing works best with <60% utilization)
	if avgUtilization > 60.0 {
		return false
	}

	// Check if time-slicing is explicitly disabled
	if pod.Annotations["gpu-autoscaler.io/time-slicing-enabled"] == "false" {
		return false
	}

	return true
}

// EstimateTimeSlicingSavings estimates cost savings from time-slicing
func (t *TimeSlicingManager) EstimateTimeSlicingSavings(ctx context.Context, replicasPerGPU int) (*TimeSlicingSavingsReport, error) {
	log := log.FromContext(ctx)
	log.Info("Estimating time-slicing savings",
		"replicasPerGPU", replicasPerGPU)

	podList := &corev1.PodList{}
	if err := t.client.List(ctx, podList); err != nil {
		return nil, err
	}

	report := &TimeSlicingSavingsReport{
		TotalPods:            0,
		TimeSlicingEligible:  0,
		PotentialSavedGPUs:   0,
		EstimatedSavingsPct:  0,
		RecommendedReplicas:  replicasPerGPU,
	}

	for i := range podList.Items {
		pod := &podList.Items[i]

		// Check if pod is using GPUs
		gpuRequest := 0
		for _, container := range pod.Spec.Containers {
			if gpuReq, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
				gpuRequest += int(gpuReq.Value())
				report.TotalPods++
			}
		}

		if gpuRequest == 0 {
			continue
		}

		// Check if workload is suitable for time-slicing
		workloadType := pod.Labels["gpu-autoscaler.io/workload-type"]
		sharingEnabled := pod.Annotations["gpu-autoscaler.io/sharing"] == "enabled"

		// Time-slicing is good for dev/test, interactive, and batch workloads
		if workloadType == "development" || workloadType == "batch" || sharingEnabled {
			report.TimeSlicingEligible++
			// With time-slicing, multiple workloads share the same GPU
			// Savings = (replicas - 1) / replicas
			savingsPerPod := float64(replicasPerGPU-1) / float64(replicasPerGPU)
			report.PotentialSavedGPUs += savingsPerPod
		}
	}

	if report.TotalPods > 0 {
		report.EstimatedSavingsPct = (report.PotentialSavedGPUs / float64(report.TotalPods)) * 100
	}

	return report, nil
}

// TimeSlicingSavingsReport contains time-slicing cost savings analysis
type TimeSlicingSavingsReport struct {
	TotalPods            int
	TimeSlicingEligible  int
	PotentialSavedGPUs   float64
	EstimatedSavingsPct  float64
	RecommendedReplicas  int
	Timestamp            metav1.Time
}

// GetTimeSlicingStatus returns the current time-slicing status for a node
func (t *TimeSlicingManager) GetTimeSlicingStatus(ctx context.Context, nodeName string) (*TimeSlicingStatus, error) {
	node := &corev1.Node{}
	if err := t.client.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
		return nil, err
	}

	status := &TimeSlicingStatus{
		NodeName: nodeName,
		Enabled:  node.Annotations["nvidia.com/time-slicing.enabled"] == "true",
	}

	if replicasStr, ok := node.Labels["nvidia.com/time-slicing.replicas"]; ok {
		fmt.Sscanf(replicasStr, "%d", &status.ReplicasPerGPU)
	}

	// Get physical GPU count
	physicalGPUs := node.Status.Capacity["nvidia.com/gpu"]
	status.PhysicalGPUs = int(physicalGPUs.Value())

	// Calculate virtual GPUs
	if status.Enabled && status.ReplicasPerGPU > 0 {
		status.VirtualGPUs = status.PhysicalGPUs * status.ReplicasPerGPU
	} else {
		status.VirtualGPUs = status.PhysicalGPUs
	}

	// Count active workloads using time-sliced GPUs
	podList := &corev1.PodList{}
	if err := t.client.List(ctx, podList, client.MatchingFields{"spec.nodeName": nodeName}); err != nil {
		return nil, err
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Status.Phase == corev1.PodRunning {
			if pod.Annotations["nvidia.com/time-slicing"] == "enabled" {
				status.ActiveWorkloads++
			}
		}
	}

	return status, nil
}

// TimeSlicingStatus represents the current time-slicing status on a node
type TimeSlicingStatus struct {
	NodeName        string
	Enabled         bool
	PhysicalGPUs    int
	VirtualGPUs     int
	ReplicasPerGPU  int
	ActiveWorkloads int
}

// CalculateOptimalReplicas calculates the optimal number of replicas per GPU
// based on workload characteristics
func CalculateOptimalReplicas(avgUtilization float64, burstiness float64) int {
	// Lower utilization allows more replicas
	// Higher burstiness allows more replicas

	if avgUtilization < 20 && burstiness > 0.7 {
		return 8 // Very low utilization, high burstiness
	} else if avgUtilization < 40 && burstiness > 0.5 {
		return 4 // Low utilization, moderate burstiness
	} else if avgUtilization < 60 {
		return 2 // Moderate utilization
	}

	return 1 // High utilization, no time-slicing recommended
}

// ConvertFractionalGPURequest converts fractional GPU requests to time-sliced resources
func ConvertFractionalGPURequest(requestedGPUs float64, replicasPerGPU int) resource.Quantity {
	// With time-slicing, fractional requests are rounded up to nearest integer
	// and the device plugin handles the time-sharing
	virtualGPUs := int(requestedGPUs * float64(replicasPerGPU))
	if virtualGPUs < 1 {
		virtualGPUs = 1
	}
	return *resource.NewQuantity(int64(virtualGPUs), resource.DecimalSI)
}
