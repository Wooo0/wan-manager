//go:build linux

package isp

import (
	"net"
	"net/http"
	"syscall"
	"time"
)

// bindTransportToInterface 将 HTTP Transport 绑定到指定网卡（Linux SO_BINDTODEVICE）
func bindTransportToInterface(transport *http.Transport, iface string) error {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			var err error
			c.Control(func(fd uintptr) {
				err = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, iface)
			})
			return err
		},
	}
	transport.DialContext = dialer.DialContext
	return nil
}
