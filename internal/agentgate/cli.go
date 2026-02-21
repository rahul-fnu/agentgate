package agentgate

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func RunCLI(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}
	paths, err := ResolvePaths()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate: %v\n", err)
		return 1
	}

	switch args[0] {
	case "init":
		return cmdInit(paths)
	case "status":
		return cmdStatus(paths)
	case "observe":
		return cmdMode(paths, ModeObserve)
	case "enforce":
		return cmdMode(paths, ModeEnforce)
	case "tail":
		return cmdTail(paths, args[1:])
	case "report":
		return cmdReport(paths, args[1:])
	case "allow-once":
		return cmdAllowOnce(paths, args[1:])
	case "uninstall":
		return cmdUninstall(paths)
	default:
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Println("AgentGate V1")
	fmt.Println("Usage:")
	fmt.Println("  agentgate init")
	fmt.Println("  agentgate status")
	fmt.Println("  agentgate observe")
	fmt.Println("  agentgate enforce")
	fmt.Println("  agentgate tail [--env production] [--tool kubectl] [--decision deny] [--follow=true]")
	fmt.Println("  agentgate report --last 7d")
	fmt.Println("  agentgate allow-once <command-id>")
	fmt.Println("  agentgate uninstall")
}

func cmdInit(paths Paths) int {
	if err := EnsureDirs(paths); err != nil {
		fmt.Fprintf(os.Stderr, "agentgate init: %v\n", err)
		return 1
	}
	cfg, err := EnsureConfig(paths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate init: %v\n", err)
		return 1
	}
	if _, err := os.Stat(paths.PoliciesPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(paths.PoliciesPath, []byte(starterPoliciesYAML), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "agentgate init: failed to write starter policies: %v\n", err)
			return 1
		}
	}
	if err := writeShims(paths); err != nil {
		fmt.Fprintf(os.Stderr, "agentgate init: failed to write shims: %v\n", err)
		return 1
	}

	fmt.Printf("Initialized AgentGate at %s\n", paths.Root)
	fmt.Printf("Mode: %s\n", cfg.Mode)
	fmt.Printf("Policies: %s\n", paths.PoliciesPath)
	fmt.Printf("Shims: %s\n", paths.BinDir)
	fmt.Println("Add this to your shell profile:")
	fmt.Printf("  export PATH=\"%s:$PATH\"\n", paths.BinDir)
	return 0
}

func writeShims(paths Paths) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	for _, tool := range tools {
		script := fmt.Sprintf("#!/usr/bin/env sh\nexec %q __intercept %s -- \"$@\"\n", exe, tool)
		target := filepath.Join(paths.BinDir, tool)
		if err := os.WriteFile(target, []byte(script), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func cmdStatus(paths Paths) int {
	cfg, err := LoadConfig(paths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate status: %v\n", err)
		return 1
	}
	policies, err := LoadPolicies(paths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate status: policy parse warning: %v\n", err)
	}

	fmt.Printf("Mode: %s\n", cfg.Mode)
	fmt.Printf("Root: %s\n", paths.Root)
	fmt.Printf("Policies loaded: %d\n", len(policies))
	fmt.Println("Shims:")
	for _, tool := range tools {
		p := filepath.Join(paths.BinDir, tool)
		_, err := os.Stat(p)
		if err == nil {
			fmt.Printf("  %s: present\n", tool)
		} else {
			fmt.Printf("  %s: missing\n", tool)
		}
	}
	lines, err := ReadLastLines(paths.EventsPath, 5)
	if err != nil {
		fmt.Println("Last events: none")
		return 0
	}
	fmt.Println("Last events:")
	for _, line := range lines {
		fmt.Printf("  %s\n", line)
	}
	return 0
}

func cmdMode(paths Paths, mode Mode) int {
	cfg, err := LoadConfig(paths)
	if err != nil {
		cfg = DefaultConfig()
	}
	cfg.Mode = mode
	if err := EnsureDirs(paths); err != nil {
		fmt.Fprintf(os.Stderr, "agentgate: %v\n", err)
		return 1
	}
	if err := SaveConfig(paths, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "agentgate: %v\n", err)
		return 1
	}
	fmt.Printf("AgentGate mode set to %s\n", mode)
	return 0
}

func cmdTail(paths Paths, args []string) int {
	fs := flag.NewFlagSet("tail", flag.ContinueOnError)
	env := fs.String("env", "", "filter environment")
	tool := fs.String("tool", "", "filter tool")
	decision := fs.String("decision", "", "filter decision")
	follow := fs.Bool("follow", true, "follow stream")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	if err := printExistingWithFilters(paths.EventsPath, *env, *tool, *decision); err != nil {
		fmt.Fprintf(os.Stderr, "agentgate tail: %v\n", err)
		return 1
	}
	if !*follow {
		return 0
	}
	return followFile(paths.EventsPath, *env, *tool, *decision)
}

func printExistingWithFilters(path, env, tool, decision string) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if matchEventFilters(line, env, tool, decision) {
			fmt.Println(line)
		}
	}
	return sc.Err()
}

func followFile(path, env, tool, decision string) int {
	offset := int64(0)
	if fi, err := os.Stat(path); err == nil {
		offset = fi.Size()
	}
	for {
		time.Sleep(750 * time.Millisecond)
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		if fi.Size() <= offset {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		_, _ = f.Seek(offset, 0)
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			if matchEventFilters(line, env, tool, decision) {
				fmt.Println(line)
			}
		}
		offset = fi.Size()
		_ = f.Close()
	}
}

func matchEventFilters(line, env, tool, decision string) bool {
	if env == "" && tool == "" && decision == "" {
		return true
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return false
	}
	if env != "" && Normalize(fmt.Sprintf("%v", m["env"])) != Normalize(env) {
		return false
	}
	if tool != "" && Normalize(fmt.Sprintf("%v", m["tool"])) != Normalize(tool) {
		return false
	}
	if decision != "" && Normalize(fmt.Sprintf("%v", m["decision"])) != Normalize(decision) {
		return false
	}
	return true
}

func cmdReport(paths Paths, args []string) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	last := fs.String("last", "7d", "time window (e.g. 7d, 24h)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	window, err := ParseLastDuration(*last)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate report: invalid --last %q\n", *last)
		return 1
	}
	start := time.Now().Add(-window)
	f, err := os.Open(paths.EventsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println("No events yet.")
			return 0
		}
		fmt.Fprintf(os.Stderr, "agentgate report: %v\n", err)
		return 1
	}
	defer f.Close()

	total := 0
	byDecision := map[string]int{}
	byTool := map[string]int{}
	byEnv := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev StartEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Phase != "start" || ev.TS.Before(start) {
			continue
		}
		total++
		byDecision[string(ev.Decision)]++
		byTool[ev.Tool]++
		byEnv[ev.Env]++
	}
	fmt.Printf("AgentGate report (last %s)\n", *last)
	fmt.Printf("  Total commands: %d\n", total)
	printSortedMap("Decisions", byDecision)
	printSortedMap("Tools", byTool)
	printSortedMap("Environments", byEnv)
	return 0
}

func printSortedMap(label string, m map[string]int) {
	fmt.Printf("  %s:\n", label)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("    %s: %d\n", k, m[k])
	}
}

func cmdAllowOnce(paths Paths, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: agentgate allow-once <command-id>")
		return 1
	}
	commandID := strings.TrimSpace(args[0])
	ev, err := FindStartEventByID(paths, commandID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate allow-once: %v\n", err)
		return 1
	}
	if ev == nil {
		fmt.Fprintf(os.Stderr, "agentgate allow-once: command-id not found: %s\n", commandID)
		return 1
	}
	token, err := IssueBypass(paths, commandID, ev.CmdHash, 5*time.Minute)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate allow-once: %v\n", err)
		return 1
	}
	fmt.Printf("Bypass token issued: %s\n", token.TokenID)
	fmt.Printf("Command hash: %s\n", token.CmdHash)
	fmt.Printf("Expires at: %s\n", token.ExpiresAt.Format(time.RFC3339))
	fmt.Println("Next matching command will be allowed once.")
	return 0
}

func cmdUninstall(paths Paths) int {
	for _, tool := range tools {
		p := filepath.Join(paths.BinDir, tool)
		_ = os.Remove(p)
	}
	fmt.Printf("Removed AgentGate shims from %s\n", paths.BinDir)
	fmt.Println("You can remove PATH entry pointing to ~/.agentgate/bin.")
	return 0
}
