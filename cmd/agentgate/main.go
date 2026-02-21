package main

import (
	"fmt"
	"os"

	"agentgate/internal/agentgate"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(agentgate.RunCLI(nil))
	}
	if os.Args[1] == "__intercept" {
		os.Exit(runIntercept(os.Args[2:]))
	}
	os.Exit(agentgate.RunCLI(os.Args[1:]))
}

func runIntercept(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "agentgate __intercept: missing tool")
		return 1
	}
	tool := args[0]
	var raw []string
	if len(args) > 1 {
		if args[1] == "--" {
			raw = args[2:]
		} else {
			raw = args[1:]
		}
	}
	paths, err := agentgate.ResolvePaths()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate __intercept: %v\n", err)
		return 1
	}
	return agentgate.Intercept(paths, tool, raw)
}
