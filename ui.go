package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

//go:embed ui/index.html
var uiFS embed.FS

// job is the single in-flight operation. One stack, one user, one thing at a
// time — a queue would be more machinery than this ever needs.
type job struct {
	mu      sync.Mutex
	running bool
	kind    string
	lines   []string
	err     string
}

func (j *job) log(format string, a ...any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lines = append(j.lines, fmt.Sprintf(format, a...))
}

func (j *job) start(kind string, fn func(func(string, ...any)) error) bool {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return false
	}
	j.running, j.kind, j.lines, j.err = true, kind, nil, ""
	j.mu.Unlock()

	go func() {
		err := fn(j.log)
		j.mu.Lock()
		defer j.mu.Unlock()
		j.running = false
		if err != nil {
			j.err = err.Error()
		}
	}()
	return true
}

func (j *job) snapshotState() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	return map[string]any{"running": j.running, "kind": j.kind, "lines": j.lines, "error": j.err}
}

func cmdUI(args []string) error {
	fs := flag.NewFlagSet("ui", flag.ExitOnError)
	dir := fs.String("C", ".", "stack directory")
	port := fs.Int("port", 7654, "port to listen on")
	s, err := stackFrom(fs, dir, args)
	if err != nil {
		return err
	}

	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	token := hex.EncodeToString(buf)
	j := &job{}
	addr := fmt.Sprintf("127.0.0.1:%d", *port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, err := uiFS.ReadFile("ui/index.html")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})

	// GET never mutates, so it needs no token.
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		snaps, err := List(s.Project)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if snaps == nil {
			snaps = []*Manifest{}
		}
		writeJSON(w, map[string]any{"project": s.Project, "file": s.File, "snapshots": snaps, "job": j.snapshotState()})
	})

	mux.Handle("/api/snapshot", guard(token, addr, func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Tag string }
		json.NewDecoder(r.Body).Decode(&body)
		if !j.start("snapshot", func(log func(string, ...any)) error {
			_, err := Snapshot(s, body.Tag, log)
			return err
		}) {
			http.Error(w, "another operation is already running", 409)
			return
		}
		w.WriteHeader(202)
	}))

	mux.Handle("/api/restore", guard(token, addr, func(w http.ResponseWriter, r *http.Request) {
		var body struct{ ID, Confirm string }
		json.NewDecoder(r.Body).Decode(&body)
		// Belt and braces: the browser asks too, but restore deletes volumes so
		// the server does not take the client's word for it.
		if body.Confirm != s.Project {
			http.Error(w, "confirmation did not match the project name", 400)
			return
		}
		snaps, err := List(s.Project)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		m, err := Resolve(snaps, body.ID)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if !j.start("restore", func(log func(string, ...any)) error { return Restore(s, m, log) }) {
			http.Error(w, "another operation is already running", 409)
			return
		}
		w.WriteHeader(202)
	}))

	url := fmt.Sprintf("http://%s/#%s", addr, token)
	fmt.Printf("rewind ui — project %s\n%s\n\nBound to 127.0.0.1 only. Ctrl-C to stop.\n", s.Project, url)
	openBrowser(url)
	return http.ListenAndServe(addr, mux)
}

// guard is the CSRF defence. Any page you visit can POST to localhost, and
// /api/restore deletes volumes — so mutating endpoints require the startup
// token (which a cross-origin script cannot read) and reject foreign Origins.
func guard(token, addr string, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !originOK(o, addr) {
			http.Error(w, "cross-origin request refused", 403)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Rewind-Token")), []byte(token)) != 1 {
			http.Error(w, "bad or missing token — open the URL printed by `rewind ui`", 403)
			return
		}
		h(w, r)
	})
}

func originOK(origin, addr string) bool {
	_, port, _ := strings.Cut(addr, ":")
	return origin == "http://127.0.0.1:"+port || origin == "http://localhost:"+port
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start() // best effort; the URL is printed either way
}
