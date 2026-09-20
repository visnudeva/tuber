package ipc_test

import (
	"os"
	"sync"
	"testing"

	"github.com/visnudeva/tuber/internal/ipc"
)

func TestHandoffRoundTrip(t *testing.T) {
	var (
		mu   sync.Mutex
		got  []string
		done = make(chan struct{}, 1)
	)
	srv, err := ipc.Listen(func(args []string) error {
		mu.Lock()
		got = append([]string{}, args...)
		mu.Unlock()
		done <- struct{}{}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	args := []string{"magnet:?xt=urn:btih:abc", "/tmp/x.torrent"}
	if err := ipc.Handoff(args); err != nil {
		t.Fatalf("handoff: %v", err)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != args[0] || got[1] != args[1] {
		t.Fatalf("got %#v", got)
	}
}

func TestHandoffEmptyConfirmsAlive(t *testing.T) {
	srv, err := ipc.Listen(func(args []string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	if err := ipc.Handoff(nil); err != nil {
		t.Fatal(err)
	}
}

func TestHandoffNoServer(t *testing.T) {
	path, err := ipc.SocketPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)

	if err := ipc.Handoff([]string{"x"}); err == nil {
		t.Fatal("expected error when no server")
	}
}
