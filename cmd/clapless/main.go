package main

import (
	"fmt"
	"os"

	"github.com/shidetake/clapless/internal/cli"
)

func main() {
	// Parse command-line flags
	config, err := cli.ParseFlags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		cli.PrintUsage()
		os.Exit(1)
	}

	// Run the synchronization workflow
	if err := cli.Run(config); err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
		os.Exit(1)
	}

	os.Exit(0)
}
