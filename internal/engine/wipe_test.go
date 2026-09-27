package engine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

func writeTorrent(t *testing.T, dir, name string) (path, hash string) {
	t.Helper()
	payload := filepath.Join(t.TempDir(), "payload.txt")
	if err := os.WriteFile(payload, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	var info metainfo.Info
	info.PieceLength = 16 * 1024
	if err := info.BuildFromFilePath(payload); err != nil {
		t.Fatal(err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{Announce: "http://tracker.example/announce"}
	mi.InfoBytes = infoBytes
	path = filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := mi.Write(f); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	hash, err = fileInfoHash(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, hash
}

func TestRemoveMatchingTorrentsDeletesDownloadCopy(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "other.torrent")
	if err := os.WriteFile(keep, []byte("not-a-torrent"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, hash := writeTorrent(t, dir, "movie.torrent")
	if err := removeMatchingTorrents(hash, path, []string{dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("source torrent still present: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated file removed: %v", err)
	}
}

func TestWipeIsNotRestoredOnNextLaunch(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	downloads := t.TempDir()
	path, hash := writeTorrent(t, downloads, "movie.torrent")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	eng, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	id, err := eng.Add(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Delete(id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("download-folder torrent still present: %v", err)
	}

	sess, err := LoadSession()
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Torrents) != 0 {
		t.Fatalf("session still has torrents: %+v", sess.Torrents)
	}
	if len(sess.Wiped) != 1 || sess.Wiped[0] != hash {
		t.Fatalf("wiped list %#v, hash %s", sess.Wiped, hash)
	}

	next, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	next.RestoreSession(sess)
	magnet := "magnet:?xt=urn:btih:" + hash
	if _, err := next.Add(magnet); !errors.Is(err, errWiped) {
		t.Fatalf("restored wiped torrent: %v", err)
	}
	if got := next.Snapshots(); len(got) != 0 {
		t.Fatalf("snapshots after restore: %+v", got)
	}

	again := filepath.Join(downloads, "movie.torrent")
	if err := os.WriteFile(again, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := next.AddFile(again); err != nil {
		t.Fatal(err)
	}
	if next.isWiped(hash) {
		t.Fatal("deliberate add left the wipe in place")
	}
	if len(next.Snapshots()) != 1 {
		t.Fatalf("deliberate add did not return the torrent")
	}
}
