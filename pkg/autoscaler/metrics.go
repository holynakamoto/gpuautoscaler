package autoscaler

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// Autoscaling action metrics
	scalingActionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gpu_autoscaler_scaling_actions_total",
			Help: "Total number of scaling actions performed",
		},
		[]string{"action", "capacity_type", "success"},
	)

	scalingDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gpu_autoscaler_scaling_duration_seconds",
			Help:    "Time taken to complete scaling actions",
			Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 1s to ~17min
		},
		[]string{"action"},
	)

	// Node metrics
	nodeCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_node_count",
			Help: "Current number of GPU nodes",
		},
		[]string{"capacity_type"},
	)

	desiredNodeCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_desired_node_count",
			Help: "Desired number of GPU nodes",
		},
	)

	// Utilization metrics
	clusterGPUUtilization = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_cluster_utilization",
			Help: "Average GPU utilization across the cluster (0-1)",
		},
	)

	pendingPodsCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_pending_pods",
			Help: "Number of pending GPU pods",
		},
	)

	underutilizedNodesCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_underutilized_nodes",
			Help: "Number of underutilized GPU nodes",
		},
	)

	// Spot instance metrics
	spotInterruptionsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gpu_autoscaler_spot_interruptions_total",
			Help: "Total number of spot instance interruptions",
		},
	)

	spotTerminationWarnings = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_spot_termination_warnings",
			Help: "Number of active spot termination warnings",
		},
	)

	spotInstanceSavings = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_spot_savings_percentage",
			Help: "Estimated cost savings from spot instances (0-1)",
		},
	)

	// Cost metrics
	estimatedMonthlyCost = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_estimated_monthly_cost_usd",
			Help: "Estimated monthly cost in USD",
		},
		[]string{"capacity_type"},
	)

	estimatedMonthlySavings = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_estimated_monthly_savings_usd",
			Help: "Estimated monthly savings from optimization",
		},
	)

	// Predictive scaling metrics
	predictiveScalingEnabled = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_predictive_scaling_enabled",
			Help: "Whether predictive scaling is enabled (0 or 1)",
		},
	)

	predictedUtilization = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_predicted_utilization",
			Help: "Predicted future GPU utilization (0-1)",
		},
	)

	predictionConfidence = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_prediction_confidence",
			Help: "Confidence level of utilization prediction (0-1)",
		},
	)

	// Cooldown metrics
	scaleUpCooldownRemaining = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_scale_up_cooldown_remaining_seconds",
			Help: "Seconds remaining in scale-up cooldown period",
		},
	)

	scaleDownCooldownRemaining = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_scale_down_cooldown_remaining_seconds",
			Help: "Seconds remaining in scale-down cooldown period",
		},
	)

	// Multi-tier scaling metrics
	multiTierScalingEnabled = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_multi_tier_scaling_enabled",
			Help: "Whether multi-tier scaling is enabled (0 or 1)",
		},
	)

	capacityTypePreference = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_capacity_type_preference",
			Help: "Current capacity type preference score",
		},
		[]string{"capacity_type"},
	)

	// Controller health metrics
	reconcileErrorsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gpu_autoscaler_reconcile_errors_total",
			Help: "Total number of reconciliation errors",
		},
	)

	reconcileDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "gpu_autoscaler_reconcile_duration_seconds",
			Help:    "Time taken to reconcile autoscaling",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 10), // 10ms to ~10s
		},
	)

	lastReconcileTimestamp = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gpu_autoscaler_last_reconcile_timestamp",
			Help: "Timestamp of the last reconciliation",
		},
	)
)

func init() {
	// Register metrics with controller-runtime's metrics registry
	metrics.Registry.MustRegister(
		scalingActionsTotal,
		scalingDuration,
		nodeCount,
		desiredNodeCount,
		clusterGPUUtilization,
		pendingPodsCount,
		underutilizedNodesCount,
		spotInterruptionsTotal,
		spotTerminationWarnings,
		spotInstanceSavings,
		estimatedMonthlyCost,
		estimatedMonthlySavings,
		predictiveScalingEnabled,
		predictedUtilization,
		predictionConfidence,
		scaleUpCooldownRemaining,
		scaleDownCooldownRemaining,
		multiTierScalingEnabled,
		capacityTypePreference,
		reconcileErrorsTotal,
		reconcileDuration,
		lastReconcileTimestamp,
	)
}

// MetricsRecorder records autoscaling metrics
type MetricsRecorder struct{}

// NewMetricsRecorder creates a new metrics recorder
func NewMetricsRecorder() *MetricsRecorder {
	return &MetricsRecorder{}
}

// RecordScalingAction records a scaling action
func (m *MetricsRecorder) RecordScalingAction(action ScalingAction, capacityType string, success bool) {
	successStr := "false"
	if success {
		successStr = "true"
	}
	scalingActionsTotal.WithLabelValues(string(action), capacityType, successStr).Inc()
}

// RecordScalingDuration records the duration of a scaling action
func (m *MetricsRecorder) RecordScalingDuration(action ScalingAction, durationSeconds float64) {
	scalingDuration.WithLabelValues(string(action)).Observe(durationSeconds)
}

// RecordNodeCount records the current node count by capacity type
func (m *MetricsRecorder) RecordNodeCount(capacityType string, count int) {
	nodeCount.WithLabelValues(capacityType).Set(float64(count))
}

// RecordDesiredNodeCount records the desired node count
func (m *MetricsRecorder) RecordDesiredNodeCount(count int) {
	desiredNodeCount.Set(float64(count))
}

// RecordClusterUtilization records the cluster GPU utilization
func (m *MetricsRecorder) RecordClusterUtilization(utilization float64) {
	clusterGPUUtilization.Set(utilization)
}

// RecordPendingPods records the number of pending GPU pods
func (m *MetricsRecorder) RecordPendingPods(count int) {
	pendingPodsCount.Set(float64(count))
}

// RecordUnderutilizedNodes records the number of underutilized nodes
func (m *MetricsRecorder) RecordUnderutilizedNodes(count int) {
	underutilizedNodesCount.Set(float64(count))
}

// RecordSpotInterruption records a spot instance interruption
func (m *MetricsRecorder) RecordSpotInterruption() {
	spotInterruptionsTotal.Inc()
}

// RecordSpotTerminationWarnings records the number of active termination warnings
func (m *MetricsRecorder) RecordSpotTerminationWarnings(count int) {
	spotTerminationWarnings.Set(float64(count))
}

// RecordSpotSavings records the estimated savings from spot instances
func (m *MetricsRecorder) RecordSpotSavings(savingsPercentage float64) {
	spotInstanceSavings.Set(savingsPercentage)
}

// RecordEstimatedCost records the estimated monthly cost
func (m *MetricsRecorder) RecordEstimatedCost(capacityType string, costUSD float64) {
	estimatedMonthlyCost.WithLabelValues(capacityType).Set(costUSD)
}

// RecordEstimatedSavings records the estimated monthly savings
func (m *MetricsRecorder) RecordEstimatedSavings(savingsUSD float64) {
	estimatedMonthlySavings.Set(savingsUSD)
}

// RecordPredictiveScaling records predictive scaling metrics
func (m *MetricsRecorder) RecordPredictiveScaling(enabled bool, predicted, confidence float64) {
	if enabled {
		predictiveScalingEnabled.Set(1)
	} else {
		predictiveScalingEnabled.Set(0)
	}
	predictedUtilization.Set(predicted)
	predictionConfidence.Set(confidence)
}

// RecordCooldown records remaining cooldown time
func (m *MetricsRecorder) RecordCooldown(scaleUp bool, remainingSeconds float64) {
	if scaleUp {
		scaleUpCooldownRemaining.Set(remainingSeconds)
	} else {
		scaleDownCooldownRemaining.Set(remainingSeconds)
	}
}

// RecordMultiTierScaling records multi-tier scaling metrics
func (m *MetricsRecorder) RecordMultiTierScaling(enabled bool) {
	if enabled {
		multiTierScalingEnabled.Set(1)
	} else {
		multiTierScalingEnabled.Set(0)
	}
}

// RecordCapacityTypePreference records capacity type preference
func (m *MetricsRecorder) RecordCapacityTypePreference(capacityType string, score float64) {
	capacityTypePreference.WithLabelValues(capacityType).Set(score)
}

// RecordReconcileError records a reconciliation error
func (m *MetricsRecorder) RecordReconcileError() {
	reconcileErrorsTotal.Inc()
}

// RecordReconcileDuration records the reconciliation duration
func (m *MetricsRecorder) RecordReconcileDuration(durationSeconds float64) {
	reconcileDuration.Observe(durationSeconds)
	lastReconcileTimestamp.SetToCurrentTime()
}
