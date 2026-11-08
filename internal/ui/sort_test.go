package ui

import (
	"testing"

	"github.com/visnudeva/tuber/internal/engine"
)

func TestCycleSortOrder(t *testing.T) {
	m := New(nil, nil, nil)
	want := []sortKey{
		sortByDone, sortByStatus, sortByDown, sortByUp, sortByETA, sortByPeers, sortByName,
	}
	for i, k := range want {
		m.cycleSort()
		if m.sortBy != k {
			t.Fatalf("step %d: got %v want %v", i, m.sortBy, k)
		}
	}
}

func TestSortByDoneDescending(t *testing.T) {
	m := New(nil, nil, nil)
	m.sortBy = sortByDone
	m.snaps = []engine.Snapshot{
		{ID: "a", Name: "a", Progress: 0.2},
		{ID: "b", Name: "b", Progress: 0.9},
		{ID: "c", Name: "c", Progress: 0.5},
	}
	m.sortSnaps()
	if m.snaps[0].ID != "b" || m.snaps[1].ID != "c" || m.snaps[2].ID != "a" {
		t.Fatalf("order %+v", ids(m.snaps))
	}
}

func TestSortByDownDescending(t *testing.T) {
	m := New(nil, nil, nil)
	m.sortBy = sortByDown
	m.snaps = []engine.Snapshot{
		{ID: "a", Name: "a", DownRate: 100},
		{ID: "b", Name: "b", DownRate: 900},
		{ID: "c", Name: "c", DownRate: 400},
	}
	m.sortSnaps()
	if m.snaps[0].ID != "b" || m.snaps[1].ID != "c" || m.snaps[2].ID != "a" {
		t.Fatalf("order %+v", ids(m.snaps))
	}
}

func TestSortByETASoonestFirst(t *testing.T) {
	m := New(nil, nil, nil)
	m.sortBy = sortByETA
	m.snaps = []engine.Snapshot{
		{ID: "slow", Name: "slow", Status: engine.StatusActive, InfoReady: true, BytesTotal: 1000, BytesDone: 0, DownRate: 10},  // 100s
		{ID: "fast", Name: "fast", Status: engine.StatusActive, InfoReady: true, BytesTotal: 1000, BytesDone: 0, DownRate: 100}, // 10s
		{ID: "stall", Name: "stall", Status: engine.StatusActive, InfoReady: true, BytesTotal: 1000, BytesDone: 0, DownRate: 0},
		{ID: "done", Name: "done", Status: engine.StatusDone, InfoReady: true, BytesTotal: 1000, BytesDone: 1000},
	}
	m.sortSnaps()
	got := ids(m.snaps)
	want := []string{"fast", "slow", "stall", "done"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %+v want %+v", got, want)
		}
	}
}

func TestSortByStatusPriority(t *testing.T) {
	m := New(nil, nil, nil)
	m.sortBy = sortByStatus
	m.snaps = []engine.Snapshot{
		{ID: "done", Name: "z", Status: engine.StatusDone},
		{ID: "active", Name: "a", Status: engine.StatusActive},
		{ID: "paused", Name: "p", Status: engine.StatusPaused},
		{ID: "meta", Name: "m", Status: engine.StatusFetching},
	}
	m.sortSnaps()
	got := ids(m.snaps)
	want := []string{"active", "meta", "paused", "done"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %+v want %+v", got, want)
		}
	}
}

func TestSortKeepsSelection(t *testing.T) {
	m := New(nil, nil, nil)
	m.snaps = []engine.Snapshot{
		{ID: "a", Name: "aaa", Progress: 0.1},
		{ID: "b", Name: "bbb", Progress: 0.9},
	}
	m.cursor = 0 // on "a"
	m.sortBy = sortByDone
	m.sortSnaps()
	m.selectID("a")
	if m.cursor != 1 {
		t.Fatalf("cursor %d after re-sort, want 1", m.cursor)
	}
}

func ids(snaps []engine.Snapshot) []string {
	out := make([]string, len(snaps))
	for i, s := range snaps {
		out[i] = s.ID
	}
	return out
}
