package wgdynamic_test

import (
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/mdlayher/wgdynamic-go"
)

func TestClientRequestIP(t *testing.T) {
	var (
		ipv4 = mustIPNet("192.0.2.1/32")
		ipv6 = mustIPNet("2001:db8::1/128")
	)

	tests := []struct {
		name, res, req string
		ipv4, ipv6     *net.IPNet
		rip            *wgdynamic.RequestIP
		ok             bool
		err            *wgdynamic.Error
	}{
		{
			name: "bad response command",
			req:  "request_ip=1\n\n",
			res:  "foo=1\n\n",
		},
		{
			name: "protocol error",
			req:  "request_ip=1\n\n",
			res: `request_ip=1
errno=1
errmsg=Out of IPs

`,
			err: &wgdynamic.Error{
				Number:  1,
				Message: "Out of IPs",
			},
		},
		{
			name: "OK nil ipv4/6",
			req:  "request_ip=1\n\n",
			res: `request_ip=1
ipv4=192.0.2.1/32
ipv6=2001:db8::1/128
leasestart=1
leasetime=10
errno=0

`,
			rip: &wgdynamic.RequestIP{
				IPv4: ipv4,
				IPv6: ipv6,
			},
			ok: true,
		},
		{
			name: "OK ipv4",
			req:  "request_ip=1\nipv4=192.0.2.1/32\n\n",
			res: `request_ip=1
ipv4=192.0.2.1/32
leasestart=1
leasetime=10
errno=0

`,
			ipv4: ipv4,
			rip: &wgdynamic.RequestIP{
				IPv4: ipv4,
			},
			ok: true,
		},
		{
			name: "OK ipv6",
			req:  "request_ip=1\nipv6=2001:db8::1/128\n\n",
			res: `request_ip=1
ipv6=2001:db8::1/128
leasestart=1
leasetime=10
errno=0

`,
			ipv6: ipv6,
			rip: &wgdynamic.RequestIP{
				IPv6: ipv6,
			},
			ok: true,
		},
		{
			name: "OK ipv4/6",
			req: `request_ip=1
ipv4=192.0.2.1/32
ipv6=2001:db8::1/128

`,
			res: `request_ip=1
ipv4=192.0.2.1/32
ipv6=2001:db8::1/128
leasestart=1
leasetime=10
errno=0

`,
			ipv4: ipv4,
			ipv6: ipv6,
			rip: &wgdynamic.RequestIP{
				IPv4: ipv4,
				IPv6: ipv6,
			},
			ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, done := testClient(t, tt.res)

			// Perform request and immediately capture the input sent to the
			// server since no more requests will be made.
			rip, err := c.RequestIP(tt.ipv4, tt.ipv6)
			req := done()
			if err != nil {
				if tt.ok {
					t.Fatalf("failed to request IPs: %v", err)
				}

				// Is the error a protocol error? If so, compare it.
				if werr, ok := err.(*wgdynamic.Error); ok {
					if diff := cmp.Diff(tt.err, werr); diff != "" {
						t.Fatalf("unexpected protocol error (-want +got):\n%s", diff)
					}
				}

				return
			}
			if !tt.ok {
				t.Fatal("expected an error, but none occurred")
			}

			if diff := cmp.Diff(tt.req, req); diff != "" {
				t.Fatalf("unexpected request (-want +got):\n%s", diff)
			}

			// Save some test table duplication.
			tt.rip.LeaseStart = time.Unix(1, 0)
			tt.rip.LeaseTime = 10 * time.Second

			if diff := cmp.Diff(tt.rip, rip); diff != "" {
				t.Fatalf("unexpected RequestIP (-want +got):\n%s", diff)
			}
		})
	}
}

// testClient creates an ephemeral test client and server. The server will
// return res for the first method invoked on Client.
//
// Invoke the cleanup closure to close all connections and return the client's
// raw request.
func testClient(t *testing.T, res string) (*wgdynamic.Client, func() string) {
	t.Helper()

	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	// Used to capture the client's request and return it to the caller.
	reqC := make(chan string, 1)
	go func() {
		defer wg.Done()

		c, err := l.Accept()
		if err != nil {
			panicf("failed to accept: %v", err)
		}
		defer c.Close()

		// Capture the request and return a canned response.
		b := make([]byte, 128)
		n, err := c.Read(b)
		if err != nil {
			panicf("failed to read request: %v", err)
		}
		reqC <- string(b[:n])

		if _, err := io.WriteString(c, res); err != nil {
			panicf("failed to write response: %v", err)
		}
	}()

	// Point the Client at our ephemeral server.
	c := wgdynamic.TempClient(l.Addr().(*net.TCPAddr))

	return c, func() string {
		defer close(reqC)

		wg.Wait()

		if err := l.Close(); err != nil {
			t.Fatalf("failed to close listener: %v", err)
		}

		return <-reqC
	}
}

func mustIPNet(s string) *net.IPNet {
	_, ipn, err := net.ParseCIDR(s)
	if err != nil {
		panicf("failed to parse CIDR: %v", err)
	}

	return ipn
}

func panicf(format string, a ...interface{}) {
	panic(fmt.Sprintf(format, a...))
}
