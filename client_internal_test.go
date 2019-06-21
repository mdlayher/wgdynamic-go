package wgdynamic

import (
	"net"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_newClient(t *testing.T) {
	const iface = "eth0"

	tests := []struct {
		name  string
		addrs []net.Addr
		c     *Client
		ok    bool
	}{
		{
			name: "no addresses",
		},
		{
			name: "no suitable addresses",
			addrs: []net.Addr{
				// This is nonsensical, but it verifies that a failed type
				// assertion won't crash the program.
				&net.TCPAddr{},
				// Link-local IPv4 address.
				mustIPNet("169.254.0.1/32"),
				// Globally routable IPv6 address.
				mustIPNet("2001:db8::1/128"),
			},
		},
		{
			name: "OK",
			addrs: []net.Addr{
				// Link-local IPv4 address.
				mustIPNet("169.254.0.1/32"),
				// Link-local IPv6 address.
				mustIPNet("fe80::1/128"),
			},
			c: &Client{
				laddr: &net.TCPAddr{
					IP:   net.ParseIP("fe80::1"),
					Port: port,
					Zone: iface,
				},
				raddr: &net.TCPAddr{
					IP:   serverIP.IP,
					Port: port,
					Zone: iface,
				},
			},
			ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := newClient(iface, tt.addrs)
			if err != nil {
				if tt.ok {
					t.Fatalf("failed to create client: %v", err)
				}

				t.Logf("OK error: %v", err)
				return
			}
			if !tt.ok {
				t.Fatal("expected an error, but none occurred")
			}

			if diff := cmp.Diff(tt.c, c, cmp.AllowUnexported(Client{})); diff != "" {
				t.Fatalf("unexpected Client (-want +got):\n%s", diff)
			}
		})
	}
}

func mustIPNet(s string) *net.IPNet {
	_, ipn, err := net.ParseCIDR(s)
	if err != nil {
		panicf("failed to parse CIDR: %v", err)
	}

	return ipn
}
