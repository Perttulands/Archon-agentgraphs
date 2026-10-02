package main

import (
	"strings"
	"testing"
)

// Every noun and command explains itself (archon-n7u.33): archon -h lists the
// nouns, each noun's -h and help list its commands, and each command's -h
// prints its usage and flags, offline and with --server. Nothing answers help
// with an unknown command.
func TestEveryNounAndCommandHasHelp(t *testing.T) {
	workspace := t.TempDir()
	runner := &fakeTmux{live: map[string]bool{}}
	for _, args := range [][]string{{"-h"}, {"--help"}, {"help"}, {}} {
		_, stderr, _ := runArchon(t, runner, args...)
		for _, help := range nounHelps {
			if !strings.Contains(stderr, "  "+help.noun+" ") {
				t.Fatalf("archon %v lacks the %s noun:\n%s", args, help.noun, stderr)
			}
		}
	}
	for _, help := range nounHelps {
		for _, args := range [][]string{{help.noun}, {help.noun, "-h"}, {help.noun, "--help"}, {help.noun, "help"}} {
			for _, global := range [][]string{{"--workspace", workspace}, {"--server", "http://127.0.0.1:9"}, {}} {
				_, stderr, code := runArchon(t, runner, append(append([]string{}, global...), args...)...)
				if code != 2 || strings.Contains(stderr, "unknown") || !strings.HasPrefix(stderr, "usage: archon "+help.noun+" <command>") {
					t.Fatalf("archon %v %v = %d:\n%s", global, args, code, stderr)
				}
				for _, command := range help.commands {
					if !strings.Contains(stderr, "  "+command.verb) {
						t.Fatalf("archon %v %v lacks %s:\n%s", global, args, command.verb, stderr)
					}
				}
			}
		}
		for _, command := range help.commands {
			name := help.noun + " " + command.verb
			for _, global := range [][]string{{"--workspace", workspace}, {"--server", "http://127.0.0.1:9"}} {
				args := append(append(append([]string{}, global...), strings.Fields(name)...), "-h")
				_, stderr, code := runArchon(t, runner, args...)
				// agent new and edit print their own usage (agent_flags.go).
				if code != 2 || strings.Contains(stderr, "unknown") || !strings.Contains(stderr, "usage: archon "+name+" ") || !strings.Contains(stderr, upperFirst(command.summary)) {
					t.Fatalf("archon %v = %d:\n%s", args, code, stderr)
				}
				if global[0] == "--server" && strings.Contains(command.args, "--json") && !strings.Contains(stderr, "-json") {
					t.Fatalf("archon %v lists no flags:\n%s", args, stderr)
				}
			}
		}
	}
}

// mission run's usage lists every flag it takes and says which are required.
func TestMissionRunUsageListsEveryFlagAndTheRequiredOnes(t *testing.T) {
	_, stderr, _ := runArchon(t, &fakeTmux{live: map[string]bool{}}, "--workspace", t.TempDir(), "mission", "run", "-h")
	usage, flags, _ := strings.Cut(stderr, "\nflags:\n")
	if !strings.Contains(usage, "Required: <mission>, and an --input or --input-file for each input the mission requires") {
		t.Fatalf("mission run usage does not say what is required:\n%s", usage)
	}
	for _, line := range strings.Split(flags, "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "-"); ok {
			name, _, _ = strings.Cut(name, " ")
			if !strings.Contains(usage, "--"+name) {
				t.Fatalf("mission run usage lacks --%s:\n%s", name, usage)
			}
		}
	}
}

// A noun's unknown command is named and its commands listed; a command that
// needs the daemon says so offline, and one that works offline says so with
// --server.
func TestUnknownAndUnavailableCommandsSayWhatToDo(t *testing.T) {
	runner := &fakeTmux{live: map[string]bool{}}
	workspace := t.TempDir()
	if _, stderr, code := runArchon(t, runner, "--workspace", workspace, "formation", "frobnicate"); code != 2 || !strings.HasPrefix(stderr, `unknown formation command "frobnicate"`) || !strings.Contains(stderr, "  set-brief <mission> <formation>") {
		t.Fatalf("unknown formation command = %d:\n%s", code, stderr)
	}
	if _, stderr, code := runArchon(t, runner, "--workspace", workspace, "run", "seats", "run_x"); code != 2 || !strings.HasPrefix(stderr, "archon run seats needs --server <url>: it reads the daemon\nusage: archon run seats <runId>") {
		t.Fatalf("offline run seats = %d %q", code, stderr)
	}
	if _, stderr, code := runArchon(t, runner, "--server", "http://127.0.0.1:9", "agent", "spawn", "scout"); code != 2 || stderr != "archon agent spawn works offline only: run it with --workspace <state-dir>\n" {
		t.Fatalf("remote agent spawn = %d %q", code, stderr)
	}
}
