package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/xiongnemo/saki/internal/version"
)

func TestCLI_CurrentMainVersion_whenVersionFlagProvided(t *testing.T) {
	// Given
	command := exec.Command("go", "run", ".", "--version")
	command.Env = append(os.Environ(), "GOPROXY=https://goproxy.cn,direct")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	// When
	err := command.Run()

	// Then
	if err != nil {
		t.Fatalf("go run . --version: %v\nstderr: %s", err, stderr.String())
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
	if got, want := stdout.String(), version.String()+"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

type callbackFailures struct {
	tui     error
	install error
}

type cliTestResult struct {
	status         int
	stdout         string
	stderr         string
	tuiCalls       int
	installCalls   int
	installOptions installOptions
}

func executeCLIForTest(args []string, failures callbackFailures) cliTestResult {
	var result cliTestResult
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runtime := commandRuntime{
		stdin:   strings.NewReader("input\n"),
		stdout:  &stdout,
		stderr:  &stderr,
		version: "v-test",
		runTUI: func() error {
			result.tuiCalls++
			return failures.tui
		},
		install: func(options installOptions) error {
			result.installCalls++
			result.installOptions = options
			return failures.install
		},
	}

	result.status = runCLI(args, runtime)
	result.stdout = stdout.String()
	result.stderr = stderr.String()
	return result
}

func assertNoCallbacks(t *testing.T, result cliTestResult) {
	t.Helper()
	if result.tuiCalls != 0 || result.installCalls != 0 {
		t.Fatalf("callback calls = tui:%d install:%d, want both zero", result.tuiCalls, result.installCalls)
	}
}

func TestCLI_NoArgs_runsTUI(t *testing.T) {
	// Given
	result := executeCLIForTest(nil, callbackFailures{})

	// Then
	if result.status != 0 || result.tuiCalls != 1 {
		t.Fatalf("status = %d, TUI calls = %d, want 0 and 1", result.status, result.tuiCalls)
	}
	if result.stdout != "" || result.stderr != "" || result.installCalls != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCLI_Version_printsExactlyOneLine(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"-v"}} {
		t.Run(args[0], func(t *testing.T) {
			// When
			result := executeCLIForTest(args, callbackFailures{})

			// Then
			if result.status != 0 || result.stdout != "v-test\n" || result.stderr != "" {
				t.Fatalf("status/stdout/stderr = %d/%q/%q", result.status, result.stdout, result.stderr)
			}
			if result.tuiCalls != 0 || result.installCalls != 0 {
				t.Fatalf("unexpected callback counts: %+v", result)
			}
		})
	}
}

func TestCLI_Help_returnsZero_withoutCallbacks(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"root long", []string{"--help"}, "Usage: saki"},
		{"root short", []string{"-h"}, "Usage: saki"},
		{"install long", []string{"install", "--help"}, "Usage: saki install"},
		{"install short", []string{"install", "-h"}, "Usage: saki install"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			result := executeCLIForTest(test.args, callbackFailures{})

			// Then
			if result.status != 0 || !strings.Contains(result.stdout, test.want) || result.stderr != "" {
				t.Fatalf("status/stdout/stderr = %d/%q/%q", result.status, result.stdout, result.stderr)
			}
			assertNoCallbacks(t, result)
		})
	}
}

func TestCLI_Install_passesParsedOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want installOptions
	}{
		{"defaults", []string{"install"}, installOptions{directory: "~/.local/bin"}},
		{"long flags", []string{"install", "--dir", "custom/bin", "--yes"}, installOptions{directory: "custom/bin", yes: true}},
		{"short yes and equals dir", []string{"install", "-y", "--dir=other/bin"}, installOptions{directory: "other/bin", yes: true}},
		{"dash-leading equals dir", []string{"install", "--dir=-custom"}, installOptions{directory: "-custom"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			result := executeCLIForTest(test.args, callbackFailures{})

			// Then
			if result.status != 0 || result.installCalls != 1 || result.installOptions != test.want {
				t.Fatalf("status/calls/options = %d/%d/%+v, want 0/1/%+v", result.status, result.installCalls, result.installOptions, test.want)
			}
			if result.stdout != "" || result.stderr != "" || result.tuiCalls != 0 {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}

func TestCLI_InvalidInput_returnsTwo_beforeCallbacks(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"unknown command", []string{"unknown"}},
		{"root unknown flag", []string{"--unknown"}},
		{"root malformed bool", []string{"--version=maybe"}},
		{"version extra", []string{"--version", "extra"}},
		{"short version extra", []string{"-v", "extra"}},
		{"root help extra", []string{"--help", "extra"}},
		{"install positional", []string{"install", "extra"}},
		{"install option then positional", []string{"install", "--yes", "extra"}},
		{"install missing dir", []string{"install", "--dir"}},
		{"install dir consumes long yes", []string{"install", "--dir", "--yes"}},
		{"install dir consumes short yes", []string{"install", "--dir", "-y"}},
		{"install dir consumes unknown flag", []string{"install", "--dir", "--unknown"}},
		{"install dir consumes help", []string{"install", "--dir", "--help"}},
		{"install single dash dir consumes long yes", []string{"install", "-dir", "--yes"}},
		{"install single dash dir consumes short yes", []string{"install", "-dir", "-y"}},
		{"install single dash dir consumes unknown flag", []string{"install", "-dir", "--unknown"}},
		{"install single dash dir consumes help", []string{"install", "-dir", "--help"}},
		{"install blank dir", []string{"install", "--dir="}},
		{"install separate blank dir", []string{"install", "--dir", ""}},
		{"install whitespace dir", []string{"install", "--dir=  "}},
		{"install unknown flag", []string{"install", "--unknown"}},
		{"install malformed bool", []string{"install", "--yes=maybe"}},
		{"install help extra", []string{"install", "--help", "extra"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			result := executeCLIForTest(test.args, callbackFailures{})

			// Then
			if result.status != 2 || result.stderr == "" || result.stdout != "" {
				t.Fatalf("status/stdout/stderr = %d/%q/%q", result.status, result.stdout, result.stderr)
			}
			assertNoCallbacks(t, result)
		})
	}
}

func TestCLI_CallbackFailure_returnsOne_withConciseStderr(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		failures callbackFailures
		want     string
	}{
		{"TUI", nil, callbackFailures{tui: errors.New("callback failed")}, "saki: TUI: callback failed\n"},
		{"installer", []string{"install"}, callbackFailures{install: errors.New("callback failed")}, "saki: install: callback failed\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			result := executeCLIForTest(test.args, test.failures)

			// Then
			if result.status != 1 || result.stderr != test.want || result.stdout != "" {
				t.Fatalf("status/stdout/stderr = %d/%q/%q", result.status, result.stdout, result.stderr)
			}
		})
	}
}

type errorWriter struct {
	err error
}

func (writer errorWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

type failOnceWriter struct {
	bytes.Buffer
	failed bool
}

func (writer *failOnceWriter) Write(data []byte) (int, error) {
	if !writer.failed {
		writer.failed = true
		return 0, errors.New("sink failed")
	}
	return writer.Buffer.Write(data)
}

func TestCLI_OutputFailure_returnsOne(t *testing.T) {
	t.Run("stdout", func(t *testing.T) {
		// Given
		var stderr bytes.Buffer
		runtime := commandRuntime{
			stdin: strings.NewReader(""), stdout: errorWriter{err: errors.New("sink failed")}, stderr: &stderr,
			version: "v-test", runTUI: func() error { return nil }, install: func(installOptions) error { return nil },
		}

		// When
		status := runCLI([]string{"--version"}, runtime)

		// Then
		if status != 1 || stderr.String() != "saki: write version: sink failed\n" {
			t.Fatalf("status/stderr = %d/%q", status, stderr.String())
		}
	})

	t.Run("parser stderr", func(t *testing.T) {
		// Given
		stderr := &failOnceWriter{}
		runtime := commandRuntime{
			stdin: strings.NewReader(""), stdout: io.Discard, stderr: stderr,
			version: "v-test", runTUI: func() error { return nil }, install: func(installOptions) error { return nil },
		}

		// When
		status := runCLI([]string{"--unknown"}, runtime)

		// Then
		if status != 1 || stderr.String() != "saki: write stderr: sink failed\n" {
			t.Fatalf("status/stderr = %d/%q", status, stderr.String())
		}
	})
}
