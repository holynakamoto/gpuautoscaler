package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// NewRootCmd creates the root command
func NewRootCmd(streams genericclioptions.IOStreams) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "gpu-autoscaler",
		Short: "GPU Autoscaler CLI - Optimize GPU cluster utilization and costs",
		Long: `GPU Autoscaler is a Kubernetes-native system that maximizes GPU cluster
utilization through intelligent workload packing, multi-tenancy support, and
cost-optimized autoscaling.

This CLI provides commands to monitor GPU usage, analyze waste, and optimize
GPU workload placement.`,
		SilenceUsage: true,
	}

	// Add subcommands
	rootCmd.AddCommand(NewStatusCmd(streams))
	rootCmd.AddCommand(NewOptimizeCmd(streams))
	rootCmd.AddCommand(NewCostCmd(streams))
	rootCmd.AddCommand(NewReportCmd(streams))
	rootCmd.AddCommand(NewVersionCmd(streams))

	return rootCmd
}
