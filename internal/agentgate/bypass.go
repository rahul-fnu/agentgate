package agentgate

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"time"
)

type BypassRecord struct {
	TS        time.Time `json:"ts"`
	Type      string    `json:"type"` // issue | consume
	TokenID   string    `json:"token_id"`
	CommandID string    `json:"command_id,omitempty"`
	CmdHash   string    `json:"cmd_hash,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

func IssueBypass(paths Paths, commandID, cmdHash string, ttl time.Duration) (BypassRecord, error) {
	rec := BypassRecord{
		TS:        time.Now().UTC(),
		Type:      "issue",
		TokenID:   "tok_" + NewCommandID(),
		CommandID: commandID,
		CmdHash:   cmdHash,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}
	if err := appendJSONL(paths.BypassesPath, rec); err != nil {
		return BypassRecord{}, err
	}
	return rec, nil
}

func ConsumeValidBypass(paths Paths, cmdHash string, now time.Time) (string, bool, error) {
	issued, consumed, err := readBypassState(paths)
	if err != nil {
		return "", false, err
	}
	for _, rec := range issued {
		if rec.CmdHash != cmdHash {
			continue
		}
		if now.After(rec.ExpiresAt) {
			continue
		}
		if consumed[rec.TokenID] {
			continue
		}
		consume := BypassRecord{
			TS:      time.Now().UTC(),
			Type:    "consume",
			TokenID: rec.TokenID,
			CmdHash: cmdHash,
		}
		if err := appendJSONL(paths.BypassesPath, consume); err != nil {
			return "", false, err
		}
		return rec.TokenID, true, nil
	}
	return "", false, nil
}

func readBypassState(paths Paths) ([]BypassRecord, map[string]bool, error) {
	f, err := os.Open(paths.BypassesPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, map[string]bool{}, nil
		}
		return nil, nil, err
	}
	defer f.Close()

	var issued []BypassRecord
	consumed := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec BypassRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		switch rec.Type {
		case "issue":
			issued = append(issued, rec)
		case "consume":
			consumed[rec.TokenID] = true
		}
	}
	return issued, consumed, sc.Err()
}
