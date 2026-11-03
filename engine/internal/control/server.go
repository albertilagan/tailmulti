// Package control exposes a small owner-authenticated Unix-socket protocol.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/tailscale/peercred"
	"tailmulti.local/engine/internal/daemon"
	"tailmulti.local/engine/internal/setup"
)

type Backend interface {
	Status() daemon.Status
	Add(string) (daemon.AccountView, error)
	Set(string, bool, bool) error
	Login(string) (string, error)
}
type uidKey struct{}

type NetworkBackend interface {
	Setup(context.Context) setup.Report
	StartNetwork(context.Context) error
	StopNetwork(bool) error
}

func authorized(next http.Handler, owner string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, _ := r.Context().Value(uidKey{}).(string)
		if uid == "" || (uid != "0" && uid != owner) {
			reply(w, 403, map[string]string{"error": "access denied"})
			return
		}
		// No browser-originated requests or reverse-proxy routing.
		if r.Header.Get("Origin") != "" || r.Host != "tailmulti" {
			reply(w, 403, map[string]string{"error": "invalid local request"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
func Handler(b Backend, owner string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, b.Status()) })
	mux.HandleFunc("GET /v1/setup", func(w http.ResponseWriter, r *http.Request) {
		if network, ok := b.(NetworkBackend); ok {
			reply(w, 200, network.Setup(r.Context()))
			return
		}
		reply(w, 409, map[string]string{"error": "network setup requires the installed service"})
	})
	mux.HandleFunc("POST /v1/network/start", func(w http.ResponseWriter, r *http.Request) {
		var body struct{}
		if !decode(w, r, &body) {
			return
		}
		if network, ok := b.(NetworkBackend); ok {
			// Closing multiple account engines can outlast the ordinary response deadline.
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
			err := network.StartNetwork(r.Context())
			result(w, map[string]bool{"ok": err == nil}, err)
			return
		}
		reply(w, 409, map[string]string{"error": "network setup requires the installed service"})
	})
	mux.HandleFunc("POST /v1/network/stop", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RestoreDNS bool `json:"restoreDNS"`
		}
		if !decode(w, r, &body) {
			return
		}
		if network, ok := b.(NetworkBackend); ok {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
			err := network.StopNetwork(body.RestoreDNS)
			result(w, map[string]bool{"ok": err == nil}, err)
			return
		}
		reply(w, 409, map[string]string{"error": "network setup requires the installed service"})
	})
	mux.HandleFunc("GET /v1/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		s := b.Status()
		reply(w, 200, map[string]any{"version": s.Version, "mode": s.Mode, "health": s.Health, "accountCount": len(s.Accounts), "deviceCount": len(s.Devices), "conflictCount": len(s.Conflicts)})
	})
	mux.HandleFunc("POST /v1/accounts", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &body) {
			return
		}
		a, err := b.Add(body.Name)
		result(w, a, err)
	})
	for _, action := range []string{"connect", "disconnect", "remove", "login"} {
		mux.HandleFunc("POST /v1/accounts/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			var empty struct{}
			if !decode(w, r, &empty) {
				return
			}
			id := r.PathValue("id")
			if action == "login" {
				u, err := b.Login(id)
				if err == nil {
					parsed, e := url.Parse(u)
					if e != nil || parsed.Scheme != "https" || parsed.Host != "login.tailscale.com" || parsed.User != nil {
						err = errors.New("engine returned an unsupported login URL")
					}
				}
				result(w, map[string]string{"url": u}, err)
				return
			}
			err := b.Set(id, action == "connect", action == "remove")
			result(w, map[string]bool{"ok": err == nil}, err)
		})
	}
	return authorized(mux, owner)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		reply(w, 400, map[string]string{"error": "invalid request"})
		return false
	}
	return true
}
func result(w http.ResponseWriter, v any, err error) {
	if err != nil {
		reply(w, 409, map[string]string{"error": err.Error()})
		return
	}
	reply(w, 200, v)
}
func reply(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		status = 500
		data = []byte(`{"error":"response failed"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

type Server struct {
	http     *http.Server
	listener net.Listener
	done     chan error
}

func Listen(path string, owner int, b Backend) (*Server, error) {
	// The caller holds the state lock. Refuse symlinks and non-socket collisions.
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("control path is not a socket")
		}
		c, e := net.DialTimeout("unix", path, 200*time.Millisecond)
		if e == nil {
			c.Close()
			return nil, errors.New("control socket is already in use")
		}
		if err = os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0666); err != nil {
		l.Close()
		return nil, err
	}
	srv := &http.Server{Handler: Handler(b, strconv.Itoa(owner)), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, ConnContext: func(ctx context.Context, c net.Conn) context.Context {
		creds, err := peercred.Get(c)
		if err != nil {
			return ctx
		}
		uid, ok := creds.UserID()
		if !ok {
			return ctx
		}
		return context.WithValue(ctx, uidKey{}, uid)
	}}
	s := &Server{http: srv, listener: l, done: make(chan error, 1)}
	go func() {
		err := srv.Serve(l)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		s.done <- err
	}()
	return s, nil
}
func (s *Server) Errors() <-chan error { return s.done }
func (s *Server) Close() error         { return s.http.Close() }
