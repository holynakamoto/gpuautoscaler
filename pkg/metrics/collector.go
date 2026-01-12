package metrics

import (
	"context"
	"fmt"
	"time"

	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GPUMetrics represents GPU utilization metrics for a pod
type GPUMetrics struct {
	PodName           string
	PodNamespace      string
	NodeName          string
	GPUIndex          int
	GPUUtilization    float64 // Percentage (0-100)
	GPUMemoryUsed     float64 // MB
	GPUMemoryTotal    float64 // MB
	GPUMemoryUtil     float64 // Percentage (0-100)
	GPUPowerUsage     float64 // Watts
	GPUTemperature    float64 // Celsius
	Timestamp         time.Time
}

// WasteMetrics represents waste analysis for underutilized GPUs
type WasteMetrics struct {
	PodName           string
	PodNamespace      string
	NodeName          string
	GPUIndex          int
	AllocatedGPUs     int
	AvgUtilization    float64
	AvgMemoryUtil     float64
	WasteScore        float64 // 0-100, higher means more waste
	Recommendation    string
	EstimatedMonthlyCost float64
}

// Collector collects GPU metrics from Prometheus and enriches with Kubernetes metadata
type Collector struct {
	promURL    string
	promClient promv1.API
	k8sClient  client.Client
}

// NewCollector creates a new metrics collector
func NewCollector(prometheusURL string) *Collector {
	return &Collector{
		promURL: prometheusURL,
	}
}

// Start initializes the collector with Kubernetes client
func (c *Collector) Start(k8sClient client.Client) error {
	c.k8sClient = k8sClient

	// Initialize Prometheus client
	promClient, err := promapi.NewClient(promapi.Config{
		Address: c.promURL,
	})
	if err != nil {
		return fmt.Errorf("failed to create Prometheus client: %w", err)
	}
	c.promClient = promv1.NewAPI(promClient)

	klog.Infof("Metrics collector started with Prometheus URL: %s", c.promURL)
	return nil
}

// GetGPUMetrics retrieves GPU metrics for all pods with GPU allocations
func (c *Collector) GetGPUMetrics(ctx context.Context) ([]GPUMetrics, error) {
	// Query Prometheus for DCGM GPU utilization metrics
	// DCGM_FI_DEV_GPU_UTIL gives us GPU utilization percentage
	query := `DCGM_FI_DEV_GPU_UTIL`

	result, warnings, err := c.promClient.Query(ctx, query, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to query Prometheus: %w", err)
	}
	if len(warnings) > 0 {
		klog.Warningf("Prometheus query warnings: %v", warnings)
	}

	metrics := []GPUMetrics{}

	if result.Type() == model.ValVector {
		vector := result.(model.Vector)
		for _, sample := range vector {
			metric := GPUMetrics{
				NodeName:       string(sample.Metric["kubernetes_node"]),
				GPUIndex:       parseGPUIndex(sample.Metric),
				GPUUtilization: float64(sample.Value),
				Timestamp:      time.Now(),
			}

			// Get pod information from node and GPU index
			podInfo, err := c.getPodForGPU(ctx, metric.NodeName, metric.GPUIndex)
			if err != nil {
				klog.Warningf("Failed to get pod info for GPU %d on node %s: %v", metric.GPUIndex, metric.NodeName, err)
				continue
			}
			metric.PodName = podInfo.Name
			metric.PodNamespace = podInfo.Namespace

			// Get additional GPU metrics (memory, power, temperature)
			if err := c.enrichGPUMetrics(ctx, &metric); err != nil {
				klog.Warningf("Failed to enrich metrics for pod %s/%s: %v", metric.PodNamespace, metric.PodName, err)
			}

			metrics = append(metrics, metric)
		}
	}

	return metrics, nil
}

// GetWasteMetrics analyzes GPU metrics to identify waste and optimization opportunities
func (c *Collector) GetWasteMetrics(ctx context.Context, lookbackMinutes int) ([]WasteMetrics, error) {
	// Query average GPU utilization over the lookback period
	query := fmt.Sprintf(`avg_over_time(DCGM_FI_DEV_GPU_UTIL[%dm])`, lookbackMinutes)

	result, warnings, err := c.promClient.Query(ctx, query, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to query Prometheus: %w", err)
	}
	if len(warnings) > 0 {
		klog.Warningf("Prometheus query warnings: %v", warnings)
	}

	wasteMetrics := []WasteMetrics{}

	if result.Type() == model.ValVector {
		vector := result.(model.Vector)
		for _, sample := range vector {
			avgUtil := float64(sample.Value)

			// Get memory utilization
			memUtil, err := c.getAvgMemoryUtil(ctx, sample.Metric, lookbackMinutes)
			if err != nil {
				klog.Warningf("Failed to get memory utilization: %v", err)
				memUtil = 0
			}

			waste := WasteMetrics{
				NodeName:       string(sample.Metric["kubernetes_node"]),
				GPUIndex:       parseGPUIndex(sample.Metric),
				AvgUtilization: avgUtil,
				AvgMemoryUtil:  memUtil,
			}

			// Calculate waste score (higher = more waste)
			// Waste when GPU util < 50% OR memory util < 40%
			waste.WasteScore = calculateWasteScore(avgUtil, memUtil)

			// Generate recommendations
			waste.Recommendation = generateRecommendation(avgUtil, memUtil)

			// Get pod information
			podInfo, err := c.getPodForGPU(ctx, waste.NodeName, waste.GPUIndex)
			if err != nil {
				klog.Warningf("Failed to get pod info: %v", err)
				continue
			}
			waste.PodName = podInfo.Name
			waste.PodNamespace = podInfo.Namespace
			waste.AllocatedGPUs = getGPUCount(podInfo)

			// Estimate monthly cost (assuming $2/hour per GPU average)
			waste.EstimatedMonthlyCost = float64(waste.AllocatedGPUs) * 2.0 * 24 * 30

			wasteMetrics = append(wasteMetrics, waste)
		}
	}

	return wasteMetrics, nil
}

// enrichGPUMetrics adds memory, power, and temperature metrics
func (c *Collector) enrichGPUMetrics(ctx context.Context, metric *GPUMetrics) error {
	// Query GPU memory used
	memQuery := fmt.Sprintf(`DCGM_FI_DEV_FB_USED{kubernetes_node="%s",gpu="%d"}`, metric.NodeName, metric.GPUIndex)
	memResult, _, err := c.promClient.Query(ctx, memQuery, time.Now())
	if err == nil && memResult.Type() == model.ValVector {
		vector := memResult.(model.Vector)
		if len(vector) > 0 {
			metric.GPUMemoryUsed = float64(vector[0].Value)
		}
	}

	// Query GPU memory total
	memTotalQuery := fmt.Sprintf(`DCGM_FI_DEV_FB_TOTAL{kubernetes_node="%s",gpu="%d"}`, metric.NodeName, metric.GPUIndex)
	memTotalResult, _, err := c.promClient.Query(ctx, memTotalQuery, time.Now())
	if err == nil && memTotalResult.Type() == model.ValVector {
		vector := memTotalResult.(model.Vector)
		if len(vector) > 0 {
			metric.GPUMemoryTotal = float64(vector[0].Value)
			if metric.GPUMemoryTotal > 0 {
				metric.GPUMemoryUtil = (metric.GPUMemoryUsed / metric.GPUMemoryTotal) * 100
			}
		}
	}

	// Query GPU power usage
	powerQuery := fmt.Sprintf(`DCGM_FI_DEV_POWER_USAGE{kubernetes_node="%s",gpu="%d"}`, metric.NodeName, metric.GPUIndex)
	powerResult, _, err := c.promClient.Query(ctx, powerQuery, time.Now())
	if err == nil && powerResult.Type() == model.ValVector {
		vector := powerResult.(model.Vector)
		if len(vector) > 0 {
			metric.GPUPowerUsage = float64(vector[0].Value)
		}
	}

	// Query GPU temperature
	tempQuery := fmt.Sprintf(`DCGM_FI_DEV_GPU_TEMP{kubernetes_node="%s",gpu="%d"}`, metric.NodeName, metric.GPUIndex)
	tempResult, _, err := c.promClient.Query(ctx, tempQuery, time.Now())
	if err == nil && tempResult.Type() == model.ValVector {
		vector := tempResult.(model.Vector)
		if len(vector) > 0 {
			metric.GPUTemperature = float64(vector[0].Value)
		}
	}

	return nil
}

// getAvgMemoryUtil gets average memory utilization over the lookback period
func (c *Collector) getAvgMemoryUtil(ctx context.Context, labels model.Metric, lookbackMinutes int) (float64, error) {
	query := fmt.Sprintf(`avg_over_time((DCGM_FI_DEV_FB_USED / DCGM_FI_DEV_FB_TOTAL * 100){kubernetes_node="%s",gpu="%s"}[%dm])`,
		labels["kubernetes_node"], labels["gpu"], lookbackMinutes)

	result, _, err := c.promClient.Query(ctx, query, time.Now())
	if err != nil {
		return 0, err
	}

	if result.Type() == model.ValVector {
		vector := result.(model.Vector)
		if len(vector) > 0 {
			return float64(vector[0].Value), nil
		}
	}

	return 0, nil
}

// getPodForGPU retrieves the pod that is using a specific GPU on a node
func (c *Collector) getPodForGPU(ctx context.Context, nodeName string, gpuIndex int) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := c.k8sClient.List(ctx, podList, client.MatchingFields{"spec.nodeName": nodeName}); err != nil {
		return nil, fmt.Errorf("failed to list pods on node %s: %w", nodeName, err)
	}

	// Find pod with GPU allocation
	for _, pod := range podList.Items {
		if hasGPUAllocation(&pod) {
			// For now, return the first pod with GPU allocation
			// In a real implementation, we'd need to track which GPU is assigned to which pod
			// This requires integration with the device plugin or node-level tracking
			return &pod, nil
		}
	}

	return nil, fmt.Errorf("no pod found using GPU %d on node %s", gpuIndex, nodeName)
}

// hasGPUAllocation checks if a pod has GPU resources allocated
func hasGPUAllocation(pod *corev1.Pod) bool {
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

// getGPUCount returns the number of GPUs allocated to a pod
func getGPUCount(pod *corev1.Pod) int {
	count := 0
	for _, container := range pod.Spec.Containers {
		if gpus, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
			count += int(gpus.Value())
		}
	}
	return count
}

// parseGPUIndex extracts the GPU index from Prometheus labels
func parseGPUIndex(labels model.Metric) int {
	if gpu, ok := labels["gpu"]; ok {
		// Parse GPU index from label
		var index int
		fmt.Sscanf(string(gpu), "%d", &index)
		return index
	}
	return 0
}

// calculateWasteScore calculates a waste score from 0-100
func calculateWasteScore(gpuUtil, memUtil float64) float64 {
	// Higher score = more waste
	// Full waste (score 100) when both GPU and memory are at 0%
	// No waste (score 0) when both are at 100%

	gpuWaste := 100 - gpuUtil
	memWaste := 100 - memUtil

	// Weight GPU utilization slightly more than memory
	score := (gpuWaste * 0.6) + (memWaste * 0.4)

	return score
}

// generateRecommendation generates an optimization recommendation based on utilization
func generateRecommendation(gpuUtil, memUtil float64) string {
	if gpuUtil < 30 && memUtil < 30 {
		return "Consider sharing this GPU via MIG or MPS - very low utilization"
	} else if gpuUtil < 50 && memUtil < 40 {
		return "This workload could share a GPU via MIG (hardware partitioning)"
	} else if gpuUtil < 50 && memUtil >= 40 {
		return "Consider MPS (multi-process service) for this workload"
	} else if gpuUtil >= 50 && memUtil < 40 {
		return "Memory-light workload - consider time-slicing"
	} else {
		return "Utilization is acceptable - no optimization needed"
	}
}

// GetNodeWithPod retrieves a pod by namespaced name
func (c *Collector) GetNodeWithPod(ctx context.Context, namespacedName types.NamespacedName) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	if err := c.k8sClient.Get(ctx, namespacedName, pod); err != nil {
		return nil, err
	}
	return pod, nil
}
