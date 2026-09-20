package engine

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type SessionTorrent struct {
	Magnet string `json:"magnet"`
	Meta   string `json:"meta,omitempty"` // local .torrent path for instant resume
	Paused bool   `json:"paused,omitempty"`
	Name   string `json:"name,omitempty"`
}

type Session struct {
	DataDir  string           `json:"data_dir,omitempty"`
	Torrents []SessionTorrent `json:"torrents,omitempty"`
	// Sources is legacy (magnet/infohash strings only).
	Sources []string `json:"sources,omitempty"`
}

func SessionPath() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cfg, "tuber")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

func LoadSession() (Session, error) {
	path, err := SessionPath()
	if err != nil {
		return Session{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Session{}, nil
		}
		return Session{}, err
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return Session{}, err
	}
	// Migrate legacy Sources into Torrents.
	if len(s.Torrents) == 0 && len(s.Sources) > 0 {
		for _, src := range s.Sources {
			src = strings.TrimSpace(src)
			if src == "" {
				continue
			}
			if !strings.HasPrefix(src, "magnet:") && looksLikeInfoHash(src) {
				src = "magnet:?xt=urn:btih:" + strings.TrimPrefix(strings.ToLower(src), "0x")
			}
			s.Torrents = append(s.Torrents, SessionTorrent{Magnet: src})
		}
	}
	return s, nil
}

func SaveSession(s Session) error {
	path, err := SessionPath()
	if err != nil {
		return err
	}
	// Keep Sources in sync for older readers.
	s.Sources = nil
	for _, t := range s.Torrents {
		if t.Magnet != "" {
			s.Sources = append(s.Sources, t.Magnet)
		}
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// IncompleteInfoHashes reads infohashes from anacrolix piece DB so a killed
// session can still resume partial downloads in dataDir.
func IncompleteInfoHashes(dataDir string) ([]string, error) {
	dbPath := filepath.Join(dataDir, ".torrent.db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT DISTINCT infohash FROM piece_completion`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := map[string]struct{}{}
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			continue
		}
		h = strings.ToLower(strings.TrimSpace(h))
		if len(h) != 40 {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out, rows.Err()
}
