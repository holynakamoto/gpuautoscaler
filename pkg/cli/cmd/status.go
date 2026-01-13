package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/gpuautoscaler/gpuautoscaler/pkg/metrics"
)

type StatusOptions struct {
	Namespace string
	streams   genericclioptions.IOStreams
}

// NewStatusCmd creates the status command
func NewStatusCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := &StatusOptions{
		streams: streams,
	}

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show GPU cluster utilization status",
		Long: `Display current GPU utilization across the cluster, including:
- GPU utilization percentage
- VRAM usage
- Number of pods using GPUs
- Node-level GPU metrics`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.Run()
		},
	}

	cmd.Flags().StringVarP(&o.Namespace, "namespace", "n", "", "Filter by namespace (all namespaces if not specified)")

	return cmd
}

func (o *StatusOptions) Run() error {
	// Load kubeconfig
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	_, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Get Prometheus URL from environment or use default
	promURL := os.Getenv("PROMETHEUS_URL")
	if promURL == "" {
		promURL = "http://prometheus-operated.gpu-autoscaler-system.svc:9090"
	}

	// Create metrics collector
	collector := metrics.NewCollector(promURL)
	// Note: In real implementation, we'd use controller-runtime client
	// For CLI, we're using clientset directly
	ctx := context.Background()

	// Get GPU metrics
	gpuMetrics, err := collector.GetGPUMetrics(ctx)
	if err != nil {
		return fmt.Errorf("failed to get GPU metrics: %w", err)
	}

	// Filter by namespace if specified
	if o.Namespace != "" {
		filtered := []metrics.GPUMetrics{}
		for _, m := range gpuMetrics {
			if m.PodNamespace == o.Namespace {
				filtered = append(filtered, m)
			}
		}
		gpuMetrics = filtered
	}

	// Print results
	fmt.Fprintf(o.streams.Out, "\n=== GPU Cluster Status ===\n\n")
	fmt.Fprintf(o.streams.Out, "Total GPUs with workloads: %d\n\n", len(gpuMetrics))

	if len(gpuMetrics) == 0 {
		fmt.Fprintf(o.streams.Out, "No GPU workloads found.\n")
		return nil
	}

	// Create table writer
	w := tabwriter.NewWriter(o.streams.Out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAMESPACE\tPOD\tNODE\tGPU\tGPU UTIL\tMEM UTIL\tMEM USED\tPOWER\tTEMP")
	fmt.Fprintln(w, "---------\t---\t----\t---\t--------\t--------\t--------\t-----\t----")

	totalUtil := 0.0
	totalMemUtil := 0.0

	for _, m := range gpuMetrics {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%.1f%%\t%.1f%%\t%.0fMB\t%.0fW\t%.0fC\n",
			m.PodNamespace,
			m.PodName,
			m.NodeName,
			m.GPUIndex,
			m.GPUUtilization,
			m.GPUMemoryUtil,
			m.GPUMemoryUsed,
			m.GPUPowerUsage,
			m.GPUTemperature,
		)
		totalUtil += m.GPUUtilization
		totalMemUtil += m.GPUMemoryUtil
	}

	w.Flush()

	// Print summary statistics
	avgUtil := totalUtil / float64(len(gpuMetrics))
	avgMemUtil := totalMemUtil / float64(len(gpuMetrics))

	fmt.Fprintf(o.streams.Out, "\n=== Summary ===\n")
	fmt.Fprintf(o.streams.Out, "Average GPU Utilization: %.1f%%\n", avgUtil)
	fmt.Fprintf(o.streams.Out, "Average Memory Utilization: %.1f%%\n", avgMemUtil)

	// Provide recommendations
	if avgUtil < 50 {
		fmt.Fprintf(o.streams.Out, "\n💡 Cluster is underutilized. Run 'gpu-autoscaler optimize' for recommendations.\n")
	}

	return nil
}
