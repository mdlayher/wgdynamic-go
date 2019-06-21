package wgdynamic

import (
	"strings"
	"testing"
)

func Test_kvParserError(t *testing.T) {
	tests := []struct {
		name string
		s    string
		fn   func(p *kvParser)
	}{
		{
			name: "bad key/value pair",
			s:    "hello=world\nkey:value\n\n",
			fn: func(p *kvParser) {
				// Advance to pick up bad key/value pair.
				_ = p.Next()
			},
		},
		{
			name: "bad integer",
			s:    "hello=string\n\n",
			fn: func(p *kvParser) {
				_ = p.Int()
			},
		},
		{
			name: "bad IPNet",
			s:    "hello=string\n\n",
			fn: func(p *kvParser) {
				// Family doesn't matter; IPNet check is performed before
				// family check.
				_ = p.IPNet(4)
			},
		},
		{
			name: "bad IPNet IPv4",
			s:    "hello=2001:db8::1/128\n\n",
			fn: func(p *kvParser) {
				_ = p.IPNet(4)
			},
		},
		{
			name: "bad IPNet IPv6",
			s:    "hello=192.0.2.1/32\n\n",
			fn: func(p *kvParser) {
				_ = p.IPNet(6)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newKVParser(strings.NewReader(tt.s))

			// Advance to the first line of input and then call into the test
			// function to generate errors.
			_ = p.Next()
			tt.fn(p)

			err := p.Err()
			if err == nil {
				t.Fatal("expected an error, but none occurred")
			}

			t.Logf("OK error: %v", err)
		})
	}
}
