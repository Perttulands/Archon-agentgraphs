package main

import "time"

// testPatience bounds how long a test waits for something it expects to
// happen. Tests wait on the event itself, so the bound only decides how long a
// failure takes to report, and it is generous so a loaded host never fails a
// passing test (archon-miz).
const testPatience = time.Minute
