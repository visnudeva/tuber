package ui

import (
	"sort"

	"github.com/visnudeva/tuber/internal/engine"
)

type sortKey int

const (
	sortByName sortKey = iota
	sortByDone
	sortByStatus
	sortByDown
	sortByUp
	sortByETA
	sortByPeers
)

// Order for the s key: each press advances one step, wrapping to name.
var sortCycle = []sortKey{
	sortByName,
	sortByDone,
	sortByStatus,
	sortByDown,
	sortByUp,
	sortByETA,
	sortByPeers,
}

func (k sortKey) label() string {
	switch k {
	case sortByDone:
		return "done"
	case sortByStatus:
		return "status"
	case sortByDown:
		return "down"
	case sortByUp:
		return "up"
	case sortByETA:
		return "eta"
	case sortByPeers:
		return "peers"
	default:
		return "name"
	}
}

func (m *Model) cycleSort() {
	for i, k := range sortCycle {
		if k == m.sortBy {
			m.sortBy = sortCycle[(i+1)%len(sortCycle)]
			return
		}
	}
	m.sortBy = sortByDone
}

func (m *Model) sortSnaps() {
	key := m.sortBy
	sort.SliceStable(m.snaps, func(i, j int) bool {
		a, b := m.snaps[i], m.snaps[j]
		if cmp := compareBy(key, a, b); cmp != 0 {
			return cmp < 0
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
}

// compareBy returns -1 if a should come before b, 1 if after, 0 if equal
// for the active key. Numeric "interest" fields prefer higher values first
// (done, rates, peers); ETA prefers sooner first.
func compareBy(key sortKey, a, b engine.Snapshot) int {
	switch key {
	case sortByDone:
		return cmpFloatDesc(a.Progress, b.Progress)
	case sortByStatus:
		return cmpIntAsc(statusRank(a.Status), statusRank(b.Status))
	case sortByDown:
		return cmpInt64Desc(a.DownRate, b.DownRate)
	case sortByUp:
		return cmpInt64Desc(a.UpRate, b.UpRate)
	case sortByETA:
		return cmpETA(a, b)
	case sortByPeers:
		if c := cmpIntDesc(a.Peers, b.Peers); c != 0 {
			return c
		}
		return cmpIntDesc(a.TotalPeers, b.TotalPeers)
	default:
		if a.Name != b.Name {
			return cmpStringAsc(a.Name, b.Name)
		}
		return 0
	}
}

func statusRank(st engine.Status) int {
	switch st {
	case engine.StatusActive:
		return 0
	case engine.StatusVerifying:
		return 1
	case engine.StatusFetching:
		return 2
	case engine.StatusPaused:
		return 3
	case engine.StatusDone:
		return 4
	default:
		return 5
	}
}

// etaRank returns (seconds, class). Lower class sorts first; within class 0,
// lower seconds sort first. Classes: 0 = known ETA, 1 = stalled (∞), 2 = n/a.
func etaRank(s engine.Snapshot) (secs int64, class int) {
	switch s.Status {
	case engine.StatusDone, engine.StatusPaused, engine.StatusFetching, engine.StatusVerifying:
		return 0, 2
	}
	if !s.InfoReady || s.BytesTotal <= 0 {
		return 0, 2
	}
	remaining := s.BytesTotal - s.BytesDone
	if remaining <= 0 {
		return 0, 2
	}
	if s.DownRate <= 0 {
		return 0, 1
	}
	return remaining / s.DownRate, 0
}

func cmpETA(a, b engine.Snapshot) int {
	as, ac := etaRank(a)
	bs, bc := etaRank(b)
	if ac != bc {
		return cmpIntAsc(ac, bc)
	}
	return cmpInt64Asc(as, bs)
}

func cmpFloatDesc(a, b float64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func cmpIntDesc(a, b int) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func cmpIntAsc(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpInt64Desc(a, b int64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func cmpInt64Asc(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpStringAsc(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
