package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	arg "github.com/alexflint/go-arg"
)

var (
	// These will be set by Goreleaser.
	version = "v0.5"
	commit  = ""
	date    = ""
)

var goExe = "go"

func init() {
	if exe := os.Getenv("GOEXE"); exe != "" {
		goExe = exe
	}
}

type config struct {
	Bench           string `help:"run only those benchmarks matching a regular expression"`
	Count           int    `help:"run benchmark count times"`
	Package         string `arg:"" help:"package to test (e.g. ./lib)" default:"."`
	Base            string `help:"Git version (tag, branch etc.) to compare with. Leave empty to run on current branch only."`
	BaseGoToolChain string `help:"The Go toolchain to use for the first run."`
	NoStash         bool   `help:"Don't stash uncommited changes (just run the benchmark against the current code)."`
	Tags            string `help:"Build -tags"`
	Race            bool   `help:"Run with -race flag"`
	IncludeRuntime  bool   `help:"Include runtime in the profile."`
	Cpu             string `help:"a comma separated list of CPU counts, e.g. -cpu 1,2,3,4"`
	ProfType        string `help:"write a profile of the given type and run pprof; valid types are 'cpu', 'mem', 'block'."`
	ProfGran        string `help:"pprof granularity, one of 'file','functions', 'filefunctions', 'files', 'lines', 'addresses'"`
	ProfNodecount   int    `help:"max number of nodes to show" default:"10"`
	ProfCallgrind   bool   `help:"write a cpu profile and callgrind data and run qcachegrind"`
	ProfSampleIndex string `help:"pprof sample index"`
	ProfAlloc       string `help:"pprof alloc space or alloc objects" default:"objects"`

	OutDir string `help:"directory to write files to. Defaults to a temp dir."`
}

// Number of runs when comparing branches (if not set).
const benchStatCountCompare = 4

func main() {
	var cfg config

	// Defaults
	cfg.Bench = "Bench*"

	p := arg.MustParse(&cfg)

	if cfg.ProfType != "" {
		if cfg.ProfType != "mem" && cfg.ProfType != "cpu" && cfg.ProfType != "block" {
			p.Fail(fmt.Sprintf("invalid profile type %q. Must be one of %v", cfg.ProfType, []string{"mem", "cpu", "block"}))
		}
	}

	if cfg.OutDir == "" {
		var err error
		cfg.OutDir, err = os.MkdirTemp("", "gobench")
		checkErr("create temp dir", err)
		defer os.Remove(cfg.OutDir)
	}

	r := runner{currentBranch: getCurrentBranch(), config: cfg}

	if r.Base != "" {
		fmt.Printf("Benchmark and compare branch %q and %q.\n", r.Base, r.currentBranch)
	} else {
		fmt.Printf("Benchmark branch %q\n", r.currentBranch)
	}

	r.runBenchmarks()

	if r.profilingEnabled() {
		r.runPprof()
	}
}

type runner struct {
	currentBranch string
	config
}

func (r *runner) runBenchmarks() {
	var hasUncommitted bool

	if !r.NoStash {
		hasUncommitted = hasUncommittedChanges()

		if hasUncommitted && r.Base != "" {
			log.Fatal("error: --base set, but there are uncommited changes.")
		}

		if r.Base == "" && hasUncommitted {
			// Compare to a stashed version.
			r.Base = "stash"
		}
	}

	if r.Count == 0 {
		r.Count = 1
		if r.Base != "" || r.BaseGoToolChain != "" {
			r.Count = benchStatCountCompare
		}
	}

	first, second := r.Base, r.currentBranch

	if hasUncommitted {
		// Stash and compare
		fmt.Println("Stash changes")
		stash("save")
		checkErr("run benchmark", r.runBenchmark(r.BaseGoToolChain, first))
		stash("pop")
	} else if r.Base != "" || r.BaseGoToolChain != "" {
		if first == "" {
			first = r.currentBranch
		}
		// Start with the "left" branch
		checkErr("checkout base", r.checkout(first))
		checkErr("run benchmark", r.runBenchmark(r.BaseGoToolChain, first))
		if second != first {
			checkErr("checkout current branch", r.checkout(second))
		}
	}

	checkErr("run benchmark", r.runBenchmark("", second))

	// Make it stand out a little.
	fmt.Print("\n\n")
	checkErr("run benchstat", r.runBenchStat(first, second))
}

func (r runner) runBenchmark(goToolChain, name string) error {
	args := append(r.asBenchArgs(name), r.Package)

	b, _ := exec.Command(goExe, "version").CombinedOutput()
	fmt.Println("\n", string(b))

	cmd := exec.Command(goExe, args...)
	if goToolChain != "" {
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN="+goToolChain)
	}

	f, err := r.createBenchOutputFile(name)
	if err != nil {
		return err
	}
	defer f.Close()

	output := io.MultiWriter(f, os.Stdout)

	cmd.Stdout = output
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to execute %q: %s", goToolChain, err)
	}

	return nil
}

func (r runner) runBenchStat(name1, name2 string) error {
	if name2 == "" {
		return errors.New("no second name")
	}
	const cmdName = "benchstat"

	name2 = r.benchOutName(name2)

	var args []string
	if name1 != "" {
		name1 = r.benchOutName(name1)
		args = []string{name1, name2}
	} else {
		args = []string{name2}
	}

	flags := []string{"-format", "text"}

	args = append(flags, args...)

	cmd := exec.Command(cmdName, args...)
	cmd.Dir = r.OutDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	fmt.Println(string(output))

	return nil
}

func (r runner) runPprof() error {
	args := []string{"tool", "pprof"}
	if r.Base != "" {
		args = append(args, "-diff_base", r.profileOutFilename(r.Base))
	}

	if !r.IncludeRuntime {
		args = append(args, "--ignore=runtime")
	}

	if r.ProfType == "mem" && r.ProfSampleIndex == "" {
		args = append(args, fmt.Sprintf("--alloc_%s", r.ProfAlloc))
	}

	if r.ProfSampleIndex != "" {
		args = append(args, "-sample_index="+r.ProfSampleIndex)
	}

	// go tool pprof -callgrind -output callgrind.out innercpu.pprof
	if r.ProfCallgrind {
		cf := r.callgrindOutFilename()
		args = append(args, "-callgrind", "-output", cf)
	}

	if r.ProfGran != "" {
		valid := map[string]bool{
			"file":          true,
			"functions":     true,
			"filefunctions": true,
			"files":         true,
			"lines":         true,
			"addresses":     true,
		}
		if !valid[r.ProfGran] {
			return fmt.Errorf("invalid granularity %q. Must be one of %v", r.ProfGran, []string{"file", "functions", "filefunctions", "files", "lines", "addresses"})
		}
		args = append(args, "-"+r.ProfGran)
	}

	if r.ProfNodecount > 0 {
		args = append(args, fmt.Sprintf("-nodecount=%d", r.ProfNodecount))
	}

	args = append(args, r.profileOutFilename(r.currentBranch))

	cmd := exec.Command(goExe, args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return err
	}

	if err := cmd.Wait(); err != nil {
		log.Fatal(err)
	}

	if r.ProfCallgrind {
		cmd := exec.Command("qcachegrind", r.callgrindOutFilename())

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Start(); err != nil {
			return err
		}

		return cmd.Wait()
	}

	return nil
}

func (r runner) checkout(branch string) error {
	output, err := exec.Command("git", "checkout", branch).CombinedOutput()
	if err != nil {
		return err
	}
	fmt.Println(string(output))
	return nil
}

func getCurrentBranch() string {
	output, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	checkErr("get current branch", err)
	return strings.TrimSpace(string(output))
}

func stash(command string) string {
	output, err := exec.Command("git", "stash", command).Output()
	checkErr("stash", err)
	return strings.TrimSpace(string(output))
}

func hasUncommittedChanges() bool {
	_, err := exec.Command("git", "diff-index", "--quiet", "HEAD", "--").Output()

	if err == nil {
		return false
	}
	if _, ok := err.(*exec.ExitError); ok {
		return true
	}

	log.Fatal(err)
	return true
}

func checkErr(what string, err error) {
	if err != nil {
		log.Fatal(what+": ", "Error: ", err)
	}
}

func (c config) asBenchArgs(name string) []string {
	args := []string{
		"test",
		"-run", "NONE",
		"-bench", c.Bench,
		fmt.Sprintf("-count=%d", c.Count),
		"-test.benchmem=true",
		"-timeout", "40m",
	}
	if c.Race {
		args = append(args, "-race")
	}

	if c.Tags != "" {
		args = append(args, "-tags", c.Tags)
	}

	if c.ProfType != "" {
		args = append(args, fmt.Sprintf("-%sprofile", c.ProfType), c.profileOutFilename(name))
	}

	if c.Cpu != "" {
		args = append(args, "-cpu", c.Cpu)
	}

	return args
}

func (c config) normalizeName(name string) string {
	// Slashes in branch names.
	return strings.ReplaceAll(name, "/", "-")
}

func (c config) benchOutFilename(name string) string {
	return filepath.Join(c.OutDir, c.benchOutName(name))
}

func (c config) benchOutName(name string) string {
	return c.normalizeName(name) + ".bench"
}

func (c config) profileOutFilename(name string) string {
	return filepath.Join(c.OutDir, (c.normalizeName(name) + ".pprof"))
}

func (c config) callgrindOutFilename() string {
	return filepath.Join(c.OutDir, ("callgrind.out"))
}

func (c config) profilingEnabled() bool {
	return c.ProfType != ""
}

func (c config) createBenchOutputFile(name string) (io.WriteCloser, error) {
	f, err := os.Create(c.benchOutFilename(name))
	if err != nil {
		return nil, err
	}
	return &pkgNormalizer{w: f}, nil
}

// Strips the major version suffix from a module path so benchstat
// sees e.g. "example.com/foo" for both "example.com/foo" and "example.com/foo/v2".
var pkgVersionRe = regexp.MustCompile(`^(pkg:\s+\S+?)/v[0-9]+\s*$`)

// pkgNormalizer normalizes the "pkg:" lines in the benchmark output.
type pkgNormalizer struct {
	w   io.WriteCloser
	buf []byte
}

func (n *pkgNormalizer) Write(p []byte) (int, error) {
	n.buf = append(n.buf, p...)
	for {
		i := bytes.IndexByte(n.buf, '\n')
		if i < 0 {
			break
		}
		if err := n.writeLine(n.buf[:i], true); err != nil {
			return 0, err
		}
		n.buf = n.buf[i+1:]
	}
	return len(p), nil
}

func (n *pkgNormalizer) writeLine(line []byte, nl bool) error {
	line = pkgVersionRe.ReplaceAll(line, []byte("$1"))
	if nl {
		line = append(line[:len(line):len(line)], '\n')
	}
	_, err := n.w.Write(line)
	return err
}

func (n *pkgNormalizer) Close() error {
	if len(n.buf) > 0 {
		if err := n.writeLine(n.buf, false); err != nil {
			n.w.Close()
			return err
		}
		n.buf = nil
	}
	return n.w.Close()
}

func (c config) Version() string {
	version := "gobench " + version

	if commit != "" || date != "" {
		version += ","
	}
	if commit != "" {
		version += " " + commit
	}

	if date != "" {
		version += " " + date
	}

	return version
}
