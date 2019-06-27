package wgdynamic

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"time"
)

// RequestIP contains IP address assignments created in response to a
// request_ip command.
type RequestIP struct {
	IPv4, IPv6 *net.IPNet
	LeaseStart time.Time
	LeaseTime  time.Duration
}

// TODO(mdlayher): request_ip protocol version is hardcoded at 1 and should
// be parameterized in some way.

// sendRequestIP writes a request_ip command with optional IPv4/6 addresses
// to w.
func sendRequestIP(w io.Writer, rip *RequestIP) error {
	if rip == nil {
		// No additional parameters to send.
		_, err := w.Write([]byte("request_ip=1\n\n"))
		return err
	}

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