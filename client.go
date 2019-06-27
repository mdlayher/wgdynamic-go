package wgdynamic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// port is the well-known port for wg-dynamic.
const port = 970

// serverIP is the well-known server IPv6 address for wg-dynamic.
var serverIP = &net.IPNet{
	IP:   net.ParseIP("fe80::"),
	Mask: net.CIDRMask(64, 128),
}

// A Client can request IP address assignment using the wg-dynamic protocol.
type Client struct {
	// The local and remote TCP addresses for client/server communication.
	laddr, raddr *net.TCPAddr
}

// NewClient creates a new Client bound to the specified WireGuard interface.
// NewClient will return an error if the interface does not have an IPv6
// link-local address configured.
func NewClient(iface string) (*Client, error) {
	// TODO(mdlayher): verify this is actually a WireGuard device.
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, err
	}

	addrs, err := ifi.Addrs()
	if err != nil {
		return nil, err
	}

	return newClient(ifi.Name, addrs)
}

// newClient constructs a Client which communicates using well-known wg-dynamic
// addresses. It is used as an entry point in tests.
func newClient(iface string, addrs []net.Addr) (*Client, error) {
	// Find a suitable link-local IPv6 address for wg-dynamic communication.
	llip, ok := linkLocalIPv6(addrs)
	if !ok {
		return nil, fmt.Errorf("wgdynamic: no link-local IPv6 address for interface %q", iface)
	}

	// Client will listen on a well-known port and send requests to the
	// well-known server address.
	return &Client{
		laddr: &net.TCPAddr{
			IP:   llip.IP,
			Port: port,
			Zone: iface,
		},
		raddr: &net.TCPAddr{
			IP:   serverIP.IP,
			Port: port,
			Zone: iface,
		},
	}, nil
}

// RequestIP requests IP address assignment from a server. Fields within req
// can be specified to request specific IP address assignment parameters. If req
// is nil, the server will automatically perform IP address assignment.
//
// The provided Context must be non-nil. If the context expires before the
// request is complete, an error is returned.
func (c *Client) RequestIP(ctx context.Context, req *RequestIP) (*RequestIP, error) {
	// Use a separate variable for the output so we don't overwrite the
	// caller's request.
	var rip *RequestIP
	err := c.execute(ctx, func(rw io.ReadWriter) error {
		if err := sendRequestIP(rw, req); err != nil {
			return err
		}

		// Begin parsing the response and ensure the server replied with the
		// appropriate command.
		p, cmd, err := parse(rw)
		if err != nil {
			return err
		}
		if cmd != "request_ip" {
			return errors.New("wgdynamic: server sent malformed request_ip command response")
		}

		// Now that we've verified the command, parse the rest of its body.
		rrip, err := parseRequestIP(p)
		if err != nil {
			return err
		}

		rip = rrip
		return nil
	})
	if err != nil {
		return nil, err
	}

	return rip, nil
}

// deadlineNow is a time in the past that indicates a connection should
// immediately time out.
var deadlineNow = time.Unix(1, 0)

// execute executes fn with a network connection backing rw.
func (c *Client) execute(ctx context.Context, fn func(rw io.ReadWriter) error) error {
	// The server expects the client to be bound to a specific local address.
	d := &net.Dialer{LocalAddr: c.laddr}
	conn, err := d.DialContext(ctx, "tcp6", c.raddr.String())
	if err != nil {
		return err
	}
	defer conn.Close()

	// Enable immediate connection cancelation via context by setting a deadline
	// in the past if/when the context is canceled. If the request completes,
	// doneC is closed and the select statement is unblocked.
	var wg sync.WaitGroup
	wg.Add(1)

	doneC := make(chan struct{})
	defer func() {
		close(doneC)
		wg.Wait()
	}()

	go func() {
		defer wg.Done()

		select {
		case <-ctx.Done():
			// Cancel the request immediately.
			_ = conn.SetDeadline(deadlineNow)
		case <-doneC:
		}
	}()

	return fn(conn)
}

// linkLocalIPv6 finds a link-local IPv6 address in addrs. It returns true when
// one is found.
func linkLocalIPv6(addrs []net.Addr) (*net.IPNet, bool) {
	var llip *net.IPNet
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}

		// Only look for link-local IPv6 addresses.
		if ipn.IP.To4() == nil && ipn.IP.IsLinkLocalUnicast() {
			llip = ipn
			break
		}
	}

	return llip, llip != nil
}
