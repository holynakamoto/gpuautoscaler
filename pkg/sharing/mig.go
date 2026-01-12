package sharing

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// MIGProfile represents an NVIDIA MIG (Multi-Instance GPU) profile
type MIGProfile struct {
	Name        string
	SliceCount  int    // Number of GPU slices
	Memory      int64  // Memory in MB
	Compute     int    // Compute units
	DeviceID    string // e.g., "1g.5gb", "2g.10gb", "3g.20gb", "4g.20gb", "7g.40gb"
	Description string
}

// MIGManager handles NVIDIA MIG configuration and management
type MIGManager struct {
	client client.Client
}

// Common MIG profiles for A100 (40GB and 80GB variants)
var (
	// A100-40GB profiles
	MIGProfile1g5gb = MIGProfile{
		Name:        "1g.5gb",
		SliceCount:  1,
		Memory:      5120,
		Compute:     1,
		DeviceID:    "1g.5gb",
		Description: "1/7th GPU, 5GB memory",
	}
	MIGProfile2g10gb = MIGProfile{
		Name:        "2g.10gb",
		SliceCount:  2,
		Memory:      10240,
		Compute:     2,
		DeviceID:    "2g.10gb",
		Description: "2/7ths GPU, 10GB memory",
	}
	MIGProfile3g20gb = MIGProfile{
		Name:        "3g.20gb",
		SliceCount:  3,
		Memory:      20480,
		Compute:     3,
		DeviceID:    "3g.20gb",
		Description: "3/7ths GPU, 20GB memory",
	}
	MIGProfile4g20gb = MIGProfile{
		Name:        "4g.20gb",
		SliceCount:  4,
		Memory:      20480,
		Compute:     4,
		DeviceID:    "4g.20gb",
		Description: "4/7ths GPU, 20GB memory",
	}
	MIGProfile7g40gb = MIGProfile{
		Name:        "7g.40gb",
		SliceCount:  7,
		Memory:      40960,
		Compute:     7,
		DeviceID:    "7g.40gb",
		Description: "Full GPU, 40GB memory",
	}

	// A100-80GB profiles
	MIGProfile1g10gb = MIGProfile{
		Name:        "1g.10gb",
		SliceCount:  1,
		Memory:      10240,
		Compute:     1,
		DeviceID:    "1g.10gb",
		Description: "1/7th GPU, 10GB memory",
	}
	MIGProfile2g20gb = MIGProfile{
		Name:        "2g.20gb",
		SliceCount:  2,
		Memory:      20480,
		Compute:     2,
		DeviceID:    "2g.20gb",
		Description: "2/7ths GPU, 20GB memory",
	}
	MIGProfile3g40gb = MIGProfile{
		Name:        "3g.40gb",
		SliceCount:  3,
		Memory:      40960,
		Compute:     3,
		DeviceID:    "3g.40gb",
		Description: "3/7ths GPU, 40GB memory",
	}
	MIGProfile4g40gb = MIGProfile{
		Name:        "4g.40gb",
		SliceCount:  4,
		Memory:      40960,
		Compute:     4,
		DeviceID:    "4g.40gb",
		Description: "4/7ths GPU, 40GB memory",
	}
	MIGProfile7g80gb = MIGProfile{
		Name:        "7g.80gb",
		SliceCount:  7,
		Memory:      81920,
		Compute:     7,
		DeviceID:    "7g.80gb",
		Description: "Full GPU, 80GB memory",
	}
)

// GetSupportedProfiles returns all supported MIG profiles
func GetSupportedProfiles() []MIGProfile {
	return []MIGProfile{
		MIGProfile1g5gb,
		MIGProfile2g10gb,
		MIGProfile3g20gb,
		MIGProfile4g20gb,
		MIGProfile7g40gb,
		MIGProfile1g10gb,
		MIGProfile2g20gb,
		MIGProfile3g40gb,
		MIGProfile4g40gb,
		MIGProfile7g80gb,
	}
}

// NewMIGManager creates a new MIG manager
func NewMIGManager(client client.Client) *MIGManager {
	return &MIGManager{
		client: client,
	}
}

// IsMIGCapable checks if a node supports MIG
func (m *MIGManager) IsMIGCapable(ctx context.Context, nodeName string) (bool, error) {
	node := &corev1.Node{}
	if err := m.client.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
		return false, err
	}

	// Check if node has MIG capability label
	capable, ok := node.Labels["nvidia.com/mig.capable"]
	if !ok {
		return false, nil
	}

	return capable == "true", nil
}

// GetMIGProfile selects appropriate MIG profile based on workload requirements
func (m *MIGManager) GetMIGProfile(gpuRequest int, memoryRequest int64) (*MIGProfile, error) {
	memoryMB := memoryRequest / (1024 * 1024)

	// Select smallest profile that satisfies requirements
	profiles := GetSupportedProfiles()
	for _, profile := range profiles {
		if profile.Compute >= gpuRequest && profile.Memory >= memoryMB {
			return &profile, nil
		}
	}

	return nil, fmt.Errorf("no suitable MIG profile found for GPU=%d, Memory=%dMB", gpuRequest, memoryMB)
}

// ApplyMIGConfiguration applies MIG configuration to a node
func (m *MIGManager) ApplyMIGConfiguration(ctx context.Context, nodeName string, profile MIGProfile) error {
	log := log.FromContext(ctx)
	log.Info("Applying MIG configuration",
		"node", nodeName,
		"profile", profile.Name)

	node := &corev1.Node{}
	if err := m.client.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	// Add MIG profile annotation
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations["nvidia.com/mig.config"] = profile.DeviceID
	node.Annotations["nvidia.com/mig.config.state"] = "pending"

	// Update node labels for device plugin discovery
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	node.Labels["nvidia.com/mig.config"] = strings.ReplaceAll(profile.DeviceID, ".", "-")

	if err := m.client.Update(ctx, node); err != nil {
		return fmt.Errorf("failed to update node: %w", err)
	}

	log.Info("MIG configuration applied successfully",
		"node", nodeName,
		"profile", profile.Name)

	return nil
}

// GetMIGDeviceResourceName returns the resource name for MIG device
func GetMIGDeviceResourceName(profile MIGProfile) string {
	// NVIDIA device plugin exposes MIG devices as nvidia.com/mig-<profile>
	// e.g., nvidia.com/mig-1g.5gb
	return fmt.Sprintf("nvidia.com/mig-%s", profile.DeviceID)
}

// ConvertPodToMIG converts a pod's GPU request to use MIG profile
func (m *MIGManager) ConvertPodToMIG(ctx context.Context, pod *corev1.Pod, profile MIGProfile) error {
	log := log.FromContext(ctx)
	log.Info("Converting pod to MIG",
		"pod", pod.Name,
		"profile", profile.Name)

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
	pod.Annotations["gpu-autoscaler.io/mig-profile"] = profile.Name

	// Replace nvidia.com/gpu with MIG device resource
	migResourceName := GetMIGDeviceResourceName(profile)
	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		if _, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
			// Remove standard GPU request
			delete(container.Resources.Requests, "nvidia.com/gpu")
			delete(container.Resources.Limits, "nvidia.com/gpu")

			// Add MIG device request
			container.Resources.Requests[corev1.ResourceName(migResourceName)] = resource.MustParse("1")
			container.Resources.Limits[corev1.ResourceName(migResourceName)] = resource.MustParse("1")
		}
	}

	log.Info("Pod converted to MIG successfully",
		"pod", pod.Name,
		"migResource", migResourceName)

	return nil
}

// EstimateMIGSavings estimates cost savings from using MIG
func (m *MIGManager) EstimateMIGSavings(ctx context.Context) (*MIGSavingsReport, error) {
	log := log.FromContext(ctx)
	log.Info("Estimating MIG savings")

	podList := &corev1.PodList{}
	if err := m.client.List(ctx, podList); err != nil {
		return nil, err
	}

	report := &MIGSavingsReport{
		TotalPods:           0,
		MIGEligiblePods:     0,
		PotentialSavedGPUs:  0,
		EstimatedSavingsPct: 0,
	}

	for i := range podList.Items {
		pod := &podList.Items[i]

		// Check if pod is using GPUs
		gpuRequest := 0
		memoryRequest := int64(0)
		for _, container := range pod.Spec.Containers {
			if gpuReq, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
				gpuRequest += int(gpuReq.Value())
				report.TotalPods++
			}
			if memReq, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
				memoryRequest += memReq.Value()
			}
		}

		if gpuRequest == 0 {
			continue
		}

		// Check if workload is eligible for MIG (small workloads)
		if gpuRequest == 1 && memoryRequest < 20*1024*1024*1024 { // Less than 20GB
			report.MIGEligiblePods++
			// Each pod using MIG profile instead of full GPU saves GPU capacity
			// For example, 7x 1g.5gb profiles can share 1 physical GPU
			report.PotentialSavedGPUs += 0.857 // (7-1)/7 = 6/7 savings per pod
		}
	}

	if report.TotalPods > 0 {
		report.EstimatedSavingsPct = (report.PotentialSavedGPUs / float64(report.TotalPods)) * 100
	}

	return report, nil
}

// MIGSavingsReport contains MIG cost savings analysis
type MIGSavingsReport struct {
	TotalPods           int
	MIGEligiblePods     int
	PotentialSavedGPUs  float64
	EstimatedSavingsPct float64
	Timestamp           metav1.Time
}
