package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
	Package         string `arg:"required" help:"package to test (e.g. ./lib)"`
	Base            string `help:"Git version (tag, branch etc.) to compare with. Leave empty to run on current branch only."`
	BaseGoExe       string `help:"The Go binary to use for the second run."`
	Tags            string `help:"Build -tags"`
	Cpu             string `help:"a comma separated list of CPU counts, e.g. -cpu 1,2,3,4"`
	ProfType        string `help:"write a profile of the given type and run pprof; valid types are 'cpu', 'mem', 'block'."`
	ProfCallgrind   bool   `help:"write a cpu profile and callgrind data and run qcachegrind"`
	ProfSampleIndex string `help:"pprof sample index"`

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
			p.Fail("Unsupported profType")
		}
	}

	if cfg.OutDir == "" {
		var err error
		cfg.OutDir, err = ioutil.TempDir("", "gobench")
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

	hasUncommitted := hasUncommittedChanges()

	if hasUncommitted && r.Base != "" {
		log.Fatal("error: --base set, but there are uncommited changes.")
	}

	if r.Base == "" && hasUncommitted {
		// Compare to a stashed version.
		r.Base = "stash"
	}

	if r.Count == 0 {
		r.Count = 1
		if r.Base != "" || r.BaseGoExe != "" {
			r.Count = benchStatCountCompare
		}
	}

	first, second := r.Base, r.currentBranch
	exe1, exe2 := goExe, r.BaseGoExe
	if exe2 == "" {
		exe2 = exe1
	}
	if hasUncommitted {
		// Stash and compare
		fmt.Println("Stash changes")
		stash("save")
		checkErr("run benchmark", r.runBenchmark(exe1, first))
		stash("pop")
	} else if r.Base != "" || r.BaseGoExe != "" {
		if first == "" {
			first = r.currentBranch
		}
		// Start with the "left" branch
		checkErr("checkout base", r.checkout(first))
		checkErr("run benchmark", r.runBenchmark(exe1, first))
		if second != first {
			checkErr("checkout current branch", r.checkout(second))
		}
	}

	checkErr("run benchmark", r.runBenchmark(exe2, second))

	if first != "" {
		// Make it stand out a little.
		fmt.Print("\n\n")
		checkErr("run benchstat", r.runBencStat(first, second))
	}
}

func (r runner) runBenchmark(exeName, name string) error {
	args := append(r.asBenchArgs(name), r.Package)

	b, _ := exec.Command(exeName, "version").CombinedOutput()
	fmt.Println("\n", string(b))

	cmd := exec.Command(exeName, args...)

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
		fmt.Errorf("failed to execute %q: %s", exeName, err)
	}

	return nil
}

func (r runner) runBencStat(name1, name2 string) error {
	filename1, filename2 := r.benchOutFilename(name1), r.benchOutFilename(name2)

	const cmdName = "benchstat"

	args := []string{filename1, filename2}
	output, err := exec.Command(cmdName, args...).CombinedOutput()
	if err != nil {
		return err
	}
	fmt.Println(string(output))

	return nil
}

func (r runner) runPprof() error {
	args := []string{"tool", "pprof"}
	if r.Base != "" {
		args = append(args, "-base", r.profileOutFilename(r.Base))
	}

	args = append(args, "--ignore=runtime")

	if r.ProfType == "mem" && r.ProfSampleIndex == "" {
		args = append(args, "--alloc_objects")
	}

	if r.ProfSampleIndex != "" {
		args = append(args, "-sample_index="+r.ProfSampleIndex)
	}

	// go tool pprof -callgrind -output callgrind.out innercpu.pprof
	if r.ProfCallgrind {
		cf := r.callgrindOutFilename()
		args = append(args, "-callgrind", "-output", cf)
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
	return filepath.Join(c.OutDir, c.normalizeName(name)+".bench")
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
	return f, nil
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
