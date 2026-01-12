package cost

import (
	"context"
	"fmt"
	"time"

	v1alpha1 "github.com/holynakamoto/gpuautoscaler/pkg/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// AttributionController reconciles CostAttribution objects
type AttributionController struct {
	client.Client
	Scheme      *runtime.Scheme
	CostTracker *CostTracker
	DB          *TimescaleDBClient
}

// Reconcile handles CostAttribution resource changes
func (r *AttributionController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the CostAttribution instance
	attribution := &v1alpha1.CostAttribution{}
	err := r.Get(ctx, req.NamespacedName, attribution)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling CostAttribution",
		"name", attribution.Name,
		"namespace", attribution.Spec.Namespace,
		"team", attribution.Spec.Team,
	)

	// Update cost attribution status
	if err := r.updateAttributionStatus(ctx, attribution); err != nil {
		logger.Error(err, "Failed to update attribution status")
		return ctrl.Result{}, err
	}

	// Update the resource
	if err := r.Status().Update(ctx, attribution); err != nil {
		logger.Error(err, "Failed to update attribution resource")
		return ctrl.Result{}, err
	}

	// Requeue after 1 minute for continuous updates
	return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
}

// updateAttributionStatus calculates and updates cost attribution metrics
func (r *AttributionController) updateAttributionStatus(ctx context.Context, attribution *v1alpha1.CostAttribution) error {
	logger := log.FromContext(ctx)

	// Get all matching pods
	pods, err := r.getMatchingPods(ctx, attribution)
	if err != nil {
		return fmt.Errorf("failed to get matching pods: %w", err)
	}

	// Calculate current metrics
	var totalCost float64
	var hourlyRate float64
	var activePods int
	var activeGPUs int
	var gpuHours float64
	podCosts := make(map[string]v1alpha1.PodCostInfo)

	for _, pod := range pods {
		podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
		podCost, ok := r.CostTracker.GetPodCost(pod.Namespace, pod.Name)
		if !ok {
			logger.V(1).Info("Pod cost not found in tracker", "pod", podKey)
			continue
		}

		totalCost += podCost.TotalCost
		hourlyRate += podCost.HourlyRate
		activePods++
		activeGPUs += podCost.GPUCount

		// Calculate GPU hours for this pod
		elapsed := time.Since(podCost.StartTime)
		gpuHours += elapsed.Hours() * float64(podCost.GPUCount)

		// Store pod cost info
		podCosts[pod.Name] = v1alpha1.PodCostInfo{
			PodName:      pod.Name,
			GPUType:      podCost.GPUType,
			GPUCount:     podCost.GPUCount,
			StartTime:    metav1.NewTime(podCost.StartTime),
			Cost:         podCost.TotalCost,
			HourlyRate:   podCost.HourlyRate,
			CapacityType: podCost.CapacityType,
			SharingMode:  podCost.SharingMode,
			Node:         podCost.Node,
		}
	}

	// Calculate daily and monthly costs from DB
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	dailyCost := totalCost // Simplified - in production, query DB for accurate daily cost
	if r.DB != nil {
		if attribution.Spec.Namespace != "" {
			dailyCost, _ = r.DB.GetCostByNamespace(ctx, attribution.Spec.Namespace, startOfDay, now)
		} else if attribution.Spec.Team != "" {
			dailyCost, _ = r.DB.GetCostByTeam(ctx, attribution.Spec.Team, startOfDay, now)
		}
	}

	monthlyCost := totalCost // Simplified
	if r.DB != nil {
		if attribution.Spec.Namespace != "" {
			monthlyCost, _ = r.DB.GetCostByNamespace(ctx, attribution.Spec.Namespace, startOfMonth, now)
		} else if attribution.Spec.Team != "" {
			monthlyCost, _ = r.DB.GetCostByTeam(ctx, attribution.Spec.Team, startOfMonth, now)
		}
	}

	// Calculate cost per GPU hour
	costPerGPUHour := 0.0
	if gpuHours > 0 {
		costPerGPUHour = totalCost / gpuHours
	}

	// Get historical data and savings
	historicalData, err := r.getHistoricalData(ctx, attribution)
	if err != nil {
		logger.Error(err, "Failed to get historical data")
		historicalData = []v1alpha1.CostDataPoint{}
	}

	savings, err := r.calculateSavings(ctx, attribution)
	if err != nil {
		logger.Error(err, "Failed to calculate savings")
		savings = v1alpha1.SavingsData{}
	}

	// Build detailed breakdown
	breakdown := r.buildDetailedBreakdown(ctx, pods, podCosts)

	// Update status
	attribution.Status = v1alpha1.CostAttributionStatus{
		TotalCost:      totalCost,
		DailyCost:      dailyCost,
		MonthlyCost:    monthlyCost,
		HourlyCost:     hourlyRate,
		ActivePods:     activePods,
		ActiveGPUs:     activeGPUs,
		GPUHours:       gpuHours,
		CostPerGPUHour: costPerGPUHour,
		LastUpdated:    metav1.NewTime(now),
		DetailedBreakdown: breakdown,
		HistoricalData: historicalData,
		Savings:        savings,
	}

	logger.V(1).Info("Updated attribution status",
		"totalCost", totalCost,
		"dailyCost", dailyCost,
		"monthlyCost", monthlyCost,
		"activePods", activePods,
		"activeGPUs", activeGPUs,
	)

	return nil
}

// getMatchingPods returns pods that match the attribution criteria
func (r *AttributionController) getMatchingPods(ctx context.Context, attribution *v1alpha1.CostAttribution) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}

	// Build list options based on attribution spec
	listOpts := []client.ListOption{
		client.MatchingLabels(attribution.Spec.Labels),
	}

	if attribution.Spec.Namespace != "" {
		listOpts = append(listOpts, client.InNamespace(attribution.Spec.Namespace))
	}

	if err := r.List(ctx, podList, listOpts...); err != nil {
		return nil, err
	}

	// Filter by additional criteria
	var matchingPods []corev1.Pod
	for _, pod := range podList.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Check experiment ID
		if attribution.Spec.ExperimentID != "" {
			if pod.Labels["experiment-id"] != attribution.Spec.ExperimentID {
				continue
			}
		}

		// Check team
		if attribution.Spec.Team != "" {
			if pod.Labels["team"] != attribution.Spec.Team {
				continue
			}
		}

		// Check project
		if attribution.Spec.Project != "" {
			if pod.Labels["project"] != attribution.Spec.Project {
				continue
			}
		}

		// Check cost center
		if attribution.Spec.CostCenter != "" {
			if pod.Labels["cost-center"] != attribution.Spec.CostCenter {
				continue
			}
		}

		// Check custom tags
		if len(attribution.Spec.Tags) > 0 {
			matches := true
			for key, value := range attribution.Spec.Tags {
				if pod.Labels[key] != value {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
		}

		// Only include GPU pods
		if !isGPUPod(&pod) {
			continue
		}

		matchingPods = append(matchingPods, pod)
	}

	return matchingPods, nil
}

// buildDetailedBreakdown creates a detailed cost breakdown
func (r *AttributionController) buildDetailedBreakdown(ctx context.Context, pods []corev1.Pod, podCosts map[string]v1alpha1.PodCostInfo) v1alpha1.DetailedBreakdown {
	breakdown := v1alpha1.DetailedBreakdown{
		ByPod:          podCosts,
		ByGPUType:      make(map[string]float64),
		ByCapacityType: make(map[string]float64),
		ByNode:         make(map[string]float64),
		ByHour:         []v1alpha1.HourlyCost{},
		ByDay:          []v1alpha1.DailyCost{},
	}

	// Aggregate by GPU type, capacity type, and node
	for _, info := range podCosts {
		breakdown.ByGPUType[info.GPUType] += info.Cost
		breakdown.ByCapacityType[info.CapacityType] += info.Cost
		breakdown.ByNode[info.Node] += info.Cost
	}

	// Get time-series data from DB (last 24 hours)
	now := time.Now()
	for i := 23; i >= 0; i-- {
		hour := now.Add(time.Duration(-i) * time.Hour)
		// Simplified - in production, query DB for actual hourly costs
		breakdown.ByHour = append(breakdown.ByHour, v1alpha1.HourlyCost{
			Timestamp: metav1.NewTime(hour),
			Cost:      0.0, // Would query from DB
			GPUHours:  0.0,
		})
	}

	// Get daily data (last 30 days)
	for i := 29; i >= 0; i-- {
		day := now.AddDate(0, 0, -i)
		breakdown.ByDay = append(breakdown.ByDay, v1alpha1.DailyCost{
			Date:     day.Format("2006-01-02"),
			Cost:     0.0, // Would query from DB
			GPUHours: 0.0,
			PeakGPUs: 0,
		})
	}

	return breakdown
}

// getHistoricalData retrieves time-series cost data
func (r *AttributionController) getHistoricalData(ctx context.Context, attribution *v1alpha1.CostAttribution) ([]v1alpha1.CostDataPoint, error) {
	if r.DB == nil {
		return []v1alpha1.CostDataPoint{}, nil
	}

	// Get last 7 days of data points (hourly)
	var points []v1alpha1.CostDataPoint

	// Simplified - in production, query TimescaleDB for actual time series
	now := time.Now()
	for i := 168; i >= 0; i-- { // 7 days * 24 hours
		timestamp := now.Add(time.Duration(-i) * time.Hour)
		points = append(points, v1alpha1.CostDataPoint{
			Timestamp: metav1.NewTime(timestamp),
			Cost:      0.0, // Would query from DB
			Rate:      0.0,
			GPUs:      0,
			Pods:      0,
		})
	}

	return points, nil
}

// calculateSavings computes cost savings from optimizations
func (r *AttributionController) calculateSavings(ctx context.Context, attribution *v1alpha1.CostAttribution) (v1alpha1.SavingsData, error) {
	savings := v1alpha1.SavingsData{}

	if r.DB == nil {
		return savings, nil
	}

	// Get savings data from last 30 days
	start := time.Now().AddDate(0, 0, -30)
	end := time.Now()

	savingsMap, err := r.DB.GetTotalSavings(ctx, start, end)
	if err != nil {
		return savings, err
	}

	// Aggregate savings by type
	for optimType, amount := range savingsMap {
		savings.TotalSavings += amount
		switch optimType {
		case "spot":
			savings.SpotSavings = amount
		case "sharing":
			savings.SharingSavings = amount
		case "autoscaling":
			savings.AutoscalingSavings = amount
		case "waste":
			savings.WasteEliminated = amount
		}
	}

	// Calculate baseline cost (what it would have been without optimization)
	savings.BaselineCost = attribution.Status.TotalCost + savings.TotalSavings

	// Calculate savings percentage
	if savings.BaselineCost > 0 {
		savings.SavingsPercentage = (savings.TotalSavings / savings.BaselineCost) * 100
	}

	return savings, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *AttributionController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.CostAttribution{}).
		Complete(r)
}
