package wgdynamic

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// The well-known server IP address and port for wg-dynamic.
const (
	serverIP = "fe80::"
	port     = 970
)

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
	var llip net.IP
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}

		// Only look for link-local IPv6 addresses.
		if ipn.IP.To4() == nil && ipn.IP.IsLinkLocalUnicast() {
			llip = ipn.IP
			break
		}
	}

	if llip == nil {
		return nil, fmt.Errorf("wgdynamic: no link-local IPv6 address for interface %q", iface)
	}

	// Client will listen on a well-known port and send requests to the
	// well-known server address.
	return &Client{
		laddr: &net.TCPAddr{
			IP:   llip,
			Port: port,
			Zone: iface,
		},
		raddr: &net.TCPAddr{
			IP:   net.ParseIP(serverIP),
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
	conn, err := net.DialTCP("tcp6", c.laddr, c.raddr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Set a reasonable timeout.
	// TODO(mdlayher): make configurable?
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, err
	}

	if err := sendRequestIP(conn, ipv4, ipv6); err != nil {
		return nil, err
	}

	return parseRequestIP(conn)
}

// sendRequestIP writes a request_ip command with optional IPv4/6 addresses
// to w.
func sendRequestIP(w io.Writer, ipv4, ipv6 *net.IPNet) error {
	// Build the command and attach optional parameters.
	b := bytes.NewBufferString("request_ip=1\n")

	if ipv4 != nil {
		b.WriteString(fmt.Sprintf("ipv4=%s\n", ipv4.String()))
	}
	if ipv6 != nil {
		b.WriteString(fmt.Sprintf("ipv6=%s\n", ipv6.String()))
	}

	// A final newline completes the request.
	b.WriteString("\n")

	_, err := b.WriteTo(w)
	return err
}

// parseRequestIP parses a RequestIP from a request_ip command response stream.
func parseRequestIP(r io.Reader) (*RequestIP, error) {
	var (
		rip RequestIP
		ok  bool
	)

	p := newKVParser(r)
	for p.Next() {
		switch p.Key() {
		case "request_ip":
			// Verify the server replied to the requested command.
			ok = p.String() == "1"
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
	if !ok {
		return nil, errors.New("wgdynamic: server sent malformed request_ip command response")
	}

	return &rip, nil
}
