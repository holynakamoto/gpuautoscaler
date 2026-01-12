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

// MPSManager handles NVIDIA MPS (Multi-Process Service) configuration
// MPS allows multiple CUDA processes to share a single GPU with improved isolation
type MPSManager struct {
	client client.Client
}

// MPSConfig represents MPS configuration for a node or workload
type MPSConfig struct {
	Enabled              bool
	MaxClients           int     // Maximum number of concurrent MPS clients
	DefaultActiveThreads int     // Default active thread percentage per client
	MemoryLimit          int64   // Memory limit per client in bytes
	ComputePercentage    float64 // Compute percentage allocation per client
}

// DefaultMPSConfig returns default MPS configuration
func DefaultMPSConfig() MPSConfig {
	return MPSConfig{
		Enabled:              true,
		MaxClients:           16, // NVIDIA default
		DefaultActiveThreads: 100,
		MemoryLimit:          0, // No limit by default
		ComputePercentage:    0, // No limit by default
	}
}

// NewMPSManager creates a new MPS manager
func NewMPSManager(client client.Client) *MPSManager {
	return &MPSManager{
		client: client,
	}
}

// IsMPSCapable checks if a node supports MPS
func (m *MPSManager) IsMPSCapable(ctx context.Context, nodeName string) (bool, error) {
	node := &corev1.Node{}
	if err := m.client.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
		return false, err
	}

	// Check if node has MPS capability label
	capable, ok := node.Labels["nvidia.com/mps.capable"]
	if !ok {
		return false, nil
	}

	return capable == "true", nil
}

// EnableMPSOnNode enables MPS on a specific node
func (m *MPSManager) EnableMPSOnNode(ctx context.Context, nodeName string, config MPSConfig) error {
	log := log.FromContext(ctx)
	log.Info("Enabling MPS on node",
		"node", nodeName,
		"maxClients", config.MaxClients)

	node := &corev1.Node{}
	if err := m.client.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	// Add MPS configuration annotations
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations["nvidia.com/mps.enabled"] = "true"
	node.Annotations["nvidia.com/mps.max-clients"] = fmt.Sprintf("%d", config.MaxClients)
	node.Annotations["nvidia.com/mps.active-threads"] = fmt.Sprintf("%d", config.DefaultActiveThreads)

	if config.MemoryLimit > 0 {
		node.Annotations["nvidia.com/mps.memory-limit"] = fmt.Sprintf("%d", config.MemoryLimit)
	}

	// Update node labels
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	node.Labels["nvidia.com/mps.enabled"] = "true"

	if err := m.client.Update(ctx, node); err != nil {
		return fmt.Errorf("failed to update node: %w", err)
	}

	log.Info("MPS enabled successfully on node", "node", nodeName)

	return nil
}

// ConvertPodToMPS converts a pod to use MPS for GPU sharing
func (m *MPSManager) ConvertPodToMPS(ctx context.Context, pod *corev1.Pod, config MPSConfig) error {
	log := log.FromContext(ctx)
	log.Info("Converting pod to MPS",
		"pod", pod.Name,
		"namespace", pod.Namespace)

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
	pod.Annotations["gpu-autoscaler.io/mps-enabled"] = "true"
	pod.Annotations["nvidia.com/mps"] = "enabled"

	// Set MPS configuration
	if config.DefaultActiveThreads > 0 {
		pod.Annotations["nvidia.com/mps.active-threads"] = fmt.Sprintf("%d", config.DefaultActiveThreads)
	}
	if config.MemoryLimit > 0 {
		pod.Annotations["nvidia.com/mps.memory-limit"] = fmt.Sprintf("%d", config.MemoryLimit)
	}

	// Update GPU resource requests to use fractional sharing
	// MPS allows multiple pods to share the same GPU
	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		if gpuReq, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
			// Keep the GPU request but mark it for MPS sharing
			// The actual sharing is handled by MPS daemon
			container.Resources.Requests["nvidia.com/gpu"] = gpuReq
			container.Resources.Limits["nvidia.com/gpu"] = gpuReq

			// Add MPS-specific resource request if using fractional GPUs
			if gpuReq.Value() < 1 {
				container.Resources.Requests["nvidia.com/gpu.shared"] = resource.MustParse("1")
				container.Resources.Limits["nvidia.com/gpu.shared"] = resource.MustParse("1")
			}
		}
	}

	// Add node selector for MPS-enabled nodes
	if pod.Spec.NodeSelector == nil {
		pod.Spec.NodeSelector = make(map[string]string)
	}
	pod.Spec.NodeSelector["nvidia.com/mps.enabled"] = "true"

	log.Info("Pod converted to MPS successfully",
		"pod", pod.Name,
		"namespace", pod.Namespace)

	return nil
}

// IsWorkloadSuitableForMPS determines if a workload is suitable for MPS
// MPS is ideal for inference workloads with low GPU utilization
func (m *MPSManager) IsWorkloadSuitableForMPS(ctx context.Context, pod *corev1.Pod, avgUtilization float64) bool {
	// MPS is suitable for:
	// 1. Inference workloads (not training)
	// 2. Low GPU utilization (<50%)
	// 3. Multiple concurrent processes
	// 4. CUDA applications

	// Check if workload is marked as inference
	workloadType, ok := pod.Labels["gpu-autoscaler.io/workload-type"]
	if ok && workloadType == "training" {
		return false // Training workloads typically need exclusive GPU access
	}

	// Check if utilization is low
	if avgUtilization > 50.0 {
		return false // High utilization workloads don't benefit from MPS
	}

	// Check if MPS is explicitly disabled
	if pod.Annotations["gpu-autoscaler.io/mps-enabled"] == "false" {
		return false
	}

	return true
}

// EstimateMPSSavings estimates cost savings from using MPS
func (m *MPSManager) EstimateMPSSavings(ctx context.Context) (*MPSSavingsReport, error) {
	log := log.FromContext(ctx)
	log.Info("Estimating MPS savings")

	podList := &corev1.PodList{}
	if err := m.client.List(ctx, podList); err != nil {
		return nil, err
	}

	report := &MPSSavingsReport{
		TotalPods:           0,
		MPSEligiblePods:     0,
		PotentialSavedGPUs:  0,
		EstimatedSavingsPct: 0,
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

		// Check if workload is suitable for MPS
		// Heuristic: Inference workloads or pods labeled for sharing
		workloadType := pod.Labels["gpu-autoscaler.io/workload-type"]
		sharingEnabled := pod.Annotations["gpu-autoscaler.io/sharing"] == "enabled"

		if workloadType == "inference" || sharingEnabled {
			report.MPSEligiblePods++
			// With MPS, multiple inference workloads can share GPUs
			// Assume 4-8 small inference workloads can share 1 GPU
			report.PotentialSavedGPUs += 0.75 // 75% savings per eligible pod
		}
	}

	if report.TotalPods > 0 {
		report.EstimatedSavingsPct = (report.PotentialSavedGPUs / float64(report.TotalPods)) * 100
	}

	return report, nil
}

// MPSSavingsReport contains MPS cost savings analysis
type MPSSavingsReport struct {
	TotalPods           int
	MPSEligiblePods     int
	PotentialSavedGPUs  float64
	EstimatedSavingsPct float64
	Timestamp           metav1.Time
}

// GetMPSStatus returns the current MPS status for a node
func (m *MPSManager) GetMPSStatus(ctx context.Context, nodeName string) (*MPSStatus, error) {
	node := &corev1.Node{}
	if err := m.client.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
		return nil, err
	}

	status := &MPSStatus{
		NodeName:   nodeName,
		Enabled:    node.Annotations["nvidia.com/mps.enabled"] == "true",
		MaxClients: 0,
	}

	if maxClientsStr, ok := node.Annotations["nvidia.com/mps.max-clients"]; ok {
		fmt.Sscanf(maxClientsStr, "%d", &status.MaxClients)
	}

	// Count active MPS clients (pods using MPS on this node)
	podList := &corev1.PodList{}
	if err := m.client.List(ctx, podList, client.MatchingFields{"spec.nodeName": nodeName}); err != nil {
		return nil, err
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Status.Phase == corev1.PodRunning {
			if pod.Annotations["nvidia.com/mps"] == "enabled" {
				status.ActiveClients++
			}
		}
	}

	return status, nil
}

// MPSStatus represents the current MPS status on a node
type MPSStatus struct {
	NodeName      string
	Enabled       bool
	MaxClients    int
	ActiveClients int
}
