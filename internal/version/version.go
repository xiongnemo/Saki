package version

import (
	"runtime/debug"
	"strings"
)

var (
	BaseVersion = "v0.0.1"
	BranchName  = ""
	CommitHash  = ""
	Dirty       = ""
)

func String() string {
	base := strings.TrimSpace(BaseVersion)
	if base == "" {
		base = "v0.0.0"
	}

	branch := sanitizePart(BranchName)
	if branch == "" {
		branch = "unknown"
	}

	commit := shortCommit(CommitHash)
	dirty := parseDirty(Dirty)
	if commit == "" || Dirty == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					if commit == "" {
						commit = shortCommit(setting.Value)
					}
				case "vcs.modified":
					if Dirty == "" {
						dirty = setting.Value == "true"
					}
				}
			}
		}
	}
	if commit == "" {
		commit = "unknown"
	}

	value := base + "-" + branch + "-" + commit
	if dirty {
		value += "-dirty"
	}
	return value
}

func sanitizePart(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		valid := r == '.' || r == '_' || r == '-' ||
			(r >= '0' && r <= '9') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z')
		if valid {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if builder.Len() > 0 && !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func shortCommit(value string) string {
	value = sanitizePart(value)
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func parseDirty(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "dirty":
		return true
	default:
		return false
	}
}
