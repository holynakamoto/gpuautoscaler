package autoscaler

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Spot instance annotations
	SpotInstanceAnnotation         = "gpu-autoscaler.io/spot-instance"
	SpotTerminationTimeAnnotation  = "gpu-autoscaler.io/spot-termination-time"
	SpotInterruptionWarningAnnotation = "gpu-autoscaler.io/spot-interruption-warning"

	// Spot instance grace period before termination
	SpotTerminationGracePeriod = 2 * time.Minute

	// Check interval for spot interruption notices
	SpotCheckInterval = 5 * time.Second
)

// SpotOrchestrator manages spot instance lifecycle and graceful eviction
type SpotOrchestrator struct {
	client        client.Client
	cloudProvider CloudProvider
	logger        logr.Logger

	// Active spot termination warnings
	terminationWarnings map[string]time.Time
}

// NewSpotOrchestrator creates a new spot orchestrator
func NewSpotOrchestrator(client client.Client, cloudProvider CloudProvider, logger logr.Logger) *SpotOrchestrator {
	return &SpotOrchestrator{
		client:              client,
		cloudProvider:       cloudProvider,
		logger:              logger.WithName("spot-orchestrator"),
		terminationWarnings: make(map[string]time.Time),
	}
}

// MonitorSpotInstances monitors spot instances for termination notices
func (s *SpotOrchestrator) MonitorSpotInstances(ctx context.Context) error {
	ticker := time.NewTicker(SpotCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.checkSpotTerminations(ctx); err != nil {
				s.logger.Error(err, "failed to check spot terminations")
			}
		}
	}
}

// checkSpotTerminations checks all spot nodes for termination notices
func (s *SpotOrchestrator) checkSpotTerminations(ctx context.Context) error {
	// Get all spot nodes
	nodes, err := s.getSpotNodes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get spot nodes: %w", err)
	}

	for _, node := range nodes {
		// Check if node has termination warning
		terminationTime, hasWarning, err := s.cloudProvider.GetSpotTerminationNotice(ctx, node.Name)
		if err != nil {
			s.logger.Error(err, "failed to get spot termination notice", "node", node.Name)
			continue
		}

		if hasWarning {
			s.logger.Info("spot termination warning received",
				"node", node.Name,
				"terminationTime", terminationTime,
			)

			// Handle termination warning
			if err := s.handleSpotTermination(ctx, &node, terminationTime); err != nil {
				s.logger.Error(err, "failed to handle spot termination", "node", node.Name)
			}
		}
	}

	return nil
}

// handleSpotTermination handles graceful eviction of workloads from spot instance
func (s *SpotOrchestrator) handleSpotTermination(ctx context.Context, node *corev1.Node, terminationTime time.Time) error {
	nodeName := node.Name

	// Check if we've already started handling this termination
	if _, exists := s.terminationWarnings[nodeName]; exists {
		return nil
	}

	// Record termination warning
	s.terminationWarnings[nodeName] = terminationTime

	// Annotate node with termination info
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations[SpotTerminationTimeAnnotation] = terminationTime.Format(time.RFC3339)
	node.Annotations[SpotInterruptionWarningAnnotation] = "true"

	if err := s.client.Update(ctx, node); err != nil {
		return fmt.Errorf("failed to update node annotations: %w", err)
	}

	// Mark node as unschedulable
	node.Spec.Unschedulable = true
	if err := s.client.Update(ctx, node); err != nil {
		return fmt.Errorf("failed to mark node unschedulable: %w", err)
	}

	// Start graceful eviction of pods
	go s.gracefullyEvictPods(context.Background(), node)

	return nil
}

// gracefullyEvictPods evicts all pods from a node with prioritization
func (s *SpotOrchestrator) gracefullyEvictPods(ctx context.Context, node *corev1.Node) {
	s.logger.Info("starting graceful pod eviction", "node", node.Name)

	// Get all pods on the node
	podList := &corev1.PodList{}
	if err := s.client.List(ctx, podList, client.MatchingFields{"spec.nodeName": node.Name}); err != nil {
		s.logger.Error(err, "failed to list pods on node", "node", node.Name)
		return
	}

	// Prioritize pods by eviction priority
	highPriorityPods := make([]corev1.Pod, 0)
	mediumPriorityPods := make([]corev1.Pod, 0)
	lowPriorityPods := make([]corev1.Pod, 0)

	for _, pod := range podList.Items {
		priority := s.getPodEvictionPriority(&pod)
		switch priority {
		case EvictionPriorityHigh:
			highPriorityPods = append(highPriorityPods, pod)
		case EvictionPriorityMedium:
			mediumPriorityPods = append(mediumPriorityPods, pod)
		case EvictionPriorityLow:
			lowPriorityPods = append(lowPriorityPods, pod)
		}
	}

	// Evict in priority order: low → medium → high
	// This ensures critical workloads have time to migrate first
	s.evictPods(ctx, lowPriorityPods, 0)
	time.Sleep(10 * time.Second) // Brief pause between priorities

	s.evictPods(ctx, mediumPriorityPods, 0)
	time.Sleep(10 * time.Second)

	s.evictPods(ctx, highPriorityPods, 30) // Give high-priority pods more grace time

	s.logger.Info("completed graceful pod eviction", "node", node.Name)

	// Clean up termination warning
	delete(s.terminationWarnings, node.Name)
}

// evictPods evicts a list of pods with specified grace period
func (s *SpotOrchestrator) evictPods(ctx context.Context, pods []corev1.Pod, gracePeriodSeconds int64) {
	for _, pod := range pods {
		s.logger.Info("evicting pod",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"node", pod.Spec.NodeName,
			"gracePeriod", gracePeriodSeconds,
		)

		if err := s.client.Delete(ctx, &pod, client.GracePeriodSeconds(gracePeriodSeconds)); err != nil {
			s.logger.Error(err, "failed to evict pod", "pod", pod.Name)
		}
	}
}

// getPodEvictionPriority determines the eviction priority for a pod
func (s *SpotOrchestrator) getPodEvictionPriority(pod *corev1.Pod) string {
	// Check explicit priority annotation
	if priority, exists := pod.Annotations[EvictionPriorityLabel]; exists {
		return priority
	}

	// Determine priority based on workload characteristics
	// Training workloads: high priority (take longer to restart)
	// Inference workloads: medium priority
	// Development/batch: low priority
	if workloadType, exists := pod.Labels["gpu-autoscaler.io/workload-type"]; exists {
		switch workloadType {
		case "training":
			return EvictionPriorityHigh
		case "inference", "serving":
			return EvictionPriorityMedium
		case "development", "batch":
			return EvictionPriorityLow
		}
	}

	// Check if pod has high priority class
	if pod.Spec.Priority != nil && *pod.Spec.Priority > 1000 {
		return EvictionPriorityHigh
	}

	// Default to medium priority
	return EvictionPriorityMedium
}

// getSpotNodes returns all spot instance nodes
func (s *SpotOrchestrator) getSpotNodes(ctx context.Context) ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	if err := s.client.List(ctx, nodeList); err != nil {
		return nil, err
	}

	spotNodes := make([]corev1.Node, 0)
	for _, node := range nodeList.Items {
		if capacityType, exists := node.Labels[CapacityTypeLabel]; exists && capacityType == CapacityTypeSpot {
			spotNodes = append(spotNodes, node)
		}
	}

	return spotNodes, nil
}

// GetSpotInstanceMetrics returns metrics about spot instance usage
func (s *SpotOrchestrator) GetSpotInstanceMetrics(ctx context.Context) (*SpotInstanceMetrics, error) {
	nodes, err := s.getSpotNodes(ctx)
	if err != nil {
		return nil, err
	}

	metrics := &SpotInstanceMetrics{
		TotalSpotNodes:       len(nodes),
		ActiveTerminations:   len(s.terminationWarnings),
		SpotInterruptionRate: 0, // TODO: Calculate from historical data
	}

	// Count nodes with termination warnings
	for _, node := range nodes {
		if _, exists := node.Annotations[SpotInterruptionWarningAnnotation]; exists {
			metrics.NodesWithWarning++
		}
	}

	return metrics, nil
}

// SpotInstanceMetrics contains metrics about spot instances
type SpotInstanceMetrics struct {
	TotalSpotNodes       int
	NodesWithWarning     int
	ActiveTerminations   int
	SpotInterruptionRate float64
	LastInterruption     time.Time
}

// SpotInstanceRecommendation provides recommendations for spot instance usage
type SpotInstanceRecommendation struct {
	RecommendedSpotPercentage float64
	DiversifyInstanceTypes    bool
	SuggestedInstanceTypes    []string
	EstimatedSavings          float64
}

// GetSpotInstanceRecommendation analyzes spot usage and provides recommendations
func (s *SpotOrchestrator) GetSpotInstanceRecommendation(ctx context.Context) (*SpotInstanceRecommendation, error) {
	metrics, err := s.GetSpotInstanceMetrics(ctx)
	if err != nil {
		return nil, err
	}

	recommendation := &SpotInstanceRecommendation{
		RecommendedSpotPercentage: 0.6, // Default 60%
		DiversifyInstanceTypes:    true,
	}

	// Adjust recommendation based on interruption rate
	if metrics.SpotInterruptionRate > 0.1 { // >10% interruption rate
		// High interruption rate - reduce spot percentage
		recommendation.RecommendedSpotPercentage = 0.4
		recommendation.DiversifyInstanceTypes = true
	} else if metrics.SpotInterruptionRate < 0.02 { // <2% interruption rate
		// Low interruption rate - can increase spot percentage
		recommendation.RecommendedSpotPercentage = 0.75
	}

	// Get instance type recommendations from cloud provider
	instanceTypes, err := s.cloudProvider.GetRecommendedSpotInstanceTypes(ctx)
	if err == nil {
		recommendation.SuggestedInstanceTypes = instanceTypes
	}

	// Calculate estimated savings
	// Spot instances typically 60-90% cheaper than on-demand
	recommendation.EstimatedSavings = recommendation.RecommendedSpotPercentage * 0.70 // Average 70% savings

	return recommendation, nil
}

// HandleSpotInstanceBidding manages spot instance bidding strategy
func (s *SpotOrchestrator) HandleSpotInstanceBidding(ctx context.Context, instanceType string) (*SpotBidStrategy, error) {
	// Get current spot price
	currentPrice, err := s.cloudProvider.GetSpotPrice(ctx, instanceType)
	if err != nil {
		return nil, fmt.Errorf("failed to get spot price: %w", err)
	}

	// Get on-demand price for comparison
	onDemandPrice, err := s.cloudProvider.GetOnDemandPrice(ctx, instanceType)
	if err != nil {
		return nil, fmt.Errorf("failed to get on-demand price: %w", err)
	}

	strategy := &SpotBidStrategy{
		InstanceType:  instanceType,
		CurrentPrice:  currentPrice,
		OnDemandPrice: onDemandPrice,
		BidPrice:      onDemandPrice * 0.8, // Bid 80% of on-demand price
		MaxPrice:      onDemandPrice,       // Never pay more than on-demand
	}

	// Adjust bid based on price volatility
	priceVolatility := s.calculatePriceVolatility(ctx, instanceType)
	if priceVolatility > 0.3 { // High volatility
		strategy.BidPrice = onDemandPrice * 0.9 // Increase bid to 90%
	}

	return strategy, nil
}

// SpotBidStrategy contains bidding strategy for spot instances
type SpotBidStrategy struct {
	InstanceType  string
	CurrentPrice  float64
	OnDemandPrice float64
	BidPrice      float64
	MaxPrice      float64
}

func (s *SpotOrchestrator) calculatePriceVolatility(ctx context.Context, instanceType string) float64 {
	// TODO: Implement price volatility calculation based on historical data
	return 0.2 // Default moderate volatility
}

// SpotPlacementStrategy determines optimal spot instance placement
type SpotPlacementStrategy struct {
	PreferredZones     []string
	InstanceTypes      []string
	DiversificationMin int // Minimum number of instance types to use
}

// GetOptimalSpotPlacement returns optimal placement strategy for spot instances
func (s *SpotOrchestrator) GetOptimalSpotPlacement(ctx context.Context) (*SpotPlacementStrategy, error) {
	// Get availability zones with lowest spot interruption rates
	zones, err := s.cloudProvider.GetAvailabilityZones(ctx)
	if err != nil {
		return nil, err
	}

	strategy := &SpotPlacementStrategy{
		PreferredZones:     zones,
		DiversificationMin: 3, // Use at least 3 different instance types
	}

	// Get GPU instance types suitable for spot
	instanceTypes, err := s.cloudProvider.GetRecommendedSpotInstanceTypes(ctx)
	if err != nil {
		return nil, err
	}

	strategy.InstanceTypes = instanceTypes

	return strategy, nil
}

// CreateSpotNodeEvent creates a Kubernetes event for spot node changes
func (s *SpotOrchestrator) CreateSpotNodeEvent(ctx context.Context, node *corev1.Node, eventType, reason, message string) error {
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s.%d", node.Name, time.Now().Unix()),
			Namespace: "default",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Node",
			Name:      node.Name,
			UID:       node.UID,
			Namespace: node.Namespace,
		},
		Reason:  reason,
		Message: message,
		Type:    eventType,
		Source: corev1.EventSource{
			Component: "spot-orchestrator",
		},
		FirstTimestamp: metav1.Now(),
		LastTimestamp:  metav1.Now(),
		Count:          1,
	}

	return s.client.Create(ctx, event)
}
