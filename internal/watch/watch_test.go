package watch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeAdd struct {
	calls []string
	fail  map[string]error
}

func (f *fakeAdd) Add(input string) (string, error) {
	f.calls = append(f.calls, input)
	if err, ok := f.fail[input]; ok {
		return "", err
	}
	return "0123456789abcdef", nil
}

type fakeNote struct {
	msgs []string
}

func (n *fakeNote) Set(msg string) { n.msgs = append(n.msgs, msg) }

func TestMagnetsIn(t *testing.T) {
	hex := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Demo"
	text := "see <" + hex + "> and also magnet:?xt=urn:btih:ABCDEFGHIJKLMNOPQRSTUVWXYZ234567."
	got := MagnetsIn(text)
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0] != hex {
		t.Fatalf("first: %s", got[0])
	}
	if strings.HasSuffix(got[1], ".") {
		t.Fatalf("trailing dot kept: %s", got[1])
	}
	if MagnetsIn("copied a git hash 0123456789abcdef0123456789abcdef01234567") != nil {
		t.Fatal("bare hash treated as magnet")
	}
	dup := MagnetsIn(hex + " " + hex)
	if len(dup) != 1 {
		t.Fatalf("dup: %#v", dup)
	}
}

func TestScanTorrentFilesTopLevelOnly(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "sub")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "movie.torrent"), "d8:announce0:e")
	write(filepath.Join(dir, "notes.txt"), "nope")
	write(filepath.Join(dir, "empty.torrent"), "")
	write(filepath.Join(nested, "hidden.torrent"), "d8:announce0:e")

	add := &fakeAdd{}
	note := &fakeNote{}
	st := &State{}
	scanTorrentFiles(add, note, st, []string{dir})
	if len(add.calls) != 1 || !strings.HasSuffix(add.calls[0], "movie.torrent") {
		t.Fatalf("calls %#v", add.calls)
	}
	if len(note.msgs) != 1 || note.msgs[0] != "added .torrent file from the download folder" {
		t.Fatalf("notes %#v", note.msgs)
	}

	scanTorrentFiles(add, note, st, []string{dir})
	if len(add.calls) != 1 {
		t.Fatalf("rescanned: %#v", add.calls)
	}
}

func TestTorrentFileAddedAgainAfterItDisappears(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.torrent")
	body := []byte("d8:announce0:e")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	add := &fakeAdd{}
	note := &fakeNote{}
	st := &State{}
	scanTorrentFiles(add, note, st, []string{dir})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	scanTorrentFiles(add, note, st, []string{dir})
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	scanTorrentFiles(add, note, st, []string{dir})
	if len(add.calls) != 2 || add.calls[0] != path || add.calls[1] != path {
		t.Fatalf("calls %#v", add.calls)
	}
}

func TestWipedTorrentStaysQuiet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.torrent")
	if err := os.WriteFile(path, []byte("d8:announce0:e"), 0o644); err != nil {
		t.Fatal(err)
	}
	add := &fakeAdd{fail: map[string]error{path: errors.New("wiped torrent")}}
	note := &fakeNote{}
	st := &State{}
	scanTorrentFiles(add, note, st, []string{dir})
	scanTorrentFiles(add, note, st, []string{dir})
	if len(add.calls) != 1 {
		t.Fatalf("calls %#v", add.calls)
	}
	if len(note.msgs) != 0 {
		t.Fatalf("notes %#v", note.msgs)
	}
}

func TestScanClipboardOncePerChange(t *testing.T) {
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
	add := &fakeAdd{}
	note := &fakeNote{}
	st := &State{}
	scanClipboard(add, note, st, "hello")
	if len(add.calls) != 0 {
		t.Fatalf("plain text added: %#v", add.calls)
	}
	scanClipboard(add, note, st, "grab "+magnet)
	scanClipboard(add, note, st, "grab "+magnet)
	if len(add.calls) != 1 {
		t.Fatalf("calls %#v", add.calls)
	}
	if note.msgs[0] != "added magnet from the clipboard" {
		t.Fatalf("note %q", note.msgs[0])
	}

	add.fail = map[string]error{magnet: errAlready{}}
	st.clipSet = false
	scanClipboard(add, note, st, magnet)
	if len(note.msgs) != 1 {
		t.Fatalf("already-added should stay quiet, notes %#v", note.msgs)
	}
}

type errAlready struct{}

func (errAlready) Error() string { return "already added: demo" }

func TestCollectDirsAlwaysIncludesDownloads(t *testing.T) {
	home := "/home/user"
	data := home + "/Downloads/tuber"
	got := collectDirs(home, home, data)
	joined := strings.Join(got, "|")
	if strings.Contains(joined, home+"|") || joined == home || strings.HasSuffix(joined, "|"+home) {
		t.Fatalf("scanned home: %#v", got)
	}
	want := home + "/Downloads"
	found := false
	for _, d := range got {
		if d == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing Downloads: %#v", got)
	}
}

func TestParseUserDirsDownloadIgnoresHome(t *testing.T) {
	text := "XDG_DOWNLOAD_DIR=\"$HOME\"\n"
	if parseUserDirsDownload(text, "/home/user") != "" {
		t.Fatal("home treated as download dir")
	}
	got := parseUserDirsDownload("XDG_DOWNLOAD_DIR=\"$HOME/Downloads\"\n", "/home/user")
	if got != "/home/user/Downloads" {
		t.Fatalf("got %q", got)
	}
}

func TestFailedTorrentFileIsRetried(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.torrent")
	if err := os.WriteFile(path, []byte("d8:announce0:e"), 0o644); err != nil {
		t.Fatal(err)
	}
	add := &fakeAdd{fail: map[string]error{path: errBoom{}}}
	note := &fakeNote{}
	st := &State{}
	scanTorrentFiles(add, note, st, []string{dir})
	scanTorrentFiles(add, note, st, []string{dir})
	if len(add.calls) != 2 {
		t.Fatalf("calls %#v", add.calls)
	}
	if len(note.msgs) != 1 || note.msgs[0] != "movie.torrent: boom" {
		t.Fatalf("notes %#v", note.msgs)
	}
	delete(add.fail, path)
	scanTorrentFiles(add, note, st, []string{dir})
	if len(add.calls) != 3 {
		t.Fatalf("no retry success: %#v", add.calls)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }

func TestPollReadsClipboard(t *testing.T) {
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
	add := &fakeAdd{}
	note := &fakeNote{}
	st := &State{}
	cfg := Config{
		ReadClipboard: func() (string, error) { return magnet, nil },
		Interval:      time.Hour,
	}
	poll(add, note, st, cfg)
	poll(add, note, st, cfg)
	if len(add.calls) != 1 {
		t.Fatalf("calls %#v", add.calls)
	}
}
