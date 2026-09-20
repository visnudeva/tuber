package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/visnudeva/tuber/internal/engine"
	"github.com/visnudeva/tuber/internal/ipc"
	"github.com/visnudeva/tuber/internal/ui"
)

func main() {
	home, _ := os.UserHomeDir()
	defaultDir := filepath.Join(home, "Downloads", "tuber")

	dataDir := flag.String("dir", defaultDir, "download directory")
	handoffOnly := flag.Bool("handoff", false, "send args to a running tuber and exit (no TUI)")
	flag.Parse()
	args := flag.Args()

	if *handoffOnly {
		if err := ipc.Handoff(args); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Magnets / files: always prefer the open session.
	if len(args) > 0 {
		if err := ipc.Handoff(args); err == nil {
			_ = focusTuberWindow()
			os.Exit(0)
		}
	}
	// Empty launch always opens a TUI. Do not "focus and exit" — on Swirl/Sway,
	// swaymsg can report success even when no window matched, which made
	// `tuber` in a terminal appear to do nothing on SweetPotatOs.

	eng, err := engine.New(*dataDir)
	if err != nil {
		fatalWait("tuber: %v", err)
	}

	shutdown := func() {
		_ = eng.Persist()
		eng.Close()
	}

	// Window close / kill often sends SIGHUP/SIGTERM — defer alone is not enough.
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sigc
		shutdown()
		os.Exit(0)
	}()
	defer shutdown()

	notes := &ui.Notes{}
	srv, err := ipc.Listen(func(addArgs []string) error {
		var added int
		var last string
		for _, a := range addArgs {
			id, err := eng.Add(a)
			if err != nil {
				if strings.Contains(err.Error(), "already added") {
					notes.Set("already in list")
					continue
				}
				notes.Set(err.Error())
				continue
			}
			added++
			last = id
		}
		if added == 1 {
			notes.Set("added " + shortID(last))
		} else if added > 1 {
			notes.Set(fmt.Sprintf("added %d torrents", added))
		}
		return nil
	})
	if err != nil {
		if len(args) > 0 {
			if err := ipc.Handoff(args); err == nil {
				_ = focusTuberWindow()
				os.Exit(0)
			}
		}
		fatalWait("tuber: ipc: %v", err)
	}
	defer srv.Close()

	p := tea.NewProgram(ui.New(eng, notes), tea.WithAltScreen())

	// Restore after the UI is up so a slow verify/session never looks like a hang.
	go func() {
		time.Sleep(50 * time.Millisecond)
		if sess, err := engine.LoadSession(); err == nil {
			eng.RestoreSession(sess)
		}
		eng.RestoreIncompleteFromDisk()
		for _, arg := range args {
			if _, err := eng.Add(arg); err != nil {
				notes.Set(err.Error())
			}
		}
		_ = eng.Persist()
	}()

	if _, err := p.Run(); err != nil {
		fatalWait("tuber: %v", err)
	}
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func focusTuberWindow() bool {
	out, err := exec.Command("swaymsg", `[title="tuber"]`, "focus").CombinedOutput()
	if err == nil && !strings.Contains(strings.ToLower(string(out)), "no matching") {
		return true
	}
	out, err = exec.Command("swaymsg", `[app_id="tuber"]`, "focus").CombinedOutput()
	if err == nil && !strings.Contains(strings.ToLower(string(out)), "no matching") {
		return true
	}
	if err := exec.Command("hyprctl", "dispatch", "focuswindow", "title:tuber").Run(); err == nil {
		return true
	}
	if err := exec.Command("hyprctl", "dispatch", "focuswindow", "class:tuber").Run(); err == nil {
		return true
	}
	return false
}

func fatalWait(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	if fi, err := os.Stderr.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
		fmt.Fprintln(os.Stderr, "press enter to close")
		_, _ = fmt.Scanln()
	} else {
		time.Sleep(3 * time.Second)
	}
	os.Exit(1)
}
