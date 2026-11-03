//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"tailmulti.local/engine/internal/daemon"
	"tailmulti.local/engine/internal/host"
	"tailmulti.local/engine/internal/names"
	"tailmulti.local/engine/internal/service"
	"tailmulti.local/engine/internal/store"
	"tailmulti.local/engine/tunmux"
)

func newSession(parent context.Context, disk *store.Store, dir string, active bool) (result service.Session, err error) {
	ctx, cancel := context.WithCancel(parent)
	var cleanups []func()
	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			cancel()
			for i := len(cleanups) - 1; i >= 0; i-- {
				cleanups[i]()
			}
		})
	}
	defer func() {
		if err != nil {
			closeAll()
		}
	}()
	resolver := new(names.Resolver)
	var network daemon.Network
	var mux *tunmux.Mux
	var factory daemon.Factory
	var done <-chan error
	var activeHost *host.Host
	if active {
		dns, e := names.ListenSystem(resolver)
		if e != nil {
			return result, errors.New("cannot bind Tailmulti DNS port")
		}
		cleanups = append(cleanups, dns.Close)
		_, port, e := net.SplitHostPort(dns.Addr())
		if e != nil {
			return result, errors.New("cannot determine Tailmulti DNS port")
		}
		h, e := host.Open(port)
		if e != nil {
			return result, e
		}
		cleanups = append(cleanups, func() {
			if h.Close() != nil {
				fmt.Fprintln(os.Stderr, "Tailmulti: host cleanup failed; reject guards remain.")
			}
		})
		network, mux, factory = h, tunmux.NewMux(h.Device(), nil, nil, nil), daemon.RealFactory(ctx, dir)
		activeHost = h
		ch := make(chan error, 1)
		done = ch
		go func() { ch <- mux.Run(ctx) }()
	}
	m, err := daemon.New(ctx, disk, factory, network, mux, resolver)
	if err != nil {
		return result, err
	}
	cleanups = append(cleanups, m.Close)
	if activeHost != nil {
		resolver.EnableForwarding(ctx, host.NewDNSForwarder().Exchange, m.ClaimNames)
		if err = activeHost.EnableAutomaticDNS(); err != nil {
			return result, err
		}
	}
	go m.Run()
	return service.Session{Backend: m, Close: closeAll, Done: done}, nil
}
