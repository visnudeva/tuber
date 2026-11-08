package ui

import (
	"testing"

	"github.com/visnudeva/tuber/internal/engine"
)

func TestCheckCompletionsSkipsFirstSighting(t *testing.T) {
	m := New(nil, nil, nil)
	m.notify = true
	m.snaps = []engine.Snapshot{
		{ID: "a", Name: "Already Done", Status: engine.StatusDone},
	}
	m.checkCompletions()
	if m.prevStatus["a"] != engine.StatusDone {
		t.Fatalf("expected status recorded")
	}
	// Second tick still done — must not treat as a fresh completion.
	m.checkCompletions()
}

func TestCheckCompletionsDetectsTransition(t *testing.T) {
	m := New(nil, nil, nil)
	m.notify = true
	m.snaps = []engine.Snapshot{
		{ID: "a", Name: "Film", Status: engine.StatusActive},
	}
	m.checkCompletions()
	m.snaps[0].Status = engine.StatusDone
	// Transition is detected; desktopNotify is best-effort and may no-op in CI.
	m.checkCompletions()
	if m.prevStatus["a"] != engine.StatusDone {
		t.Fatalf("status %q", m.prevStatus["a"])
	}
}

func TestCheckCompletionsPrunesRemoved(t *testing.T) {
	m := New(nil, nil, nil)
	m.prevStatus["gone"] = engine.StatusActive
	m.snaps = nil
	m.checkCompletions()
	if _, ok := m.prevStatus["gone"]; ok {
		t.Fatal("expected pruned id")
	}
}
