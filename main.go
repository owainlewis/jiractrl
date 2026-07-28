package main

import (
	"fmt"
	"os"

	"github.com/owainlewis/jiractrl/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !cli.IsReported(err) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(cli.ExitCode(err))
	}
}
