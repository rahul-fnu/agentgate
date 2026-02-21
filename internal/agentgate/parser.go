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
	ctx.ActionType = classifyActionType("terraform", action)
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
	ctx.Resource = strings.ToLower(pos[0])
	if len(pos) > 1 {
		ctx.Action = strings.ToLower(pos[1])
		ctx.ActionType = classifyActionType("gcloud", ctx.Action)
		ctx.ParseStatus = ParseStatusParsed
		return
	}
	ctx.Action = ctx.Resource
	ctx.ActionType = classifyActionType("gcloud", ctx.Action)
	ctx.ParseStatus = ParseStatusPartial
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
			if a == "-n" || a == "--namespace" || a == "--profile" || a == "--project" || a == "--context" {
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
				if key == "namespace" || key == "profile" || key == "project" || key == "context" || key == "chdir" {
					skipNext = true
				}
			}
			continue
		}
		if strings.HasPrefix(a, "-") {
			if a == "-n" {
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
	}
	writeOps := map[string]bool{
		"apply": true, "patch": true, "upgrade": true, "install": true, "create": true, "update": true, "set": true,
	}
	destructiveOps := map[string]bool{
		"delete": true, "destroy": true, "terminate": true, "uninstall": true, "drain": true, "rm": true,
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
