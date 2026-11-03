package names

import (
	"errors"
	"net"
	"strconv"
	"syscall"
)

// Privileged ports prevent an unprivileged process taking over retained DNS entries after shutdown.
func SystemDNSPorts() []uint16 {
	return []uint16{53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63}
}

func ListenSystem(r *Resolver) (*Server, error) {
	return listenPorts(r, SystemDNSPorts(), Listen)
}

func listenPorts(r *Resolver, ports []uint16, listen func(string, *Resolver) (*Server, error)) (*Server, error) {
	for _, port := range ports {
		s, err := listen(net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), r)
		if err == nil {
			return s, nil
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, err
		}
	}
	return nil, errors.New("no protected local DNS port is available")
}
