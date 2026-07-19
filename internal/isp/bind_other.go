//go:build !linux

package isp

import (
	"fmt"
	"net/http"
)

// bindTransportToInterface 非 Linux 平台不支持绑定网卡，直接返回错误。
func bindTransportToInterface(transport *http.Transport, iface string) error {
	return fmt.Errorf("SO_BINDTODEVICE 仅 Linux 支持（当前平台不可用）")
}
