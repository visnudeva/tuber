// Package watch adds .torrent files dropped in the user's download folder
// and magnet links that appear on the clipboard while tuber is running.
package watch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Adder is the subset of the torrent engine used to enqueue inputs.
type Adder interface {
	Add(input string) (string, error)
}

// Notifier receives a short status line for the TUI.
type Notifier interface {
	Set(msg string)
}

// Config controls where and how often tuber looks for new torrents.
type Config struct {
	// Dirs are scanned non-recursively for .torrent files.
	Dirs []string
	// ReadClipboard returns the current clipboard text. Nil disables clipboard watch.
	ReadClipboard func() (string, error)
	// Interval is how often to look. Zero defaults to 2s.
	Interval time.Duration
}

// Start polls until the returned stop function is called.
func Start(add Adder, note Notifier, cfg Config) (stop func()) {
	if cfg.Interval <= 0 {
		cfg.Interval = 2 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		st := &State{}
		poll(add, note, st, cfg)
		t := time.NewTicker(cfg.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				poll(add, note, st, cfg)
			}
		}
	}()
	return cancel
}

// State remembers files and clipboard text already handled.
type State struct {
	files   map[string]stamp
	clip    string
	clipSet bool
}

type stamp struct {
	mod  int64
	size int64
}

func poll(add Adder, note Notifier, st *State, cfg Config) {
	if st.files == nil {
		st.files = map[string]stamp{}
	}
	scanTorrentFiles(add, note, st, cfg.Dirs)
	if cfg.ReadClipboard != nil {
		text, err := cfg.ReadClipboard()
		if err == nil {
			scanClipboard(add, note, st, text)
		}
	}
}

func scanTorrentFiles(add Adder, note Notifier, st *State, dirs []string) {
	if st.files == nil {
		st.files = map[string]stamp{}
	}
	var added []string
	for _, dir := range uniqueDirs(dirs) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, ent := range entries {
			if ent.IsDir() {
				continue
			}
			name := ent.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".torrent") {
				continue
			}
			info, err := ent.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
				continue
			}
			path := filepath.Join(dir, name)
			stmp := stamp{mod: info.ModTime().UnixNano(), size: info.Size()}
			if prev, ok := st.files[path]; ok && prev == stmp {
				continue
			}
			id, err := add.Add(path)
			st.files[path] = stmp
			if err != nil {
				if !strings.Contains(err.Error(), "already added") && note != nil {
					note.Set(err.Error())
				}
				continue
			}
			added = append(added, id)
		}
	}
	if note == nil || len(added) == 0 {
		return
	}
	if len(added) == 1 {
		note.Set("added torrent " + short(added[0]))
		return
	}
	note.Set(fmt.Sprintf("added %d torrents from Downloads", len(added)))
}

func scanClipboard(add Adder, note Notifier, st *State, text string) {
	if st.clipSet && text == st.clip {
		return
	}
	st.clip = text
	st.clipSet = true
	var added []string
	for _, magnet := range MagnetsIn(text) {
		id, err := add.Add(magnet)
		if err != nil {
			if !strings.Contains(err.Error(), "already added") && note != nil {
				note.Set(err.Error())
			}
			continue
		}
		added = append(added, id)
	}
	if note == nil || len(added) == 0 {
		return
	}
	if len(added) == 1 {
		note.Set("added magnet " + short(added[0]))
		return
	}
	note.Set(fmt.Sprintf("added %d magnets from clipboard", len(added)))
}

var (
	magnetRE = regexp.MustCompile(`(?i)magnet:\?\S+`)
	xtRE     = regexp.MustCompile(`(?i)(?:^|[?&])xt=urn:bt(?:ih|mh):`)
)

// MagnetsIn returns unique magnet URIs found in text.
func MagnetsIn(text string) []string {
	raw := magnetRE.FindAllString(text, -1)
	if len(raw) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, m := range raw {
		m = strings.TrimRight(m, ".,);]}>\"'")
		if !xtRE.MatchString(m) {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

// UserDownloadDir is the XDG download folder, or ~/Downloads.
func UserDownloadDir() string {
	if out, err := exec.Command("xdg-user-dir", "DOWNLOAD").Output(); err == nil {
		p := strings.TrimSpace(string(out))
		if p != "" && !strings.Contains(p, "\n") {
			return p
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "Downloads")
}

// Dirs is the user download folder plus the tuber data directory.
func Dirs(dataDir string) []string {
	var dirs []string
	if d := UserDownloadDir(); d != "" {
		dirs = append(dirs, d)
	}
	if strings.TrimSpace(dataDir) != "" {
		dirs = append(dirs, dataDir)
	}
	return uniqueDirs(dirs)
}

func uniqueDirs(dirs []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, d := range dirs {
		d = filepath.Clean(strings.TrimSpace(d))
		if d == "" || d == "." {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	return out
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
