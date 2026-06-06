//go:build windows

package tun

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"syscall"

	wgtun "golang.zx2c4.com/wireguard/tun"
)

const createNoWindow = 0x08000000

func hiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	return cmd
}

func createTunDevice(ifName string, mtu int) (wgtun.Device, error) {
	if err := EnsureWintunDLL(); err != nil {
		return nil, err
	}
	return wgtun.CreateTUN(ifName, mtu)
}

// ConfigureAddressAndRoute installs the room CIDR plus discovery routes on the
// Wintun adapter. LAN games commonly discover servers through limited
// broadcast, subnet broadcast, or IPv4 multicast; keeping those destinations on
// the room adapter lets that traffic enter the encrypted overlay without
// enabling a global VPN route.
func ConfigureAddressAndRoute(ifName, ip, mask, roomCIDR, gateway string) error {
	if err := hiddenCommand("netsh.exe", "interface", "ip", "set", "address", ifName, "static", ip, mask).Run(); err != nil {
		return fmt.Errorf("configure tun address: %w", err)
	}
	_, ipNet, err := net.ParseCIDR(roomCIDR)
	if err != nil {
		return err
	}
	prefix, _ := ipNet.Mask.Size()
	dst := fmt.Sprintf("%s/%d", ipNet.IP.String(), prefix)
	multicastDst := "224.0.0.0/4"
	limitedBroadcastDst := "255.255.255.255/32"

	ps := fmt.Sprintf(`
$alias = %q
$dst = %q
$multicastDst = %q
$limitedBroadcastDst = %q
$gw = %q
$firewallRule = "LwyLink Virtual Adapter Inbound"
$firewallOutRule = "LwyLink Virtual Adapter Outbound"
Start-Sleep -Milliseconds 500
$ifIndex = (Get-NetIPInterface -AddressFamily IPv4 -InterfaceAlias $alias -ErrorAction Stop | Select-Object -First 1).InterfaceIndex
Set-NetIPInterface -AddressFamily IPv4 -InterfaceIndex $ifIndex -AutomaticMetric Disabled -InterfaceMetric 5 -ErrorAction Stop
Set-NetIPInterface -AddressFamily IPv4 -InterfaceIndex $ifIndex -WeakHostSend Enabled -WeakHostReceive Enabled -ErrorAction SilentlyContinue
Disable-NetAdapterBinding -Name $alias -ComponentID ms_tcpip6 -ErrorAction SilentlyContinue | Out-Null

foreach ($store in @("ActiveStore", "PersistentStore")) {
  Get-NetRoute -AddressFamily IPv4 -PolicyStore $store -ErrorAction SilentlyContinue |
    Where-Object {
      $_.InterfaceIndex -eq $ifIndex -and (
        $_.DestinationPrefix -eq $dst -or
        $_.DestinationPrefix -eq $multicastDst -or
        $_.DestinationPrefix -eq $limitedBroadcastDst -or
        $_.DestinationPrefix -like "10.66.*" -or
        $_.DestinationPrefix -like "172.16.*"
      )
    } |
    Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue
}
try {
  New-NetRoute -AddressFamily IPv4 -DestinationPrefix $dst -InterfaceIndex $ifIndex -NextHop $gw -RouteMetric 5 -PolicyStore ActiveStore -ErrorAction Stop
} catch {
  New-NetRoute -AddressFamily IPv4 -DestinationPrefix $dst -InterfaceIndex $ifIndex -NextHop "0.0.0.0" -RouteMetric 5 -PolicyStore ActiveStore -ErrorAction Stop
}
New-NetRoute -AddressFamily IPv4 -DestinationPrefix $multicastDst -InterfaceIndex $ifIndex -NextHop "0.0.0.0" -RouteMetric 5 -PolicyStore ActiveStore -ErrorAction Stop
New-NetRoute -AddressFamily IPv4 -DestinationPrefix $limitedBroadcastDst -InterfaceIndex $ifIndex -NextHop "0.0.0.0" -RouteMetric 1 -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Out-Null

Get-NetFirewallRule -DisplayName $firewallRule -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue
Get-NetFirewallRule -DisplayName $firewallOutRule -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue
New-NetFirewallRule -DisplayName $firewallRule -Direction Inbound -Action Allow -InterfaceAlias $alias -Profile Any -Protocol Any -Enabled True -ErrorAction SilentlyContinue | Out-Null
New-NetFirewallRule -DisplayName $firewallOutRule -Direction Outbound -Action Allow -InterfaceAlias $alias -Profile Any -Protocol Any -Enabled True -ErrorAction SilentlyContinue | Out-Null
`, ifName, dst, multicastDst, limitedBroadcastDst, gateway)
	return runPowerShell(ps)
}

// CleanupRoute removes temporary discovery routes when the client disconnects.
func CleanupRoute(ifName, roomCIDR string) error {
	_, ipNet, err := net.ParseCIDR(roomCIDR)
	if err != nil {
		return err
	}
	prefix, _ := ipNet.Mask.Size()
	dst := fmt.Sprintf("%s/%d", ipNet.IP.String(), prefix)
	multicastDst := "224.0.0.0/4"
	limitedBroadcastDst := "255.255.255.255/32"
	ps := fmt.Sprintf(`
$alias = %q
$dst = %q
$multicastDst = %q
$limitedBroadcastDst = %q
$firewallRule = "LwyLink Virtual Adapter Inbound"
$firewallOutRule = "LwyLink Virtual Adapter Outbound"
$if = Get-NetIPInterface -AddressFamily IPv4 -InterfaceAlias $alias -ErrorAction SilentlyContinue | Select-Object -First 1
if ($if) {
  foreach ($store in @("ActiveStore", "PersistentStore")) {
    Get-NetRoute -AddressFamily IPv4 -PolicyStore $store -ErrorAction SilentlyContinue |
      Where-Object {
        $_.InterfaceIndex -eq $if.InterfaceIndex -and (
          $_.DestinationPrefix -eq $dst -or
          $_.DestinationPrefix -eq $multicastDst -or
          $_.DestinationPrefix -eq $limitedBroadcastDst -or
          $_.DestinationPrefix -like "10.66.*" -or
          $_.DestinationPrefix -like "172.16.*"
        )
      } |
      Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue
  }
  Set-NetIPInterface -AddressFamily IPv4 -InterfaceIndex $if.InterfaceIndex -WeakHostSend Disabled -WeakHostReceive Disabled -ErrorAction SilentlyContinue
  Enable-NetAdapterBinding -Name $alias -ComponentID ms_tcpip6 -ErrorAction SilentlyContinue | Out-Null
}
Get-NetFirewallRule -DisplayName $firewallRule -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue
Get-NetFirewallRule -DisplayName $firewallOutRule -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue
`, ifName, dst, multicastDst, limitedBroadcastDst)
	return runPowerShell(ps)
}

func runPowerShell(script string) error {
	cmd := hiddenCommand(
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden",
		"-Command", script,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
