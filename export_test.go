package wgdynamic

import "net"

// TempClient creates a Client which connects to the specified address, rather
// than the default well-known address.
func TempClient(addr *net.TCPAddr) *Client {
	return &Client{
		// Nil local address means to choose an address automatically.
		raddr: addr,
	}
}
