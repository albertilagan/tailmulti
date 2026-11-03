package control

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tailmulti.local/engine/internal/daemon"
	"tailmulti.local/engine/internal/setup"
)

type networkBackend struct {
	*daemon.Manager
	started, stopped, inspected int
	restored                    bool
}

func (b *networkBackend) Setup(context.Context) setup.Report {
	b.inspected++
	return setup.Report{Version: 1, Ready: true, Checks: []setup.Check{}}
}
func (b *networkBackend) StartNetwork(context.Context) error { b.started++; return nil }
func (b *networkBackend) StopNetwork(restore bool) error {
	b.stopped++
	b.restored = restore
	return nil
}

func TestNetworkOperationsAreAuthenticatedAndStrict(t *testing.T) {
	b := &networkBackend{Manager: backend(t)}
	h := Handler(b, "501")
	request := func(method, path, body, uid, origin string) int {
		r := httptest.NewRequest(method, "http://tailmulti"+path, strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), uidKey{}, uid))
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	for _, path := range []string{"/v1/network/start", "/v1/network/stop"} {
		if request("POST", path, `{}`, "502", "") != 403 {
			t.Fatal("foreign owner admitted")
		}
		if request("POST", path, `{}`, "501", "https://example.test") != 403 {
			t.Fatal("browser request admitted")
		}
		if request("POST", path, `{"command":"arbitrary"}`, "501", "") != 400 {
			t.Fatal("unexpected privileged argument admitted")
		}
	}
	if request("GET", "/v1/setup", "", "502", "") != 403 || b.inspected != 0 || b.started != 0 || b.stopped != 0 {
		t.Fatal("unauthorized request reached network backend")
	}
	if request("GET", "/v1/setup", "", "501", "") != 200 || b.inspected != 1 {
		t.Fatal("owner setup unavailable")
	}
	if request("POST", "/v1/network/start", `{}`, "501", "") != 200 || b.started != 1 {
		t.Fatal("owner start unavailable")
	}
	if request("POST", "/v1/network/stop", `{}`, "501", "") != 200 || b.restored {
		t.Fatal("DNS restored without request")
	}
	if request("POST", "/v1/network/stop", `{"restoreDNS":true}`, "501", "") != 200 || !b.restored {
		t.Fatal("explicit restoration ignored")
	}
}

func TestPreviewCannotActivateThroughNetworkAPI(t *testing.T) {
	h := Handler(backend(t), "501")
	r := httptest.NewRequest("POST", "http://tailmulti/v1/network/start", strings.NewReader(`{}`))
	r = r.WithContext(context.WithValue(r.Context(), uidKey{}, "501"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 409 {
		t.Fatal("preview exposed network activation")
	}
}

type slowNetworkBackend struct{ *networkBackend }

func (b slowNetworkBackend) StartNetwork(ctx context.Context) error {
	time.Sleep(30 * time.Millisecond)
	return b.networkBackend.StartNetwork(ctx)
}
func (b slowNetworkBackend) StopNetwork(restore bool) error {
	time.Sleep(30 * time.Millisecond)
	return b.networkBackend.StopNetwork(restore)
}

func TestNetworkChangesCanFinishBeyondOrdinaryResponseDeadline(t *testing.T) {
	b := slowNetworkBackend{&networkBackend{Manager: backend(t)}}
	h := Handler(b, "501")
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), uidKey{}, "501"))
		h.ServeHTTP(w, r)
	}))
	s.Config.WriteTimeout = 5 * time.Millisecond
	s.Start()
	defer s.Close()
	client := &http.Client{Timeout: time.Second}
	for _, action := range []string{"start", "stop"} {
		request, err := http.NewRequest("POST", s.URL+"/v1/network/"+action, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Host = "tailmulti"
		response, err := client.Do(request)
		if err != nil {
			t.Fatal("network change lost its response", err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ok":true`) {
			t.Fatal("network change was not confirmed", err, string(body))
		}
	}
}
