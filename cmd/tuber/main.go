package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

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

	// Prefer an existing instance: magnets / .torrents go there, no second TUI.
	if err := ipc.Handoff(args); err == nil {
		os.Exit(0)
	}

	eng, err := engine.New(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tuber: %v\n", err)
		os.Exit(1)
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

	if sess, err := engine.LoadSession(); err == nil {
		if sess.DataDir != "" && *dataDir == defaultDir {
			// keep flag override; otherwise session dir is informational
		}
		eng.RestoreSession(sess)
	}
	eng.RestoreIncompleteFromDisk()

	for _, arg := range args {
		if _, err := eng.Add(arg); err != nil {
			fmt.Fprintf(os.Stderr, "tuber: add %q: %v\n", arg, err)
		}
	}
	_ = eng.Persist()

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
		// Rare race: another primary appeared — hand off and exit.
		if err := ipc.Handoff(args); err == nil {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "tuber: ipc: %v\n", err)
		os.Exit(1)
	}
	defer srv.Close()

	p := tea.NewProgram(ui.New(eng, notes), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tuber: %v\n", err)
		os.Exit(1)
	}
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
