//go:build darwin && !cgo

package host

import "errors"

type nativeDNS struct{}

func readDNSSnapshot() ([]byte, error) {
	return nil, errors.New("automatic DNS requires a native cgo build")
}
func installDefaultDNS(uint16) (*nativeDNS, error) {
	return nil, errors.New("automatic DNS requires a native cgo build")
}
func (*nativeDNS) present() bool { return false }
func (*nativeDNS) close()        {}
