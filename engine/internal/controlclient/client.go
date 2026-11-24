// Package controlclient is a client for the local control protocol.
// It does not import the engine, so command-line tools stay small.
package controlclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"syscall"
	"time"

	"github.com/tailscale/peercred"
)

const ServicePath = "/Library/Application Support/Tailmulti/run/control.sock"

// ErrUnavailable means nothing is listening on the control socket.
var ErrUnavailable = errors.New("Tailmulti's background service isn't running. Open the Tailmulti app to set it up.")

type Account struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	State       string `json:"state"`
	PeerCount   int    `json:"peerCount"`
	HasIdentity bool   `json:"hasIdentity"`
	LoginReady  bool   `json:"loginReady"`
}

type Device struct {
	ProfileID          string   `json:"profileId"`
	Name               string   `json:"name"`
	Addresses          []string `json:"addresses"`
	TailscaleAddresses []string `json:"tailscaleAddresses,omitempty"`
	Online             bool     `json:"online"`
}

type Status struct {
	Version   int       `json:"version"`
	Mode      string    `json:"mode"`
	Accounts  []Account `json:"accounts"`
	Devices   []Device  `json:"devices"`
	Conflicts []string  `json:"conflicts"`
	Health    string    `json:"health"`
}

type Check struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type Setup struct {
	Version int     `json:"version"`
	Ready   bool    `json:"ready"`
	Checks  []Check `json:"checks"`
}

// Client verifies the server's kernel peer UID on every connection.
type Client struct {
	Path string
	// ServerUID is 0 for the installed service, or the current user for a preview socket.
	ServerUID int
}

func (c Client) Status(ctx context.Context) (Status, error) {
	var s Status
	if err := c.call(ctx, "GET", "/v1/status", nil, &s); err != nil {
		return s, err
	}
	if s.Version != 1 {
		return s, errors.New("this command and the running service use different protocol versions")
	}
	return s, nil
}

// Raw returns a GET endpoint's JSON without dropping fields this client does not know.
func (c Client) Raw(ctx context.Context, endpoint string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.call(ctx, "GET", endpoint, nil, &raw)
	return raw, err
}

func (c Client) Setup(ctx context.Context) (Setup, error) {
	var s Setup
	return s, c.call(ctx, "GET", "/v1/setup", nil, &s)
}

func (c Client) StartNetwork(ctx context.Context) error {
	return c.call(ctx, "POST", "/v1/network/start", struct{}{}, nil)
}

func (c Client) StopNetwork(ctx context.Context, restoreDNS bool) error {
	return c.call(ctx, "POST", "/v1/network/stop", map[string]bool{"restoreDNS": restoreDNS}, nil)
}

func (c Client) Add(ctx context.Context, name string) (Account, error) {
	var a Account
	return a, c.call(ctx, "POST", "/v1/accounts", map[string]string{"name": name}, &a)
}

// Action is connect, disconnect or remove.
func (c Client) Action(ctx context.Context, action, id string) error {
	if (action != "connect" && action != "disconnect" && action != "remove") || !ValidID(id) {
		return errors.New("invalid account action")
	}
	return c.call(ctx, "POST", "/v1/accounts/"+id+"/"+action, struct{}{}, nil)
}

// Login returns a sensitive browser sign-in URL. Never log it.
func (c Client) Login(ctx context.Context, id string) (string, error) {
	if !ValidID(id) {
		return "", errors.New("invalid account")
	}
	var reply struct {
		URL string `json:"url"`
	}
	if err := c.call(ctx, "POST", "/v1/accounts/"+id+"/login", struct{}{}, &reply); err != nil {
		return "", err
	}
	u, err := url.Parse(reply.URL)
	if err != nil || u.Scheme != "https" || u.Host != "login.tailscale.com" || u.User != nil {
		return "", errors.New("the service returned an unsupported sign-in address")
	}
	return reply.URL, nil
}

func (c Client) ServiceBuild(ctx context.Context) (string, error) {
	var reply struct {
		Build string `json:"build"`
	}
	return reply.Build, c.call(ctx, "GET", "/v1/service", nil, &reply)
}

func ValidID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func (c Client) call(ctx context.Context, method, endpoint string, body, out any) error {
	timeout := 15 * time.Second
	if endpoint == "/v1/network/start" || endpoint == "/v1/network/stop" {
		timeout = 110 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.Path)
		if err != nil {
			if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
				return nil, ErrUnavailable
			}
			return nil, errors.New("cannot reach the Tailmulti service")
		}
		creds, err := peercred.Get(conn)
		uid, ok := "", false
		if err == nil {
			uid, ok = creds.UserID()
		}
		if !ok || uid != strconv.Itoa(c.ServerUID) {
			conn.Close()
			return nil, errors.New("control server identity does not match")
		}
		return conn, nil
	}}
	defer transport.CloseIdleConnections()
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://tailmulti"+endpoint, payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		// Unwrap the dial error so callers see its message, not transport framing.
		var opErr *net.OpError
		if errors.Is(err, ErrUnavailable) {
			return ErrUnavailable
		}
		if errors.As(err, &opErr) && opErr.Err != nil {
			return opErr.Err
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return errors.New("the Tailmulti service did not answer in time")
		}
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
	if err != nil || len(data) > 4<<20 {
		return errors.New("the Tailmulti service sent an unreadable response")
	}
	if resp.StatusCode != 200 {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &failure) != nil || failure.Error == "" {
			failure.Error = "request failed"
		}
		return errors.New(failure.Error)
	}
	if out == nil {
		return nil
	}
	if json.Unmarshal(data, out) != nil {
		return errors.New("the Tailmulti service sent an unreadable response")
	}
	return nil
}
