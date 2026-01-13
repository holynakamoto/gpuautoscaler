package cost

import (
	"context"
	"fmt"
	"time"

	v1alpha1 "github.com/gpuautoscaler/gpuautoscaler/pkg/apis/v1alpha1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ROIReporter calculates cost savings and return on investment
type ROIReporter struct {
	k8sClient   client.Client
	clientset   kubernetes.Interface
	costTracker *CostTracker
	db          *TimescaleDBClient
}

// ROIReport contains comprehensive savings and ROI data
type ROIReport struct {
	Period            ReportPeriod
	TotalSavings      float64
	SavingsBreakdown  SavingsBreakdown
	ActualCost        float64
	BaselineCost      float64
	SavingsPercentage float64
	ROIMetrics        ROIMetrics
	Optimizations     []OptimizationImpact
	Recommendations   []CostRecommendation
}

// ReportPeriod defines the time range for the report
type ReportPeriod struct {
	StartDate time.Time
	EndDate   time.Time
	Duration  time.Duration
	Label     string // "Last 7 Days", "Last 30 Days", etc.
}

// SavingsBreakdown categorizes savings by optimization type
type SavingsBreakdown struct {
	SpotInstanceSavings    float64
	GPUSharingSavings      float64
	AutoscalingSavings     float64
	WasteEliminationSavings float64
	IdleResourceSavings    float64
}

// ROIMetrics provides investment return calculations
type ROIMetrics struct {
	InvestmentCost         float64 // Cost of running autoscaler
	MonthlySavings         float64
	ROIPercentage          float64
	PaybackPeriodDays      int
	ProjectedAnnualSavings float64
}

// OptimizationImpact shows the effect of a specific optimization
type OptimizationImpact struct {
	Type           string
	Description    string
	SavingsAmount  float64
	ResourcesImpacted int
	Timestamp      time.Time
}

// CostRecommendation suggests ways to reduce costs further
type CostRecommendation struct {
	Priority      string // high, medium, low
	Type          string
	Description   string
	EstimatedSavings float64
	ImplementationEffort string // low, medium, high
}

// NewROIReporter creates a new ROI reporter
func NewROIReporter(k8sClient client.Client, clientset kubernetes.Interface, costTracker *CostTracker, db *TimescaleDBClient) *ROIReporter {
	return &ROIReporter{
		k8sClient:   k8sClient,
		clientset:   clientset,
		costTracker: costTracker,
		db:          db,
	}
}

// GenerateReport creates a comprehensive ROI report
func (r *ROIReporter) GenerateReport(ctx context.Context, period ReportPeriod) (*ROIReport, error) {
	logger := log.FromContext(ctx)
	logger.Info("Generating ROI report", "period", period.Label)

	report := &ROIReport{
		Period: period,
	}

	// Calculate savings from each optimization type
	savings, err := r.calculateTotalSavings(ctx, period)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate savings: %w", err)
	}
	report.SavingsBreakdown = savings
	report.TotalSavings = savings.SpotInstanceSavings +
		savings.GPUSharingSavings +
		savings.AutoscalingSavings +
		savings.WasteEliminationSavings +
		savings.IdleResourceSavings

	// Calculate actual vs baseline costs
	actualCost, baselineCost, err := r.calculateCosts(ctx, period, report.TotalSavings)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate costs: %w", err)
	}
	report.ActualCost = actualCost
	report.BaselineCost = baselineCost

	// Calculate savings percentage
	if report.BaselineCost > 0 {
		report.SavingsPercentage = (report.TotalSavings / report.BaselineCost) * 100
	}

	// Calculate ROI metrics
	report.ROIMetrics = r.calculateROIMetrics(report)

	// Get optimization impacts
	report.Optimizations = r.getOptimizationImpacts(ctx, period)

	// Generate recommendations
	report.Recommendations = r.generateRecommendations(ctx, report)

	return report, nil
}

// calculateTotalSavings computes savings from all optimization types
func (r *ROIReporter) calculateTotalSavings(ctx context.Context, period ReportPeriod) (SavingsBreakdown, error) {
	savings := SavingsBreakdown{}

	if r.db == nil {
		// Without DB, estimate from current state
		return r.estimateSavingsFromCurrentState(ctx)
	}

	// Query savings data from TimescaleDB
	dbSavings, err := r.db.GetTotalSavings(ctx, period.StartDate, period.EndDate)
	if err != nil {
		return savings, err
	}

	// Map database savings to breakdown
	for optimType, amount := range dbSavings {
		switch optimType {
		case "spot":
			savings.SpotInstanceSavings = amount
		case "sharing":
			savings.GPUSharingSavings = amount
		case "autoscaling":
			savings.AutoscalingSavings = amount
		case "waste":
			savings.WasteEliminationSavings = amount
		case "idle":
			savings.IdleResourceSavings = amount
		}
	}

	return savings, nil
}

// estimateSavingsFromCurrentState estimates savings without historical data
func (r *ROIReporter) estimateSavingsFromCurrentState(ctx context.Context) (SavingsBreakdown, error) {
	savings := SavingsBreakdown{}

	// Calculate spot savings
	spotSavings, onDemandCost := r.calculateSpotSavings(ctx)
	savings.SpotInstanceSavings = spotSavings

	// Calculate sharing savings
	sharingSavings := r.calculateSharingSavings(ctx)
	savings.GPUSharingSavings = sharingSavings

	// Estimate autoscaling savings (typically 30-40% from dynamic scaling)
	savings.AutoscalingSavings = onDemandCost * 0.35

	// Estimate waste elimination (from idle GPU detection)
	savings.WasteEliminationSavings = onDemandCost * 0.15

	return savings, nil
}

// calculateSpotSavings determines savings from spot instance usage
func (r *ROIReporter) calculateSpotSavings(ctx context.Context) (savings, onDemandEquivalent float64) {
	var spotCost float64
	var spotGPUs int

	// Iterate through all pods to find spot instances
	r.costTracker.podCostCache.Range(func(key, value interface{}) bool {
		podCost := value.(*PodCost)
		if podCost.CapacityType == "spot" {
			spotCost += podCost.TotalCost
			spotGPUs += podCost.GPUCount
		}
		return true
	})

	// Calculate what these GPUs would cost on-demand (typically 60-70% savings with spot)
	// Spot provides 65% discount on average
	onDemandEquivalent = spotCost / 0.35 // If spot is 35% of on-demand
	savings = onDemandEquivalent - spotCost

	return savings, onDemandEquivalent
}

// calculateSharingSavings determines savings from GPU sharing (MIG/MPS/time-slicing)
func (r *ROIReporter) calculateSharingSavings(ctx context.Context) float64 {
	var sharedGPUCost float64
	var sharedGPUCount int

	r.costTracker.podCostCache.Range(func(key, value interface{}) bool {
		podCost := value.(*PodCost)
		if podCost.SharingMode != "exclusive" {
			sharedGPUCost += podCost.TotalCost
			sharedGPUCount += podCost.GPUCount
		}
		return true
	})

	// Without sharing, would need 4x more GPUs (average consolidation ratio)
	exclusiveEquivalent := sharedGPUCost * 4
	savings := exclusiveEquivalent - sharedGPUCost

	return savings
}

// calculateCosts returns actual and baseline costs
func (r *ROIReporter) calculateCosts(ctx context.Context, period ReportPeriod, totalSavings float64) (actual, baseline float64, err error) {
	// Actual cost is total spent
	actual = r.costTracker.GetTotalCost()

	// Baseline is what it would have cost without optimizations
	baseline = actual + totalSavings

	return actual, baseline, nil
}

// calculateROIMetrics computes return on investment metrics
func (r *ROIReporter) calculateROIMetrics(report *ROIReport) ROIMetrics {
	metrics := ROIMetrics{}

	// Estimate investment cost (autoscaler infrastructure)
	// Controller pods, TimescaleDB, monitoring - roughly $200-500/month
	metrics.InvestmentCost = 300.0

	// Calculate monthly savings (extrapolate from report period)
	daysInPeriod := report.Period.Duration.Hours() / 24
	if daysInPeriod > 0 {
		metrics.MonthlySavings = (report.TotalSavings / daysInPeriod) * 30
	}

	// Calculate ROI percentage
	if metrics.InvestmentCost > 0 {
		metrics.ROIPercentage = ((metrics.MonthlySavings - metrics.InvestmentCost) / metrics.InvestmentCost) * 100
	}

	// Calculate payback period
	if metrics.MonthlySavings > metrics.InvestmentCost {
		netMonthlySavings := metrics.MonthlySavings - metrics.InvestmentCost
		if netMonthlySavings > 0 {
			metrics.PaybackPeriodDays = int((metrics.InvestmentCost / netMonthlySavings) * 30)
		}
	}

	// Project annual savings
	metrics.ProjectedAnnualSavings = metrics.MonthlySavings * 12

	return metrics
}

// getOptimizationImpacts returns specific optimization actions and their impact
func (r *ROIReporter) getOptimizationImpacts(ctx context.Context, period ReportPeriod) []OptimizationImpact {
	impacts := []OptimizationImpact{}

	// Get autoscaling policies
	policyList := &v1alpha1.AutoscalingPolicyList{}
	if err := r.k8sClient.List(ctx, policyList); err == nil {
		for _, policy := range policyList.Items {
			// Count spot nodes
			spotNodes := 0
			for _, pool := range policy.Spec.NodePools {
				if pool.CapacityType == "spot" {
					spotNodes = policy.Status.SpotNodes
				}
			}

			if spotNodes > 0 {
				impacts = append(impacts, OptimizationImpact{
					Type:              "spot-instances",
					Description:       fmt.Sprintf("Using %d spot GPU nodes in policy %s", spotNodes, policy.Name),
					SavingsAmount:     0, // Would calculate from actual costs
					ResourcesImpacted: spotNodes,
					Timestamp:         time.Now(),
				})
			}
		}
	}

	// Get GPU sharing policies
	sharingList := &v1alpha1.GPUSharingPolicyList{}
	if err := r.k8sClient.List(ctx, sharingList); err == nil {
		for _, policy := range sharingList.Items {
			impacts = append(impacts, OptimizationImpact{
				Type:              "gpu-sharing",
				Description:       fmt.Sprintf("GPU sharing enabled (%s) via policy %s", policy.Spec.Strategy, policy.Name),
				SavingsAmount:     0, // Would calculate
				ResourcesImpacted: 0,
				Timestamp:         time.Now(),
			})
		}
	}

	return impacts
}

// generateRecommendations provides actionable cost reduction suggestions
func (r *ROIReporter) generateRecommendations(ctx context.Context, report *ROIReport) []CostRecommendation {
	recommendations := []CostRecommendation{}

	// Analyze current utilization and costs
	hourlyRate := r.costTracker.GetHourlyRate()

	// Recommendation: Increase spot instance usage
	if report.SavingsBreakdown.SpotInstanceSavings < report.ActualCost*0.3 {
		recommendations = append(recommendations, CostRecommendation{
			Priority:             "high",
			Type:                 "spot-instances",
			Description:          "Increase spot instance usage to 60-70% of GPU fleet for non-critical workloads",
			EstimatedSavings:     report.ActualCost * 0.35,
			ImplementationEffort: "low",
		})
	}

	// Recommendation: Enable GPU sharing
	if report.SavingsBreakdown.GPUSharingSavings < report.ActualCost*0.2 {
		recommendations = append(recommendations, CostRecommendation{
			Priority:             "high",
			Type:                 "gpu-sharing",
			Description:          "Enable MIG or MPS for inference workloads to consolidate multiple jobs per GPU",
			EstimatedSavings:     report.ActualCost * 0.40,
			ImplementationEffort: "medium",
		})
	}

	// Recommendation: Implement autoscaling
	if report.SavingsBreakdown.AutoscalingSavings < report.ActualCost*0.15 {
		recommendations = append(recommendations, CostRecommendation{
			Priority:             "medium",
			Type:                 "autoscaling",
			Description:          "Enable predictive autoscaling to scale down during off-peak hours",
			EstimatedSavings:     report.ActualCost * 0.25,
			ImplementationEffort: "low",
		})
	}

	// Recommendation: Eliminate idle GPUs
	if report.SavingsBreakdown.WasteEliminationSavings < hourlyRate*0.1 {
		recommendations = append(recommendations, CostRecommendation{
			Priority:             "high",
			Type:                 "idle-detection",
			Description:          "Identify and terminate idle GPU pods (< 20% utilization)",
			EstimatedSavings:     hourlyRate * 730 * 0.15, // Monthly
			ImplementationEffort: "low",
		})
	}

	// Recommendation: Use reserved instances for baseline workloads
	if report.ActualCost > 10000 { // Only for significant spend
		recommendations = append(recommendations, CostRecommendation{
			Priority:             "medium",
			Type:                 "reserved-instances",
			Description:          "Purchase reserved instances for 20-30% of baseline GPU capacity (1-3 year commitment)",
			EstimatedSavings:     report.ActualCost * 0.15,
			ImplementationEffort: "medium",
		})
	}

	// Recommendation: Optimize GPU selection
	recommendations = append(recommendations, CostRecommendation{
		Priority:             "low",
		Type:                 "right-sizing",
		Description:          "Review workloads using high-end GPUs (A100/H100) that could run on cheaper alternatives (T4/L4)",
		EstimatedSavings:     report.ActualCost * 0.10,
		ImplementationEffort: "high",
	})

	return recommendations
}

// FormatReport generates a human-readable report
func (r *ROIReporter) FormatReport(report *ROIReport) string {
	return fmt.Sprintf(`
=================================================================
                GPU AUTOSCALER ROI REPORT
=================================================================

Period: %s (%s to %s)

-----------------------------------------------------------------
COST SUMMARY
-----------------------------------------------------------------
Actual Cost:        $%0.2f
Baseline Cost:      $%0.2f
Total Savings:      $%0.2f (%.1f%%)

-----------------------------------------------------------------
SAVINGS BREAKDOWN
-----------------------------------------------------------------
Spot Instances:     $%0.2f
GPU Sharing:        $%0.2f
Autoscaling:        $%0.2f
Waste Elimination:  $%0.2f
Idle Resources:     $%0.2f

-----------------------------------------------------------------
ROI METRICS
-----------------------------------------------------------------
Monthly Investment:         $%0.2f
Monthly Savings:            $%0.2f
ROI Percentage:             %.1f%%
Payback Period:             %d days
Projected Annual Savings:   $%0.2f

-----------------------------------------------------------------
RECOMMENDATIONS
-----------------------------------------------------------------
%s

=================================================================
`,
		report.Period.Label,
		report.Period.StartDate.Format("2006-01-02"),
		report.Period.EndDate.Format("2006-01-02"),
		report.ActualCost,
		report.BaselineCost,
		report.TotalSavings,
		report.SavingsPercentage,
		report.SavingsBreakdown.SpotInstanceSavings,
		report.SavingsBreakdown.GPUSharingSavings,
		report.SavingsBreakdown.AutoscalingSavings,
		report.SavingsBreakdown.WasteEliminationSavings,
		report.SavingsBreakdown.IdleResourceSavings,
		report.ROIMetrics.InvestmentCost,
		report.ROIMetrics.MonthlySavings,
		report.ROIMetrics.ROIPercentage,
		report.ROIMetrics.PaybackPeriodDays,
		report.ROIMetrics.ProjectedAnnualSavings,
		r.formatRecommendations(report.Recommendations),
	)
}

func (r *ROIReporter) formatRecommendations(recommendations []CostRecommendation) string {
	if len(recommendations) == 0 {
		return "No additional recommendations at this time."
	}

	result := ""
	for i, rec := range recommendations {
		result += fmt.Sprintf("%d. [%s] %s\n   Estimated Savings: $%0.2f/month\n   Implementation Effort: %s\n\n",
			i+1,
			rec.Priority,
			rec.Description,
			rec.EstimatedSavings,
			rec.ImplementationEffort,
		)
	}
	return result
}
