package agentgate

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const policyExitCode = 77

func Intercept(paths Paths, tool string, args []string) int {
	start := time.Now()

	if isInfoOnlyCommand(args) {
		realPath, err := findRealBinary(paths, tool)
		if err != nil {
			fmt.Fprintf(os.Stderr, "agentgate: unable to locate real %s binary (%v)\n", tool, err)
			return 1
		}
		return execPassthrough(realPath, args)
	}
	if shouldBypassLikelyCLIUsageError(tool, args) {
		realPath, err := findRealBinary(paths, tool)
		if err != nil {
			fmt.Fprintf(os.Stderr, "agentgate: unable to locate real %s binary (%v)\n", tool, err)
			return 1
		}
		return execPassthrough(realPath, args)
	}

	cfg, cfgErr := LoadConfig(paths)
	if cfgErr != nil {
		cfg = DefaultConfig()
	}
	policies, policyErr := LoadPolicies(paths)
	if policyErr != nil {
		policies = nil
	}
	cwd, _ := os.Getwd()

	ctx := ParseCommand(tool, args, cwd, IsInteractive())
	DetectEnvironment(paths, cfg, &ctx)

	commandID := NewCommandID()
	cmdHash := CommandHash(ctx.Tool, ctx.RawCommand, ctx.Environment)

	result := DecisionResult{
		Decision:   DecisionAllow,
		PolicyName: "",
		Suggestion: "",
		Risk:       baseRisk(ctx),
	}
	if cfgErr != nil || policyErr != nil {
		result.PolicyName = "fail-open"
		result.Suggestion = "AgentGate failed open due to configuration/policy parse issue."
		result.Decision = DecisionAllow
		result.Risk = scoreRisk(ctx, DecisionWarn)
	} else {
		result = EvaluatePolicies(paths, cfg, policies, ctx)
	}

	bypassUsed := false
	if result.Decision == DecisionDeny || result.Decision == DecisionConfirm {
		if _, ok, err := ConsumeValidBypass(paths, cmdHash, time.Now().UTC()); err == nil && ok {
			bypassUsed = true
			result.PolicyName = "allow-once-bypass"
			result.Suggestion = "Temporary bypass token consumed."
			result.Decision = DecisionAllow
		}
	}

	mode := cfg.Mode
	effective := result.Decision
	confirmRequired := false
	outcome := "executed"

	if mode == ModeObserve {
		if result.Decision == DecisionDeny || result.Decision == DecisionConfirm {
			effective = DecisionAllow
			outcome = "observe-simulated"
		}
	}

	if mode == ModeEnforce && result.Decision == DecisionConfirm && !bypassUsed {
		confirmRequired = true
		if !ctx.Interactive {
			effective = DecisionDeny
			result.Suggestion = "Non-interactive session cannot confirm. Use agentgate allow-once <command-id>."
			outcome = "blocked-non-interactive-confirm"
		} else {
			ok := promptConfirm(ctx.RawCommand)
			if !ok {
				effective = DecisionDeny
				outcome = "blocked-user-declined"
			}
		}
	}
	if mode == ModeEnforce && result.Decision == DecisionDeny && !bypassUsed {
		effective = DecisionDeny
		outcome = "blocked-policy"
	}

	usr := "unknown"
	if u, err := user.Current(); err == nil {
		usr = u.Username
	}

	startEvent := StartEvent{
		TS:          time.Now().UTC(),
		ID:          commandID,
		Phase:       "start",
		Mode:        mode,
		Tool:        tool,
		Action:      ctx.Action,
		ActionType:  ctx.ActionType,
		Cmd:         ctx.RawCommand,
		CmdHash:     cmdHash,
		Env:         ctx.Environment,
		EnvReason:   ctx.EnvReason,
		WorkingDir:  ctx.WorkingDir,
		Decision:    result.Decision,
		Policy:      result.PolicyName,
		Risk:        result.Risk,
		User:        usr,
		Interactive: ctx.Interactive,
		Parse:       ctx.ParseStatus,
	}
	_ = AppendEvent(paths, startEvent)

	printContract(result, ctx, commandID, confirmRequired)

	if effective == DecisionWarn {
		fmt.Fprintf(os.Stderr, "AgentGate warning: policy=%s cmd=%s\n", fallback(result.PolicyName, "none"), ctx.RawCommand)
	}
	if mode == ModeObserve && outcome == "observe-simulated" {
		fmt.Fprintln(os.Stderr, "AgentGate observe mode: command allowed, policy decision was simulated.")
	}

	if effective == DecisionDeny {
		fmt.Fprintf(os.Stderr, "AgentGate denied command: %s\n", fallback(result.PolicyName, "policy"))
		_ = AppendEvent(paths, EndEvent{
			TS:         time.Now().UTC(),
			ID:         commandID,
			Phase:      "end",
			ExitCode:   policyExitCode,
			DurationMS: time.Since(start).Milliseconds(),
			Outcome:    outcome,
		})
		return policyExitCode
	}

	realPath, err := findRealBinary(paths, tool)
	if err != nil {
		fmt.Fprintf(os.Stderr, "AgentGate warning: unable to locate real %s binary (%v), failing open.\n", tool, err)
		_ = AppendEvent(paths, EndEvent{
			TS:         time.Now().UTC(),
			ID:         commandID,
			Phase:      "end",
			ExitCode:   0,
			DurationMS: time.Since(start).Milliseconds(),
			Outcome:    "fail-open-no-real-binary",
		})
		return 0
	}

	exitCode := execPassthrough(realPath, args)
	_ = AppendEvent(paths, EndEvent{
		TS:         time.Now().UTC(),
		ID:         commandID,
		Phase:      "end",
		ExitCode:   exitCode,
		DurationMS: time.Since(start).Milliseconds(),
		Outcome:    outcome,
	})
	return exitCode
}

func printContract(result DecisionResult, ctx CommandContext, commandID string, confirmRequired bool) {
	fmt.Fprintln(os.Stderr, "AGENTGATE_ACTIVE=true")
	fmt.Fprintf(os.Stderr, "AGENTGATE_DECISION=%s\n", result.Decision)
	fmt.Fprintf(os.Stderr, "AGENTGATE_POLICY=%s\n", fallback(result.PolicyName, ""))
	fmt.Fprintf(os.Stderr, "AGENTGATE_RISK=%d\n", result.Risk)
	fmt.Fprintf(os.Stderr, "AGENTGATE_ENVIRONMENT=%s\n", fallback(ctx.Environment, "unknown"))
	fmt.Fprintf(os.Stderr, "AGENTGATE_COMMAND_ID=%s\n", commandID)
	fmt.Fprintf(os.Stderr, "AGENTGATE_CONFIRM_REQUIRED=%s\n", strconv.FormatBool(confirmRequired))
	fmt.Fprintf(os.Stderr, "AGENTGATE_SUGGESTION=%s\n", fallback(result.Suggestion, ""))
}

func promptConfirm(rawCommand string) bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	fmt.Fprintf(tty, "AgentGate confirmation required:\n  %s\nProceed? [y/N]: ", rawCommand)
	reader := bufio.NewReader(tty)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

func execPassthrough(path string, args []string) int {
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "AgentGate warning: command exec failed (%v)\n", err)
		return 1
	}
	return 0
}

func findRealBinary(paths Paths, tool string) (string, error) {
	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		if filepath.Clean(dir) == filepath.Clean(paths.BinDir) {
			continue
		}
		candidate := filepath.Join(dir, tool)
		fi, err := os.Stat(candidate)
		if err == nil && fi.Mode().IsRegular() && (fi.Mode()&0o111) != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("binary not found in PATH outside shim dir")
}

func fallback(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}

func isInfoOnlyCommand(args []string) bool {
	if len(args) == 0 {
		return true
	}

	for _, a := range args {
		switch a {
		case "--version", "--help", "-v", "-h":
			return true
		}
	}

	if action, _ := firstPositional(args); action == "help" || action == "version" {
		return true
	}

	return false
}

func shouldBypassLikelyCLIUsageError(tool string, args []string) bool {
	if tool != "kubectl" {
		return false
	}
	action, idx := firstPositional(args)
	if action == "" {
		return false
	}
	switch action {
	case "delete", "get", "describe", "logs", "patch", "scale", "rollout":
		if hasManifestInput(args) {
			return false
		}
		targets := collectPositionals(args[idx+1:])
		return len(targets) == 0
	default:
		return false
	}
}

func hasManifestInput(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--filename=") || strings.HasPrefix(a, "--kustomize=") {
			return true
		}
		if a == "-f" || a == "--filename" || a == "-k" || a == "--kustomize" {
			if i+1 < len(args) {
				return true
			}
		}
	}
	return false
}
