package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/gpuautoscaler/gpuautoscaler/pkg/metrics"
)

type OptimizeOptions struct {
	Namespace       string
	LookbackMinutes int
	MinWasteScore   float64
	streams         genericclioptions.IOStreams
}

// NewOptimizeCmd creates the optimize command
func NewOptimizeCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := &OptimizeOptions{
		streams:         streams,
		LookbackMinutes: 10,
		MinWasteScore:   50.0,
	}

	cmd := &cobra.Command{
		Use:   "optimize",
		Short: "Analyze GPU waste and provide optimization recommendations",
		Long: `Analyze GPU utilization patterns and identify optimization opportunities:
- Underutilized GPUs that could be shared
- Workloads suitable for MIG (Multi-Instance GPU)
- Workloads suitable for MPS (Multi-Process Service)
- Potential cost savings from optimization`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.Run()
		},
	}

	cmd.Flags().StringVarP(&o.Namespace, "namespace", "n", "", "Filter by namespace (all namespaces if not specified)")
	cmd.Flags().IntVar(&o.LookbackMinutes, "lookback", 10, "Minutes of historical data to analyze")
	cmd.Flags().Float64Var(&o.MinWasteScore, "min-waste-score", 50.0, "Minimum waste score to report (0-100)")

	return cmd
}

func (o *OptimizeOptions) Run() error {
	// Get Prometheus URL
	promURL := os.Getenv("PROMETHEUS_URL")
	if promURL == "" {
		promURL = "http://prometheus-operated.gpu-autoscaler-system.svc:9090"
	}

	// Create metrics collector
	collector := metrics.NewCollector(promURL)
	ctx := context.Background()

	// Get waste metrics
	wasteMetrics, err := collector.GetWasteMetrics(ctx, o.LookbackMinutes)
	if err != nil {
		return fmt.Errorf("failed to get waste metrics: %w", err)
	}

	// Filter by namespace and waste score
	filtered := []metrics.WasteMetrics{}
	for _, w := range wasteMetrics {
		if o.Namespace != "" && w.PodNamespace != o.Namespace {
			continue
		}
		if w.WasteScore >= o.MinWasteScore {
			filtered = append(filtered, w)
		}
	}

	// Print results
	fmt.Fprintf(o.streams.Out, "\n=== GPU Optimization Opportunities ===\n\n")
	fmt.Fprintf(o.streams.Out, "Analysis period: last %d minutes\n", o.LookbackMinutes)
	fmt.Fprintf(o.streams.Out, "Minimum waste score: %.0f\n\n", o.MinWasteScore)

	if len(filtered) == 0 {
		fmt.Fprintf(o.streams.Out, "✅ No significant optimization opportunities found.\n")
		fmt.Fprintf(o.streams.Out, "Your cluster is well-utilized!\n")
		return nil
	}

	fmt.Fprintf(o.streams.Out, "Found %d optimization opportunities:\n\n", len(filtered))

	// Create table writer
	w := tabwriter.NewWriter(o.streams.Out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAMESPACE\tPOD\tWASTE SCORE\tGPU UTIL\tMEM UTIL\tMONTHLY COST\tRECOMMENDATION")
	fmt.Fprintln(w, "---------\t---\t-----------\t--------\t--------\t------------\t--------------")

	totalMonthlyCost := 0.0
	potentialSavings := 0.0

	for _, waste := range filtered {
		fmt.Fprintf(w, "%s\t%s\t%.0f\t%.1f%%\t%.1f%%\t$%.2f\t%s\n",
			waste.PodNamespace,
			waste.PodName,
			waste.WasteScore,
			waste.AvgUtilization,
			waste.AvgMemoryUtil,
			waste.EstimatedMonthlyCost,
			waste.Recommendation,
		)
		totalMonthlyCost += waste.EstimatedMonthlyCost
		// Assume 30-50% savings through optimization
		potentialSavings += waste.EstimatedMonthlyCost * 0.4
	}

	w.Flush()

	// Print summary
	fmt.Fprintf(o.streams.Out, "\n=== Optimization Summary ===\n")
	fmt.Fprintf(o.streams.Out, "Total monthly cost (underutilized workloads): $%.2f\n", totalMonthlyCost)
	fmt.Fprintf(o.streams.Out, "Potential monthly savings: $%.2f (40%% average)\n", potentialSavings)
	fmt.Fprintf(o.streams.Out, "Annual savings potential: $%.2f\n\n", potentialSavings*12)

	// Print actionable steps
	fmt.Fprintf(o.streams.Out, "💡 Next Steps:\n")
	fmt.Fprintf(o.streams.Out, "1. Enable GPU sharing by adding annotation: gpu-autoscaler.io/sharing=enabled\n")
	fmt.Fprintf(o.streams.Out, "2. For MIG-capable GPUs (A100/H100), configure MIG profiles\n")
	fmt.Fprintf(o.streams.Out, "3. Enable the admission webhook to automatically optimize new workloads\n")
	fmt.Fprintf(o.streams.Out, "4. Monitor results with: gpu-autoscaler status\n")

	return nil
}
