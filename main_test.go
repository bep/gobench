package main

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func TestBenchmarkCompareToBranch(t *testing.T) {
	os.Args = []string{"-bench=Sleep", "-base=testing", "-package=./testing"}

	out := captureOutput(main)

	// Assert that the correct steps have been run.
	assertContainsAll(t, out,
		"old bytes",
		`Benchmark and compare branch "testing" and "master"`)
}

func TestBenchmarkCurrent(t *testing.T) {
	os.Args = []string{"-bench=Sleep", "-package=./testing"}

	out := captureOutput(main)

	// Assert that the correct steps have been run.
	assertContainsAll(t, out,
		"ns/op",
		`Benchmark branch "master"`)

	assertNotContainsAll(t, out,
		"old bytes")
}

func TestBenchmarkCompareToStashed(t *testing.T) {
	os.Args = []string{"-bench=Sleep", "-package=./testing"}

	const dummy = "gobenchdummyfile.txt"

	ioutil.WriteFile(dummy, []byte("hello"), 0o777)
	defer os.Remove(dummy)

	out := captureOutput(main)

	// Assert that the correct steps have been run.
	assertContainsAll(t, out,
		"old bytes",
		`Stash changes`)
}

func assertContainsAll(t *testing.T, content string, values ...string) {
	for _, value := range values {
		if !strings.Contains(content, value) {
			t.Fatalf("%q not found in:\n\n%s", value, content)
		}
	}
}

func assertNotContainsAll(t *testing.T, content string, values ...string) {
	for _, value := range values {
		if strings.Contains(content, value) {
			t.Fatalf("%q found in:\n\n%s", value, content)
		}
	}
}

func captureOutput(f func()) string {
	oldOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	out, _ := ioutil.ReadAll(r)
	os.Stdout = oldOut

	return string(out)
}
