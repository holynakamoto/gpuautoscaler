package cmd

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gpuautoscaler/gpuautoscaler/pkg/apis/v1alpha1"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CostOptions struct {
	Namespace string
	Team      string
	Last      string
	Format    string
	ShowROI   bool
	streams   genericclioptions.IOStreams
	client    client.Client
}

// NewCostCmd creates the cost command
func NewCostCmd(streams genericclioptions.IOStreams, k8sClient client.Client) *cobra.Command {
	o := &CostOptions{
		streams: streams,
		client:  k8sClient,
		Last:    "7d",
		Format:  "summary",
	}

	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Show GPU cost breakdown and attribution",
		Long: `Display GPU costs attributed to:
- Namespaces / teams
- Individual pods / workloads
- Cost centers (via labels)
- Time periods

Examples:
  # Show cost summary for all namespaces
  gpu-autoscaler cost

  # Show costs for specific namespace
  gpu-autoscaler cost --namespace ml-training

  # Show costs for specific team
  gpu-autoscaler cost --team data-science

  # Show detailed cost breakdown
  gpu-autoscaler cost --format detailed

  # Show ROI report
  gpu-autoscaler cost --roi

  # Show last 30 days
  gpu-autoscaler cost --last 30d`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.Run()
		},
	}

	cmd.Flags().StringVarP(&o.Namespace, "namespace", "n", "", "Filter by namespace")
	cmd.Flags().StringVar(&o.Team, "team", "", "Filter by team")
	cmd.Flags().StringVar(&o.Last, "last", "7d", "Time period (e.g., 1h, 24h, 7d, 30d)")
	cmd.Flags().StringVar(&o.Format, "format", "summary", "Output format (summary, detailed, json)")
	cmd.Flags().BoolVar(&o.ShowROI, "roi", false, "Show ROI and savings analysis")

	return cmd
}

func (o *CostOptions) Run() error {
	ctx := context.Background()

	fmt.Fprintf(o.streams.Out, "\n")
	fmt.Fprintf(o.streams.Out, "=================================================================\n")
	fmt.Fprintf(o.streams.Out, "                     GPU COST REPORT\n")
	fmt.Fprintf(o.streams.Out, "=================================================================\n\n")

	// Show ROI report if requested
	if o.ShowROI {
		return o.showROIReport(ctx)
	}

	// Get cost attributions
	attributions := &v1alpha1.CostAttributionList{}
	listOpts := []client.ListOption{}

	if o.Namespace != "" {
		// Filter will be applied after fetching
		fmt.Fprintf(o.streams.Out, "Scope: Namespace '%s'\n", o.Namespace)
	} else if o.Team != "" {
		fmt.Fprintf(o.streams.Out, "Scope: Team '%s'\n", o.Team)
	} else {
		fmt.Fprintf(o.streams.Out, "Scope: All Namespaces\n")
	}
	fmt.Fprintf(o.streams.Out, "Period: Last %s\n\n", o.Last)

	if err := o.client.List(ctx, attributions, listOpts...); err != nil {
		fmt.Fprintf(o.streams.ErrOut, "Error fetching cost data: %v\n", err)
		fmt.Fprintf(o.streams.Out, "\n⚠️  Cost tracking may not be enabled or configured.\n")
		fmt.Fprintf(o.streams.Out, "To enable cost tracking:\n")
		fmt.Fprintf(o.streams.Out, "  helm upgrade gpu-autoscaler charts/gpu-autoscaler --set cost.enabled=true\n\n")
		return nil
	}

	// Filter attributions based on options
	filtered := o.filterAttributions(attributions.Items)

	if len(filtered) == 0 {
		fmt.Fprintf(o.streams.Out, "No cost data available for the specified criteria.\n\n")
		return nil
	}

	// Calculate totals
	var totalCost, totalDaily, totalMonthly, totalHourly float64
	var totalPods, totalGPUs int

	for _, attr := range filtered {
		totalCost += attr.Status.TotalCost
		totalDaily += attr.Status.DailyCost
		totalMonthly += attr.Status.MonthlyCost
		totalHourly += attr.Status.HourlyCost
		totalPods += attr.Status.ActivePods
		totalGPUs += attr.Status.ActiveGPUs
	}

	// Show summary
	o.showSummary(totalCost, totalDaily, totalMonthly, totalHourly, totalPods, totalGPUs)

	// Show breakdown by namespace/team
	if o.Format == "detailed" || o.Format == "summary" {
		o.showBreakdown(filtered)
	}

	// Show budgets status
	o.showBudgets(ctx)

	// Show savings
	o.showSavings(filtered)

	fmt.Fprintf(o.streams.Out, "\n")
	fmt.Fprintf(o.streams.Out, "=================================================================\n\n")

	return nil
}

func (o *CostOptions) filterAttributions(items []v1alpha1.CostAttribution) []v1alpha1.CostAttribution {
	var filtered []v1alpha1.CostAttribution

	for _, item := range items {
		// Filter by namespace
		if o.Namespace != "" && item.Spec.Namespace != o.Namespace {
			continue
		}

		// Filter by team
		if o.Team != "" && item.Spec.Team != o.Team {
			continue
		}

		filtered = append(filtered, item)
	}

	return filtered
}

func (o *CostOptions) showSummary(totalCost, dailyCost, monthlyCost, hourlyCost float64, pods, gpus int) {
	fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")
	fmt.Fprintf(o.streams.Out, "COST SUMMARY\n")
	fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")
	fmt.Fprintf(o.streams.Out, "Total Cost:        $%0.2f\n", totalCost)
	fmt.Fprintf(o.streams.Out, "Daily Cost:        $%0.2f\n", dailyCost)
	fmt.Fprintf(o.streams.Out, "Monthly Cost:      $%0.2f\n", monthlyCost)
	fmt.Fprintf(o.streams.Out, "Current Rate:      $%0.2f/hour\n", hourlyCost)
	fmt.Fprintf(o.streams.Out, "\n")
	fmt.Fprintf(o.streams.Out, "Active Resources:\n")
	fmt.Fprintf(o.streams.Out, "  Pods:            %d\n", pods)
	fmt.Fprintf(o.streams.Out, "  GPUs:            %d\n", gpus)
	fmt.Fprintf(o.streams.Out, "\n")
}

func (o *CostOptions) showBreakdown(attributions []v1alpha1.CostAttribution) {
	fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")
	fmt.Fprintf(o.streams.Out, "COST BREAKDOWN\n")
	fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")

	// Sort by monthly cost descending
	sort.Slice(attributions, func(i, j int) bool {
		return attributions[i].Status.MonthlyCost > attributions[j].Status.MonthlyCost
	})

	fmt.Fprintf(o.streams.Out, "%-30s %12s %12s %10s %8s\n",
		"NAME", "MONTHLY", "DAILY", "HOURLY", "GPUS")
	fmt.Fprintf(o.streams.Out, strings.Repeat("-", 80)+"\n")

	for _, attr := range attributions {
		name := attr.Name
		if attr.Spec.Namespace != "" {
			name = attr.Spec.Namespace
		} else if attr.Spec.Team != "" {
			name = attr.Spec.Team
		}

		if len(name) > 28 {
			name = name[:28] + ".."
		}

		fmt.Fprintf(o.streams.Out, "%-30s $%11.2f $%11.2f $%9.2f %8d\n",
			name,
			attr.Status.MonthlyCost,
			attr.Status.DailyCost,
			attr.Status.HourlyCost,
			attr.Status.ActiveGPUs,
		)
	}

	fmt.Fprintf(o.streams.Out, "\n")

	// Show top pod costs if detailed
	if o.Format == "detailed" && len(attributions) > 0 {
		o.showTopPods(attributions)
	}
}

func (o *CostOptions) showTopPods(attributions []v1alpha1.CostAttribution) {
	fmt.Fprintf(o.streams.Out, "Top 10 Most Expensive Pods:\n")
	fmt.Fprintf(o.streams.Out, "%-40s %12s %10s %15s\n",
		"POD", "COST", "HOURLY", "GPU TYPE")
	fmt.Fprintf(o.streams.Out, strings.Repeat("-", 80)+"\n")

	type podCost struct {
		name       string
		cost       float64
		hourly     float64
		gpuType    string
	}

	var allPods []podCost
	for _, attr := range attributions {
		for _, pod := range attr.Status.DetailedBreakdown.ByPod {
			allPods = append(allPods, podCost{
				name:    pod.PodName,
				cost:    pod.Cost,
				hourly:  pod.HourlyRate,
				gpuType: pod.GPUType,
			})
		}
	}

	// Sort by cost
	sort.Slice(allPods, func(i, j int) bool {
		return allPods[i].cost > allPods[j].cost
	})

	// Show top 10
	for i := 0; i < 10 && i < len(allPods); i++ {
		pod := allPods[i]
		name := pod.name
		if len(name) > 38 {
			name = name[:38] + ".."
		}
		fmt.Fprintf(o.streams.Out, "%-40s $%11.2f $%9.2f %15s\n",
			name, pod.cost, pod.hourly, pod.gpuType)
	}

	fmt.Fprintf(o.streams.Out, "\n")
}

func (o *CostOptions) showBudgets(ctx context.Context) {
	budgets := &v1alpha1.CostBudgetList{}
	if err := o.client.List(ctx, budgets); err != nil {
		return
	}

	if len(budgets.Items) == 0 {
		return
	}

	fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")
	fmt.Fprintf(o.streams.Out, "BUDGET STATUS\n")
	fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")
	fmt.Fprintf(o.streams.Out, "%-30s %12s %12s %10s %10s\n",
		"BUDGET", "LIMIT", "SPENT", "USED", "STATUS")
	fmt.Fprintf(o.streams.Out, strings.Repeat("-", 80)+"\n")

	for _, budget := range budgets.Items {
		name := budget.Name
		if len(name) > 28 {
			name = name[:28] + ".."
		}

		statusSymbol := "✓"
		if budget.Status.BudgetStatus == "warning" {
			statusSymbol = "⚠"
		} else if budget.Status.BudgetStatus == "exceeded" {
			statusSymbol = "✗"
		}

		fmt.Fprintf(o.streams.Out, "%-30s $%11.2f $%11.2f %9.1f%% %10s\n",
			name,
			budget.Spec.MonthlyLimit,
			budget.Status.CurrentSpend,
			budget.Status.PercentageUsed,
			statusSymbol+" "+budget.Status.BudgetStatus,
		)
	}

	fmt.Fprintf(o.streams.Out, "\n")
}

func (o *CostOptions) showSavings(attributions []v1alpha1.CostAttribution) {
	var totalSavings float64
	var spotSavings, sharingSavings, autoscalingSavings float64

	for _, attr := range attributions {
		totalSavings += attr.Status.Savings.TotalSavings
		spotSavings += attr.Status.Savings.SpotSavings
		sharingSavings += attr.Status.Savings.SharingSavings
		autoscalingSavings += attr.Status.Savings.AutoscalingSavings
	}

	if totalSavings > 0 {
		fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")
		fmt.Fprintf(o.streams.Out, "SAVINGS FROM OPTIMIZATIONS\n")
		fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")
		fmt.Fprintf(o.streams.Out, "Spot Instances:      $%0.2f\n", spotSavings)
		fmt.Fprintf(o.streams.Out, "GPU Sharing:         $%0.2f\n", sharingSavings)
		fmt.Fprintf(o.streams.Out, "Autoscaling:         $%0.2f\n", autoscalingSavings)
		fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")
		fmt.Fprintf(o.streams.Out, "Total Savings:       $%0.2f\n", totalSavings)
		fmt.Fprintf(o.streams.Out, "\n")
	}
}

func (o *CostOptions) showROIReport(ctx context.Context) error {
	fmt.Fprintf(o.streams.Out, "Generating ROI report for last %s...\n\n", o.Last)

	// Parse time period
	duration, err := parseDuration(o.Last)
	if err != nil {
		return fmt.Errorf("invalid time period: %w", err)
	}

	// Query cost attributions to aggregate savings
	attributions := &v1alpha1.CostAttributionList{}
	if err := o.client.List(ctx, attributions); err != nil {
		return fmt.Errorf("failed to query cost attributions: %w", err)
	}

	// Aggregate totals and savings
	var totalCost, spotSavings, sharingSavings, autoscalingSavings, wasteEliminated float64
	for _, attr := range attributions.Items {
		totalCost += attr.Status.TotalCost
		spotSavings += attr.Status.Savings.SpotSavings
		sharingSavings += attr.Status.Savings.SharingSavings
		autoscalingSavings += attr.Status.Savings.AutoscalingSavings
		wasteEliminated += attr.Status.Savings.WasteEliminated
	}

	totalSavings := spotSavings + sharingSavings + autoscalingSavings + wasteEliminated
	baselineCost := totalCost + totalSavings
	savingsPercentage := 0.0
	if baselineCost > 0 {
		savingsPercentage = (totalSavings / baselineCost) * 100
	}

	// Calculate ROI metrics
	investmentCost := 300.0 // Estimated monthly infrastructure cost
	daysInPeriod := duration.Hours() / 24
	monthlySavings := totalSavings
	if daysInPeriod > 0 && daysInPeriod < 30 {
		monthlySavings = (totalSavings / daysInPeriod) * 30
	}

	roiPercentage := 0.0
	paybackDays := 0
	if investmentCost > 0 {
		roiPercentage = ((monthlySavings - investmentCost) / investmentCost) * 100
		if monthlySavings > investmentCost {
			netMonthlySavings := monthlySavings - investmentCost
			if netMonthlySavings > 0 {
				paybackDays = int((investmentCost / netMonthlySavings) * 30)
			}
		}
	}
	projectedAnnualSavings := monthlySavings * 12

	// Display report
	fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")
	fmt.Fprintf(o.streams.Out, "ROI SUMMARY\n")
	fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")
	fmt.Fprintf(o.streams.Out, "Report Period:           Last %s\n", o.Last)
	fmt.Fprintf(o.streams.Out, "Actual Cost:             $%0.2f\n", totalCost)
	fmt.Fprintf(o.streams.Out, "Baseline Cost:           $%0.2f\n", baselineCost)
	fmt.Fprintf(o.streams.Out, "Total Savings:           $%0.2f (%.1f%%)\n", totalSavings, savingsPercentage)
	fmt.Fprintf(o.streams.Out, "\n")
	fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")
	fmt.Fprintf(o.streams.Out, "SAVINGS BREAKDOWN\n")
	fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")
	fmt.Fprintf(o.streams.Out, "Spot Instances:          $%0.2f\n", spotSavings)
	fmt.Fprintf(o.streams.Out, "GPU Sharing:             $%0.2f\n", sharingSavings)
	fmt.Fprintf(o.streams.Out, "Autoscaling:             $%0.2f\n", autoscalingSavings)
	fmt.Fprintf(o.streams.Out, "Waste Elimination:       $%0.2f\n", wasteEliminated)
	fmt.Fprintf(o.streams.Out, "\n")
	fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")
	fmt.Fprintf(o.streams.Out, "ROI METRICS\n")
	fmt.Fprintf(o.streams.Out, "-----------------------------------------------------------------\n")
	fmt.Fprintf(o.streams.Out, "Monthly Investment:      $%0.2f\n", investmentCost)
	fmt.Fprintf(o.streams.Out, "Monthly Savings:         $%0.2f\n", monthlySavings)
	fmt.Fprintf(o.streams.Out, "ROI Percentage:          %.1f%%\n", roiPercentage)
	fmt.Fprintf(o.streams.Out, "Payback Period:          %d days\n", paybackDays)
	fmt.Fprintf(o.streams.Out, "Projected Annual:        $%0.2f\n", projectedAnnualSavings)
	fmt.Fprintf(o.streams.Out, "\n")

	return nil
}

func parseDuration(s string) (time.Duration, error) {
	// Parse duration strings like "1h", "7d", "30d"
	if strings.HasSuffix(s, "d") {
		daysStr := strings.TrimSuffix(s, "d")
		daysFloat, err := strconv.ParseFloat(daysStr, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid day value %q: %w", daysStr, err)
		}
		return time.Duration(daysFloat * 24 * float64(time.Hour)), nil
	}
	return time.ParseDuration(s)
}
