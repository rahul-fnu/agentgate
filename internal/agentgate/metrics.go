package agentgate

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type decisionMetricKey struct {
	Tool        string
	Environment string
	Decision    string
	Mode        string
}

type blockedMetricKey struct {
	Tool        string
	Environment string
	Policy      string
}

type allowedMetricKey struct {
	Tool        string
	Environment string
	Outcome     string
}

type parseMetricKey struct {
	Tool        string
	ParseStatus string
}

type MetricsSnapshot struct {
	GeneratedAt    time.Time
	CommandsTotal  int
	InFlightTotal  int
	DecisionCounts map[decisionMetricKey]int
	BlockedCounts  map[blockedMetricKey]int
	AllowedCounts  map[allowedMetricKey]int
	ParseCounts    map[parseMetricKey]int
}

func cmdMetrics(paths Paths, args []string) int {
	fs := flag.NewFlagSet("metrics", flag.ContinueOnError)
	last := fs.String("last", "24h", "time window (e.g. 24h, 7d)")
	format := fs.String("format", "prometheus", "output format: prometheus|json")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	window, err := ParseLastDuration(*last)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate metrics: invalid --last %q\n", *last)
		return 1
	}
	since := time.Now().Add(-window)
	snapshot, err := CollectMetrics(paths, since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate metrics: %v\n", err)
		return 1
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "prometheus":
		printPrometheusMetrics(os.Stdout, snapshot)
	case "json":
		printJSONMetrics(os.Stdout, snapshot, *last)
	default:
		fmt.Fprintf(os.Stderr, "agentgate metrics: unsupported --format %q\n", *format)
		return 1
	}
	return 0
}

func cmdServeMetrics(paths Paths, args []string) int {
	fs := flag.NewFlagSet("serve-metrics", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:9765", "listen address")
	last := fs.String("last", "24h", "time window (e.g. 24h, 7d)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	window, err := ParseLastDuration(*last)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentgate serve-metrics: invalid --last %q\n", *last)
		return 1
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		snapshot, err := CollectMetrics(paths, time.Now().Add(-window))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		printPrometheusMetrics(w, snapshot)
	})

	server := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}
	fmt.Printf("Serving metrics at http://%s/metrics (window=%s)\n", *addr, *last)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "agentgate serve-metrics: %v\n", err)
		return 1
	}
	return 0
}

func CollectMetrics(paths Paths, since time.Time) (MetricsSnapshot, error) {
	snapshot := MetricsSnapshot{
		GeneratedAt:    time.Now().UTC(),
		DecisionCounts: map[decisionMetricKey]int{},
		BlockedCounts:  map[blockedMetricKey]int{},
		AllowedCounts:  map[allowedMetricKey]int{},
		ParseCounts:    map[parseMetricKey]int{},
	}
	startByID := map[string]StartEvent{}

	for _, path := range eventFilesOldestFirst(paths) {
		if err := scanEventFile(path, since, &snapshot, startByID); err != nil {
			return snapshot, err
		}
	}

	snapshot.InFlightTotal = len(startByID)
	return snapshot, nil
}

func scanEventFile(path string, since time.Time, snapshot *MetricsSnapshot, startByID map[string]StartEvent) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var envelope struct {
			Phase string `json:"phase"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			continue
		}
		switch envelope.Phase {
		case "start":
			var ev StartEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			if ev.TS.Before(since) {
				continue
			}
			startByID[ev.ID] = ev
			snapshot.CommandsTotal++
			snapshot.DecisionCounts[decisionMetricKey{
				Tool:        fallback(ev.Tool, "unknown"),
				Environment: fallback(ev.Env, "unknown"),
				Decision:    fallback(string(ev.Decision), "unknown"),
				Mode:        fallback(string(ev.Mode), "unknown"),
			}]++
			snapshot.ParseCounts[parseMetricKey{
				Tool:        fallback(ev.Tool, "unknown"),
				ParseStatus: fallback(string(ev.Parse), "unknown"),
			}]++
		case "end":
			var ev EndEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			start, ok := startByID[ev.ID]
			if !ok {
				continue
			}
			if ev.ExitCode == policyExitCode || strings.HasPrefix(ev.Outcome, "blocked") {
				snapshot.BlockedCounts[blockedMetricKey{
					Tool:        fallback(start.Tool, "unknown"),
					Environment: fallback(start.Env, "unknown"),
					Policy:      fallback(start.Policy, "none"),
				}]++
			} else {
				snapshot.AllowedCounts[allowedMetricKey{
					Tool:        fallback(start.Tool, "unknown"),
					Environment: fallback(start.Env, "unknown"),
					Outcome:     fallback(ev.Outcome, "executed"),
				}]++
			}
			delete(startByID, ev.ID)
		}
	}
	return sc.Err()
}

func eventFilesOldestFirst(paths Paths) []string {
	files := make([]string, 0, eventsRotateKeep+1)
	for i := eventsRotateKeep; i >= 1; i-- {
		p := fmt.Sprintf("%s.%d", paths.EventsPath, i)
		if _, err := os.Stat(p); err == nil {
			files = append(files, p)
		}
	}
	if _, err := os.Stat(paths.EventsPath); err == nil {
		files = append(files, paths.EventsPath)
	}
	return files
}

func printPrometheusMetrics(w interface {
	Write([]byte) (int, error)
}, snapshot MetricsSnapshot) {
	var sb strings.Builder
	sb.WriteString("# HELP agentgate_commands_total Number of intercepted commands grouped by decision.\n")
	sb.WriteString("# TYPE agentgate_commands_total counter\n")
	for _, key := range sortDecisionMetricKeys(snapshot.DecisionCounts) {
		fmt.Fprintf(&sb,
			"agentgate_commands_total{tool=%q,environment=%q,decision=%q,mode=%q} %d\n",
			labelEscape(key.Tool), labelEscape(key.Environment), labelEscape(key.Decision), labelEscape(key.Mode), snapshot.DecisionCounts[key],
		)
	}

	sb.WriteString("# HELP agentgate_commands_blocked_total Number of commands actually blocked by policy.\n")
	sb.WriteString("# TYPE agentgate_commands_blocked_total counter\n")
	for _, key := range sortBlockedMetricKeys(snapshot.BlockedCounts) {
		fmt.Fprintf(&sb,
			"agentgate_commands_blocked_total{tool=%q,environment=%q,policy=%q} %d\n",
			labelEscape(key.Tool), labelEscape(key.Environment), labelEscape(key.Policy), snapshot.BlockedCounts[key],
		)
	}

	sb.WriteString("# HELP agentgate_commands_allowed_total Number of commands allowed to execute.\n")
	sb.WriteString("# TYPE agentgate_commands_allowed_total counter\n")
	for _, key := range sortAllowedMetricKeys(snapshot.AllowedCounts) {
		fmt.Fprintf(&sb,
			"agentgate_commands_allowed_total{tool=%q,environment=%q,outcome=%q} %d\n",
			labelEscape(key.Tool), labelEscape(key.Environment), labelEscape(key.Outcome), snapshot.AllowedCounts[key],
		)
	}

	sb.WriteString("# HELP agentgate_parse_status_total Number of commands by parse status.\n")
	sb.WriteString("# TYPE agentgate_parse_status_total counter\n")
	for _, key := range sortParseMetricKeys(snapshot.ParseCounts) {
		fmt.Fprintf(&sb,
			"agentgate_parse_status_total{tool=%q,parse_status=%q} %d\n",
			labelEscape(key.Tool), labelEscape(key.ParseStatus), snapshot.ParseCounts[key],
		)
	}

	sb.WriteString("# HELP agentgate_inflight_commands Number of commands with start event but no end event.\n")
	sb.WriteString("# TYPE agentgate_inflight_commands gauge\n")
	fmt.Fprintf(&sb, "agentgate_inflight_commands %d\n", snapshot.InFlightTotal)

	sb.WriteString("# HELP agentgate_metrics_generated_unix Metrics generation timestamp (unix seconds).\n")
	sb.WriteString("# TYPE agentgate_metrics_generated_unix gauge\n")
	fmt.Fprintf(&sb, "agentgate_metrics_generated_unix %d\n", snapshot.GeneratedAt.Unix())

	_, _ = w.Write([]byte(sb.String()))
}

func printJSONMetrics(w interface {
	Write([]byte) (int, error)
}, snapshot MetricsSnapshot, window string) {
	payload := map[string]any{
		"generated_at":   snapshot.GeneratedAt.Format(time.RFC3339),
		"window":         window,
		"commands_total": snapshot.CommandsTotal,
		"inflight_total": snapshot.InFlightTotal,
		"decisions":      []map[string]any{},
		"blocked":        []map[string]any{},
		"allowed":        []map[string]any{},
		"parse_status":   []map[string]any{},
	}

	decisions := make([]map[string]any, 0, len(snapshot.DecisionCounts))
	for _, key := range sortDecisionMetricKeys(snapshot.DecisionCounts) {
		decisions = append(decisions, map[string]any{
			"tool":        key.Tool,
			"environment": key.Environment,
			"decision":    key.Decision,
			"mode":        key.Mode,
			"count":       snapshot.DecisionCounts[key],
		})
	}
	blocked := make([]map[string]any, 0, len(snapshot.BlockedCounts))
	for _, key := range sortBlockedMetricKeys(snapshot.BlockedCounts) {
		blocked = append(blocked, map[string]any{
			"tool":        key.Tool,
			"environment": key.Environment,
			"policy":      key.Policy,
			"count":       snapshot.BlockedCounts[key],
		})
	}
	allowed := make([]map[string]any, 0, len(snapshot.AllowedCounts))
	for _, key := range sortAllowedMetricKeys(snapshot.AllowedCounts) {
		allowed = append(allowed, map[string]any{
			"tool":        key.Tool,
			"environment": key.Environment,
			"outcome":     key.Outcome,
			"count":       snapshot.AllowedCounts[key],
		})
	}
	parse := make([]map[string]any, 0, len(snapshot.ParseCounts))
	for _, key := range sortParseMetricKeys(snapshot.ParseCounts) {
		parse = append(parse, map[string]any{
			"tool":         key.Tool,
			"parse_status": key.ParseStatus,
			"count":        snapshot.ParseCounts[key],
		})
	}
	payload["decisions"] = decisions
	payload["blocked"] = blocked
	payload["allowed"] = allowed
	payload["parse_status"] = parse

	b, _ := json.MarshalIndent(payload, "", "  ")
	_, _ = w.Write(append(b, '\n'))
}

func sortDecisionMetricKeys(m map[decisionMetricKey]int) []decisionMetricKey {
	keys := make([]decisionMetricKey, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a := keys[i]
		b := keys[j]
		return strings.Join([]string{a.Tool, a.Environment, a.Decision, a.Mode}, "|") <
			strings.Join([]string{b.Tool, b.Environment, b.Decision, b.Mode}, "|")
	})
	return keys
}

func sortBlockedMetricKeys(m map[blockedMetricKey]int) []blockedMetricKey {
	keys := make([]blockedMetricKey, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a := keys[i]
		b := keys[j]
		return strings.Join([]string{a.Tool, a.Environment, a.Policy}, "|") <
			strings.Join([]string{b.Tool, b.Environment, b.Policy}, "|")
	})
	return keys
}

func sortAllowedMetricKeys(m map[allowedMetricKey]int) []allowedMetricKey {
	keys := make([]allowedMetricKey, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a := keys[i]
		b := keys[j]
		return strings.Join([]string{a.Tool, a.Environment, a.Outcome}, "|") <
			strings.Join([]string{b.Tool, b.Environment, b.Outcome}, "|")
	})
	return keys
}

func sortParseMetricKeys(m map[parseMetricKey]int) []parseMetricKey {
	keys := make([]parseMetricKey, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a := keys[i]
		b := keys[j]
		return strings.Join([]string{a.Tool, a.ParseStatus}, "|") <
			strings.Join([]string{b.Tool, b.ParseStatus}, "|")
	})
	return keys
}

func labelEscape(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return value
}
