package main

import (
	"fmt"
	"os"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/gpuautoscaler/gpuautoscaler/pkg/cli/cmd"
)

func main() {
	streams := genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	rootCmd := cmd.NewRootCmd(streams)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
