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

type config struct {
	Bench   string `help:"run only those benchmarks matching a regular expression"`
	Count   int    `help:"run benchmark count times"`
	Package string `arg:"required" help:"package to test (e.g. ./lib)"`
	Base    string `help:"Git version (tag, branch etc.) to compare with. Leave empty to run on current branch only."`

	ProfMem bool `help:"write a mem profile and run pprof"`
	ProfCpu bool `help:"write a cpu profile and run pprof"`

	OutDir string `help:"directory to write files to. Defaults to a temp dir."`
}

func main() {

	var cfg config

	// Defaults
	cfg.Bench = "Bench*"

	p := arg.MustParse(&cfg)

	if cfg.ProfCpu && cfg.ProfMem {
		p.Fail("Use either --profmem or --profcpu not both.")
	}

	if cfg.OutDir == "" {
		var err error
		cfg.OutDir, err = ioutil.TempDir("", "gobench")
		checkErr(err)
		defer os.Remove(cfg.OutDir)
	}

	if cfg.Count == 0 {
		cfg.Count = 1
		if cfg.Base != "" {
			// We pick the best result when doing compare.
			cfg.Count = 3
		}
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

func (r runner) runBenchmarks() {

	if r.Base != "" {
		// Start with the "left" branch
		checkErr(r.checkout(r.Base))
		checkErr(r.runBenchmark(r.Base))
		checkErr(r.checkout(r.currentBranch))
	} else if hasUncommittedChanges() {
		fmt.Println("Stash changes")
		// Stash and compare
		stash("save")
		r.Base = "stash"
		checkErr(r.runBenchmark(r.Base))
		stash("pop")

	}

	checkErr(r.runBenchmark(r.currentBranch))

	if r.Base != "" {
		// Make it stand out a little.
		fmt.Print("\n\n")
		checkErr(r.runBenchcmp(r.Base, r.currentBranch))
	}
}

func (r runner) runBenchmark(name string) error {
	args := append(r.asBenchArgs(name), r.Package)

	cmd := exec.Command("go", args...)

	f, err := r.createBenchOutputFile(name)
	if err != nil {
		return err
	}
	defer f.Close()

	output := io.MultiWriter(f, os.Stdout)

	cmd.Stdout = output
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (r runner) runBenchcmp(name1, name2 string) error {
	filename1, filename2 := r.benchOutFilename(name1), r.benchOutFilename(name2)

	args := []string{"-best", filename1, filename2}
	output, err := exec.Command("benchcmp", args...).CombinedOutput()
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
	args = append(args, r.profileOutFilename(r.currentBranch))

	cmd := exec.Command("go", args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return err
	}

	return cmd.Wait()

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
	checkErr(err)
	return strings.TrimSpace(string(output))
}

func stash(command string) string {
	output, err := exec.Command("git", "stash", command).Output()
	checkErr(err)
	return strings.TrimSpace(string(output))
}

func hasUncommittedChanges() bool {
	_, err := exec.Command("git", "diff-index", "--quiet", "HEAD").Output()

	if err == nil {
		return false
	}
	if _, ok := err.(*exec.ExitError); ok {
		return true
	}

	log.Fatal(err)
	return true
}

func checkErr(err error) {
	if err != nil {
		log.Fatal("Error: ", err)
	}
}

func (c config) asBenchArgs(name string) []string {
	args := []string{
		"test",
		"-run", "NONE",
		"-bench", c.Bench,
		fmt.Sprintf("-count=%d", c.Count),
		"-test.benchmem=true",
	}

	if c.ProfMem {
		args = append(args, "-memprofile", c.profileOutFilename(name))
	}
	if c.ProfCpu {
		args = append(args, "-cpuprofile", c.profileOutFilename(name))
	}

	return args
}

func (c config) benchOutFilename(name string) string {
	return filepath.Join(c.OutDir, name+".bench")
}

func (c config) profileOutFilename(name string) string {
	return filepath.Join(c.OutDir, (name + ".pprof"))
}

func (c config) profilingEnabled() bool {
	return c.ProfCpu || c.ProfMem
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
