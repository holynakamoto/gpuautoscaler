package cost

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CostTracker calculates real-time GPU costs per second
type CostTracker struct {
	clientset     kubernetes.Interface
	pricingClient *PricingClient
	db            *TimescaleDBClient

	// Cache for pod cost calculations
	podCostCache sync.Map // pod name -> *PodCost

	// Metrics
	totalCostGauge    prometheus.Gauge
	hourlyCostGauge   prometheus.Gauge
	podCostGauge      *prometheus.GaugeVec
	savingsGauge      prometheus.Gauge
}

// PodCost represents the cost calculation for a running pod
type PodCost struct {
	PodName       string
	Namespace     string
	Node          string
	GPUType       string
	GPUCount      int
	CapacityType  string // spot, on-demand, reserved
	SharingMode   string // mig, mps, timeslicing, exclusive
	StartTime     time.Time
	HourlyRate    float64 // USD per hour
	TotalCost     float64 // Cumulative cost in USD
	LastUpdated   time.Time

	// Attribution metadata
	Labels        map[string]string
	ExperimentID  string
	Team          string
	Project       string
	CostCenter    string
}

// NewCostTracker creates a new cost tracking instance
func NewCostTracker(clientset kubernetes.Interface, pricingClient *PricingClient, db *TimescaleDBClient) *CostTracker {
	return &CostTracker{
		clientset:     clientset,
		pricingClient: pricingClient,
		db:            db,
		totalCostGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gpu_autoscaler_total_cost_usd",
			Help: "Total accumulated GPU cost in USD",
		}),
		hourlyCostGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gpu_autoscaler_hourly_cost_rate_usd",
			Help: "Current GPU cost rate in USD per hour",
		}),
		podCostGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gpu_autoscaler_pod_cost_usd",
			Help: "Cost per pod in USD",
		}, []string{"namespace", "pod", "gpu_type", "capacity_type"}),
		savingsGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gpu_autoscaler_total_savings_usd",
			Help: "Total cost savings from optimizations in USD",
		}),
	}
}

// Start begins the cost tracking loop
func (ct *CostTracker) Start(ctx context.Context, interval time.Duration) {
	logger := log.FromContext(ctx)
	logger.Info("Starting cost tracker", "interval", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Stopping cost tracker")
			return
		case <-ticker.C:
			if err := ct.updateCosts(ctx); err != nil {
				logger.Error(err, "Failed to update costs")
			}
		}
	}
}

// updateCosts recalculates costs for all GPU pods
func (ct *CostTracker) updateCosts(ctx context.Context) error {
	logger := log.FromContext(ctx)

	// Get all GPU pods across all namespaces
	pods, err := ct.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	var totalHourlyRate float64
	var totalCost float64
	activePods := make(map[string]bool)

	for _, pod := range pods.Items {
		// Check if pod is using GPUs
		if !isGPUPod(&pod) {
			continue
		}

		podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
		activePods[podKey] = true

		// Calculate or update cost for this pod
		podCost, err := ct.calculatePodCost(ctx, &pod)
		if err != nil {
			logger.Error(err, "Failed to calculate pod cost", "pod", podKey)
			continue
		}

		// Update cache
		ct.podCostCache.Store(podKey, podCost)

		// Update metrics
		ct.podCostGauge.WithLabelValues(
			pod.Namespace,
			pod.Name,
			podCost.GPUType,
			podCost.CapacityType,
		).Set(podCost.TotalCost)

		totalHourlyRate += podCost.HourlyRate
		totalCost += podCost.TotalCost

		// Persist to TimescaleDB
		if ct.db != nil {
			if err := ct.db.InsertCostDataPoint(ctx, podCost); err != nil {
				logger.Error(err, "Failed to persist cost data", "pod", podKey)
			}
		}
	}

	// Clean up cache for terminated pods
	ct.podCostCache.Range(func(key, value interface{}) bool {
		podKey := key.(string)
		if !activePods[podKey] {
			ct.podCostCache.Delete(podKey)
			// Remove metrics
			podCost := value.(*PodCost)
			ct.podCostGauge.DeleteLabelValues(
				podCost.Namespace,
				podCost.PodName,
				podCost.GPUType,
				podCost.CapacityType,
			)
		}
		return true
	})

	// Update aggregate metrics
	ct.hourlyCostGauge.Set(totalHourlyRate)
	ct.totalCostGauge.Set(totalCost)

	logger.V(1).Info("Updated costs",
		"activePods", len(activePods),
		"hourlyRate", totalHourlyRate,
		"totalCost", totalCost,
	)

	return nil
}

// calculatePodCost determines the cost for a single pod
func (ct *CostTracker) calculatePodCost(ctx context.Context, pod *corev1.Pod) (*PodCost, error) {
	logger := log.FromContext(ctx)
	podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)

	// Check cache first
	if cached, ok := ct.podCostCache.Load(podKey); ok {
		oldCost := cached.(*PodCost)

		// Copy the struct to avoid data race
		podCost := *oldCost

		// Update cumulative cost based on time elapsed
		elapsed := time.Since(podCost.LastUpdated)
		incrementalCost := podCost.HourlyRate * elapsed.Hours()
		podCost.TotalCost += incrementalCost
		podCost.LastUpdated = time.Now()

		// Store the updated copy back
		ct.podCostCache.Store(podKey, &podCost)

		return &podCost, nil
	}

	// New pod - calculate from scratch
	gpuCount := getGPUCount(pod)
	if gpuCount == 0 {
		return nil, fmt.Errorf("pod has no GPUs")
	}

	// Get node information
	node, err := ct.clientset.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	// Determine GPU type from node labels
	gpuType := getGPUType(node)
	if gpuType == "" {
		gpuType = "unknown"
		logger.V(1).Info("Could not determine GPU type", "node", node.Name)
	}

	// Determine capacity type (spot, on-demand, reserved)
	capacityType := getCapacityType(node)

	// Determine sharing mode
	sharingMode := getSharingMode(pod)

	// Get pricing from cloud provider
	pricing, err := ct.pricingClient.GetGPUPricing(ctx, GPUPricingRequest{
		GPUType:      gpuType,
		CapacityType: capacityType,
		Region:       getRegion(node),
		Zone:         getZone(node),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get pricing: %w", err)
	}

	// Calculate hourly rate
	hourlyRate := pricing.PricePerGPUHour * float64(gpuCount)

	// Apply sharing discount if applicable
	if sharingMode != "exclusive" {
		sharingFactor := getSharingFactor(pod, sharingMode)
		hourlyRate *= sharingFactor
	}

	// Determine pod start time
	startTime := time.Now()
	if pod.Status.StartTime != nil {
		startTime = pod.Status.StartTime.Time
		if startTime.IsZero() {
			startTime = time.Now()
		}
	}

	// Calculate total cost from start to now
	elapsed := time.Since(startTime)
	totalCost := hourlyRate * elapsed.Hours()

	podCost := &PodCost{
		PodName:      pod.Name,
		Namespace:    pod.Namespace,
		Node:         pod.Spec.NodeName,
		GPUType:      gpuType,
		GPUCount:     gpuCount,
		CapacityType: capacityType,
		SharingMode:  sharingMode,
		StartTime:    startTime,
		HourlyRate:   hourlyRate,
		TotalCost:    totalCost,
		LastUpdated:  time.Now(),
		Labels:       pod.Labels,
		ExperimentID: pod.Labels["experiment-id"],
		Team:         pod.Labels["team"],
		Project:      pod.Labels["project"],
		CostCenter:   pod.Labels["cost-center"],
	}

	logger.V(1).Info("Calculated pod cost",
		"pod", podKey,
		"gpuType", gpuType,
		"gpuCount", gpuCount,
		"capacityType", capacityType,
		"sharingMode", sharingMode,
		"hourlyRate", hourlyRate,
		"totalCost", totalCost,
	)

	return podCost, nil
}

// GetPodCost returns the current cost for a specific pod
func (ct *CostTracker) GetPodCost(namespace, name string) (*PodCost, bool) {
	podKey := fmt.Sprintf("%s/%s", namespace, name)
	cached, ok := ct.podCostCache.Load(podKey)
	if !ok {
		return nil, false
	}
	return cached.(*PodCost), true
}

// GetTotalCost returns the total accumulated cost
func (ct *CostTracker) GetTotalCost() float64 {
	var total float64
	ct.podCostCache.Range(func(key, value interface{}) bool {
		podCost := value.(*PodCost)
		total += podCost.TotalCost
		return true
	})
	return total
}

// GetHourlyRate returns the current hourly cost rate
func (ct *CostTracker) GetHourlyRate() float64 {
	var rate float64
	ct.podCostCache.Range(func(key, value interface{}) bool {
		podCost := value.(*PodCost)
		rate += podCost.HourlyRate
		return true
	})
	return rate
}

// GetCostByNamespace returns costs grouped by namespace
func (ct *CostTracker) GetCostByNamespace() map[string]float64 {
	costs := make(map[string]float64)
	ct.podCostCache.Range(func(key, value interface{}) bool {
		podCost := value.(*PodCost)
		costs[podCost.Namespace] += podCost.TotalCost
		return true
	})
	return costs
}

// GetCostByLabel returns costs grouped by label value
func (ct *CostTracker) GetCostByLabel(labelKey string) map[string]float64 {
	costs := make(map[string]float64)
	ct.podCostCache.Range(func(key, value interface{}) bool {
		podCost := value.(*PodCost)
		if labelValue, ok := podCost.Labels[labelKey]; ok {
			costs[labelValue] += podCost.TotalCost
		}
		return true
	})
	return costs
}

// Helper functions

func isGPUPod(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if requests := container.Resources.Requests; requests != nil {
			if _, hasGPU := requests["nvidia.com/gpu"]; hasGPU {
				return true
			}
			if _, hasMIG := requests["nvidia.com/mig-1g.5gb"]; hasMIG {
				return true
			}
			if _, hasMPS := requests["nvidia.com/gpu-shared"]; hasMPS {
				return true
			}
		}
	}
	return false
}

func getGPUCount(pod *corev1.Pod) int {
	count := 0
	for _, container := range pod.Spec.Containers {
		if requests := container.Resources.Requests; requests != nil {
			if gpuQty, hasGPU := requests["nvidia.com/gpu"]; hasGPU {
				count += int(gpuQty.Value())
			}
			// For MIG/MPS, each instance counts as fractional GPU
			if migQty, hasMIG := requests["nvidia.com/mig-1g.5gb"]; hasMIG {
				count += int(migQty.Value())
			}
			if mpsQty, hasMPS := requests["nvidia.com/gpu-shared"]; hasMPS {
				count += int(mpsQty.Value())
			}
		}
	}
	return count
}

func getGPUType(node *corev1.Node) string {
	// Try various label conventions
	if gpuType, ok := node.Labels["nvidia.com/gpu.product"]; ok {
		return gpuType
	}
	if gpuType, ok := node.Labels["accelerator"]; ok {
		return gpuType
	}
	if gpuType, ok := node.Labels["node.kubernetes.io/instance-type"]; ok {
		return gpuType
	}
	return "unknown"
}

func getCapacityType(node *corev1.Node) string {
	// Check for spot/preemptible labels
	if spot, ok := node.Labels["karpenter.sh/capacity-type"]; ok {
		return spot
	}
	if spot, ok := node.Labels["cloud.google.com/gke-preemptible"]; ok && spot == "true" {
		return "spot"
	}
	if spot, ok := node.Labels["kubernetes.azure.com/scalesetpriority"]; ok && spot == "spot" {
		return "spot"
	}
	if node.Labels["node-lifecycle"] == "spot" {
		return "spot"
	}
	return "on-demand"
}

func getSharingMode(pod *corev1.Pod) string {
	if mode, ok := pod.Annotations["gpu-autoscaler.io/sharing-mode"]; ok {
		return mode
	}

	// Detect from resource requests
	for _, container := range pod.Spec.Containers {
		if requests := container.Resources.Requests; requests != nil {
			if _, hasMIG := requests["nvidia.com/mig-1g.5gb"]; hasMIG {
				return "mig"
			}
			if _, hasMPS := requests["nvidia.com/gpu-shared"]; hasMPS {
				return "mps"
			}
		}
	}

	// Check for time-slicing annotation
	if _, ok := pod.Annotations["gpu-autoscaler.io/timeslicing"]; ok {
		return "timeslicing"
	}

	return "exclusive"
}

func getSharingFactor(pod *corev1.Pod, mode string) float64 {
	switch mode {
	case "mig":
		// MIG provides isolated slices - charge full fraction
		return 1.0
	case "mps":
		// MPS shares GPU - divide cost by number of clients
		if clients, ok := pod.Annotations["gpu-autoscaler.io/mps-clients"]; ok {
			// Parse client count and divide
			// Simplified: assume 4 clients average
			return 0.25
		}
		return 0.25
	case "timeslicing":
		// Time-slicing - divide by replica count
		if replicas, ok := pod.Annotations["gpu-autoscaler.io/timeslice-replicas"]; ok {
			// Parse replicas and divide
			// Simplified: assume 4 replicas average
			return 0.25
		}
		return 0.25
	default:
		return 1.0
	}
}

func getRegion(node *corev1.Node) string {
	if region, ok := node.Labels["topology.kubernetes.io/region"]; ok {
		return region
	}
	if region, ok := node.Labels["failure-domain.beta.kubernetes.io/region"]; ok {
		return region
	}
	return "us-east-1" // default
}

func getZone(node *corev1.Node) string {
	if zone, ok := node.Labels["topology.kubernetes.io/zone"]; ok {
		return zone
	}
	if zone, ok := node.Labels["failure-domain.beta.kubernetes.io/zone"]; ok {
		return zone
	}
	return ""
}

// RegisterMetrics registers Prometheus metrics
func (ct *CostTracker) RegisterMetrics(registry prometheus.Registerer) {
	registry.MustRegister(ct.totalCostGauge)
	registry.MustRegister(ct.hourlyCostGauge)
	registry.MustRegister(ct.podCostGauge)
	registry.MustRegister(ct.savingsGauge)
}
