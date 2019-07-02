package wgdynamic_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/mdlayher/wgdynamic-go"
)

type subtest struct {
	name string
	s    *wgdynamic.Server
	fn   func(t *testing.T, c *wgdynamic.Client)
}

func TestServer(t *testing.T) {
	tests := []struct {
		name string
		subs []subtest
	}{
		{
			name: "RequestIP",
			subs: requestIPTests(t),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, st := range tt.subs {
				t.Run(st.name, func(t *testing.T) {
					c, done := testServer(t, st.s)
					defer done()

					st.fn(t, c)
				})
			}
		})
	}
}

func requestIPTests(t *testing.T) []subtest {
	var (
		ipv4 = mustIPNet("192.0.2.1/32")
		ipv6 = mustIPNet("2001:db8::1/128")

		want = &wgdynamic.RequestIP{
			IPv4:       ipv4,
			IPv6:       ipv6,
			LeaseStart: time.Unix(1, 0),
			LeaseTime:  10 * time.Second,
		}

		errInternal = &wgdynamic.Error{
			Number:  1,
			Message: "Internal server error",
		}
	)

	return []subtest{
		{
			name: "not implemented",
			s: &wgdynamic.Server{
				RequestIP: nil,
			},
			fn: func(t *testing.T, c *wgdynamic.Client) {
				_, err := c.RequestIP(context.Background(), nil)
				if diff := cmp.Diff(errInternal, err); diff != "" {
					t.Fatalf("unexpected error (-want +got):\n%s", diff)
				}
			},
		},
		{
			name: "generic error",
			s: &wgdynamic.Server{
				RequestIP: func(_ net.Addr, _ *wgdynamic.RequestIP) (*wgdynamic.RequestIP, error) {
					return nil, errors.New("some error")
				},
			},
			fn: func(t *testing.T, c *wgdynamic.Client) {
				_, err := c.RequestIP(context.Background(), nil)
				if diff := cmp.Diff(errInternal, err); diff != "" {
					t.Fatalf("unexpected error (-want +got):\n%s", diff)
				}
			},
		},
		{
			name: "OK client request",
			s: &wgdynamic.Server{
				RequestIP: func(_ net.Addr, r *wgdynamic.RequestIP) (*wgdynamic.RequestIP, error) {
					// Return the addresses requested by client, but also
					// populate lease time fields.
					r.LeaseStart = want.LeaseStart
					r.LeaseTime = want.LeaseTime
					return r, nil
				},
			},
			fn: func(t *testing.T, c *wgdynamic.Client) {
				got, err := c.RequestIP(context.Background(), &wgdynamic.RequestIP{
					IPv4: ipv4,
					IPv6: ipv6,
				})
				if err != nil {
					t.Fatalf("failed to request IP: %v", err)
				}

				if diff := cmp.Diff(want, got); diff != "" {
					t.Fatalf("unexpected RequestIP (-want +got):\n%s", diff)
				}
			},
		},
		{
			name: "OK auto assign",
			s: &wgdynamic.Server{
				RequestIP: func(_ net.Addr, r *wgdynamic.RequestIP) (*wgdynamic.RequestIP, error) {
					// Ensure the Client does not request any addresses.
					if r.IPv4 != nil || r.IPv6 != nil {
						return nil, errors.New("could not assign requested addresses")
					}

					return want, nil
				},
			},
			fn: func(t *testing.T, c *wgdynamic.Client) {
				got, err := c.RequestIP(context.Background(), nil)
				if err != nil {
					t.Fatalf("failed to request IP: %v", err)
				}

				if diff := cmp.Diff(want, got); diff != "" {
					t.Fatalf("unexpected RequestIP (-want +got):\n%s", diff)
				}
			},
		},
	}
}

func testServer(t *testing.T, s *wgdynamic.Server) (*wgdynamic.Client, func()) {
	t.Helper()

	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		if err := s.Serve(l); err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}

			panicf("failed to serve: %v", err)
		}
	}()

	c := &wgdynamic.Client{
		RemoteAddr: l.Addr().(*net.TCPAddr),
	}

	return c, func() {
		defer wg.Wait()

		if err := s.Close(); err != nil {
			t.Fatalf("failed to close server listener: %v", err)
		}
	}
}
