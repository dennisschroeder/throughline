package cli

import (
	"fmt"
	"runtime/debug"
)

// version, commit, and date are populated at release build time via
// -ldflags "-X github.com/dennisschroeder/throughline/internal/cli.version=..." (and .commit, .date).
// They stay empty for `go build`/`go run` and other non-release builds.
var version, commit, date string

// versionInfo resolves the version, commit, and date to report. Injected
// values (set via -ldflags) are returned verbatim; anything left empty
// falls back to information from the module's build info.
func versionInfo() (resolvedVersion, resolvedCommit, resolvedDate string) {
	resolvedVersion, resolvedCommit, resolvedDate = version, commit, date
	if resolvedVersion != "" && resolvedCommit != "" && resolvedDate != "" {
		return resolvedVersion, resolvedCommit, resolvedDate
	}

	bi, ok := debug.ReadBuildInfo()
	if resolvedVersion == "" {
		resolvedVersion = "devel"
		if ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			resolvedVersion = bi.Main.Version
		}
	}
	if ok && (resolvedCommit == "" || resolvedDate == "") {
		for _, setting := range bi.Settings {
			switch setting.Key {
			case "vcs.revision":
				if resolvedCommit == "" {
					resolvedCommit = setting.Value
					if len(resolvedCommit) > 12 {
						resolvedCommit = resolvedCommit[:12]
					}
				}
			case "vcs.time":
				if resolvedDate == "" {
					resolvedDate = setting.Value
				}
			}
		}
	}
	if resolvedCommit == "" {
		resolvedCommit = "unknown"
	}
	if resolvedDate == "" {
		resolvedDate = "unknown"
	}
	return resolvedVersion, resolvedCommit, resolvedDate
}

// versionLine renders the one-line human-readable version report shared by
// both the `version` command and the `--version` flag.
func versionLine() string {
	resolvedVersion, resolvedCommit, resolvedDate := versionInfo()
	return fmt.Sprintf("throughline version %s (commit %s, built %s)", resolvedVersion, resolvedCommit, resolvedDate)
}
