package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/visnudeva/tuber/internal/engine"
)

type tickMsg time.Time

// Notes carries status text from the IPC handoff goroutine into the TUI.
type Notes struct {
	mu  sync.Mutex
	msg string
}

func (n *Notes) Set(msg string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	n.msg = msg
	n.mu.Unlock()
}

func (n *Notes) Take() string {
	if n == nil {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	msg := n.msg
	n.msg = ""
	return msg
}

type Model struct {
	eng      *engine.Engine
	notes    *Notes
	snaps    []engine.Snapshot
	cursor   int
	width    int
	height   int
	adding   bool
	input    textinput.Model
	status   string
	errFlash string
	quit     bool
}

func New(eng *engine.Engine, notes *Notes) Model {
	ti := textinput.New()
	ti.Placeholder = "magnet:?xt=…  or  /path/to/file.torrent  or  infohash"
	ti.CharLimit = 2048
	ti.Width = 72
	return Model{
		eng:    eng,
		notes:  notes,
		input:  ti,
		status: "ready",
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tick(), textinput.Blink)
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(20, msg.Width-8)
		return m, nil

	case tickMsg:
		m.snaps = m.eng.Snapshots()
		if m.cursor >= len(m.snaps) && len(m.snaps) > 0 {
			m.cursor = len(m.snaps) - 1
		}
		if len(m.snaps) == 0 {
			m.cursor = 0
		}
		if note := m.notes.Take(); note != "" {
			m.errFlash = ""
			m.status = note
		}
		return m, tick()

	case tea.KeyMsg:
		if m.adding {
			return m.updateAdding(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m Model) updateAdding(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.adding = false
		m.input.Blur()
		m.input.SetValue("")
		m.status = "cancelled"
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.input.Value())
		m.adding = false
		m.input.Blur()
		m.input.SetValue("")
		if val == "" {
			m.status = "nothing to add"
			return m, nil
		}
		id, err := m.eng.Add(val)
		if err != nil {
			m.errFlash = err.Error()
			m.status = "add failed"
			return m, nil
		}
		m.errFlash = ""
		m.status = "added " + short(id)
		m.snaps = m.eng.Snapshots()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quit = true
		return m, tea.Quit
	case "a":
		m.adding = true
		m.input.SetValue("")
		m.input.Focus()
		m.status = "paste magnet / path / hash, enter to add, esc cancel"
		m.errFlash = ""
		return m, textinput.Blink
	case "down":
		if m.cursor < len(m.snaps)-1 {
			m.cursor++
		}
	case "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g":
		m.cursor = 0
	case "G":
		if len(m.snaps) > 0 {
			m.cursor = len(m.snaps) - 1
		}
	case "p", " ":
		if id, ok := m.selectedID(); ok {
			paused, err := m.eng.TogglePause(id)
			if err != nil {
				m.errFlash = err.Error()
			} else {
				m.errFlash = ""
				if paused {
					m.status = "paused"
				} else {
					m.status = "playing"
				}
				m.snaps = m.eng.Snapshots()
			}
		}
	case "r", "R":
		if id, ok := m.selectedID(); ok {
			if err := m.eng.Delete(id, false); err != nil {
				m.errFlash = err.Error()
			} else {
				m.errFlash = ""
				m.status = "removed (files kept)"
				m.snaps = m.eng.Snapshots()
				if m.cursor >= len(m.snaps) && m.cursor > 0 {
					m.cursor--
				}
			}
		}
	case "w", "W":
		if id, ok := m.selectedID(); ok {
			if err := m.eng.Delete(id, true); err != nil {
				m.errFlash = err.Error()
			} else {
				m.errFlash = ""
				m.status = "wiped (files deleted)"
				m.snaps = m.eng.Snapshots()
				if m.cursor >= len(m.snaps) && m.cursor > 0 {
					m.cursor--
				}
			}
		}
	case "?":
		m.status = "a add · p/space pause · r remove · w wipe · ↑/↓ move · q quit"
	}
	return m, nil
}

func (m Model) selectedID() (string, bool) {
	if len(m.snaps) == 0 || m.cursor < 0 || m.cursor >= len(m.snaps) {
		return "", false
	}
	return m.snaps[m.cursor].ID, true
}

var (
	// SweetPotato palette — title orange kept as-is.
	colTitle  = lipgloss.Color("#f79b29") // potato orange
	colText   = lipgloss.Color("#f5e6e8") // soft cream
	colMuted  = lipgloss.Color("#8a5a62") // muted rose
	colHeader = lipgloss.Color("#c47a3a") // warm brown
	colOk     = lipgloss.Color("#c47a3a")
	colWarn   = lipgloss.Color("#f79b29")
	colErr    = lipgloss.Color("#a73b50") // potato red
	colSelFg  = lipgloss.Color("#1d1f21") // charcoal
	colSelBg  = lipgloss.Color("#f79b29")

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colTitle)
	dimStyle    = lipgloss.NewStyle().Foreground(colMuted)
	selStyle    = lipgloss.NewStyle().Foreground(colSelFg).Background(colSelBg).Bold(true)
	okStyle     = lipgloss.NewStyle().Foreground(colOk)
	warnStyle   = lipgloss.NewStyle().Foreground(colWarn)
	errStyle    = lipgloss.NewStyle().Foreground(colErr)
	headerStyle = lipgloss.NewStyle().Foreground(colHeader).Bold(true)
	textStyle   = lipgloss.NewStyle().Foreground(colText)
)

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("tuber"))
	b.WriteString(dimStyle.Render("  ·  light torrent tui  ·  "))
	b.WriteString(dimStyle.Render(m.eng.DataDir()))
	b.WriteString("\n\n")

	if m.adding {
		b.WriteString(headerStyle.Render("add torrent"))
		b.WriteString("\n")
		b.WriteString(textStyle.Render(m.input.View()))
		b.WriteString("\n\n")
		b.WriteString(dimStyle.Render("enter confirm · esc cancel"))
		b.WriteString("\n")
		return b.String()
	}

	if len(m.snaps) == 0 {
		b.WriteString(dimStyle.Render("no torrents yet — press a to add a magnet or .torrent"))
		b.WriteString("\n\n")
	} else {
		b.WriteString(headerStyle.Render(fmt.Sprintf("  %-6s  %-8s  %7s  %8s  %8s  %6s  %s",
			"done", "status", "size", "down", "up", "peers", "name")))
		b.WriteString("\n")
		for i, s := range m.snaps {
			line := formatRow(s)
			if i == m.cursor {
				b.WriteString(selStyle.Render("▸ " + stripANSI(line)))
			} else {
				b.WriteString("  " + line)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.errFlash != "" {
		b.WriteString(errStyle.Render(m.errFlash))
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render(m.status))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("a add  p pause/play  r remove  w wipe  ↑/↓  ? help  q quit"))
	return b.String()
}

func formatRow(s engine.Snapshot) string {
	pct := fmt.Sprintf("%5.1f%%", s.Progress*100)
	size := humanBytes(s.BytesTotal)
	if !s.InfoReady {
		size = "—"
		pct = "  —  "
	}
	name := s.Name
	if len(name) > 48 {
		name = name[:45] + "…"
	}
	return fmt.Sprintf("%-6s  %-14s  %7s  %8s  %8s  %3d/%-3d  %s",
		pct,
		padStatus(s.Status),
		size,
		humanRate(s.DownRate),
		humanRate(s.UpRate),
		s.Peers,
		s.TotalPeers,
		name,
	)
}

func padStatus(st engine.Status) string {
	switch st {
	case engine.StatusDone:
		return okStyle.Render(fmt.Sprintf("%-8s", "done"))
	case engine.StatusPaused:
		return warnStyle.Render(fmt.Sprintf("%-8s", "paused"))
	case engine.StatusVerifying:
		return warnStyle.Render(fmt.Sprintf("%-8s", "verify"))
	case engine.StatusFetching:
		return dimStyle.Render(fmt.Sprintf("%-8s", "meta"))
	default:
		return okStyle.Render(fmt.Sprintf("%-8s", "active"))
	}
}

func humanBytes(n int64) string {
	if n <= 0 {
		return "0B"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(n)/float64(div), "KMGTPE"[exp])
}

func humanRate(n int64) string {
	if n <= 0 {
		return "0B/s"
	}
	return humanBytes(n) + "/s"
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// stripANSI removes SGR sequences so selection highlight isn't muddied.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
