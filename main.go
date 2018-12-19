package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	arg "github.com/alexflint/go-arg"
)

const (
	branchMaster = "master"
)

type config struct {
	Bench    string `help:"run only those benchmarks matching a regular expression"`
	Count    int    `help:"run benchmark count times"`
	BenchMem bool   `arg:"--bench-mem" help:"enable to benchmark memory usage"`
	Package  string `help:"package to test"`
	Branch1  string `help:"first Git branch"`
	Branch2  string `help:"second Git branch"`

	EnableMemProfile bool `help:"write a mem profile and run pprof"`
	EnableCpuProfile bool `help:"write a cpu profile and run pprof"`

	OutputDir string
}

func main() {

	var cfg config

	// Defaults
	cfg.Bench = "Bench*"
	cfg.Count = 3
	cfg.Package = "./..."
	cfg.BenchMem = true
	cfg.Branch1 = branchMaster
	cfg.Branch2 = branchMaster

	arg.MustParse(&cfg)

	if cfg.EnableCpuProfile && cfg.EnableMemProfile {
		log.Fatal("Use either --enablememprofile or --enablecpuprofile -- not both.")
	}

	if cfg.OutputDir == "" {
		var err error
		cfg.OutputDir, err = ioutil.TempDir("", "gobench")
		must(err)
		defer os.Remove(cfg.OutputDir)
	}

	r := runner{config: cfg}

	r.runBenchmarks()

	if r.profilingEnabled() {
		r.runPprof()
	}

}

type runner struct {
	config
}

func (r runner) runBenchmarks() {
	must(r.checkout(r.Branch1))
	must(r.runBenchmark(r.Branch1))
	if r.Branch1 == r.Branch2 {
		return
	}
	must(r.checkout(r.Branch2))
	must(r.runBenchmark(r.Branch2))
	must(r.runBenchcmp(r.Branch1, r.Branch2))
}

func (r runner) runBenchmark(name string) error {
	args := append(r.asBenchArgs(name), r.Package)

	cmd := exec.Command("go", args...)
	f, err := r.createOutputFile(name)
	if err != nil {
		return err
	}
	defer f.Close()

	output := io.MultiWriter(f, os.Stdout)

	cmd.Stdout = output
	cmd.Stderr = output

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
	if r.Branch1 != r.Branch2 {
		args = append(args, "-base", r.profileOutFilename(r.Branch1))
	}
	args = append(args, r.profileOutFilename(r.Branch2))

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

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func (c config) asBenchArgs(name string) []string {
	args := []string{
		"test",
		"-run", "NONE",
		"-bench", c.Bench,
		fmt.Sprintf("-count=%d", c.Count),
		fmt.Sprintf("-test.benchmem=%t", c.BenchMem),
	}

	if c.EnableMemProfile {
		args = append(args, "-memprofile", c.profileOutFilename(name))
	}
	if c.EnableCpuProfile {
		args = append(args, "-cpuprofile", c.profileOutFilename(name))
	}

	return args
}

func (c config) benchOutFilename(name string) string {
	return filepath.Join(c.OutputDir, c.benchOutName(name))
}

func (c config) benchOutName(name string) string {
	return name + ".bench"
}

func (c config) profileOutFilename(name string) string {
	return filepath.Join(c.OutputDir, (name + ".pprof"))
}

func (c config) profilingEnabled() bool {
	return c.EnableCpuProfile || c.EnableMemProfile
}

func (c config) createOutputFile(name string) (io.WriteCloser, error) {
	f, err := os.Create(filepath.Join(c.OutputDir, c.benchOutName(name)))
	if err != nil {
		return nil, err
	}

	return f, nil

}
