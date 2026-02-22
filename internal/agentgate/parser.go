package agentgate

import (
	"path/filepath"
	"strings"
	"time"
)

func ParseCommand(tool string, args []string, cwd string, interactive bool) CommandContext {
	ctx := CommandContext{
		Tool:        tool,
		Flags:       map[string]string{},
		RawArgs:     append([]string(nil), args...),
		RawCommand:  BuildRawCommand(tool, args),
		WorkingDir:  cwd,
		Timestamp:   time.Now().UTC(),
		Interactive: interactive,
		ParseStatus: ParseStatusUnknown,
	}

	switch tool {
	case "kubectl":
		parseKubectl(&ctx, args)
	case "terraform":
		parseTerraform(&ctx, args)
	case "helm":
		parseHelm(&ctx, args)
	case "aws":
		parseAWS(&ctx, args)
	case "gcloud":
		parseGCloud(&ctx, args)
	case "git":
		parseGit(&ctx, args)
	case "docker":
		parseDocker(&ctx, args)
	default:
		ctx.ParseStatus = ParseStatusUnknown
	}
	if ctx.ActionType == "" {
		ctx.ActionType = "other"
	}
	return ctx
}

func parseKubectl(ctx *CommandContext, args []string) {
	action, idx := firstPositional(args)
	if action == "" {
		ctx.ParseStatus = ParseStatusUnknown
		return
	}
	ctx.Action = action
	ctx.ActionType = classifyActionType("kubectl", action)
	next := collectPositionals(args[idx+1:])
	if len(next) > 0 {
		ctx.Resource = strings.ToLower(next[0])
	}
	if len(next) > 1 {
		ctx.ResourceName = next[1]
	}
	parseNamespaceAndFlags(ctx, args)
	if action == "scale" && looksLikeScaleToZero(args) {
		ctx.ActionType = "destructive"
	}
	if ctx.Resource == "" {
		ctx.ParseStatus = ParseStatusPartial
		return
	}
	ctx.ParseStatus = ParseStatusParsed
}

func parseTerraform(ctx *CommandContext, args []string) {
	action, idx := firstPositional(args)
	if action == "" {
		ctx.ParseStatus = ParseStatusUnknown
		return
	}
	ctx.Action = action
	if (action == "state" || action == "workspace") && len(args) > idx+1 {
		sub := strings.ToLower(args[idx+1])
		if !strings.HasPrefix(sub, "-") {
			ctx.Action = action + "-" + sub
		}
	}
	ctx.ActionType = classifyActionType("terraform", ctx.Action)
	parseNamespaceAndFlags(ctx, args)
	for _, a := range args {
		if strings.HasPrefix(a, "-chdir=") {
			dir := strings.TrimPrefix(a, "-chdir=")
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(ctx.WorkingDir, dir)
			}
			ctx.WorkingDir = dir
		}
	}
	if len(args) > idx+1 {
		ctx.Resource = strings.ToLower(args[idx+1])
	}
	if ctx.Action == "force-unlock" {
		ctx.ActionType = "destructive"
	}
	if strings.HasPrefix(ctx.Action, "state-") {
		ctx.ActionType = "destructive"
	}
	ctx.ParseStatus = ParseStatusPartial
	if ctx.Action != "" {
		ctx.ParseStatus = ParseStatusParsed
	}
}

func parseHelm(ctx *CommandContext, args []string) {
	action, idx := firstPositional(args)
	if action == "" {
		ctx.ParseStatus = ParseStatusUnknown
		return
	}
	ctx.Action = action
	ctx.ActionType = classifyActionType("helm", action)
	rest := collectPositionals(args[idx+1:])
	if len(rest) > 0 {
		ctx.ResourceName = rest[0]
	}
	if len(rest) > 1 {
		ctx.Resource = rest[1]
	}
	parseNamespaceAndFlags(ctx, args)
	ctx.ParseStatus = ParseStatusParsed
}

func parseAWS(ctx *CommandContext, args []string) {
	parseNamespaceAndFlags(ctx, args)
	pos := collectPositionals(args)
	if len(pos) < 2 {
		ctx.ParseStatus = ParseStatusPartial
		if len(pos) > 0 {
			ctx.Action = pos[0]
		}
		return
	}
	ctx.Resource = strings.ToLower(pos[0])
	ctx.Action = strings.ToLower(pos[1])
	ctx.ActionType = classifyActionType("aws", ctx.Action)
	ctx.ParseStatus = ParseStatusParsed
}

func parseGCloud(ctx *CommandContext, args []string) {
	parseNamespaceAndFlags(ctx, args)
	pos := collectPositionals(args)
	if len(pos) == 0 {
		ctx.ParseStatus = ParseStatusUnknown
		return
	}

	actionIdx := -1
	for i := 1; i < len(pos); i++ {
		if looksLikeActionToken(pos[i]) {
			actionIdx = i
			break
		}
	}
	if actionIdx == -1 {
		ctx.Resource = strings.ToLower(strings.Join(pos, "/"))
		ctx.Action = strings.ToLower(pos[0])
		ctx.ActionType = classifyActionType("gcloud", ctx.Action)
		ctx.ParseStatus = ParseStatusPartial
		return
	}
	ctx.Resource = strings.ToLower(strings.Join(pos[:actionIdx], "/"))
	ctx.Action = strings.ToLower(pos[actionIdx])
	ctx.ActionType = classifyActionType("gcloud", ctx.Action)
	if len(pos) > actionIdx+1 {
		ctx.ResourceName = pos[actionIdx+1]
	}
	ctx.ParseStatus = ParseStatusParsed
}

func parseGit(ctx *CommandContext, args []string) {
	action, idx := firstPositional(args)
	if action == "" {
		ctx.ParseStatus = ParseStatusUnknown
		return
	}
	ctx.Action = action
	parseNamespaceAndFlags(ctx, args)

	if action == "push" && (hasFlag(args, "--force") || hasFlag(args, "-f") || hasFlag(args, "--force-with-lease")) {
		ctx.Action = "push-force"
	}
	if action == "reset" && (hasFlag(args, "--hard") || hasFlag(args, "--mixed")) {
		ctx.Action = "reset-hard"
	}
	if action == "clean" && hasFlag(args, "-f") {
		ctx.Action = "clean-force"
	}
	ctx.ActionType = classifyActionType("git", ctx.Action)

	pos := collectPositionals(args[idx+1:])
	if len(pos) > 0 {
		ctx.Resource = strings.ToLower(pos[0])
	}
	if len(pos) > 1 {
		ctx.ResourceName = pos[1]
	}
	ctx.ParseStatus = ParseStatusParsed
}

func parseDocker(ctx *CommandContext, args []string) {
	action, idx := firstPositional(args)
	if action == "" {
		ctx.ParseStatus = ParseStatusUnknown
		return
	}
	ctx.Action = action
	parseNamespaceAndFlags(ctx, args)
	pos := collectPositionals(args)
	if len(pos) > 0 {
		ctx.Resource = strings.ToLower(pos[0])
	}

	if action == "system" && len(pos) > 1 && strings.ToLower(pos[1]) == "prune" {
		ctx.Action = "system-prune"
	}
	if action == "compose" && len(pos) > 1 {
		ctx.Action = "compose-" + strings.ToLower(pos[1])
	}
	if action == "rm" && hasFlag(args, "-f") {
		ctx.Action = "rm-force"
	}
	ctx.ActionType = classifyActionType("docker", ctx.Action)
	if len(args) > idx+1 {
		ctx.ResourceName = args[idx+1]
	}
	ctx.ParseStatus = ParseStatusParsed
}

func parseNamespaceAndFlags(ctx *CommandContext, args []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		switch {
		case arg == "-n" || arg == "--namespace":
			if i+1 < len(args) {
				ctx.Namespace = args[i+1]
				ctx.Flags["namespace"] = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--namespace="):
			ctx.Namespace = strings.TrimPrefix(arg, "--namespace=")
			ctx.Flags["namespace"] = ctx.Namespace
		case strings.HasPrefix(arg, "--"):
			parts := strings.SplitN(strings.TrimPrefix(arg, "--"), "=", 2)
			if len(parts) == 2 {
				ctx.Flags[strings.ToLower(parts[0])] = parts[1]
			} else {
				ctx.Flags[strings.ToLower(parts[0])] = "true"
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					ctx.Flags[strings.ToLower(parts[0])] = args[i+1]
					i++
				}
			}
		default:
			ctx.Flags[strings.TrimPrefix(arg, "-")] = "true"
		}
	}
}

func firstPositional(args []string) (string, int) {
	skipNext := false
	for i, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(a, "-") {
			if consumesNextValueFlag(strings.TrimPrefix(a, "--")) || a == "-n" || a == "-f" || a == "-k" || a == "-c" {
				skipNext = true
			}
			continue
		}
		return strings.ToLower(a), i
	}
	return "", -1
}

func collectPositionals(args []string) []string {
	out := make([]string, 0, len(args))
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(a, "--") {
			if !strings.Contains(a, "=") {
				key := strings.TrimPrefix(a, "--")
				if consumesNextValueFlag(key) {
					skipNext = true
				}
			}
			continue
		}
		if strings.HasPrefix(a, "-") {
			if a == "-n" || a == "-f" || a == "-k" || a == "-c" {
				skipNext = true
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

func looksLikeScaleToZero(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--replicas=") && strings.TrimPrefix(a, "--replicas=") == "0" {
			return true
		}
		if a == "--replicas" && i+1 < len(args) && args[i+1] == "0" {
			return true
		}
	}
	return false
}

func classifyActionType(tool, action string) string {
	action = strings.ToLower(action)
	readOps := map[string]bool{
		"get": true, "describe": true, "list": true, "logs": true, "status": true, "show": true,
		"version": true, "help": true, "diff": true, "log": true,
	}
	writeOps := map[string]bool{
		"apply": true, "patch": true, "upgrade": true, "install": true, "create": true, "update": true, "set": true,
		"replace": true, "rollout": true, "restart": true, "label": true, "annotate": true, "cordon": true, "uncordon": true,
		"enable": true, "disable": true, "start": true, "stop": true, "deploy": true,
		"commit": true, "merge": true, "rebase": true, "checkout": true, "switch": true, "pull": true,
		"tag": true, "branch": true, "compose-up": true, "compose-start": true,
	}
	destructiveOps := map[string]bool{
		"delete": true, "destroy": true, "terminate": true, "uninstall": true, "drain": true, "rm": true,
		"force-unlock": true, "state-rm": true, "state-mv": true, "system-prune": true, "compose-down": true, "rm-force": true,
		"reset-hard": true, "clean-force": true, "push-force": true,
	}
	if readOps[action] {
		return "read"
	}
	if destructiveOps[action] {
		return "destructive"
	}
	if writeOps[action] {
		return "write"
	}
	if tool == "aws" || tool == "gcloud" {
		switch {
		case strings.HasPrefix(action, "list"), strings.HasPrefix(action, "describe"), strings.HasPrefix(action, "get"):
			return "read"
		case strings.HasPrefix(action, "delete"), strings.HasPrefix(action, "terminate"), strings.HasPrefix(action, "destroy"):
			return "destructive"
		case strings.HasPrefix(action, "create"), strings.HasPrefix(action, "update"), strings.HasPrefix(action, "put"), strings.HasPrefix(action, "set"), strings.HasPrefix(action, "run"):
			return "write"
		}
	}
	return "other"
}

func consumesNextValueFlag(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "namespace", "profile", "project", "context", "chdir", "filename", "file", "kustomize", "cluster", "zone", "region", "name":
		return true
	default:
		return false
	}
}

func looksLikeActionToken(token string) bool {
	t := strings.ToLower(token)
	switch {
	case strings.HasPrefix(t, "create"),
		strings.HasPrefix(t, "update"),
		strings.HasPrefix(t, "delete"),
		strings.HasPrefix(t, "destroy"),
		strings.HasPrefix(t, "patch"),
		strings.HasPrefix(t, "set"),
		strings.HasPrefix(t, "get"),
		strings.HasPrefix(t, "list"),
		strings.HasPrefix(t, "describe"),
		strings.HasPrefix(t, "terminate"),
		strings.HasPrefix(t, "deploy"),
		strings.HasPrefix(t, "run"),
		strings.HasPrefix(t, "enable"),
		strings.HasPrefix(t, "disable"),
		strings.HasPrefix(t, "start"),
		strings.HasPrefix(t, "stop"),
		strings.HasPrefix(t, "restart"):
		return true
	default:
		return false
	}
}

func hasFlag(args []string, flagName string) bool {
	for _, a := range args {
		if a == flagName {
			return true
		}
		if strings.HasPrefix(a, flagName+"=") {
			return true
		}
	}
	return false
}
