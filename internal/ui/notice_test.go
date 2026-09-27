package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/visnudeva/tuber/internal/engine"
)

func TestAddedNoticeReturnsToFocusedTorrent(t *testing.T) {
	notes := &Notes{}
	notes.Added("added magnet from the clipboard", "abc")
	m := New(nil, notes, nil)
	m.snaps = []engine.Snapshot{{ID: "abc", Name: "Ubuntu ISO"}}

	next, _ := m.Update(tickMsg(time.Now()))
	m = next.(Model)
	if m.status != "added magnet from the clipboard" {
		t.Fatalf("notice %q", m.status)
	}
	if m.cursor != 0 {
		t.Fatalf("cursor %d", m.cursor)
	}

	// Same notice posted again must not keep the line stuck.
	notes.Added("added magnet from the clipboard", "abc")
	m.statusUntil = time.Now().Add(-time.Millisecond)
	next, _ = m.Update(tickMsg(time.Now()))
	m = next.(Model)
	if m.status != "Ubuntu ISO" {
		t.Fatalf("after 3s got %q", m.status)
	}

	next, _ = m.Update(noticeExpiredMsg{gen: m.noticeGen})
	m = next.(Model)
	if m.status != "Ubuntu ISO" {
		t.Fatalf("expiry msg got %q", m.status)
	}

	notes.Added("added .torrent file from the download folder", "def")
	m.snaps = []engine.Snapshot{
		{ID: "abc", Name: "Ubuntu ISO"},
		{ID: "def", Name: "Movie"},
	}
	next, _ = m.Update(tickMsg(time.Now()))
	m = next.(Model)
	if m.status != "added .torrent file from the download folder" {
		t.Fatalf("second notice %q", m.status)
	}
	if m.cursor != 1 {
		t.Fatalf("cursor %d", m.cursor)
	}
}

func TestWipeStatusReturnsToNextTorrent(t *testing.T) {
	m := New(nil, &Notes{}, nil)
	m.snaps = []engine.Snapshot{
		{ID: "a", Name: "First"},
		{ID: "b", Name: "Second"},
	}
	m.cursor = 0 // wipe moved the cursor onto the torrent that remains
	cmd := m.setSticky("wiped (files deleted)")
	if m.status != "wiped (files deleted)" {
		t.Fatalf("status %q", m.status)
	}
	if cmd == nil {
		t.Fatal("expected expiry command")
	}

	m.statusUntil = time.Now().Add(-time.Millisecond)
	next, _ := m.Update(tickMsg(time.Now()))
	m = next.(Model)
	if m.status != "First" {
		t.Fatalf("after wipe got %q", m.status)
	}
}

func TestNoticeCmdIsReturned(t *testing.T) {
	notes := &Notes{}
	notes.Added("added .torrent file from the download folder", "abc")
	m := New(nil, notes, nil)
	m.snaps = []engine.Snapshot{{ID: "abc", Name: "Movie"}}
	_, cmd := m.Update(tickMsg(time.Now()))
	if cmd == nil {
		t.Fatal("expected a follow-up command")
	}
	// Batch runs the commands; one of them is the 3s expiry tick.
	msg := cmd()
	if _, ok := msg.(tea.BatchMsg); !ok && msg != nil {
		// A single command is also fine if Batch collapses.
		if _, ok := msg.(noticeExpiredMsg); ok {
			return
		}
	}
}
