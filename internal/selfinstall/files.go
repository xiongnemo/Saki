package selfinstall

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

// Install validates the current executable, previews the operation, and then
// installs it at the canonical target name. All prompts and status output use
// the streams supplied through Runtime.
func (installer *Installer) Install() error {
	paths, err := installer.Resolve()
	if err != nil {
		return err
	}
	report := installer.pathReport(paths)
	if err := installer.writeStatus("Source: %s\nTarget: %s\n", paths.Source, paths.Target); err != nil {
		return err
	}
	if err := installer.writeStatus("PATH: %s\n", report.Summary()); err != nil {
		return err
	}
	reader := bufio.NewReader(installer.runtime.Stdin)

	directoryExists, err := installer.pathExists(paths.Directory)
	if err != nil {
		return fmt.Errorf("inspect install directory %q: %w", paths.Directory, err)
	}
	if !directoryExists {
		if !installer.options.AssumeYes {
			ok, err := installer.confirm(reader, fmt.Sprintf("Create install directory %q?", paths.Directory))
			if err != nil {
				return err
			}
			if !ok {
				return installer.cancelled()
			}
		}
		if err := installer.runtime.MkdirAll(paths.Directory, 0o755); err != nil {
			return fmt.Errorf("create install directory %q: %w", paths.Directory, err)
		}
	}

	if err := installer.validateDirectory(paths.Directory); err != nil {
		return err
	}
	if err := installer.validateTarget(paths.Target); err != nil {
		return err
	}
	targetExists, err := installer.pathExists(paths.Target)
	if err != nil {
		return fmt.Errorf("inspect install target %q: %w", paths.Target, err)
	}
	if targetExists {
		same, err := installer.samePath(paths.Source, paths.Target)
		if err != nil {
			return fmt.Errorf("compare source and install target: %w", err)
		}
		if same {
			if err := installer.repairExecutableMode(paths.Target, paths.SourceMode); err != nil {
				return err
			}
			return installer.writeStatus("Already installed at %s.\n", paths.Target)
		}

		identical, err := installer.sameContent(paths.Source, paths.Target)
		if err != nil {
			return fmt.Errorf("compare source and install target contents: %w", err)
		}
		if identical {
			if err := installer.repairExecutableMode(paths.Target, paths.SourceMode); err != nil {
				return err
			}
			return installer.writeStatus("Already up to date at %s.\n", paths.Target)
		}

		if !installer.options.AssumeYes {
			ok, err := installer.confirm(reader, fmt.Sprintf("Replace existing install target %q?", paths.Target))
			if err != nil {
				return err
			}
			if !ok {
				return installer.cancelled()
			}
		}
	}

	return installer.replace(paths, targetExists)
}

func (installer *Installer) replace(paths Paths, targetExists bool) error {
	stage, err := installer.copyToStage(paths)
	if err != nil {
		return err
	}
	backup := ""
	cleanupStage := func() error {
		if stage == "" {
			return nil
		}
		if err := installer.runtime.Remove(stage); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove temporary install %q: %w", stage, err)
		}
		stage = ""
		return nil
	}

	if targetExists {
		backup, err = installer.uniquePath(paths.Directory, ".saki-backup")
		if err != nil {
			_ = cleanupStage()
			return err
		}
		if err := installer.runtime.Rename(paths.Target, backup); err != nil {
			cleanupErr := cleanupStage()
			if cleanupErr != nil {
				return errors.Join(fmt.Errorf("backup install target %q: %w", paths.Target, err), cleanupErr)
			}
			return fmt.Errorf("backup install target %q: %w", paths.Target, err)
		}
	}

	if err := installer.runtime.Rename(stage, paths.Target); err != nil {
		activationErr := fmt.Errorf("activate staged install %q: %w", paths.Target, err)
		cleanupErr := cleanupStage()
		if backup == "" {
			if cleanupErr != nil {
				return errors.Join(activationErr, cleanupErr)
			}
			return activationErr
		}
		restoreErr := installer.runtime.Rename(backup, paths.Target)
		if restoreErr != nil {
			if cleanupErr != nil {
				return errors.Join(activationErr, fmt.Errorf("restore backup %q: %w", backup, restoreErr), cleanupErr)
			}
			return errors.Join(activationErr, fmt.Errorf("restore backup %q: %w", backup, restoreErr), fmt.Errorf("backup preserved at %q", backup))
		}
		if cleanupErr != nil {
			return errors.Join(activationErr, cleanupErr)
		}
		return activationErr
	}
	stage = ""

	if backup != "" {
		if err := installer.runtime.Remove(backup); err != nil {
			return fmt.Errorf("remove backup %q after successful install: %w (installed target remains active; backup preserved)", backup, err)
		}
	}
	return installer.writeStatus("Installed Saki at %s.\n", paths.Target)
}

func (installer *Installer) confirm(reader *bufio.Reader, question string) (bool, error) {
	for {
		if _, err := fmt.Fprintf(installer.runtime.Stderr, "%s [y/N]: ", question); err != nil {
			return false, fmt.Errorf("write install prompt: %w", err)
		}
		line, readErr := reader.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		switch answer {
		case "y", "yes":
			return true, nil
		case "", "n", "no":
			return false, nil
		default:
			if _, err := fmt.Fprintln(installer.runtime.Stderr, "Please answer yes or no."); err != nil {
				return false, fmt.Errorf("write install prompt guidance: %w", err)
			}
			if errors.Is(readErr, io.EOF) {
				return false, nil
			}
			if readErr != nil {
				return false, fmt.Errorf("read install prompt: %w", readErr)
			}
		}
	}
}

func (installer *Installer) cancelled() error {
	if err := installer.writeStatus("Installation cancelled.\n"); err != nil {
		return err
	}
	return nil
}

func (installer *Installer) writeStatus(format string, args ...any) error {
	if _, err := fmt.Fprintf(installer.runtime.Stdout, format, args...); err != nil {
		return fmt.Errorf("write install status: %w", err)
	}
	return nil
}
