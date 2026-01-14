package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/gpuautoscaler/gpuautoscaler/pkg/version"
)

func NewVersionCmd(streams genericclioptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the client version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(streams.Out, "GPU Autoscaler CLI\n")
			fmt.Fprintf(streams.Out, "  Version: %s\n", version.Version)
			fmt.Fprintf(streams.Out, "  Commit:  %s\n", version.Commit)
			fmt.Fprintf(streams.Out, "  Date:    %s\n", version.Date)
		},
	}
	return cmd
}
