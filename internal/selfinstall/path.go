package selfinstall

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PathReport describes whether the installed command would be reachable from
// the current process environment. It is advisory; the installer never edits
// PATH, shell profiles, or registry settings.
type PathReport struct {
	DirectoryOnPath bool
	Reachable       bool
	ShadowedBy      string
	CommandPath     string
	Hint            string
}

func (report PathReport) Summary() string {
	if report.ShadowedBy != "" {
		return fmt.Sprintf("target directory is on PATH, but saki is shadowed by %q", report.ShadowedBy)
	}
	if report.Reachable {
		return "target directory is on PATH"
	}
	if report.Hint != "" {
		return "target directory is not on PATH; " + report.Hint
	}
	return "target directory is not on PATH"
}

func (installer *Installer) pathReport(paths Paths) PathReport {
	entries := installer.pathEntries()
	targetIndex := -1
	for index, entry := range entries {
		if installer.pathsEqual(entry, paths.Directory) {
			targetIndex = index
			break
		}
	}
	report := PathReport{DirectoryOnPath: targetIndex >= 0, Hint: installer.pathHint(paths.Directory)}
	if targetIndex < 0 {
		return report
	}
	for index := 0; index < targetIndex; index++ {
		if candidate, ok := installer.executableInPathEntry(entries[index]); ok {
			report.ShadowedBy = candidate
			return report
		}
	}
	report.Reachable = true
	report.CommandPath = paths.Target
	return report
}

func (installer *Installer) pathEntries() []string {
	separator := installer.runtime.PathSeparator
	if separator == "" {
		separator = ":"
		if installer.runtime.GOOS == "windows" {
			separator = ";"
		}
	}
	raw := installer.runtime.Getenv("PATH")
	parts := strings.Split(raw, separator)
	entries := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) >= 2 && ((part[0] == '"' && part[len(part)-1] == '"') || (part[0] == '\'' && part[len(part)-1] == '\'')) {
			part = part[1 : len(part)-1]
		}
		if part == "" {
			continue
		}
		entries = append(entries, part)
	}
	return entries
}

func (installer *Installer) pathsEqual(left, right string) bool {
	leftPath, err := installer.normalizePath(left)
	if err != nil {
		return false
	}
	rightPath, err := installer.normalizePath(right)
	if err != nil {
		return false
	}
	if installer.runtime.GOOS == "windows" {
		return strings.EqualFold(leftPath, rightPath)
	}
	return leftPath == rightPath
}

func (installer *Installer) executableInPathEntry(directory string) (string, bool) {
	for _, name := range installer.commandNames() {
		candidate := filepath.Join(directory, name)
		info, err := installer.runtime.Lstat(candidate)
		if err != nil || info == nil || !info.Mode().IsRegular() {
			continue
		}
		if installer.runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			continue
		}
		return candidate, true
	}
	return "", false
}

func (installer *Installer) commandNames() []string {
	if installer.runtime.GOOS != "windows" {
		return []string{"saki"}
	}
	pathext := installer.runtime.Getenv("PATHEXT")
	if strings.TrimSpace(pathext) == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	names := make([]string, 0, 1+len(strings.Split(pathext, ";")))
	seen := make(map[string]struct{})
	for _, extension := range strings.Split(pathext, ";") {
		extension = strings.TrimSpace(extension)
		if extension == "" {
			continue
		}
		if !strings.HasPrefix(extension, ".") {
			extension = "." + extension
		}
		name := "saki" + extension
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return []string{"saki.exe"}
	}
	return names
}

func (installer *Installer) pathHint(directory string) string {
	if installer.runtime.GOOS == "windows" {
		if strings.TrimSpace(installer.runtime.Getenv("PSModulePath")) != "" {
			return fmt.Sprintf("PowerShell hint: $env:Path += %s (advisory only)", quotePowerShell(directory))
		}
		return fmt.Sprintf("shell hint: set PATH=%%PATH%%;%s (advisory only)", quoteWindows(directory))
	}
	shell := strings.ToLower(filepath.Base(strings.TrimSpace(installer.runtime.Getenv("SHELL"))))
	if shell == "fish" {
		return fmt.Sprintf("fish hint: set -gx PATH %s $PATH (advisory only)", quotePOSIX(directory))
	}
	if shell == "zsh" || shell == "bash" || shell == "sh" || shell == "dash" || shell == "ksh" {
		return fmt.Sprintf("shell hint: export PATH=%s:$PATH (advisory only)", quotePOSIX(directory))
	}
	return fmt.Sprintf("shell hint: export PATH=%s:$PATH (advisory only)", quotePOSIX(directory))
}

func quotePOSIX(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func quotePowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func quoteWindows(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
