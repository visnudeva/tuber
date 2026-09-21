package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
					m.status = "downloading"
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
		b.WriteString(headerStyle.Render("  " + formatHeader()))
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

// Column widths must match between header and rows. Status is padded to
// colStatus *before* ANSI styling so escape codes don't shift later columns.
const (
	colDone   = 6
	colStatus = 11
	colSize   = 7
	colRate   = 8
	colETA    = 8
	colPeers  = 7
)

func formatHeader() string {
	return joinCols(
		padRight("done", colDone),
		padRight("status", colStatus),
		padLeft("size", colSize),
		padLeft("down", colRate),
		padLeft("up", colRate),
		padLeft("eta", colETA),
		padRight("peers", colPeers),
		"name",
	)
}

func formatRow(s engine.Snapshot) string {
	pct := fmt.Sprintf("%5.1f%%", s.Progress*100)
	size := humanBytes(s.BytesTotal)
	if !s.InfoReady {
		size = "—"
		pct = "—"
	}
	name := s.Name
	if len(name) > 48 {
		name = name[:45] + "…"
	}
	peers := fmt.Sprintf("%d/%d", s.Peers, s.TotalPeers)
	return joinCols(
		padRight(pct, colDone),
		padStatus(s.Status),
		padLeft(size, colSize),
		padLeft(humanRate(s.DownRate), colRate),
		padLeft(humanRate(s.UpRate), colRate),
		padLeft(formatETA(s), colETA),
		padRight(peers, colPeers),
		name,
	)
}

func joinCols(cols ...string) string {
	return strings.Join(cols, "  ")
}

func padLeft(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return strings.Repeat(" ", width-n) + s
}

func padRight(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// formatETA estimates remaining download time from leftover bytes / down rate.
func formatETA(s engine.Snapshot) string {
	switch s.Status {
	case engine.StatusDone, engine.StatusPaused, engine.StatusFetching, engine.StatusVerifying:
		return "—"
	}
	if !s.InfoReady || s.BytesTotal <= 0 {
		return "—"
	}
	remaining := s.BytesTotal - s.BytesDone
	if remaining <= 0 {
		return "—"
	}
	if s.DownRate <= 0 {
		return "∞"
	}
	return humanDuration(remaining / s.DownRate)
}

func humanDuration(secs int64) string {
	if secs < 0 {
		return "—"
	}
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	if secs < 3600 {
		m, s := secs/60, secs%60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm %ds", m, s)
	}
	if secs < 86400 {
		h, m := secs/3600, (secs%3600)/60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	}
	d, h := secs/86400, (secs%86400)/3600
	if h == 0 {
		return fmt.Sprintf("%dd", d)
	}
	return fmt.Sprintf("%dd %dh", d, h)
}

func padStatus(st engine.Status) string {
	label := "downloading"
	style := okStyle
	switch st {
	case engine.StatusDone:
		label, style = "done", okStyle
	case engine.StatusPaused:
		label, style = "paused", warnStyle
	case engine.StatusVerifying:
		label, style = "verifying", warnStyle
	case engine.StatusFetching:
		label, style = "meta", dimStyle
	}
	return style.Render(padRight(label, colStatus))
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
