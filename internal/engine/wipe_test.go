package engine

import (
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
