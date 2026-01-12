package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

type CostOptions struct {
	Namespace string
	Last      string
	streams   genericclioptions.IOStreams
}

// NewCostCmd creates the cost command
func NewCostCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := &CostOptions{
		streams: streams,
		Last:    "7d",
	}

	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Show GPU cost breakdown and attribution",
		Long: `Display GPU costs attributed to:
- Namespaces / teams
- Individual pods / workloads
- Cost centers (via labels)
- Time periods`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.Run()
		},
	}

	cmd.Flags().StringVarP(&o.Namespace, "namespace", "n", "", "Filter by namespace")
	cmd.Flags().StringVar(&o.Last, "last", "7d", "Time period (e.g., 1h, 24h, 7d, 30d)")

	return cmd
}

func (o *CostOptions) Run() error {
	fmt.Fprintf(o.streams.Out, "\n=== GPU Cost Report ===\n\n")
	fmt.Fprintf(o.streams.Out, "Period: last %s\n", o.Last)

	if o.Namespace != "" {
		fmt.Fprintf(o.streams.Out, "Namespace: %s\n\n", o.Namespace)
	} else {
		fmt.Fprintf(o.streams.Out, "Scope: All namespaces\n\n")
	}

	// TODO: Implement actual cost calculation
	// For now, show placeholder data
	fmt.Fprintf(o.streams.Out, "⚠️  Cost tracking requires TimescaleDB to be configured.\n")
	fmt.Fprintf(o.streams.Out, "Run the following to enable cost tracking:\n")
	fmt.Fprintf(o.streams.Out, "  helm upgrade gpu-autoscaler charts/gpu-autoscaler --set cost.enabled=true\n\n")

	fmt.Fprintf(o.streams.Out, "Once enabled, you'll see:\n")
	fmt.Fprintf(o.streams.Out, "- Total GPU spend by namespace\n")
	fmt.Fprintf(o.streams.Out, "- Cost per pod/workload\n")
	fmt.Fprintf(o.streams.Out, "- Trend analysis (week-over-week)\n")
	fmt.Fprintf(o.streams.Out, "- Budget tracking and alerts\n")

	return nil
}
