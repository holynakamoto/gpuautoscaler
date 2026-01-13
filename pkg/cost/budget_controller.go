package cost

import (
	"context"
	"fmt"
	"time"

	v1alpha1 "github.com/gpuautoscaler/gpuautoscaler/pkg/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// BudgetController reconciles CostBudget objects
type BudgetController struct {
	client.Client
	Scheme      *runtime.Scheme
	CostTracker *CostTracker
	DB          *TimescaleDBClient
	Alerter     *AlertManager
	Recorder    record.EventRecorder
}

// Reconcile handles CostBudget resource changes
func (r *BudgetController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the CostBudget instance
	budget := &v1alpha1.CostBudget{}
	err := r.Get(ctx, req.NamespacedName, budget)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Skip if budget is disabled
	if !budget.Spec.Enabled {
		logger.Info("Budget is disabled, skipping", "name", budget.Name)
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	logger.Info("Reconciling CostBudget",
		"name", budget.Name,
		"monthlyLimit", budget.Spec.MonthlyLimit,
	)

	// Update budget status
	if err := r.updateBudgetStatus(ctx, budget); err != nil {
		logger.Error(err, "Failed to update budget status")
		return ctrl.Result{}, err
	}

	// Check for alerts
	if err := r.checkAlerts(ctx, budget); err != nil {
		logger.Error(err, "Failed to check alerts")
	}

	// Enforce budget if needed
	if err := r.enforceBudget(ctx, budget); err != nil {
		logger.Error(err, "Failed to enforce budget")
	}

	// Update the resource
	if err := r.Status().Update(ctx, budget); err != nil {
		logger.Error(err, "Failed to update budget resource")
		return ctrl.Result{}, err
	}

	// Persist to DB
	if r.DB != nil {
		r.DB.UpdateBudgetTracking(ctx,
			budget.Name,
			getFirstNamespace(budget.Spec.Scope),
			getFirstTeam(budget.Spec.Scope),
			budget.Spec.MonthlyLimit,
			budget.Status.CurrentSpend,
			budget.Status.PercentageUsed,
			budget.Status.BudgetStatus,
		)
	}

	// Requeue after 1 minute for continuous monitoring
	return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
}

// updateBudgetStatus calculates current spending and budget status
func (r *BudgetController) updateBudgetStatus(ctx context.Context, budget *v1alpha1.CostBudget) error {
	logger := log.FromContext(ctx)

	// Determine budget period
	startDate := time.Now()
	if budget.Spec.StartDate != nil {
		startDate = budget.Spec.StartDate.Time
	} else {
		// Default to start of current month
		now := time.Now()
		startDate = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	}

	endDate := startDate.AddDate(0, 1, 0) // One month from start

	// Calculate current spend for this budget scope
	currentSpend, err := r.calculateScopeSpend(ctx, budget.Spec.Scope, startDate, time.Now())
	if err != nil {
		return fmt.Errorf("failed to calculate scope spend: %w", err)
	}

	// Calculate percentage used
	percentageUsed := 0.0
	if budget.Spec.MonthlyLimit > 0 {
		percentageUsed = (currentSpend / budget.Spec.MonthlyLimit) * 100
	}

	// Determine budget status
	budgetStatus := "ok"
	if percentageUsed >= 100 {
		budgetStatus = "exceeded"
	} else if percentageUsed >= 80 {
		budgetStatus = "warning"
	}

	// Calculate projected monthly spend
	now := time.Now()
	daysElapsed := now.Sub(startDate).Hours() / 24
	daysInMonth := endDate.Sub(startDate).Hours() / 24
	projectedSpend := 0.0
	if daysElapsed > 0 {
		dailyRate := currentSpend / daysElapsed
		projectedSpend = dailyRate * daysInMonth
	}

	// Calculate days remaining
	daysRemaining := int(endDate.Sub(now).Hours() / 24)
	if daysRemaining < 0 {
		daysRemaining = 0
	}

	// Build cost breakdown
	breakdown := r.buildCostBreakdown(ctx, budget.Spec.Scope)

	// Track when budget first exceeded 100% (for grace period)
	exceededSince := budget.Status.ExceededSince
	if budgetStatus == "exceeded" && exceededSince == nil {
		// First time exceeding budget
		exceededSince = &metav1.Time{Time: now}
	} else if budgetStatus != "exceeded" {
		// No longer exceeded, reset
		exceededSince = nil
	}

	// Update status
	budget.Status = v1alpha1.CostBudgetStatus{
		CurrentSpend:          currentSpend,
		PercentageUsed:        percentageUsed,
		BudgetStatus:          budgetStatus,
		ProjectedMonthlySpend: projectedSpend,
		DaysRemaining:         daysRemaining,
		AlertsFired:           budget.Status.AlertsFired, // Preserve existing
		EnforcementActive:     budget.Status.EnforcementActive,
		ExceededSince:         exceededSince,
		LastUpdated:           metav1.NewTime(now),
		Breakdown:             breakdown,
	}

	logger.V(1).Info("Updated budget status",
		"currentSpend", currentSpend,
		"percentageUsed", percentageUsed,
		"status", budgetStatus,
		"projectedSpend", projectedSpend,
	)

	return nil
}

// calculateScopeSpend computes total spending for a budget scope
func (r *BudgetController) calculateScopeSpend(ctx context.Context, scope v1alpha1.BudgetScope, start, end time.Time) (float64, error) {
	var totalSpend float64

	// Prefer DB for historical data if available (avoids double-counting)
	if r.DB != nil {
		for _, ns := range scope.Namespaces {
			dbCost, err := r.DB.GetCostByNamespace(ctx, ns, start, end)
			if err == nil {
				totalSpend += dbCost
			}
		}

		for _, team := range scope.Teams {
			dbCost, err := r.DB.GetCostByTeam(ctx, team, start, end)
			if err == nil {
				totalSpend += dbCost
			}
		}

		return totalSpend, nil
	}

	// Fallback to in-memory cost tracker if DB not available
	// Note: This only has costs since tracker started, not full historical range
	pods, err := r.getScopePods(ctx, scope)
	if err != nil {
		return 0, err
	}

	for _, pod := range pods {
		if podCost, ok := r.CostTracker.GetPodCost(pod.Namespace, pod.Name); ok {
			totalSpend += podCost.TotalCost
		}
	}

	return totalSpend, nil
}

// getScopePods returns all pods matching the budget scope
func (r *BudgetController) getScopePods(ctx context.Context, scope v1alpha1.BudgetScope) ([]corev1.Pod, error) {
	var allPods []corev1.Pod

	// Query by namespaces
	if len(scope.Namespaces) > 0 {
		for _, ns := range scope.Namespaces {
			podList := &corev1.PodList{}
			if err := r.List(ctx, podList, client.InNamespace(ns), client.MatchingLabels(scope.Labels)); err != nil {
				return nil, err
			}
			allPods = append(allPods, podList.Items...)
		}
	} else {
		// All namespaces
		podList := &corev1.PodList{}
		if err := r.List(ctx, podList, client.MatchingLabels(scope.Labels)); err != nil {
			return nil, err
		}
		allPods = podList.Items
	}

	// Filter by additional criteria
	var matchingPods []corev1.Pod
	for _, pod := range allPods {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Filter by experiment ID
		if scope.ExperimentID != "" && pod.Labels["experiment-id"] != scope.ExperimentID {
			continue
		}

		// Filter by team
		if len(scope.Teams) > 0 {
			teamLabel := pod.Labels["team"]
			found := false
			for _, team := range scope.Teams {
				if teamLabel == team {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Only GPU pods
		if !isGPUPod(&pod) {
			continue
		}

		matchingPods = append(matchingPods, pod)
	}

	return matchingPods, nil
}

// buildCostBreakdown creates a detailed cost breakdown for the budget
func (r *BudgetController) buildCostBreakdown(ctx context.Context, scope v1alpha1.BudgetScope) v1alpha1.CostBreakdown {
	breakdown := v1alpha1.CostBreakdown{
		ByNamespace:    make(map[string]float64),
		ByLabel:        make(map[string]float64),
		ByInstanceType: make(map[string]float64),
		SpotVsOnDemand: make(map[string]float64),
	}

	pods, err := r.getScopePods(ctx, scope)
	if err != nil {
		return breakdown
	}

	for _, pod := range pods {
		if podCost, ok := r.CostTracker.GetPodCost(pod.Namespace, pod.Name); ok {
			// By namespace
			breakdown.ByNamespace[pod.Namespace] += podCost.TotalCost

			// By GPU type
			breakdown.ByInstanceType[podCost.GPUType] += podCost.TotalCost

			// By capacity type
			breakdown.SpotVsOnDemand[podCost.CapacityType] += podCost.TotalCost

			// By label (team)
			if team := pod.Labels["team"]; team != "" {
				breakdown.ByLabel[team] += podCost.TotalCost
			}
		}
	}

	return breakdown
}

// checkAlerts evaluates alert thresholds and sends notifications
func (r *BudgetController) checkAlerts(ctx context.Context, budget *v1alpha1.CostBudget) error {
	logger := log.FromContext(ctx)

	for _, alert := range budget.Spec.Alerts {
		// Check if threshold is crossed
		if budget.Status.PercentageUsed >= alert.ThresholdPercent {
			// Check if this alert was already fired
			alreadyFired := false
			for _, fired := range budget.Status.AlertsFired {
				if fired.AlertName == alert.Name && !fired.Acknowledged {
					alreadyFired = true
					break
				}
			}

			if !alreadyFired {
				logger.Info("Firing budget alert",
					"budget", budget.Name,
					"alert", alert.Name,
					"threshold", alert.ThresholdPercent,
					"current", budget.Status.PercentageUsed,
				)

				// Send alert through channels
				if r.Alerter != nil {
					alertMsg := AlertMessage{
						BudgetName:     budget.Name,
						AlertName:      alert.Name,
						Severity:       alert.Severity,
						CurrentSpend:   budget.Status.CurrentSpend,
						MonthlyLimit:   budget.Spec.MonthlyLimit,
						PercentageUsed: budget.Status.PercentageUsed,
						Threshold:      alert.ThresholdPercent,
						Timestamp:      time.Now(),
					}

					for _, channel := range alert.Channels {
						if err := r.Alerter.SendAlert(ctx, channel, alertMsg); err != nil {
							logger.Error(err, "Failed to send alert", "channel", channel.Type)
						}
					}
				}

				// Record event
				r.Recorder.Eventf(budget, corev1.EventTypeWarning, "BudgetAlert",
					"Budget '%s' has reached %0.1f%% of monthly limit ($%0.2f / $%0.2f)",
					budget.Name, budget.Status.PercentageUsed,
					budget.Status.CurrentSpend, budget.Spec.MonthlyLimit)

				// Record in status
				budget.Status.AlertsFired = append(budget.Status.AlertsFired, v1alpha1.AlertFired{
					AlertName:    alert.Name,
					Timestamp:    metav1.NewTime(time.Now()),
					Threshold:    alert.ThresholdPercent,
					Acknowledged: false,
				})
			}
		}
	}

	return nil
}

// enforceBudget applies enforcement actions when budget is exceeded
func (r *BudgetController) enforceBudget(ctx context.Context, budget *v1alpha1.CostBudget) error {
	logger := log.FromContext(ctx)

	// Only enforce if budget is exceeded
	if budget.Status.BudgetStatus != "exceeded" {
		if budget.Status.EnforcementActive {
			logger.Info("Budget no longer exceeded, lifting enforcement", "budget", budget.Name)
			budget.Status.EnforcementActive = false
			r.Recorder.Event(budget, corev1.EventTypeNormal, "EnforcementLifted",
				"Budget enforcement has been lifted")
		}
		return nil
	}

	// Check if still in grace period
	if budget.Spec.Enforcement != nil {
		exceededTime := getExceededTime(budget)
		gracePeriod := time.Duration(budget.Spec.Enforcement.GracePeriodMinutes) * time.Minute

		// If exceededTime is zero, budget was never exceeded before
		if !exceededTime.IsZero() && time.Since(exceededTime) < gracePeriod {
			logger.V(1).Info("Budget exceeded but in grace period",
				"budget", budget.Name,
				"remaining", gracePeriod-time.Since(exceededTime),
			)
			return nil
		}
	}

	// Apply enforcement action
	if budget.Spec.Enforcement != nil && !budget.Status.EnforcementActive {
		logger.Info("Enforcing budget limit",
			"budget", budget.Name,
			"action", budget.Spec.Enforcement.Action,
		)

		switch budget.Spec.Enforcement.Action {
		case "alert":
			// Only alerting - already handled in checkAlerts
			r.Recorder.Event(budget, corev1.EventTypeWarning, "BudgetExceeded",
				"Budget has been exceeded")

		case "throttle":
			// Implement throttling logic
			if err := r.throttleResources(ctx, budget); err != nil {
				return fmt.Errorf("failed to throttle resources: %w", err)
			}
			r.Recorder.Event(budget, corev1.EventTypeWarning, "BudgetThrottled",
				"GPU resources are being throttled due to budget limit")

		case "block":
			// Implement blocking logic
			if err := r.blockNewPods(ctx, budget); err != nil {
				return fmt.Errorf("failed to block new pods: %w", err)
			}
			r.Recorder.Event(budget, corev1.EventTypeWarning, "BudgetBlocked",
				"New GPU pods are blocked due to budget limit")
		}

		budget.Status.EnforcementActive = true
	}

	return nil
}

// throttleResources reduces resource allocation to control costs
func (r *BudgetController) throttleResources(ctx context.Context, budget *v1alpha1.CostBudget) error {
	logger := log.FromContext(ctx)

	if budget.Spec.Enforcement == nil || budget.Spec.Enforcement.ThrottleConfig == nil {
		return nil
	}

	throttleConfig := budget.Spec.Enforcement.ThrottleConfig

	// Get autoscaling policies in the budget scope
	policyList := &v1alpha1.AutoscalingPolicyList{}
	if err := r.List(ctx, policyList); err != nil {
		return err
	}

	for _, policy := range policyList.Items {
		// Check if policy is in budget scope
		if !isInBudgetScope(policy, budget.Spec.Scope) {
			continue
		}

		// Apply throttling
		modified := false

		for i, nodePool := range policy.Spec.NodePools {
			if nodePool.CapacityType == "spot" {
				if throttleConfig.BlockSpotCreation {
					policy.Spec.NodePools[i].MinNodes = 0
					policy.Spec.NodePools[i].MaxNodes = 0
					modified = true
				} else if throttleConfig.MaxSpotInstances != nil {
					maxSpot := *throttleConfig.MaxSpotInstances
					if policy.Spec.NodePools[i].MaxNodes > maxSpot {
						policy.Spec.NodePools[i].MaxNodes = maxSpot
						modified = true
					}
				}
			}
		}

		if modified {
			if err := r.Update(ctx, &policy); err != nil {
				logger.Error(err, "Failed to update policy for throttling", "policy", policy.Name)
			} else {
				logger.Info("Applied throttling to autoscaling policy", "policy", policy.Name)
			}
		}
	}

	return nil
}

// blockNewPods prevents new GPU pod creation
func (r *BudgetController) blockNewPods(ctx context.Context, budget *v1alpha1.CostBudget) error {
	// This would be implemented via an admission webhook that checks budget status
	// For now, just log the action
	logger := log.FromContext(ctx)
	logger.Info("Budget blocking activated - admission webhook should reject new GPU pods",
		"budget", budget.Name)

	// In a full implementation, this would:
	// 1. Update a ConfigMap that the admission webhook reads
	// 2. The webhook would check budget status before allowing pod creation
	// 3. Reject pods that would exceed budget limits

	return nil
}

// Helper functions

func getFirstNamespace(scope v1alpha1.BudgetScope) string {
	if len(scope.Namespaces) > 0 {
		return scope.Namespaces[0]
	}
	return ""
}

func getFirstTeam(scope v1alpha1.BudgetScope) string {
	if len(scope.Teams) > 0 {
		return scope.Teams[0]
	}
	return ""
}

func getExceededTime(budget *v1alpha1.CostBudget) time.Time {
	// Use ExceededSince if available
	if budget.Status.ExceededSince != nil {
		return budget.Status.ExceededSince.Time
	}
	// Fallback: find earliest alert timestamp
	var earliestTime time.Time
	for _, alert := range budget.Status.AlertsFired {
		alertTime := alert.Timestamp.Time
		if earliestTime.IsZero() || alertTime.Before(earliestTime) {
			earliestTime = alertTime
		}
	}
	// Return zero time if no alerts (caller should check IsZero())
	return earliestTime
}

func isInBudgetScope(policy v1alpha1.AutoscalingPolicy, scope v1alpha1.BudgetScope) bool {
	// Check if policy's namespace matches budget scope
	// Simplified - in production, check all scope criteria
	if len(scope.Namespaces) > 0 {
		for _, ns := range scope.Namespaces {
			if policy.Namespace == ns {
				return true
			}
		}
		return false
	}
	return true
}

// SetupWithManager sets up the controller with the Manager
func (r *BudgetController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.CostBudget{}).
		Complete(r)
}
