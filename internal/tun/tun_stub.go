//go:build !windows

package tun

import (
	"fmt"

	wgtun "golang.zx2c4.com/wireguard/tun"
)

func createTunDevice(ifName string, mtu int) (wgtun.Device, error) {
	return nil, fmt.Errorf("wintun client is only implemented on Windows")
}

func EnsureWintunDLL() error {
	return nil
}

func ConfigureAddressAndRoute(ifName, ip, mask, roomCIDR, gateway string) error {
	return fmt.Errorf("wintun route configuration is only implemented on Windows")
}

func CleanupRoute(ifName, roomCIDR string) error {
	return nil
}

func EnableFirewallProfiles() error {
	return nil
}
