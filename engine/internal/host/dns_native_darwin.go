//go:build darwin && cgo

package host

// #cgo LDFLAGS: -framework Foundation -framework CoreFoundation -framework SystemConfiguration
// #include "dns_native.h"
import "C"

import (
	"errors"
	"sync"
	"unsafe"
)

func readDNSSnapshot() ([]byte, error) {
	b := C.tm_dns_snapshot()
	if b == nil {
		return nil, errors.New("cannot read ordinary network DNS")
	}
	defer C.free(unsafe.Pointer(b))
	return []byte(C.GoString(b)), nil
}

type nativeDNS struct {
	mu     sync.Mutex
	handle *C.tm_dns_overlay
}

func installDefaultDNS(port uint16) (*nativeDNS, error) {
	h := C.tm_dns_install(C.int(port))
	if h == nil {
		return nil, errors.New("cannot install temporary Tailmulti DNS service")
	}
	return &nativeDNS{handle: h}, nil
}
func (n *nativeDNS) present() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.handle != nil && C.tm_dns_present(n.handle) != 0
}
func (n *nativeDNS) close() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.handle != nil {
		C.tm_dns_close(n.handle)
		n.handle = nil
	}
}
