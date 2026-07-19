package main

import (
	"bytes"
	"testing"

	qt "github.com/frankban/quicktest"
)

type nopCloser struct {
	*bytes.Buffer
}

func (nopCloser) Close() error { return nil }

func TestPkgNormalizer(t *testing.T) {
	c := qt.New(t)

	const in = `goos: darwin
goarch: arm64
pkg: github.com/gohugoio/hugo-goldmark-extensions/passthrough/v2
cpu: Apple M1 Pro
BenchmarkFoo-10   	 1000000	      1013 ns/op
PASS
ok  	github.com/gohugoio/hugo-goldmark-extensions/passthrough/v2	1.234s`

	const expect = `goos: darwin
goarch: arm64
pkg: github.com/gohugoio/hugo-goldmark-extensions/passthrough
cpu: Apple M1 Pro
BenchmarkFoo-10   	 1000000	      1013 ns/op
PASS
ok  	github.com/gohugoio/hugo-goldmark-extensions/passthrough/v2	1.234s`

	for _, chunk := range []int{1, 7, len(in)} {
		buf := nopCloser{&bytes.Buffer{}}
		n := &pkgNormalizer{w: buf}
		for i := 0; i < len(in); i += chunk {
			end := min(i+chunk, len(in))
			_, err := n.Write([]byte(in[i:end]))
			c.Assert(err, qt.IsNil)
		}
		c.Assert(n.Close(), qt.IsNil)
		c.Assert(buf.String(), qt.Equals, expect, qt.Commentf("chunk size %d", chunk))
	}
}
