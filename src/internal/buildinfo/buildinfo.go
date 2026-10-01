package buildinfo

import (
	"fmt"
	"runtime/debug"
)

// Release builds set these values with Go linker flags.
var Version = "dev"
var Commit = "unknown"

// Current is the running build's version and commit. A build without the
// linker flags takes the commit Go recorded from version control, when it
// recorded one.
func Current() (version, commit string) {
	commit = Commit
	if commit == "unknown" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" && setting.Value != "" {
					commit = setting.Value
				}
			}
		}
	}
	return Version, commit
}

func String() string {
	version, commit := Current()
	return fmt.Sprintf("Archon %s (%s)", version, commit)
}
