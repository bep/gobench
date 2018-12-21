package main

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func TestGobench(t *testing.T) {
	os.Args = []string{"-bench Sleep", "--package=./testing"}

	oldOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	main()

	w.Close()
	out, _ := ioutil.ReadAll(r)
	outStr := string(out)

	os.Stdout = oldOut

	assertContainsAll(t, outStr, "8 B/op", `Benchmark branch "master"`)
}

func assertContainsAll(t *testing.T, content string, values ...string) {
	for _, value := range values {
		if !strings.Contains(content, value) {
			t.Fatalf("%q not found in:\n\n%s", value, content)
		}
	}
}
