package names

import (
	"errors"
	"net"
	"strconv"
	"syscall"
	"testing"
)

func TestSystemDNSPortsStayPrivileged(t *testing.T) {
	seen := map[uint16]bool{}
	for _, port := range SystemDNSPorts() {
		if port == 0 || port >= 1024 || seen[port] {
			t.Fatal("unsafe or duplicate system DNS port")
		}
		seen[port] = true
	}
	if !seen[53] || len(seen) < 2 {
		t.Fatal("missing default or fallback ports")
	}
}

func TestDNSListenerFallsBackWithoutDisplacingExistingListener(t *testing.T) {
	for _, network := range []string{"udp4", "tcp4"} {
		t.Run(network, func(t *testing.T) {
			var address string
			if network == "udp4" {
				occupied, err := net.ListenPacket(network, "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer occupied.Close()
				address = occupied.LocalAddr().String()
			} else {
				occupied, err := net.Listen(network, "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer occupied.Close()
				address = occupied.Addr().String()
			}
			_, rawPort, _ := net.SplitHostPort(address)
			port, _ := strconv.Atoi(rawPort)
			server, err := listenPorts(new(Resolver), []uint16{uint16(port), 0}, Listen)
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			if server.Addr() == address {
				t.Fatal("listener reused an occupied port")
			}
			if network == "tcp4" {
				// A failed TCP bind must release the UDP socket acquired first.
				udp, err := net.ListenPacket("udp4", address)
				if err != nil {
					t.Fatal("failed TCP attempt leaked UDP listener", err)
				}
				udp.Close()
			}
		})
	}
}

func TestDNSListenerDoesNotHideOtherErrors(t *testing.T) {
	for _, failure := range []error{syscall.EADDRINUSE, syscall.EACCES} {
		calls := 0
		_, err := listenPorts(new(Resolver), []uint16{53, 54}, func(string, *Resolver) (*Server, error) { calls++; return nil, failure })
		if err == nil || failure == syscall.EACCES && (!errors.Is(err, failure) || calls != 1) || failure == syscall.EADDRINUSE && calls != 2 {
			t.Fatal("wrong listener failure policy", err, calls)
		}
	}
}
