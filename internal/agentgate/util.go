package agentgate

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func NewCommandID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("ag_%d", time.Now().UnixNano())
	}
	return "ag_" + hex.EncodeToString(b[:])
}

func BuildRawCommand(tool string, args []string) string {
	var sb strings.Builder
	sb.WriteString(tool)
	for _, a := range args {
		sb.WriteByte(' ')
		if strings.ContainsAny(a, " \t\n\"'") {
			sb.WriteString(fmt.Sprintf("%q", a))
		} else {
			sb.WriteString(a)
		}
	}
	return strings.TrimSpace(sb.String())
}

func CommandHash(tool, rawCmd, env string) string {
	h := sha256.Sum256([]byte(strings.Join([]string{tool, rawCmd, env}, "|")))
	return hex.EncodeToString(h[:])
}

func Normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func MatchPattern(pattern, value string) bool {
	pattern = Normalize(pattern)
	value = Normalize(value)
	if pattern == "" {
		return false
	}
	if strings.Contains(pattern, "*") {
		ok, err := filepath.Match(pattern, value)
		return err == nil && ok
	}
	return strings.Contains(value, pattern)
}

func IsInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func ParseWindow(value string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(value)
	if err == nil {
		return d
	}
	return fallback
}

func ParseLastDuration(input string) (time.Duration, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return 24 * time.Hour, nil
	}
	if strings.HasSuffix(input, "d") {
		n := strings.TrimSuffix(input, "d")
		var days int
		if _, err := fmt.Sscanf(n, "%d", &days); err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(input)
}

func restrictiveness(d Decision) int {
	switch d {
	case DecisionDeny:
		return 4
	case DecisionConfirm:
		return 3
	case DecisionWarn:
		return 2
	default:
		return 1
	}
}
