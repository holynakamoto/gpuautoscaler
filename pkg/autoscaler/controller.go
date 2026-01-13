package autoscaler

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gpuautoscaler/gpuautoscaler/pkg/metrics"
)

const (
	// Default configuration values
	DefaultReconcileInterval      = 30 * time.Second
	DefaultScaleUpThreshold       = 0.8  // 80% GPU utilization
	DefaultScaleDownThreshold     = 0.2  // 20% GPU utilization
	DefaultScaleUpCooldown        = 3 * time.Minute
	DefaultScaleDownCooldown      = 10 * time.Minute
	DefaultPendingPodTimeout      = 2 * time.Minute
	DefaultMaxNodes               = 100
	DefaultMinNodes               = 0
	DefaultSpotInstancePercentage = 0.6 // 60% spot instances

	// Node labels
	NodePoolLabel        = "gpu-autoscaler.io/node-pool"
	InstanceTypeLabel    = "gpu-autoscaler.io/instance-type"
	CapacityTypeLabel    = "gpu-autoscaler.io/capacity-type"
	GPUTypeLabel         = "gpu-autoscaler.io/gpu-type"
	ScalingGroupLabel    = "gpu-autoscaler.io/scaling-group"
	EvictionPriorityLabel = "gpu-autoscaler.io/eviction-priority"

	// Capacity types
	CapacityTypeSpot      = "spot"
	CapacityTypeOnDemand  = "on-demand"
	CapacityTypeReserved  = "reserved"

	// Eviction priorities
	EvictionPriorityHigh   = "high"
	EvictionPriorityMedium = "medium"
	EvictionPriorityLow    = "low"
)

// AutoscalerController manages GPU node autoscaling
type AutoscalerController struct {
	client.Client
	Scheme           *runtime.Scheme
	Log              logr.Logger
	MetricsCollector *metrics.Collector
	CloudProvider    CloudProvider
	PredictiveScaler *PredictiveScaler
	SpotOrchestrator *SpotOrchestrator

	// Configuration
	Config AutoscalerConfig

	// State tracking
	lastScaleUpTime   time.Time
	lastScaleDownTime time.Time
	scalingHistory    []ScalingEvent
}

// AutoscalerConfig holds the autoscaler configuration
type AutoscalerConfig struct {
	ReconcileInterval      time.Duration
	ScaleUpThreshold       float64
	ScaleDownThreshold     float64
	ScaleUpCooldown        time.Duration
	ScaleDownCooldown      time.Duration
	PendingPodTimeout      time.Duration
	MaxNodes               int
	MinNodes               int
	SpotInstancePercentage float64
	EnablePredictiveScaling bool
	EnableSpotInstances    bool
	EnableMultiTierScaling bool
	NodePools              []NodePoolConfig
}

// NodePoolConfig defines a GPU node pool
type NodePoolConfig struct {
	Name             string
	MinSize          int
	MaxSize          int
	GPUType          string
	InstanceTypes    []string
	CapacityType     string
	SpotPercentage   float64
	Priority         int
	Labels           map[string]string
	Taints           []corev1.Taint
}

// ScalingEvent records a scaling action
type ScalingEvent struct {
	Timestamp    time.Time
	Action       ScalingAction
	Reason       string
	NodeCount    int
	CapacityType string
	Success      bool
}

// ScalingAction represents a scaling operation
type ScalingAction string

const (
	ScaleUp   ScalingAction = "scale-up"
	ScaleDown ScalingAction = "scale-down"
	NoAction  ScalingAction = "no-action"
)

// ScalingDecision represents the result of autoscaling analysis
type ScalingDecision struct {
	Action           ScalingAction
	Reason           string
	DesiredNodeCount int
	CapacityType     string
	NodePool         string
	Priority         int
	GPUUtilization   float64
	PendingPods      int
	UnderutilizedNodes int
}

// NewAutoscalerController creates a new autoscaler controller
func NewAutoscalerController(
	client client.Client,
	scheme *runtime.Scheme,
	metricsCollector *metrics.Collector,
	cloudProvider CloudProvider,
	config AutoscalerConfig,
) *AutoscalerController {
	logger := log.Log.WithName("autoscaler")

	ac := &AutoscalerController{
		Client:           client,
		Scheme:           scheme,
		Log:              logger,
		MetricsCollector: metricsCollector,
		CloudProvider:    cloudProvider,
		Config:           config,
		scalingHistory:   make([]ScalingEvent, 0),
	}

	// Initialize predictive scaler if enabled
	if config.EnablePredictiveScaling {
		ac.PredictiveScaler = NewPredictiveScaler(metricsCollector)
	}

	// Initialize spot orchestrator if enabled
	if config.EnableSpotInstances {
		ac.SpotOrchestrator = NewSpotOrchestrator(client, cloudProvider, logger)
	}

	return ac
}

// Reconcile implements the reconciliation loop
func (r *AutoscalerController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("request", req.NamespacedName)

	// Analyze cluster state and make scaling decision
	decision, err := r.analyzeClusterState(ctx)
	if err != nil {
		logger.Error(err, "failed to analyze cluster state")
		return ctrl.Result{RequeueAfter: r.Config.ReconcileInterval}, err
	}

	// Log decision
	logger.Info("scaling decision",
		"action", decision.Action,
		"reason", decision.Reason,
		"desiredNodes", decision.DesiredNodeCount,
		"capacityType", decision.CapacityType,
		"gpuUtilization", decision.GPUUtilization,
		"pendingPods", decision.PendingPods,
	)

	// Execute scaling action
	if decision.Action != NoAction {
		if err := r.executeScalingAction(ctx, decision); err != nil {
			logger.Error(err, "failed to execute scaling action")
			r.recordScalingEvent(decision.Action, decision.Reason, 0, decision.CapacityType, false)
			return ctrl.Result{RequeueAfter: r.Config.ReconcileInterval}, err
		}
		r.recordScalingEvent(decision.Action, decision.Reason, decision.DesiredNodeCount, decision.CapacityType, true)
	}

	return ctrl.Result{RequeueAfter: r.Config.ReconcileInterval}, nil
}

// analyzeClusterState analyzes the cluster and makes a scaling decision
func (r *AutoscalerController) analyzeClusterState(ctx context.Context) (*ScalingDecision, error) {
	// Get current GPU nodes
	nodes, err := r.getGPUNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get GPU nodes: %w", err)
	}

	// Get pending GPU pods
	pendingPods, err := r.getPendingGPUPods(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending GPU pods: %w", err)
	}

	// Get GPU utilization metrics
	avgUtilization, err := r.getAverageGPUUtilization(ctx)
	if err != nil {
		r.Log.Error(err, "failed to get GPU utilization, using 0")
		avgUtilization = 0
	}

	// Count underutilized nodes
	underutilizedNodes := r.countUnderutilizedNodes(ctx, nodes)

	decision := &ScalingDecision{
		Action:             NoAction,
		Reason:             "cluster stable",
		DesiredNodeCount:   len(nodes),
		GPUUtilization:     avgUtilization,
		PendingPods:        len(pendingPods),
		UnderutilizedNodes: underutilizedNodes,
	}

	// Check for scale-up conditions
	if r.shouldScaleUp(ctx, nodes, pendingPods, avgUtilization) {
		decision.Action = ScaleUp
		decision.Reason = r.getScaleUpReason(pendingPods, avgUtilization)
		decision.DesiredNodeCount = r.calculateScaleUpNodeCount(nodes, pendingPods)

		// Determine capacity type for new nodes (multi-tier strategy)
		decision.CapacityType, decision.NodePool = r.selectCapacityType(nodes)
		decision.Priority = r.calculateScalingPriority(pendingPods)
	} else if r.shouldScaleDown(ctx, nodes, avgUtilization, underutilizedNodes) {
		decision.Action = ScaleDown
		decision.Reason = r.getScaleDownReason(avgUtilization, underutilizedNodes)
		decision.DesiredNodeCount = r.calculateScaleDownNodeCount(nodes, underutilizedNodes)

		// Select nodes to remove (prefer spot instances)
		decision.CapacityType = r.selectNodesForScaleDown(nodes)
	}

	// Apply predictive scaling if enabled
	if r.Config.EnablePredictiveScaling && r.PredictiveScaler != nil {
		r.applyPredictiveScaling(ctx, decision)
	}

	return decision, nil
}

// shouldScaleUp determines if the cluster should scale up
func (r *AutoscalerController) shouldScaleUp(ctx context.Context, nodes []corev1.Node, pendingPods []corev1.Pod, avgUtilization float64) bool {
	// Check cooldown period
	if time.Since(r.lastScaleUpTime) < r.Config.ScaleUpCooldown {
		return false
	}

	// Check max nodes limit
	if len(nodes) >= r.Config.MaxNodes {
		return false
	}

	// Scale up if there are pending GPU pods waiting too long
	if len(pendingPods) > 0 {
		oldestPendingPod := r.getOldestPendingPod(pendingPods)
		if time.Since(oldestPendingPod.CreationTimestamp.Time) > r.Config.PendingPodTimeout {
			return true
		}
	}

	// Scale up if GPU utilization is too high
	if avgUtilization > r.Config.ScaleUpThreshold && len(nodes) > 0 {
		return true
	}

	return false
}

// shouldScaleDown determines if the cluster should scale down
func (r *AutoscalerController) shouldScaleDown(ctx context.Context, nodes []corev1.Node, avgUtilization float64, underutilizedNodes int) bool {
	// Check cooldown period
	if time.Since(r.lastScaleDownTime) < r.Config.ScaleDownCooldown {
		return false
	}

	// Check min nodes limit
	if len(nodes) <= r.Config.MinNodes {
		return false
	}

	// Scale down if there are underutilized nodes
	if underutilizedNodes > 0 && avgUtilization < r.Config.ScaleDownThreshold {
		return true
	}

	return false
}

// executeScalingAction executes the scaling decision
func (r *AutoscalerController) executeScalingAction(ctx context.Context, decision *ScalingDecision) error {
	switch decision.Action {
	case ScaleUp:
		return r.scaleUp(ctx, decision)
	case ScaleDown:
		return r.scaleDown(ctx, decision)
	default:
		return nil
	}
}

// scaleUp adds new GPU nodes to the cluster
func (r *AutoscalerController) scaleUp(ctx context.Context, decision *ScalingDecision) error {
	r.Log.Info("scaling up cluster",
		"currentNodes", decision.DesiredNodeCount-1,
		"targetNodes", decision.DesiredNodeCount,
		"capacityType", decision.CapacityType,
		"reason", decision.Reason,
	)

	// Get node pool configuration
	nodePool := r.getNodePoolByName(decision.NodePool)
	if nodePool == nil {
		return fmt.Errorf("node pool %s not found", decision.NodePool)
	}

	// Calculate number of nodes to add
	nodesToAdd := decision.DesiredNodeCount - (decision.DesiredNodeCount - 1)

	// Use cloud provider to add nodes
	if err := r.CloudProvider.ScaleUp(ctx, nodePool, nodesToAdd); err != nil {
		return fmt.Errorf("failed to scale up: %w", err)
	}

	// Update timestamp
	r.lastScaleUpTime = time.Now()

	return nil
}

// scaleDown removes GPU nodes from the cluster
func (r *AutoscalerController) scaleDown(ctx context.Context, decision *ScalingDecision) error {
	r.Log.Info("scaling down cluster",
		"currentNodes", decision.DesiredNodeCount+1,
		"targetNodes", decision.DesiredNodeCount,
		"capacityType", decision.CapacityType,
		"reason", decision.Reason,
	)

	// Get GPU nodes
	nodes, err := r.getGPUNodes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get GPU nodes: %w", err)
	}

	// Select nodes to remove
	nodesToRemove := r.selectNodesToRemove(nodes, decision)

	// Drain and remove nodes
	for _, node := range nodesToRemove {
		if err := r.drainNode(ctx, &node); err != nil {
			r.Log.Error(err, "failed to drain node", "node", node.Name)
			continue
		}

		if err := r.CloudProvider.ScaleDown(ctx, node.Name); err != nil {
			r.Log.Error(err, "failed to remove node", "node", node.Name)
			continue
		}
	}

	// Update timestamp
	r.lastScaleDownTime = time.Now()

	return nil
}

// getGPUNodes returns all GPU nodes in the cluster
func (r *AutoscalerController) getGPUNodes(ctx context.Context) ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return nil, err
	}

	gpuNodes := make([]corev1.Node, 0)
	for _, node := range nodeList.Items {
		// Check if node has GPU resources
		if _, hasNvidiaGPU := node.Status.Capacity["nvidia.com/gpu"]; hasNvidiaGPU {
			gpuNodes = append(gpuNodes, node)
		}
	}

	return gpuNodes, nil
}

// getPendingGPUPods returns all pending pods requesting GPUs
func (r *AutoscalerController) getPendingGPUPods(ctx context.Context) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList); err != nil {
		return nil, err
	}

	pendingPods := make([]corev1.Pod, 0)
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodPending && r.isGPUPod(&pod) {
			pendingPods = append(pendingPods, pod)
		}
	}

	return pendingPods, nil
}

// isGPUPod checks if a pod requests GPU resources
func (r *AutoscalerController) isGPUPod(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if _, hasGPU := container.Resources.Requests["nvidia.com/gpu"]; hasGPU {
			return true
		}
		// Check for MIG profiles
		for resourceName := range container.Resources.Requests {
			if len(resourceName) > 11 && string(resourceName)[:11] == "nvidia.com/mig-" {
				return true
			}
		}
	}
	return false
}

// getAverageGPUUtilization returns the average GPU utilization across all nodes
func (r *AutoscalerController) getAverageGPUUtilization(ctx context.Context) (float64, error) {
	gpuMetrics, err := r.MetricsCollector.GetGPUMetrics(ctx)
	if err != nil {
		return 0, err
	}

	if len(gpuMetrics) == 0 {
		return 0, nil
	}

	var totalUtilization float64
	for _, metric := range gpuMetrics {
		totalUtilization += metric.GPUUtilization
	}

	return totalUtilization / float64(len(gpuMetrics)), nil
}

// countUnderutilizedNodes counts nodes with low GPU utilization
func (r *AutoscalerController) countUnderutilizedNodes(ctx context.Context, nodes []corev1.Node) int {
	count := 0
	for _, node := range nodes {
		utilization, err := r.getNodeGPUUtilization(ctx, node.Name)
		if err != nil {
			continue
		}
		if utilization < r.Config.ScaleDownThreshold {
			count++
		}
	}
	return count
}

// getNodeGPUUtilization returns the average GPU utilization for a node
func (r *AutoscalerController) getNodeGPUUtilization(ctx context.Context, nodeName string) (float64, error) {
	gpuMetrics, err := r.MetricsCollector.GetGPUMetrics(ctx)
	if err != nil {
		return 0, err
	}

	var totalUtilization float64
	var count int
	for _, metric := range gpuMetrics {
		if metric.NodeName == nodeName {
			totalUtilization += metric.GPUUtilization
			count++
		}
	}

	if count == 0 {
		return 0, nil
	}

	return totalUtilization / float64(count), nil
}

// Helper methods

func (r *AutoscalerController) getOldestPendingPod(pods []corev1.Pod) corev1.Pod {
	if len(pods) == 0 {
		return corev1.Pod{}
	}
	oldest := pods[0]
	for _, pod := range pods[1:] {
		if pod.CreationTimestamp.Before(&oldest.CreationTimestamp) {
			oldest = pod
		}
	}
	return oldest
}

func (r *AutoscalerController) getScaleUpReason(pendingPods []corev1.Pod, utilization float64) string {
	if len(pendingPods) > 0 {
		return fmt.Sprintf("%d pending GPU pods waiting", len(pendingPods))
	}
	return fmt.Sprintf("GPU utilization %.1f%% exceeds threshold %.1f%%", utilization*100, r.Config.ScaleUpThreshold*100)
}

func (r *AutoscalerController) getScaleDownReason(utilization float64, underutilized int) string {
	return fmt.Sprintf("GPU utilization %.1f%% below threshold %.1f%%, %d underutilized nodes", utilization*100, r.Config.ScaleDownThreshold*100, underutilized)
}

func (r *AutoscalerController) calculateScaleUpNodeCount(nodes []corev1.Node, pendingPods []corev1.Pod) int {
	// Estimate nodes needed based on pending pods
	// Assume each node can handle 4-8 GPU pods (conservative)
	nodesNeeded := int(math.Ceil(float64(len(pendingPods)) / 4.0))
	targetNodes := len(nodes) + nodesNeeded

	if targetNodes > r.Config.MaxNodes {
		targetNodes = r.Config.MaxNodes
	}

	return targetNodes
}

func (r *AutoscalerController) calculateScaleDownNodeCount(nodes []corev1.Node, underutilized int) int {
	// Remove underutilized nodes gradually (max 20% at a time)
	nodesToRemove := int(math.Min(float64(underutilized), float64(len(nodes))*0.2))
	targetNodes := len(nodes) - nodesToRemove

	if targetNodes < r.Config.MinNodes {
		targetNodes = r.Config.MinNodes
	}

	return targetNodes
}

func (r *AutoscalerController) calculateScalingPriority(pendingPods []corev1.Pod) int {
	// Calculate priority based on pod priorities and age
	maxPriority := 0
	for _, pod := range pendingPods {
		if pod.Spec.Priority != nil && int(*pod.Spec.Priority) > maxPriority {
			maxPriority = int(*pod.Spec.Priority)
		}
	}
	return maxPriority
}

func (r *AutoscalerController) selectCapacityType(nodes []corev1.Node) (string, string) {
	// Multi-tier strategy: prefer spot instances up to configured percentage
	spotNodes := 0
	totalNodes := len(nodes)

	for _, node := range nodes {
		if node.Labels[CapacityTypeLabel] == CapacityTypeSpot {
			spotNodes++
		}
	}

	spotPercentage := float64(spotNodes) / float64(totalNodes)
	if spotPercentage < r.Config.SpotInstancePercentage {
		// Add spot instance
		return CapacityTypeSpot, r.getPreferredNodePool(CapacityTypeSpot)
	}

	// Add on-demand instance
	return CapacityTypeOnDemand, r.getPreferredNodePool(CapacityTypeOnDemand)
}

func (r *AutoscalerController) selectNodesForScaleDown(nodes []corev1.Node) string {
	// Prefer removing spot instances first
	for _, node := range nodes {
		if node.Labels[CapacityTypeLabel] == CapacityTypeSpot {
			return CapacityTypeSpot
		}
	}
	return CapacityTypeOnDemand
}

func (r *AutoscalerController) selectNodesToRemove(nodes []corev1.Node, decision *ScalingDecision) []corev1.Node {
	// Sort nodes by eviction priority (spot > on-demand > reserved)
	// and by utilization (lowest first)
	nodesToRemove := make([]corev1.Node, 0)

	currentCount := len(nodes)
	targetCount := decision.DesiredNodeCount
	removeCount := currentCount - targetCount

	if removeCount <= 0 {
		return nodesToRemove
	}

	// Prioritize spot instances for removal
	for _, node := range nodes {
		if len(nodesToRemove) >= removeCount {
			break
		}
		if node.Labels[CapacityTypeLabel] == CapacityTypeSpot {
			nodesToRemove = append(nodesToRemove, node)
		}
	}

	// If still need to remove more, use on-demand
	if len(nodesToRemove) < removeCount {
		for _, node := range nodes {
			if len(nodesToRemove) >= removeCount {
				break
			}
			if node.Labels[CapacityTypeLabel] == CapacityTypeOnDemand {
				nodesToRemove = append(nodesToRemove, node)
			}
		}
	}

	return nodesToRemove
}

func (r *AutoscalerController) drainNode(ctx context.Context, node *corev1.Node) error {
	r.Log.Info("draining node", "node", node.Name)

	// Mark node as unschedulable
	node.Spec.Unschedulable = true
	if err := r.Update(ctx, node); err != nil {
		return fmt.Errorf("failed to mark node unschedulable: %w", err)
	}

	// Get all pods on the node
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.MatchingFields{"spec.nodeName": node.Name}); err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	// Evict pods gracefully
	for _, pod := range podList.Items {
		if err := r.Delete(ctx, &pod, client.GracePeriodSeconds(30)); err != nil && !errors.IsNotFound(err) {
			r.Log.Error(err, "failed to evict pod", "pod", pod.Name)
		}
	}

	// Wait for pods to be evicted (with timeout)
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for pods to be evicted")
		case <-ticker.C:
			podList := &corev1.PodList{}
			if err := r.List(ctx, podList, client.MatchingFields{"spec.nodeName": node.Name}); err != nil {
				return err
			}
			if len(podList.Items) == 0 {
				return nil
			}
		}
	}
}

func (r *AutoscalerController) applyPredictiveScaling(ctx context.Context, decision *ScalingDecision) {
	if r.PredictiveScaler == nil {
		return
	}

	// Get predictive scaling recommendation
	prediction := r.PredictiveScaler.PredictFutureLoad(ctx)
	if prediction.ShouldPreWarm {
		r.Log.Info("predictive scaling recommendation",
			"prediction", prediction.PredictedUtilization,
			"recommendedNodes", prediction.RecommendedNodes,
		)

		// Adjust decision based on prediction
		if prediction.RecommendedNodes > decision.DesiredNodeCount {
			decision.Action = ScaleUp
			decision.Reason = fmt.Sprintf("predictive scaling: expected load increase to %.1f%%", prediction.PredictedUtilization*100)
			decision.DesiredNodeCount = prediction.RecommendedNodes
		}
	}
}

func (r *AutoscalerController) getNodePoolByName(name string) *NodePoolConfig {
	for i := range r.Config.NodePools {
		if r.Config.NodePools[i].Name == name {
			return &r.Config.NodePools[i]
		}
	}
	return nil
}

func (r *AutoscalerController) getPreferredNodePool(capacityType string) string {
	// Return the first node pool with matching capacity type
	for _, pool := range r.Config.NodePools {
		if pool.CapacityType == capacityType {
			return pool.Name
		}
	}
	// Default to first pool
	if len(r.Config.NodePools) > 0 {
		return r.Config.NodePools[0].Name
	}
	return "default"
}

func (r *AutoscalerController) recordScalingEvent(action ScalingAction, reason string, nodeCount int, capacityType string, success bool) {
	event := ScalingEvent{
		Timestamp:    time.Now(),
		Action:       action,
		Reason:       reason,
		NodeCount:    nodeCount,
		CapacityType: capacityType,
		Success:      success,
	}
	r.scalingHistory = append(r.scalingHistory, event)

	// Keep only last 100 events
	if len(r.scalingHistory) > 100 {
		r.scalingHistory = r.scalingHistory[len(r.scalingHistory)-100:]
	}
}

// SetupWithManager sets up the controller with the Manager
func (r *AutoscalerController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Complete(r)
}
