package control

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"tailmulti.local/engine/internal/daemon"
	"tailmulti.local/engine/internal/names"
	"tailmulti.local/engine/internal/store"
	"testing"
	"time"
)

func backend(t *testing.T) *daemon.Manager {
	t.Helper()
	d, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	m, err := daemon.New(context.Background(), d, nil, nil, nil, new(names.Resolver))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}
func TestEveryEndpointRequiresOwnerIncludingStatusAndLogin(t *testing.T) {
	h := Handler(backend(t), "501")
	for _, path := range []string{"/v1/status", "/v1/diagnostics", "/v1/accounts", "/v1/setup", "/v1/network/start", "/v1/network/stop"} {
		for _, uid := range []string{"", "502", "501", "0"} {
			r := httptest.NewRequest("GET", "http://tailmulti"+path, nil)
			r = r.WithContext(context.WithValue(r.Context(), uidKey{}, uid))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			denied := uid == "" || uid == "502"
			if denied != (w.Code == 403) {
				t.Fatalf("uid %s path %s: %d", uid, path, w.Code)
			}
		}
	}
}
func TestStrictBodiesAndBrowserOrigin(t *testing.T) {
	h := Handler(backend(t), "501")
	for _, body := range []string{`{"name":"Home","stateDir":"/tmp/escape"}`, `{"name":"Home","authKey":"SECRET"}`, `{"name":"Home"} {}`, strings.Repeat("x", 5000)} {
		r := httptest.NewRequest("POST", "http://tailmulti/v1/accounts", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), uidKey{}, "501"))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("unsafe body accepted: %d", w.Code)
		}
		if strings.Contains(w.Body.String(), "SECRET") {
			t.Fatal("credential reflected")
		}
	}
	r := httptest.NewRequest("GET", "http://tailmulti/v1/status", nil)
	r = r.WithContext(context.WithValue(r.Context(), uidKey{}, "501"))
	r.Header.Set("Origin", "https://evil.test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("browser origin accepted")
	}
}
func TestUnixPeerCredentialsAndPreviewJourney(t *testing.T) {
	m := backend(t)
	shortDir, err := os.MkdirTemp("", "tm-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(shortDir)
	path := filepath.Join(shortDir, "s")
	s, err := Listen(path, os.Getuid(), m)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}}
	defer client.CloseIdleConnections()
	request := func(method, path, body string) (int, []byte) {
		t.Helper()
		r, _ := http.NewRequest(method, "http://tailmulti"+path, strings.NewReader(body))
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	code, b := request("POST", "/v1/accounts", `{"name":"Home"}`)
	if code != 200 {
		t.Fatalf("add %d %s", code, b)
	}
	var a daemon.AccountView
	json.Unmarshal(b, &a)
	code, b = request("POST", "/v1/accounts/"+a.ID+"/connect", `{}`)
	if code != 409 {
		t.Fatal("preview connected", code)
	}
	code, b = request("GET", "/v1/status", "")
	if code != 200 || !strings.Contains(string(b), "Home") || strings.Contains(string(b), "authUrl") {
		t.Fatal("bad status", code)
	}
	// Same socket kernel credentials, different configured owner: even GET is refused.
	s.Close()
	client.CloseIdleConnections()
	s, err = Listen(path, os.Getuid()+1, m)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	code, _ = request("GET", "/v1/status", "")
	if os.Getuid() != 0 && code != 403 {
		t.Fatal("nonowner status admitted")
	}
}
