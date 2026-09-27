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
	errs    map[string]string
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
	if st.errs == nil {
		st.errs = map[string]string{}
	}
	var added []string
	for _, dir := range uniqueDirs(dirs) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, ent := range entries {
			name := ent.Name()
			if ent.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".torrent") {
				continue
			}
			path := filepath.Join(dir, name)
			info, err := os.Stat(path)
			if err != nil || info.IsDir() || info.Size() == 0 {
				continue
			}
			stmp := stamp{mod: info.ModTime().UnixNano(), size: info.Size()}
			if prev, ok := st.files[path]; ok && prev == stmp {
				continue
			}
			id, err := add.Add(path)
			if err != nil {
				if strings.Contains(err.Error(), "already added") {
					st.files[path] = stmp
					delete(st.errs, path)
					continue
				}
				msg := name + ": " + err.Error()
				if note != nil && st.errs[path] != msg {
					note.Set(msg)
					st.errs[path] = msg
				}
				continue
			}
			st.files[path] = stmp
			delete(st.errs, path)
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

// UserDownloadDir is the XDG download folder when it is set to a real
// directory, otherwise ~/Downloads. An unconfigured xdg-user-dir returns the
// home directory itself; that is not the download folder.
func UserDownloadDir() string {
	home, _ := os.UserHomeDir()
	if d := configuredDownload(home); d != "" {
		return d
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, "Downloads")
}

// Dirs is ~/Downloads, the XDG download folder when that is somewhere else,
// and the tuber data directory. The home directory itself is never scanned.
func Dirs(dataDir string) []string {
	home, _ := os.UserHomeDir()
	return collectDirs(home, configuredDownload(home), dataDir)
}

func configuredDownload(home string) string {
	if d := downloadFromUserDirs(home); d != "" {
		return d
	}
	out, err := exec.Command("xdg-user-dir", "DOWNLOAD").Output()
	if err != nil {
		return ""
	}
	return acceptDownload(strings.TrimSpace(string(out)), home)
}

func downloadFromUserDirs(home string) string {
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		if home == "" {
			return ""
		}
		cfg = filepath.Join(home, ".config")
	}
	b, err := os.ReadFile(filepath.Join(cfg, "user-dirs.dirs"))
	if err != nil {
		return ""
	}
	return parseUserDirsDownload(string(b), home)
}

func parseUserDirsDownload(text, home string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "XDG_DOWNLOAD_DIR" {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"`)
		val = strings.ReplaceAll(val, "${HOME}", home)
		val = strings.ReplaceAll(val, "$HOME", home)
		return acceptDownload(val, home)
	}
	return ""
}

func acceptDownload(path, home string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	home = filepath.Clean(strings.TrimSpace(home))
	if path == "" || path == "." || path == "/" || strings.Contains(path, "\n") {
		return ""
	}
	if home != "" && home != "." && path == home {
		return ""
	}
	return path
}

func collectDirs(home, xdg, dataDir string) []string {
	var dirs []string
	add := func(p string) {
		if p = acceptDownload(p, home); p != "" {
			dirs = append(dirs, p)
		}
	}
	home = filepath.Clean(strings.TrimSpace(home))
	if home != "" && home != "." {
		add(filepath.Join(home, "Downloads"))
	}
	add(xdg)
	add(dataDir)
	if p := filepath.Clean(strings.TrimSpace(dataDir)); p != "" && p != "." {
		add(filepath.Dir(p))
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
