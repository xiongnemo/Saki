package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

const (
	rootHelp = `Usage: saki [--version|-v]
       saki install [--dir PATH] [--yes|-y]

Commands:
  install  install the running Saki executable

Options:
  -h, --help     show help
  -v, --version  print version and exit
`
	installHelp = `Usage: saki install [--dir PATH] [--yes|-y]

Options:
      --dir PATH  install directory (default "~/.local/bin")
  -h, --help      show help
  -y, --yes       accept installation prompts
`
)

type installOptions struct {
	directory string
	yes       bool
}

type commandRuntime struct {
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
	version string
	runTUI  func() error
	install func(installOptions) error
}

type checkedWriter struct {
	writer io.Writer
	err    error
}

func (writer *checkedWriter) Write(data []byte) (int, error) {
	if writer.err != nil {
		return len(data), nil
	}
	written, err := writer.writer.Write(data)
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		writer.err = err
	}
	return written, err
}

func runCLI(args []string, runtime commandRuntime) int {
	if len(args) > 0 && args[0] == "install" {
		return runInstallCLI(args[1:], runtime)
	}

	flags := flag.NewFlagSet("saki", flag.ContinueOnError)
	parserOutput := &checkedWriter{writer: runtime.stderr}
	flags.SetOutput(parserOutput)
	var showVersion bool
	var showHelp bool
	flags.BoolVar(&showVersion, "version", false, "print version and exit")
	flags.BoolVar(&showVersion, "v", false, "print version and exit")
	flags.BoolVar(&showHelp, "help", false, "show help")
	flags.BoolVar(&showHelp, "h", false, "show help")
	if err := flags.Parse(args); err != nil {
		if parserOutput.err != nil {
			return reportFailure(runtime.stderr, "write stderr", parserOutput.err)
		}
		return 2
	}
	if flags.NArg() != 0 {
		return reportInvalid(runtime.stderr, fmt.Sprintf("unexpected argument %q", flags.Arg(0)))
	}
	if showHelp {
		if err := writeText(runtime.stdout, rootHelp); err != nil {
			return reportFailure(runtime.stderr, "write help", err)
		}
		return 0
	}
	if showVersion {
		if err := writeText(runtime.stdout, runtime.version+"\n"); err != nil {
			return reportFailure(runtime.stderr, "write version", err)
		}
		return 0
	}
	if err := runtime.runTUI(); err != nil {
		return reportFailure(runtime.stderr, "TUI", err)
	}
	return 0
}

func runInstallCLI(args []string, runtime commandRuntime) int {
	for index, argument := range args {
		separatedDirectory := argument == "--dir" || argument == "-dir"
		if separatedDirectory && (index+1 == len(args) || strings.HasPrefix(args[index+1], "-")) {
			return reportInvalid(runtime.stderr, "--dir requires a path value")
		}
	}

	flags := flag.NewFlagSet("saki install", flag.ContinueOnError)
	parserOutput := &checkedWriter{writer: runtime.stderr}
	flags.SetOutput(parserOutput)
	options := installOptions{directory: "~/.local/bin"}
	var showHelp bool
	flags.StringVar(&options.directory, "dir", options.directory, "install directory")
	flags.BoolVar(&options.yes, "yes", false, "accept installation prompts")
	flags.BoolVar(&options.yes, "y", false, "accept installation prompts")
	flags.BoolVar(&showHelp, "help", false, "show help")
	flags.BoolVar(&showHelp, "h", false, "show help")
	if err := flags.Parse(args); err != nil {
		if parserOutput.err != nil {
			return reportFailure(runtime.stderr, "write stderr", parserOutput.err)
		}
		return 2
	}
	if flags.NArg() != 0 {
		return reportInvalid(runtime.stderr, fmt.Sprintf("unexpected install argument %q", flags.Arg(0)))
	}
	if strings.TrimSpace(options.directory) == "" {
		return reportInvalid(runtime.stderr, "install directory must not be blank")
	}
	if showHelp {
		if err := writeText(runtime.stdout, installHelp); err != nil {
			return reportFailure(runtime.stderr, "write help", err)
		}
		return 0
	}
	if err := runtime.install(options); err != nil {
		return reportFailure(runtime.stderr, "install", err)
	}
	return 0
}

func reportInvalid(stderr io.Writer, message string) int {
	if err := writeText(stderr, "saki: "+message+"\n"); err != nil {
		return reportFailure(stderr, "write stderr", err)
	}
	return 2
}

func reportFailure(stderr io.Writer, operation string, err error) int {
	if writeErr := writeText(stderr, fmt.Sprintf("saki: %s: %v\n", operation, err)); writeErr != nil {
		return 1
	}
	return 1
}

func writeText(writer io.Writer, text string) error {
	written, err := io.WriteString(writer, text)
	if err != nil {
		return err
	}
	if written != len(text) {
		return io.ErrShortWrite
	}
	return nil
}
