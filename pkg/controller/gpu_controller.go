package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/gpuautoscaler/gpuautoscaler/pkg/metrics"
	"github.com/gpuautoscaler/gpuautoscaler/pkg/scheduler"
	"github.com/gpuautoscaler/gpuautoscaler/pkg/sharing"
)

const (
	// Annotations for GPU autoscaling
	AnnotationGPUSharing    = "gpu-autoscaler.io/sharing"
	AnnotationSpotOk        = "gpu-autoscaler.io/spot-ok"
	AnnotationCostCenter    = "gpu-autoscaler.io/cost-center"
	AnnotationPriority      = "gpu-autoscaler.io/priority"
	AnnotationMIGProfile    = "gpu-autoscaler.io/mig-profile"
	AnnotationMPSEnabled    = "gpu-autoscaler.io/mps-enabled"
	AnnotationOriginalGPUs  = "gpu-autoscaler.io/original-gpu-request"

	// Reconciliation interval
	ReconcileInterval = 30 * time.Second
)

// GPUController reconciles GPU pods and optimizes their placement
type GPUController struct {
	client.Client
	Scheme           *runtime.Scheme
	Log              logr.Logger
	MetricsCollector *metrics.Collector
	Scheduler        *scheduler.BinPackingScheduler
	MIGManager       *sharing.MIGManager
	MPSManager       *sharing.MPSManager
	TSManager        *sharing.TimeSlicingManager
	EnableBinPacking bool
	EnableMIG        bool
	EnableMPS        bool
	EnableTS         bool
}

// NewGPUController creates a new GPU controller
func NewGPUController(client client.Client, log logr.Logger, metricsCollector *metrics.Collector) *GPUController {
	return &GPUController{
		Client:           client,
		Log:              log,
		MetricsCollector: metricsCollector,
		Scheduler:        scheduler.NewBinPackingScheduler(client, scheduler.BestFit),
		MIGManager:       sharing.NewMIGManager(client),
		MPSManager:       sharing.NewMPSManager(client),
		TSManager:        sharing.NewTimeSlicingManager(client),
		EnableBinPacking: true,
		EnableMIG:        false, // Disabled by default, enable via Helm values
		EnableMPS:        false,
		EnableTS:         false,
	}
}

// Reconcile handles GPU pod events
func (r *GPUController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("pod", req.NamespacedName)

	// Get the pod
	pod := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, pod); err != nil {
		// Pod might have been deleted
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Only process pods with GPU requests
	if !hasGPURequest(pod) {
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling GPU pod")

	// Analyze GPU utilization
	if err := r.analyzeGPUUtilization(ctx, pod); err != nil {
		log.Error(err, "Failed to analyze GPU utilization")
		return ctrl.Result{RequeueAfter: ReconcileInterval}, nil
	}

	// Check for optimization opportunities
	if err := r.checkOptimizationOpportunities(ctx, pod); err != nil {
		log.Error(err, "Failed to check optimization opportunities")
		return ctrl.Result{RequeueAfter: ReconcileInterval}, nil
	}

	// Requeue after interval for continuous monitoring
	return ctrl.Result{RequeueAfter: ReconcileInterval}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *GPUController) SetupWithManager(mgr ctrl.Manager) error {
	// Create a predicate to filter GPU pods
	gpuPodPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			pod, ok := e.Object.(*corev1.Pod)
			return ok && hasGPURequest(pod)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			pod, ok := e.ObjectNew.(*corev1.Pod)
			return ok && hasGPURequest(pod)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			pod, ok := e.Object.(*corev1.Pod)
			return ok && hasGPURequest(pod)
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithEventFilter(gpuPodPredicate).
		Complete(r)
}

// analyzeGPUUtilization retrieves and logs GPU metrics for a pod
func (r *GPUController) analyzeGPUUtilization(ctx context.Context, pod *corev1.Pod) error {
	// Skip if pod is not running
	if pod.Status.Phase != corev1.PodRunning {
		return nil
	}

	// Get GPU metrics for this pod
	// This is a simplified version - in production, we'd maintain a cache of metrics
	allMetrics, err := r.MetricsCollector.GetGPUMetrics(ctx)
	if err != nil {
		return fmt.Errorf("failed to get GPU metrics: %w", err)
	}

	// Find metrics for this pod
	for _, metric := range allMetrics {
		if metric.PodName == pod.Name && metric.PodNamespace == pod.Namespace {
			r.Log.Info("GPU metrics",
				"pod", pod.Name,
				"gpuUtil", fmt.Sprintf("%.1f%%", metric.GPUUtilization),
				"memUtil", fmt.Sprintf("%.1f%%", metric.GPUMemoryUtil),
				"memUsed", fmt.Sprintf("%.0fMB", metric.GPUMemoryUsed),
				"power", fmt.Sprintf("%.0fW", metric.GPUPowerUsage),
				"temp", fmt.Sprintf("%.0fC", metric.GPUTemperature),
			)
		}
	}

	return nil
}

// checkOptimizationOpportunities identifies if a pod can benefit from GPU sharing
func (r *GPUController) checkOptimizationOpportunities(ctx context.Context, pod *corev1.Pod) error {
	// Skip if pod explicitly opts out of sharing
	if pod.Annotations[AnnotationGPUSharing] == "disabled" {
		return nil
	}

	// Skip if pod is not running long enough
	if pod.Status.Phase != corev1.PodRunning {
		return nil
	}

	// Check if pod has been running for at least 5 minutes
	startTime := pod.Status.StartTime
	if startTime == nil || time.Since(startTime.Time) < 5*time.Minute {
		return nil
	}

	// Get waste metrics for this pod
	wasteMetrics, err := r.MetricsCollector.GetWasteMetrics(ctx, 10) // 10 minute lookback
	if err != nil {
		return fmt.Errorf("failed to get waste metrics: %w", err)
	}

	// Find waste metrics for this pod
	for _, waste := range wasteMetrics {
		if waste.PodName == pod.Name && waste.PodNamespace == pod.Namespace {
			if waste.WasteScore > 50 { // Significant waste
				r.Log.Info("Optimization opportunity detected",
					"pod", pod.Name,
					"wasteScore", fmt.Sprintf("%.1f", waste.WasteScore),
					"avgGPUUtil", fmt.Sprintf("%.1f%%", waste.AvgUtilization),
					"avgMemUtil", fmt.Sprintf("%.1f%%", waste.AvgMemoryUtil),
					"recommendation", waste.Recommendation,
					"monthlyCost", fmt.Sprintf("$%.2f", waste.EstimatedMonthlyCost),
				)

				// Create an event to notify the user
				r.createOptimizationEvent(ctx, pod, waste)
			}
		}
	}

	return nil
}

// createOptimizationEvent creates a Kubernetes event with optimization recommendations
func (r *GPUController) createOptimizationEvent(ctx context.Context, pod *corev1.Pod, waste metrics.WasteMetrics) {
	// In a real implementation, we would create a Kubernetes Event object
	// For now, just log the recommendation
	r.Log.Info("Creating optimization recommendation event",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"recommendation", waste.Recommendation,
	)

	// TODO: Create actual Kubernetes Event
	// event := &corev1.Event{
	//     ObjectMeta: metav1.ObjectMeta{
	//         Name:      fmt.Sprintf("%s.optimization.%d", pod.Name, time.Now().Unix()),
	//         Namespace: pod.Namespace,
	//     },
	//     InvolvedObject: corev1.ObjectReference{
	//         Kind:      "Pod",
	//         Name:      pod.Name,
	//         Namespace: pod.Namespace,
	//         UID:       pod.UID,
	//     },
	//     Reason:  "OptimizationOpportunity",
	//     Message: waste.Recommendation,
	//     Type:    "Normal",
	// }
}

// hasGPURequest checks if a pod requests GPU resources
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

// GetGPUCount returns the number of GPUs requested by a pod
func GetGPUCount(pod *corev1.Pod) int {
	count := 0
	for _, container := range pod.Spec.Containers {
		if gpus, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
			count += int(gpus.Value())
		}
	}
	if count == 0 {
		for _, container := range pod.Spec.Containers {
			if gpus, ok := container.Resources.Limits["nvidia.com/gpu"]; ok {
				count += int(gpus.Value())
			}
		}
	}
	return count
}

// RunBinPackingAnalysis runs the bin-packing algorithm to consolidate workloads
func (r *GPUController) RunBinPackingAnalysis(ctx context.Context) error {
	if !r.EnableBinPacking {
		return nil
	}

	r.Log.Info("Running bin-packing analysis")

	result, err := r.Scheduler.PackWorkloads(ctx)
	if err != nil {
		return fmt.Errorf("failed to pack workloads: %w", err)
	}

	r.Log.Info("Bin-packing analysis complete",
		"consolidatedPods", result.ConsolidatedPods,
		"savedGPUs", result.SavedGPUs,
		"placements", len(result.Placements))

	return nil
}

// RunConsolidationAnalysis analyzes consolidation opportunities
func (r *GPUController) RunConsolidationAnalysis(ctx context.Context) error {
	if !r.EnableBinPacking {
		return nil
	}

	r.Log.Info("Running consolidation analysis")

	report, err := r.Scheduler.AnalyzeConsolidationOpportunities(ctx)
	if err != nil {
		return fmt.Errorf("failed to analyze consolidation: %w", err)
	}

	if report.UnderutilizedNodes > 0 {
		r.Log.Info("Consolidation opportunities found",
			"underutilizedNodes", report.UnderutilizedNodes,
			"potentialSavings", report.PotentialSavings,
			"recommendations", len(report.Recommendations))

		for _, rec := range report.Recommendations {
			r.Log.Info("Consolidation recommendation", "message", rec)
		}
	}

	return nil
}

// EstimateSharingS avings estimates savings from GPU sharing mechanisms
func (r *GPUController) EstimateSharingSavings(ctx context.Context) error {
	r.Log.Info("Estimating GPU sharing savings")

	// MIG savings
	if r.EnableMIG {
		migReport, err := r.MIGManager.EstimateMIGSavings(ctx)
		if err != nil {
			r.Log.Error(err, "Failed to estimate MIG savings")
		} else if migReport.MIGEligiblePods > 0 {
			r.Log.Info("MIG savings estimate",
				"totalPods", migReport.TotalPods,
				"eligiblePods", migReport.MIGEligiblePods,
				"potentialSavedGPUs", fmt.Sprintf("%.1f", migReport.PotentialSavedGPUs),
				"savingsPct", fmt.Sprintf("%.1f%%", migReport.EstimatedSavingsPct))
		}
	}

	// MPS savings
	if r.EnableMPS {
		mpsReport, err := r.MPSManager.EstimateMPSSavings(ctx)
		if err != nil {
			r.Log.Error(err, "Failed to estimate MPS savings")
		} else if mpsReport.MPSEligiblePods > 0 {
			r.Log.Info("MPS savings estimate",
				"totalPods", mpsReport.TotalPods,
				"eligiblePods", mpsReport.MPSEligiblePods,
				"potentialSavedGPUs", fmt.Sprintf("%.1f", mpsReport.PotentialSavedGPUs),
				"savingsPct", fmt.Sprintf("%.1f%%", mpsReport.EstimatedSavingsPct))
		}
	}

	// Time-slicing savings
	if r.EnableTS {
		tsReport, err := r.TSManager.EstimateTimeSlicingSavings(ctx, 4)
		if err != nil {
			r.Log.Error(err, "Failed to estimate time-slicing savings")
		} else if tsReport.TimeSlicingEligible > 0 {
			r.Log.Info("Time-slicing savings estimate",
				"totalPods", tsReport.TotalPods,
				"eligiblePods", tsReport.TimeSlicingEligible,
				"potentialSavedGPUs", fmt.Sprintf("%.1f", tsReport.PotentialSavedGPUs),
				"savingsPct", fmt.Sprintf("%.1f%%", tsReport.EstimatedSavingsPct))
		}
	}

	return nil
}
