package wgdynamic

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
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

// RequestIP contains IP address assignments created in response to a
// request_ip command.
type RequestIP struct {
	IPv4, IPv6 *net.IPNet
	LeaseStart time.Time
	LeaseTime  time.Duration
}

// RequestIP requests IP address assignment from a server. ipv4 and ipv6 can
// be specified to request a specific IP address assignment. If ipv4 and/or ipv6
// are nil, the client will not request a specific IP address for that family.
func (c *Client) RequestIP(ipv4, ipv6 *net.IPNet) (*RequestIP, error) {
	var rip *RequestIP
	err := c.execute(func(rw io.ReadWriter) error {
		// TODO(mdlayher): can client specify a lease time or duration?
		req := &RequestIP{IPv4: ipv4, IPv6: ipv6}
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

// execute executes fn with a network connection backing rw.
func (c *Client) execute(fn func(rw io.ReadWriter) error) error {
	conn, err := net.DialTCP("tcp6", c.laddr, c.raddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Set a reasonable timeout.
	// TODO(mdlayher): make configurable?
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}

	return fn(conn)
}

// sendRequestIP writes a request_ip command with optional IPv4/6 addresses
// to w.
func sendRequestIP(w io.Writer, rip *RequestIP) error {
	// Build the command and attach optional parameters.
	b := bytes.NewBufferString("request_ip=1\n")

	if rip.IPv4 != nil {
		b.WriteString(fmt.Sprintf("ipv4=%s\n", rip.IPv4.String()))
	}
	if rip.IPv6 != nil {
		b.WriteString(fmt.Sprintf("ipv6=%s\n", rip.IPv6.String()))
	}
	if !rip.LeaseStart.IsZero() {
		b.WriteString(fmt.Sprintf("leasestart=%d\n", rip.LeaseStart.Unix()))
	}
	if rip.LeaseTime > 0 {
		b.WriteString(fmt.Sprintf("leasetime=%d\n", int(rip.LeaseTime.Seconds())))
	}

	// A final newline completes the request.
	b.WriteString("\n")

	_, err := b.WriteTo(w)
	return err
}

// parse begins the parsing process for reading a request or response, returning
// a kvParser and the command being performed.
func parse(r io.Reader) (*kvParser, string, error) {
	// Consume the first line to retrieve the command.
	p := newKVParser(r)
	if !p.Next() {
		return nil, "", p.Err()
	}

	return p, p.Key(), nil
}

// parseRequestIP parses a RequestIP from a request_ip command response stream.
func parseRequestIP(p *kvParser) (*RequestIP, error) {
	var rip RequestIP
	for p.Next() {
		switch p.Key() {
		case "ipv4":
			rip.IPv4 = p.IPNet(4)
		case "ipv6":
			rip.IPv6 = p.IPNet(6)
		case "leasestart":
			rip.LeaseStart = time.Unix(int64(p.Int()), 0)
		case "leasetime":
			rip.LeaseTime = time.Duration(p.Int()) * time.Second
		}
	}

	if err := p.Err(); err != nil {
		return nil, err
	}

	return &rip, nil
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
