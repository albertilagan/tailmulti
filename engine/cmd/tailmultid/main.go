//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"tailmulti.local/engine/internal/control"
	"tailmulti.local/engine/internal/host"
	"tailmulti.local/engine/internal/service"
	"tailmulti.local/engine/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Tailmulti:", err)
		os.Exit(1)
	}
}
func run() error {
	activate := flag.Bool("activate", false, "explicitly activate privileged host routing and DNS")
	managed := flag.Bool("service", false, "run the installed control service; networking starts inactive")
	dir := flag.String("state-dir", "", "private absolute state directory; required in preview mode")
	socket := flag.String("socket", "", "absolute control socket path; required in preview mode")
	owner := flag.Int("owner-uid", -1, "sole authorized desktop user; required for foreground activation")
	recovery := flag.Bool("recovery-report", false, "show read-only cleanup guidance")
	checkHost := flag.Bool("check-host", false, "print read-only host setup checks as JSON")
	buildInfo := flag.Bool("build-info", false, "print build information without starting a service")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if *buildInfo {
		if flag.NFlag() != 1 {
			return errors.New("--build-info cannot be combined with other options")
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			Version      int    `json:"version"`
			OwnerBinding string `json:"ownerBinding"`
		}{Version: 2, OwnerBinding: "first-console-user"})
	}
	if *activate && *managed {
		return errors.New("choose service mode or foreground activation")
	}
	if *checkHost {
		if *activate || *managed || *recovery || *dir != "" || *socket != "" || *owner != -1 {
			return errors.New("--check-host cannot be combined with service options")
		}
		return json.NewEncoder(os.Stdout).Encode(host.Preflight(context.Background()))
	}
	if *recovery {
		if *activate || *managed {
			return errors.New("recovery report cannot be combined with activation")
		}
		fmt.Println(host.RecoveryReport())
		return nil
	}
	privileged := *activate || *managed
	if privileged {
		if os.Geteuid() != 0 {
			return errors.New("privileged modes require root")
		}
		if *managed == (*owner != -1) || (*activate && *owner < 501) {
			return errors.New("foreground activation requires --owner-uid; the service determines its owner")
		}
		if *dir != "" || *socket != "" {
			return errors.New("activated mode uses fixed system paths")
		}
		if *managed {
			bound, err := host.ProvisionInstallation()
			if err != nil {
				return err
			}
			*owner = bound
		}
		if *activate && !host.Preflight(context.Background()).Ready {
			return errors.New("host setup checks failed; run --check-host for details")
		}
		*dir = host.StateDir
		*socket = host.ControlSocket
		for _, p := range []string{filepath.Dir(*dir), filepath.Dir(*socket)} {
			if err := os.MkdirAll(p, 0755); err != nil {
				return err
			}
			if err := host.SecureParents(p, 0); err != nil {
				return err
			}
			// launchd's private umask must not prevent the desktop owner reaching the socket.
			if err := os.Chmod(p, 0755); err != nil {
				return err
			}
		}
	} else {
		if os.Geteuid() == 0 {
			return errors.New("preview must run as a regular user")
		}
		if *owner != -1 && *owner != os.Getuid() {
			return errors.New("preview owner must be current user")
		}
		*owner = os.Getuid()
		if !filepath.IsAbs(*dir) || !filepath.IsAbs(*socket) {
			return errors.New("preview requires absolute --state-dir and --socket paths")
		}
		// The socket parent must be private; do not create a world-readable credential directory.
		parent := filepath.Dir(*socket)
		if err := os.MkdirAll(parent, 0700); err != nil {
			return err
		}
		fi, err := os.Lstat(parent)
		if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0700 {
			return errors.New("preview socket parent must have mode 0700")
		}
	}
	disk, err := store.Open(*dir)
	if err != nil {
		return err
	}
	defer disk.Close()
	if privileged {
		if err = host.SecureParents(*dir, 0); err != nil {
			return err
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var backend control.Backend
	var muxErr <-chan error
	if *managed {
		factory := func(ctx context.Context, active bool) (service.Session, error) {
			return newSession(ctx, disk, *dir, active)
		}
		manager, err := service.New(ctx, factory, host.Preflight, host.RestoreResolvers)
		if err != nil {
			return err
		}
		defer manager.Close()
		backend = manager
	} else {
		session, err := newSession(ctx, disk, *dir, *activate)
		if err != nil {
			return err
		}
		defer session.Close()
		backend, muxErr = session.Backend, session.Done
	}
	api, err := control.Listen(*socket, *owner, backend)
	if err != nil {
		return err
	}
	defer api.Close()
	fmt.Println("Tailmulti control ready; owner UID " + strconv.Itoa(*owner))
	select {
	case <-ctx.Done():
		return nil
	case <-api.Errors():
		return errors.New("control service stopped")
	case <-muxErr:
		return errors.New("host packet service stopped")
	}
}
