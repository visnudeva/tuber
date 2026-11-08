package ui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/visnudeva/tuber/internal/engine"
)

type prefs struct {
	Notify bool `json:"notify"`
}

func prefsPath() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cfg, "tuber")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "prefs.json"), nil
}

// loadNotifyPref returns whether desktop notifications are on.
// Missing prefs default to on.
func loadNotifyPref() bool {
	path, err := prefsPath()
	if err != nil {
		return true
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return true
	}
	v, ok := raw["notify"]
	if !ok {
		return true
	}
	var on bool
	if err := json.Unmarshal(v, &on); err != nil {
		return true
	}
	return on
}

func saveNotifyPref(on bool) {
	path, err := prefsPath()
	if err != nil {
		return
	}
	b, err := json.MarshalIndent(prefs{Notify: on}, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func desktopNotify(title, body string) {
	if title == "" {
		title = "tuber"
	}
	cmd := exec.Command("notify-send", "-a", "tuber", "-i", "tuber", title, body)
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run()
}

// checkCompletions fires a desktop notice when a known torrent becomes done.
// First sighting of an id only records status so session restore is quiet.
func (m *Model) checkCompletions() {
	live := make(map[string]struct{}, len(m.snaps))
	for _, s := range m.snaps {
		live[s.ID] = struct{}{}
		prev, seen := m.prevStatus[s.ID]
		m.prevStatus[s.ID] = s.Status
		if !seen {
			continue
		}
		if !m.notify || s.Status != engine.StatusDone || prev == engine.StatusDone {
			continue
		}
		name := s.Name
		if name == "" {
			name = short(s.ID)
		}
		go desktopNotify("Download complete", name)
	}
	for id := range m.prevStatus {
		if _, ok := live[id]; !ok {
			delete(m.prevStatus, id)
		}
	}
}
