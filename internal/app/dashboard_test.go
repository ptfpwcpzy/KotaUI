package app

import (
	"reflect"
	"testing"
)

func TestSplitHostAddresses(t *testing.T) {
	ipv4, ipv6 := splitHostAddresses([]string{
		"10.0.0.2/24",
		"192.168.1.10/24",
		"10.0.0.2/24",
		"127.0.0.1/8",
		"169.254.1.2/16",
		"2001:db8::2/64",
		"2001:db8::1%eth0/64",
		"::1/128",
		"fe80::1%eth0/64",
		"ff02::1/64",
		"not-an-ip",
		"",
	})
	want4 := []string{"10.0.0.2", "192.168.1.10"}
	want6 := []string{"2001:db8::1", "2001:db8::2"}
	if !reflect.DeepEqual(ipv4, want4) {
		t.Fatalf("ipv4 = %v, want %v", ipv4, want4)
	}
	if !reflect.DeepEqual(ipv6, want6) {
		t.Fatalf("ipv6 = %v, want %v", ipv6, want6)
	}
}
