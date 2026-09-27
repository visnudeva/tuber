package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/log"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"github.com/visnudeva/tuber/internal/watch"
)

type Status string

const (
	StatusFetching  Status = "fetching"
	StatusVerifying Status = "verifying"
	StatusActive    Status = "active"
	StatusPaused    Status = "paused"
	StatusDone      Status = "done"
)

type Snapshot struct {
	ID         string
	Name       string
	Status     Status
	Progress   float64
	BytesDone  int64
	BytesTotal int64
	DownRate   int64
	UpRate     int64
	Peers      int
	TotalPeers int
	InfoReady  bool
	Source     string
}

// errWiped is returned when an automatic restore tries to add a torrent the
// user already wiped. A deliberate add (the a prompt, a magnet open) clears it.
var errWiped = errors.New("wiped torrent")

type Engine struct {
	mu      sync.Mutex
	client  *torrent.Client
	dataDir string
	items   map[string]*item
	wiped   map[string]struct{}
}

type item struct {
	t           *torrent.Torrent
	source      string
	torrentFile string // .torrent in the download folder, if that is how it was added
	paused      bool
	dropped     bool
	verifying   bool
	lastRead    int64
	lastWrite   int64
	lastSample  time.Time
	downRate    int64
	upRate      int64
}

func New(dataDir string) (*Engine, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	// .part mode clears piece completion whenever the final file isn't present,
	// which wipes resume progress on every restart. Keep persistent sqlite
	// completion and write straight to the final filename instead.
	_ = migratePartFiles(dataDir)

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dataDir
	cfg.Seed = true
	cfg.NoDHT = false
	cfg.DisableTrackers = false
	cfg.ListenPort = 0
	cfg.Logger = log.NewLogger().WithFilterLevel(log.Disabled)

	pc, err := storage.NewDefaultPieceCompletionForDir(dataDir)
	if err != nil {
		pc = storage.NewMapPieceCompletion()
	}
	cfg.DefaultStorage = storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   dataDir,
		UsePartFiles:    g.Some(false),
		PieceCompletion: pc,
	})

	client, err := torrent.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("torrent client: %w", err)
	}

	e := &Engine{
		client:  client,
		dataDir: dataDir,
		items:   make(map[string]*item),
		wiped:   map[string]struct{}{},
	}
	if sess, err := LoadSession(); err == nil {
		e.rememberWiped(sess.Wiped)
	}
	return e, nil
}

// migratePartFiles renames name.ext.part -> name.ext so existing downloads
// keep working after disabling part-file storage.
func migratePartFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".part") {
			continue
		}
		src := filepath.Join(dir, name)
		dst := filepath.Join(dir, strings.TrimSuffix(name, ".part"))
		if _, err := os.Stat(dst); err == nil {
			continue // already have final file
		}
		_ = os.Rename(src, dst)
	}
	return nil
}

func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.client != nil {
		e.client.Close()
		e.client = nil
	}
}

func (e *Engine) DataDir() string { return e.dataDir }

// Add enqueues a torrent. Wiped infohashes are refused so a later launch does
// not bring them back from the session, the piece database, or a magnet still
// sitting on the clipboard.
func (e *Engine) Add(input string) (string, error) {
	return e.add(input, false)
}

// AddUser enqueues a torrent the user asked for and forgets a previous wipe.
func (e *Engine) AddUser(input string) (string, error) {
	return e.add(input, true)
}

// AddFile starts a .torrent placed in a download folder. Saving that file
// again after a wipe starts the torrent; session and clipboard restores do not.
func (e *Engine) AddFile(path string) (string, error) {
	return e.add(path, true)
}

func (e *Engine) add(input string, user bool) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("empty input")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	preview := strings.ToLower(previewInfoHash(input))
	if preview != "" && !user && e.isWiped(preview) {
		return preview, errWiped
	}

	var (
		t   *torrent.Torrent
		err error
	)

	switch {
	case strings.HasPrefix(input, "magnet:"):
		if spec, serr := torrent.TorrentSpecFromMagnetUri(input); serr == nil {
			if mp := localMetaPath(spec.InfoHash.HexString()); fileLooksLikeTorrent(mp) {
				t, err = e.client.AddTorrentFromFile(mp)
			}
		}
		if t == nil {
			t, err = e.client.AddMagnet(input)
		}
	case fileLooksLikeTorrent(input):
		t, err = e.client.AddTorrentFromFile(input)
	case looksLikeInfoHash(input):
		h := strings.TrimPrefix(strings.ToLower(input), "0x")
		if mp := localMetaPath(h); fileLooksLikeTorrent(mp) {
			t, err = e.client.AddTorrentFromFile(mp)
		} else {
			t, err = e.client.AddMagnet("magnet:?xt=urn:btih:" + h)
		}
	default:
		return "", fmt.Errorf("need magnet, .torrent path, or infohash")
	}
	if err != nil {
		return "", err
	}

	id := strings.ToLower(t.InfoHash().HexString())
	if !user && e.isWiped(id) {
		t.Drop()
		return id, errWiped
	}
	if user {
		delete(e.wiped, id)
		if preview != "" {
			delete(e.wiped, preview)
		}
	}
	if existing, ok := e.items[id]; ok {
		return id, fmt.Errorf("already added: %s", existing.t.Name())
	}

	it := &item{
		t:          t,
		source:     input,
		lastSample: time.Now(),
	}
	if origin := downloadTorrentPath(input); origin != "" {
		it.torrentFile = origin
	}
	e.items[id] = it

	go e.bootstrap(it)
	e.persistLocked()
	return id, nil
}

func (e *Engine) bootstrap(it *item) {
	<-it.t.GotInfo()
	if !e.stillHere(it) {
		return
	}
	_ = saveMetainfo(it.t)
	if !e.stillHere(it) {
		_ = os.Remove(localMetaPath(it.t.InfoHash().HexString()))
		return
	}

	e.mu.Lock()
	id := strings.ToLower(it.t.InfoHash().HexString())
	if it.dropped || e.items[id] != it {
		e.mu.Unlock()
		_ = os.Remove(localMetaPath(id))
		return
	}
	paused := it.paused
	it.source = magnetFor(it.t)
	it.verifying = true
	e.persistLocked()
	e.mu.Unlock()

	if !e.stillHere(it) {
		return
	}
	// Re-hash bytes already on disk so incomplete-session data becomes counted progress.
	_ = it.t.VerifyData()

	e.mu.Lock()
	if e.items[strings.ToLower(it.t.InfoHash().HexString())] == it {
		it.verifying = false
		e.persistLocked()
	}
	e.mu.Unlock()

	if !paused && e.stillHere(it) {
		it.t.DownloadAll()
	}
}

func (e *Engine) stillHere(it *item) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if it == nil || it.dropped || it.t == nil {
		return false
	}
	id := strings.ToLower(it.t.InfoHash().HexString())
	return e.items[id] == it
}

func (e *Engine) rememberWiped(hashes []string) {
	if e.wiped == nil {
		e.wiped = map[string]struct{}{}
	}
	for _, h := range hashes {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" {
			e.wiped[h] = struct{}{}
		}
	}
}

func (e *Engine) isWiped(id string) bool {
	if e.wiped == nil {
		return false
	}
	_, ok := e.wiped[strings.ToLower(strings.TrimSpace(id))]
	return ok
}

func (e *Engine) tombstone(id string) {
	if e.wiped == nil {
		e.wiped = map[string]struct{}{}
	}
	id = strings.ToLower(strings.TrimSpace(id))
	if id != "" {
		e.wiped[id] = struct{}{}
	}
}

func (e *Engine) wipedSlice() []string {
	if len(e.wiped) == 0 {
		return nil
	}
	out := make([]string, 0, len(e.wiped))
	for h := range e.wiped {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

func previewInfoHash(input string) string {
	input = strings.TrimSpace(input)
	switch {
	case strings.HasPrefix(input, "magnet:"):
		spec, err := torrent.TorrentSpecFromMagnetUri(input)
		if err != nil {
			return ""
		}
		return spec.InfoHash.HexString()
	case fileLooksLikeTorrent(input):
		h, err := fileInfoHash(input)
		if err != nil {
			return ""
		}
		return h
	case looksLikeInfoHash(input):
		return strings.TrimPrefix(strings.ToLower(input), "0x")
	default:
		return ""
	}
}

func saveMetainfo(t *torrent.Torrent) error {
	dir, err := metaDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, t.InfoHash().HexString()+".torrent")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	mi := t.Metainfo()
	return (&mi).Write(f)
}

func metaDir() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cfg, "tuber", "meta")
	return dir, os.MkdirAll(dir, 0o755)
}

func localMetaPath(infoHash string) string {
	dir, err := metaDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, strings.ToLower(infoHash)+".torrent")
}

func (e *Engine) Pause(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	it, ok := e.items[id]
	if !ok {
		return fmt.Errorf("unknown torrent")
	}
	it.paused = true
	it.t.DisallowDataDownload()
	it.t.DisallowDataUpload()
	e.persistLocked()
	return nil
}

func (e *Engine) Resume(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	it, ok := e.items[id]
	if !ok {
		return fmt.Errorf("unknown torrent")
	}
	it.paused = false
	it.t.AllowDataDownload()
	it.t.AllowDataUpload()
	if it.t.Info() != nil {
		it.t.DownloadAll()
	}
	e.persistLocked()
	return nil
}

// TogglePause returns true when the torrent is paused after the call.
func (e *Engine) TogglePause(id string) (paused bool, err error) {
	e.mu.Lock()
	it, ok := e.items[id]
	if !ok {
		e.mu.Unlock()
		return false, fmt.Errorf("unknown torrent")
	}
	wasPaused := it.paused
	e.mu.Unlock()
	if wasPaused {
		return false, e.Resume(id)
	}
	return true, e.Pause(id)
}

func (e *Engine) Delete(id string, removeFiles bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	id = strings.ToLower(id)
	it, ok := e.items[id]
	if !ok {
		return fmt.Errorf("unknown torrent")
	}

	var roots []string
	if removeFiles {
		if it.t.Info() == nil {
			return fmt.Errorf("wipe: metadata not ready yet — wait until info is fetched, or press r to remove without deleting files")
		}
		// Wipe top-level paths (torrent folder or single file), not each leaf
		// with os.Remove — that silently fails on directories.
		seen := make(map[string]struct{})
		for _, f := range it.t.Files() {
			rel := filepath.Clean(f.Path())
			if rel == "." || rel == "" {
				continue
			}
			top, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
			if top == "" || top == ".." {
				continue
			}
			root := filepath.Join(e.dataDir, top)
			if _, ok := seen[root]; ok {
				continue
			}
			seen[root] = struct{}{}
			roots = append(roots, root)
		}
	}

	origin := it.torrentFile
	it.dropped = true
	it.t.Drop()
	delete(e.items, id)
	if removeFiles {
		e.tombstone(id)
	}
	forgetTorrentState(e.dataDir, id)
	e.persistLocked()

	if removeFiles {
		for _, p := range roots {
			if err := os.RemoveAll(p); err != nil {
				return fmt.Errorf("wipe %s: %w", p, err)
			}
			_ = os.RemoveAll(p + ".part")
		}
		if err := removeMatchingTorrents(id, origin, watch.Dirs(e.dataDir)); err != nil {
			return err
		}
	}
	return nil
}

// Persist writes the current session to disk. Safe to call anytime.
func (e *Engine) Persist() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.persistLocked()
}

func (e *Engine) persistLocked() error {
	return SaveSession(e.sessionLocked())
}

func (e *Engine) sessionLocked() Session {
	s := Session{DataDir: e.dataDir, Wiped: e.wipedSlice()}
	for id, it := range e.items {
		src := it.source
		if !strings.HasPrefix(src, "magnet:") {
			src = magnetFor(it.t)
		}
		meta := localMetaPath(id)
		if !fileLooksLikeTorrent(meta) {
			meta = ""
		}
		s.Torrents = append(s.Torrents, SessionTorrent{
			Magnet: src,
			Meta:   meta,
			File:   it.torrentFile,
			Paused: it.paused,
			Name:   it.t.Name(),
		})
	}
	return s
}

func magnetFor(t *torrent.Torrent) string {
	m := metainfo.Magnet{InfoHash: t.InfoHash()}
	if name := t.Name(); name != "" {
		m.DisplayName = name
	}
	return m.String()
}

func (e *Engine) Snapshots() []Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	out := make([]Snapshot, 0, len(e.items))
	for id, it := range e.items {
		stats := it.t.Stats()
		read := stats.BytesReadUsefulData.Int64()
		write := stats.BytesWrittenData.Int64()
		elapsed := now.Sub(it.lastSample).Seconds()
		if elapsed >= 0.5 {
			if it.lastSample.IsZero() {
				it.downRate, it.upRate = 0, 0
			} else {
				it.downRate = int64(float64(read-it.lastRead) / elapsed)
				it.upRate = int64(float64(write-it.lastWrite) / elapsed)
				if it.downRate < 0 {
					it.downRate = 0
				}
				if it.upRate < 0 {
					it.upRate = 0
				}
			}
			it.lastRead = read
			it.lastWrite = write
			it.lastSample = now
		}

		info := it.t.Info()
		var total, done int64
		var progress float64
		name := it.t.Name()
		status := StatusFetching
		infoReady := info != nil
		if infoReady {
			total = it.t.Length()
			done = it.t.BytesCompleted()
			if total > 0 {
				progress = float64(done) / float64(total)
			}
			switch {
			case it.verifying:
				status = StatusVerifying
			case it.paused:
				status = StatusPaused
			case total > 0 && done >= total:
				status = StatusDone
			default:
				status = StatusActive
			}
			if name == "" {
				name = id[:8]
			}
		} else if name == "" {
			name = "fetching metadata…"
		}

		out = append(out, Snapshot{
			ID:         id,
			Name:       name,
			Status:     status,
			Progress:   progress,
			BytesDone:  done,
			BytesTotal: total,
			DownRate:   it.downRate,
			UpRate:     it.upRate,
			Peers:      stats.ActivePeers,
			TotalPeers: stats.TotalPeers,
			InfoReady:  infoReady,
			Source:     it.source,
		})
	}

	// Stable-ish order by name then id.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Name < out[i].Name || (out[j].Name == out[i].Name && out[j].ID < out[i].ID) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// RestoreSession re-adds torrents from a saved session (and optional paused flags).
func (e *Engine) RestoreSession(sess Session) {
	for _, t := range sess.Torrents {
		src := strings.TrimSpace(t.Meta)
		if !fileLooksLikeTorrent(src) {
			src = strings.TrimSpace(t.Magnet)
		}
		if src == "" {
			continue
		}
		id, err := e.Add(src)
		if err != nil {
			continue
		}
		e.noteTorrentFile(id, t.File)
		if t.Paused {
			_ = e.Pause(id)
		}
	}
}

// RestoreIncompleteFromDisk picks up infohashes left in the piece DB when the
// process was killed before session save. Only resumes hashes that still have
// a saved .torrent meta — remove/wipe deletes that file so they stay gone.
func (e *Engine) RestoreIncompleteFromDisk() {
	hashes, err := IncompleteInfoHashes(e.dataDir)
	if err != nil || len(hashes) == 0 {
		return
	}
	e.mu.Lock()
	have := make(map[string]struct{}, len(e.items))
	for id := range e.items {
		have[id] = struct{}{}
	}
	e.mu.Unlock()

	for _, h := range hashes {
		if _, ok := have[h]; ok {
			continue
		}
		if !fileLooksLikeTorrent(localMetaPath(h)) {
			continue
		}
		_, _ = e.Add("magnet:?xt=urn:btih:" + h)
	}
}

// downloadTorrentPath is a user-facing .torrent, not the copy tuber keeps
// under its config directory for resume.
func downloadTorrentPath(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." || isInternalMeta(path) || !fileLooksLikeTorrent(path) {
		return ""
	}
	return path
}

func isInternalMeta(path string) bool {
	dir, err := metaDir()
	if err != nil {
		return false
	}
	return filepath.Dir(filepath.Clean(path)) == filepath.Clean(dir)
}

func (e *Engine) noteTorrentFile(id, path string) {
	path = downloadTorrentPath(path)
	if path == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	it, ok := e.items[id]
	if !ok || it.torrentFile != "" {
		return
	}
	it.torrentFile = path
	e.persistLocked()
}

// removeMatchingTorrents deletes the .torrent that was picked up from a
// download folder, plus any other top-level .torrent in those folders with
// the same infohash.
func removeMatchingTorrents(infoHash, remembered string, dirs []string) error {
	seen := map[string]struct{}{}
	var doomed []string
	add := func(p string) {
		p = filepath.Clean(strings.TrimSpace(p))
		if p == "" || isInternalMeta(p) || !fileLooksLikeTorrent(p) {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		doomed = append(doomed, p)
	}
	add(remembered)
	want := strings.ToLower(strings.TrimSpace(infoHash))
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, ent := range entries {
			if ent.IsDir() || !strings.HasSuffix(strings.ToLower(ent.Name()), ".torrent") {
				continue
			}
			path := filepath.Join(dir, ent.Name())
			got, err := fileInfoHash(path)
			if err != nil || !strings.EqualFold(got, want) {
				continue
			}
			add(path)
		}
	}
	for _, p := range doomed {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("wipe %s: %w", p, err)
		}
	}
	return nil
}

func fileInfoHash(path string) (string, error) {
	mi, err := metainfo.LoadFromFile(path)
	if err != nil {
		return "", err
	}
	return mi.HashInfoBytes().HexString(), nil
}

func fileLooksLikeTorrent(path string) bool {
	if !strings.HasSuffix(strings.ToLower(path), ".torrent") {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func looksLikeInfoHash(s string) bool {
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	if len(s) != 40 && len(s) != 32 {
		return false
	}
	for _, c := range s {
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		isBase32 := (c >= 'a' && c <= 'z') || (c >= '2' && c <= '7')
		if len(s) == 40 && !isHex {
			return false
		}
		if len(s) == 32 && !isBase32 {
			return false
		}
	}
	return true
}
