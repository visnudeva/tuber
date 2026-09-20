package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Request is sent by a secondary tuber process to the primary.
type Request struct {
	Args []string `json:"args"`
}

// Response is returned by the primary after handling a Request.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// SocketPath is ~/.config/tuber/tuber.sock
func SocketPath() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cfg, "tuber")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "tuber.sock"), nil
}

// Handoff sends args to a running primary instance. Succeeds even with no args
// (used to detect an already-running tuber without opening another window).
func Handoff(args []string) error {
	path, err := SocketPath()
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	if err := json.NewEncoder(conn).Encode(Request{Args: args}); err != nil {
		return err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}
	if !resp.OK {
		if resp.Error != "" {
			return fmt.Errorf("%s", resp.Error)
		}
		return fmt.Errorf("handoff rejected")
	}
	return nil
}

// Handler is called on the primary for each handoff request.
// It should add torrents and return a non-nil error only on hard failure.
type Handler func(args []string) error

// Server is the primary-instance listener.
type Server struct {
	ln   net.Listener
	path string
}

// Listen starts the primary IPC socket. Removes any stale socket file first.
func Listen(handle Handler) (*Server, error) {
	path, err := SocketPath()
	if err != nil {
		return nil, err
	}
	_ = os.Remove(path)

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// Restrict to the current user.
	_ = os.Chmod(path, 0o600)

	s := &Server{ln: ln, path: path}
	go s.serve(handle)
	return s, nil
}

func (s *Server) serve(handle Handler) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn, handle)
	}
}

func (s *Server) handleConn(conn net.Conn, handle Handler) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: err.Error()})
		return
	}
	resp := Response{OK: true}
	if handle != nil {
		if err := handle(req.Args); err != nil {
			resp = Response{OK: false, Error: err.Error()}
		}
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

// Close stops the listener and removes the socket file.
func (s *Server) Close() error {
	if s == nil || s.ln == nil {
		return nil
	}
	err := s.ln.Close()
	_ = os.Remove(s.path)
	return err
}
