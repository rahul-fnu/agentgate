package agentgate

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type StartEvent struct {
	TS          time.Time   `json:"ts"`
	ID          string      `json:"id"`
	Phase       string      `json:"phase"`
	Mode        Mode        `json:"mode"`
	Tool        string      `json:"tool"`
	Cmd         string      `json:"cmd"`
	CmdHash     string      `json:"cmd_hash"`
	Env         string      `json:"env"`
	EnvReason   string      `json:"env_reason"`
	Decision    Decision    `json:"decision"`
	Policy      string      `json:"policy"`
	Risk        int         `json:"risk"`
	User        string      `json:"user"`
	Interactive bool        `json:"interactive"`
	Parse       ParseStatus `json:"parse"`
}

type EndEvent struct {
	TS         time.Time `json:"ts"`
	ID         string    `json:"id"`
	Phase      string    `json:"phase"`
	ExitCode   int       `json:"exit_code"`
	DurationMS int64     `json:"duration_ms"`
	Outcome    string    `json:"outcome,omitempty"`
}

func AppendEvent(paths Paths, payload any) error {
	if err := rotateEventsIfNeeded(paths); err != nil {
		return err
	}
	return appendJSONL(paths.EventsPath, payload)
}

func AppendHistory(paths Paths, payload HistoryRecord) error {
	return appendJSONL(paths.HistoryPath, payload)
}

func appendJSONL(path string, payload any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

func rotateEventsIfNeeded(paths Paths) error {
	fi, err := os.Stat(paths.EventsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if fi.Size() < eventsRotateSize {
		return nil
	}
	lockFile, err := os.OpenFile(paths.EventsLock, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	fi, err = os.Stat(paths.EventsPath)
	if err != nil || fi.Size() < eventsRotateSize {
		return nil
	}

	last := fmt.Sprintf("%s.%d", paths.EventsPath, eventsRotateKeep)
	_ = os.Remove(last)
	for i := eventsRotateKeep - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", paths.EventsPath, i)
		newPath := fmt.Sprintf("%s.%d", paths.EventsPath, i+1)
		if _, err := os.Stat(oldPath); err == nil {
			_ = os.Rename(oldPath, newPath)
		}
	}
	return os.Rename(paths.EventsPath, paths.EventsPath+".1")
}

func ReadLastLines(path string, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	const blockSize = 4096
	var size int64
	if fi, err := f.Stat(); err == nil {
		size = fi.Size()
	}
	var out []string
	var chunk []byte
	var offset int64 = size
	for offset > 0 && len(out) <= n {
		readSize := int64(blockSize)
		if offset < readSize {
			readSize = offset
		}
		offset -= readSize
		_, err := f.Seek(offset, io.SeekStart)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, readSize)
		if _, err := f.Read(buf); err != nil {
			return nil, err
		}
		chunk = append(buf, chunk...)
		lines := strings.Split(string(chunk), "\n")
		if len(lines) > 1 {
			chunk = []byte(lines[0])
			out = append(lines[1:], out...)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	clean := make([]string, 0, len(out))
	for _, line := range out {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		clean = append(clean, line)
	}
	if len(clean) > n {
		clean = clean[len(clean)-n:]
	}
	return clean, nil
}

func FindStartEventByID(paths Paths, id string) (*StartEvent, error) {
	// Search recent files first.
	files := []string{paths.EventsPath}
	for i := 1; i <= eventsRotateKeep; i++ {
		files = append(files, fmt.Sprintf("%s.%d", paths.EventsPath, i))
	}
	for _, path := range files {
		ev, err := findStartEventInFile(path, id)
		if err != nil {
			continue
		}
		if ev != nil {
			return ev, nil
		}
	}
	return nil, nil
}

func findStartEventInFile(path, id string) (*StartEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		if m["id"] != id || m["phase"] != "start" {
			continue
		}
		var ev StartEvent
		if err := json.Unmarshal([]byte(line), &ev); err == nil {
			return &ev, nil
		}
	}
	return nil, sc.Err()
}
