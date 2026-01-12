package scheduler

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// GPUNode represents a node with GPU resources and current allocation
type GPUNode struct {
	Name              string
	TotalGPUs         int
	AvailableGPUs     int
	GPUType           string
	AllocatedPods     []*corev1.Pod
	UtilizationScore  float64
	MemoryUtilization float64
	SupportsMIG       bool
	SupportsMPS       bool
	SupportsTimeSlice bool
}

// GPUWorkload represents a workload requesting GPU resources
type GPUWorkload struct {
	Pod            *corev1.Pod
	GPURequest     int
	MemoryRequest  int64
	Priority       int32
	SharingEnabled bool
	PreferredMode  string // "mig", "mps", "timeslice", "exclusive"
}

// BinPackingScheduler implements intelligent GPU workload packing
type BinPackingScheduler struct {
	client       client.Client
	packStrategy PackStrategy
}

// PackStrategy defines the bin-packing strategy
type PackStrategy string

const (
	// BestFit packs workloads into the most utilized node that can fit them
	BestFit PackStrategy = "bestfit"
	// FirstFit packs workloads into the first node that can fit them
	FirstFit PackStrategy = "firstfit"
	// WorstFit packs workloads into the least utilized node
	WorstFit PackStrategy = "worstfit"
)

// NewBinPackingScheduler creates a new bin-packing scheduler
func NewBinPackingScheduler(client client.Client, strategy PackStrategy) *BinPackingScheduler {
	return &BinPackingScheduler{
		client:       client,
		packStrategy: strategy,
	}
}

// PackWorkloads applies bin-packing algorithm to consolidate GPU workloads
func (s *BinPackingScheduler) PackWorkloads(ctx context.Context) (*PackingResult, error) {
	log := log.FromContext(ctx)
	log.Info("Starting bin-packing algorithm")

	// Get all GPU nodes
	nodes, err := s.getGPUNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get GPU nodes: %w", err)
	}

	// Get all pending GPU workloads
	workloads, err := s.getPendingGPUWorkloads(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending workloads: %w", err)
	}

	// Sort workloads by priority (descending) and GPU request (descending)
	sort.Slice(workloads, func(i, j int) bool {
		if workloads[i].Priority != workloads[j].Priority {
			return workloads[i].Priority > workloads[j].Priority
		}
		return workloads[i].GPURequest > workloads[j].GPURequest
	})

	// Apply packing strategy
	result := &PackingResult{
		Placements:       make(map[string]string),
		ConsolidatedPods: 0,
		SavedGPUs:        0,
	}

	for _, workload := range workloads {
		node := s.selectNode(nodes, workload)
		if node != nil {
			result.Placements[workload.Pod.Name] = node.Name
			node.AvailableGPUs -= workload.GPURequest
			node.AllocatedPods = append(node.AllocatedPods, workload.Pod)
			result.ConsolidatedPods++
			log.Info("Packed workload",
				"pod", workload.Pod.Name,
				"node", node.Name,
				"gpus", workload.GPURequest,
				"strategy", s.packStrategy)
		}
	}

	return result, nil
}

// selectNode selects the best node for a workload based on packing strategy
func (s *BinPackingScheduler) selectNode(nodes []*GPUNode, workload *GPUWorkload) *GPUNode {
	var candidates []*GPUNode

	// Filter nodes that can fit the workload
	for _, node := range nodes {
		if node.AvailableGPUs >= workload.GPURequest {
			candidates = append(candidates, node)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	switch s.packStrategy {
	case BestFit:
		return s.bestFitNode(candidates, workload)
	case FirstFit:
		return candidates[0]
	case WorstFit:
		return s.worstFitNode(candidates)
	default:
		return s.bestFitNode(candidates, workload)
	}
}

// bestFitNode returns the node with highest utilization that can fit the workload
func (s *BinPackingScheduler) bestFitNode(nodes []*GPUNode, workload *GPUWorkload) *GPUNode {
	var bestNode *GPUNode
	minWaste := float64(999999)

	for _, node := range nodes {
		waste := float64(node.AvailableGPUs - workload.GPURequest)
		if waste < minWaste {
			minWaste = waste
			bestNode = node
		}
	}

	return bestNode
}

// worstFitNode returns the node with lowest utilization
func (s *BinPackingScheduler) worstFitNode(nodes []*GPUNode) *GPUNode {
	var worstNode *GPUNode
	maxAvailable := 0

	for _, node := range nodes {
		if node.AvailableGPUs > maxAvailable {
			maxAvailable = node.AvailableGPUs
			worstNode = node
		}
	}

	return worstNode
}

// getGPUNodes retrieves all nodes with GPU resources
func (s *BinPackingScheduler) getGPUNodes(ctx context.Context) ([]*GPUNode, error) {
	nodeList := &corev1.NodeList{}
	if err := s.client.List(ctx, nodeList); err != nil {
		return nil, err
	}

	var gpuNodes []*GPUNode
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		gpuCapacity, hasGPU := node.Status.Capacity["nvidia.com/gpu"]
		if !hasGPU {
			continue
		}

		totalGPUs := int(gpuCapacity.Value())

		// Get allocated GPUs from running pods
		podList := &corev1.PodList{}
		if err := s.client.List(ctx, podList, client.MatchingFields{"spec.nodeName": node.Name}); err != nil {
			return nil, err
		}

		allocatedGPUs := 0
		var allocatedPods []*corev1.Pod
		for j := range podList.Items {
			pod := &podList.Items[j]
			if pod.Status.Phase == corev1.PodRunning {
				for _, container := range pod.Spec.Containers {
					if gpuReq, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
						allocatedGPUs += int(gpuReq.Value())
						allocatedPods = append(allocatedPods, pod)
					}
				}
			}
		}

		// Check GPU capabilities from node labels
		supportsMIG := node.Labels["nvidia.com/mig.capable"] == "true"
		supportsMPS := node.Labels["nvidia.com/mps.capable"] == "true"
		supportsTimeSlice := node.Labels["nvidia.com/time-slicing.capable"] == "true"
		gpuType := node.Labels["nvidia.com/gpu.product"]

		gpuNode := &GPUNode{
			Name:              node.Name,
			TotalGPUs:         totalGPUs,
			AvailableGPUs:     totalGPUs - allocatedGPUs,
			GPUType:           gpuType,
			AllocatedPods:     allocatedPods,
			SupportsMIG:       supportsMIG,
			SupportsMPS:       supportsMPS,
			SupportsTimeSlice: supportsTimeSlice,
		}

		gpuNodes = append(gpuNodes, gpuNode)
	}

	return gpuNodes, nil
}

// getPendingGPUWorkloads retrieves all pending pods requesting GPUs
func (s *BinPackingScheduler) getPendingGPUWorkloads(ctx context.Context) ([]*GPUWorkload, error) {
	podList := &corev1.PodList{}
	if err := s.client.List(ctx, podList); err != nil {
		return nil, err
	}

	var workloads []*GPUWorkload
	for i := range podList.Items {
		pod := &podList.Items[i]

		// Only consider pending pods
		if pod.Status.Phase != corev1.PodPending {
			continue
		}

		// Calculate total GPU request
		totalGPURequest := 0
		totalMemoryRequest := int64(0)
		hasGPURequest := false

		for _, container := range pod.Spec.Containers {
			if gpuReq, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
				totalGPURequest += int(gpuReq.Value())
				hasGPURequest = true
			}
			if memReq, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
				totalMemoryRequest += memReq.Value()
			}
		}

		if !hasGPURequest {
			continue
		}

		// Get workload preferences from annotations
		sharingEnabled := pod.Annotations["gpu-autoscaler.io/sharing"] == "enabled"
		preferredMode := pod.Annotations["gpu-autoscaler.io/sharing-mode"]
		if preferredMode == "" {
			preferredMode = "exclusive"
		}

		priority := int32(0)
		if pod.Spec.Priority != nil {
			priority = *pod.Spec.Priority
		}

		workload := &GPUWorkload{
			Pod:            pod,
			GPURequest:     totalGPURequest,
			MemoryRequest:  totalMemoryRequest,
			Priority:       priority,
			SharingEnabled: sharingEnabled,
			PreferredMode:  preferredMode,
		}

		workloads = append(workloads, workload)
	}

	return workloads, nil
}

// PackingResult contains the results of bin-packing operation
type PackingResult struct {
	Placements       map[string]string // pod name -> node name
	ConsolidatedPods int
	SavedGPUs        int
	Timestamp        metav1.Time
}

// AnalyzeConsolidationOpportunities identifies opportunities to consolidate workloads
func (s *BinPackingScheduler) AnalyzeConsolidationOpportunities(ctx context.Context) (*ConsolidationReport, error) {
	log := log.FromContext(ctx)
	log.Info("Analyzing consolidation opportunities")

	nodes, err := s.getGPUNodes(ctx)
	if err != nil {
		return nil, err
	}

	report := &ConsolidationReport{
		TotalNodes:         len(nodes),
		UnderutilizedNodes: 0,
		PotentialSavings:   0,
		Recommendations:    []string{},
	}

	for _, node := range nodes {
		utilizationPct := float64(node.TotalGPUs-node.AvailableGPUs) / float64(node.TotalGPUs) * 100

		// Node is underutilized if less than 50% GPUs are allocated
		if utilizationPct < 50.0 {
			report.UnderutilizedNodes++
			report.PotentialSavings += node.AvailableGPUs

			recommendation := fmt.Sprintf(
				"Node %s is underutilized (%.1f%% used, %d/%d GPUs allocated). Consider consolidating workloads.",
				node.Name, utilizationPct, node.TotalGPUs-node.AvailableGPUs, node.TotalGPUs,
			)
			report.Recommendations = append(report.Recommendations, recommendation)
		}
	}

	return report, nil
}

// ConsolidationReport contains analysis of consolidation opportunities
type ConsolidationReport struct {
	TotalNodes         int
	UnderutilizedNodes int
	PotentialSavings   int
	Recommendations    []string
	Timestamp          metav1.Time
}

// CalculateFragmentation calculates GPU fragmentation across the cluster
func CalculateFragmentation(nodes []*GPUNode) float64 {
	if len(nodes) == 0 {
		return 0
	}

	totalGPUs := 0
	totalAvailable := 0
	nodesWithAvailable := 0

	for _, node := range nodes {
		totalGPUs += node.TotalGPUs
		totalAvailable += node.AvailableGPUs
		if node.AvailableGPUs > 0 {
			nodesWithAvailable++
		}
	}

	if totalAvailable == 0 {
		return 0
	}

	// Fragmentation score: higher when available GPUs are spread across many nodes
	// Score ranges from 0 (no fragmentation) to 1 (maximum fragmentation)
	avgAvailablePerNode := float64(totalAvailable) / float64(len(nodes))
	fragmentationScore := float64(nodesWithAvailable) / float64(len(nodes))

	return fragmentationScore * (1 - avgAvailablePerNode/float64(totalGPUs))
}

// GetGPURequestFromPod extracts GPU request from pod spec
func GetGPURequestFromPod(pod *corev1.Pod) int {
	totalGPUs := 0
	for _, container := range pod.Spec.Containers {
		if gpuReq, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
			totalGPUs += int(gpuReq.Value())
		}
	}
	return totalGPUs
}

// ParseGPUQuantity parses a resource.Quantity as GPU count
func ParseGPUQuantity(q resource.Quantity) int {
	return int(q.Value())
}
