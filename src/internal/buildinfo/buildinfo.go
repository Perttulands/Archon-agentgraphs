package buildinfo

import "fmt"

// Release builds set these values with Go linker flags.
var Version = "dev"
var Commit = "unknown"

func String() string { return fmt.Sprintf("Archon %s (%s)", Version, Commit) }
