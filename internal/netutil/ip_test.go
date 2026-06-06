package netutil

import (
	"net"
	"testing"
)

func TestIPv4Conversions(t *testing.T) {
	ip := net.IPv4(10, 66, 7, 128)
	v := IPToUint32(ip)
	if got := Uint32ToIP(v).String(); got != "10.66.7.128" {
		t.Fatalf("Uint32ToIP(IPToUint32(%s)) = %s", ip, got)
	}
}

func TestPrefixToMask(t *testing.T) {
	if got := PrefixToMask(24); got != "255.255.255.0" {
		t.Fatalf("PrefixToMask(24) = %s", got)
	}
}

func TestSubnetBroadcast(t *testing.T) {
	got := SubnetBroadcast(net.ParseIP("10.77.3.12"), net.ParseIP("255.255.255.0"))
	if got == nil || got.String() != "10.77.3.255" {
		t.Fatalf("SubnetBroadcast = %v", got)
	}
}
