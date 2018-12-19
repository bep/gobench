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

	if cfg.OutputDir == "" {
		var err error
		cfg.OutputDir, err = ioutil.TempDir("", "gobench")
		must(err)
		defer os.Remove(cfg.OutputDir)
	}

	r := runner{config: cfg}

	r.runBenchmarks()
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
	args := append(r.asBenchArgs(), r.Package)

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

func (c config) asBenchArgs() []string {
	return []string{
		"test",
		"-run", "NONE",
		"-bench", c.Bench,
		fmt.Sprintf("-count=%d", c.Count),
		fmt.Sprintf("-test.benchmem=%t", c.BenchMem),
	}
}

func (c config) benchOutFilename(name string) string {
	return filepath.Join(c.OutputDir, c.benchOutName(name))
}

func (c config) benchOutName(name string) string {
	return name + ".bench"
}

func (c config) createOutputFile(name string) (io.WriteCloser, error) {
	f, err := os.Create(filepath.Join(c.OutputDir, c.benchOutName(name)))
	if err != nil {
		return nil, err
	}

	return f, nil

}
