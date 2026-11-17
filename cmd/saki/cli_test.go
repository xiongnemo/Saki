package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestCLIProcess_unknownCommandExitsTwo(t *testing.T) {
	binary := buildCLIBinary(t)
	command := exec.Command(binary, "unknown")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	if err == nil || command.ProcessState.ExitCode() != 2 {
		t.Fatalf("unknown command error=%v exit=%d stdout=%q stderr=%q", err, command.ProcessState.ExitCode(), stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "unexpected argument") {
		t.Fatalf("unknown command output stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLIProcess_installAndInstalledVersion(t *testing.T) {
	binary := buildCLIBinary(t)
	directory := t.TempDir()
	command := exec.Command(binary, "install", "--dir", directory, "--yes")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("install command: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	leaf := "saki"
	if runtime.GOOS == "windows" {
		leaf = "saki.exe"
	}
	installed := filepath.Join(directory, leaf)
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("installed target %q: %v", installed, err)
	}
	sourceVersion, err := exec.Command(binary, "--version").Output()
	if err != nil {
		t.Fatalf("source --version: %v", err)
	}
	versionCommand := exec.Command(installed, "--version")
	versionOutput, err := versionCommand.Output()
	if err != nil {
		t.Fatalf("installed --version: %v", err)
	}
	if got, want := string(versionOutput), string(sourceVersion); got != want {
		t.Fatalf("installed version = %q, want source version %q", got, want)
	}
}

func buildCLIBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "saki-cli-test")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	command := exec.Command("go", "build", "-o", binary, ".")
	command.Env = append(os.Environ(), "GOPROXY=https://goproxy.cn,direct")
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output.String())
	}
	return binary
}

// sandboxEnv is the process environment with the home directory, the XDG
// variables, APPDATA and LOCALAPPDATA pointed at home, so that a Saki started
// by a test never sees the real user's files.
func sandboxEnv(home string) []string {
	overrides := map[string]string{
		"HOME":            home,
		"USERPROFILE":     home,
		"APPDATA":         filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA":    filepath.Join(home, "AppData", "Local"),
		"XDG_CONFIG_HOME": "",
		"XDG_DATA_HOME":   "",
		"XDG_STATE_HOME":  "",
		"XDG_CACHE_HOME":  "",
		"XDG_RUNTIME_DIR": "",
	}
	var env []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		overridden := false
		for key := range overrides {
			if strings.EqualFold(name, key) {
				overridden = true
				break
			}
		}
		if !overridden {
			env = append(env, entry)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

func TestCLIProcess_migrateMovesLegacyConfigInSandboxedHome(t *testing.T) {
	binary := buildCLIBinary(t)
	home := t.TempDir()
	legacy := filepath.Join(home, ".saki", "config.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"account":{"username":"nemo"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, string, error) {
		command := exec.Command(binary, args...)
		command.Env = sandboxEnv(home)
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		err := command.Run()
		return stdout.String(), stderr.String(), err
	}
	moved := filepath.Join(home, ".config", "saki", "config.json")

	stdout, stderr, err := run("migrate", "--dry-run")
	if err != nil || !strings.Contains(stdout, legacy+" -> "+moved) || stderr != "" {
		t.Fatalf("dry run: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("dry run moved the config: %v", err)
	}

	if stdout, stderr, err = run("migrate"); err != nil {
		t.Fatalf("migrate: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("config not moved: %v\n%s", err, stdout)
	}
	if stdout, _, err = run("migrate"); err != nil || !strings.Contains(stdout, "Nothing to migrate.") {
		t.Fatalf("second migrate: %v\n%s", err, stdout)
	}
}

type callbackFailures struct {
	tui     error
	install error
	migrate error
}

type cliTestResult struct {
	status         int
	stdout         string
	stderr         string
	tuiCalls       int
	installCalls   int
	installOptions installOptions
	migrateCalls   int
	migrateOptions migrateOptions
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
		migrate: func(options migrateOptions, _ io.Writer) error {
			result.migrateCalls++
			result.migrateOptions = options
			return failures.migrate
		},
	}

	result.status = runCLI(args, runtime)
	result.stdout = stdout.String()
	result.stderr = stderr.String()
	return result
}

func assertNoCallbacks(t *testing.T, result cliTestResult) {
	t.Helper()
	if result.tuiCalls != 0 || result.installCalls != 0 || result.migrateCalls != 0 {
		t.Fatalf("callback calls = tui:%d install:%d migrate:%d, want all zero", result.tuiCalls, result.installCalls, result.migrateCalls)
	}
}

func TestCLI_Migrate_passesParsedOptions(t *testing.T) {
	for _, test := range []struct {
		args []string
		want migrateOptions
	}{
		{[]string{"migrate"}, migrateOptions{}},
		{[]string{"migrate", "--dry-run"}, migrateOptions{dryRun: true}},
		{[]string{"migrate", "-dry-run"}, migrateOptions{dryRun: true}},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			result := executeCLIForTest(test.args, callbackFailures{})
			if result.status != 0 || result.migrateCalls != 1 || result.migrateOptions != test.want {
				t.Fatalf("status/calls/options = %d/%d/%+v, want 0/1/%+v", result.status, result.migrateCalls, result.migrateOptions, test.want)
			}
			if result.stderr != "" || result.tuiCalls != 0 || result.installCalls != 0 {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
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
		{"migrate long", []string{"migrate", "--help"}, "Usage: saki migrate"},
		{"migrate short", []string{"migrate", "-h"}, "Usage: saki migrate"},
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
		{"migrate positional", []string{"migrate", "extra"}},
		{"migrate unknown flag", []string{"migrate", "--force"}},
		{"migrate malformed bool", []string{"migrate", "--dry-run=maybe"}},
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
		{"migrate", []string{"migrate"}, callbackFailures{migrate: errors.New("callback failed")}, "saki: migrate: callback failed\n"},
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
