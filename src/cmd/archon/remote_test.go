package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/coordinator"
	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestRemoteStartUsesBoardRevisionAndNeverFallsBack(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/formations/boards/proof":
			w.Write([]byte(`{"success":true,"timestamp":"test","data":{"board":{"rev":9}}}`))
		case "/api/formations/runs":
			buf := new(bytes.Buffer)
			buf.ReadFrom(r.Body)
			received = buf.String()
			w.WriteHeader(202)
			w.Write([]byte(`{"success":true,"timestamp":"test","data":{"runId":"run_proof"}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	var out, stderr bytes.Buffer
	if code := runRemote(server.URL, []string{"mission", "run", "proof", "--mission", "mis_proof", "--json"}, &out, &stderr); code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	if !strings.Contains(received, `"maxAttempts":3`) || !strings.Contains(received, `"expectedRev":9`) || !strings.Contains(out.String(), "run_proof") {
		t.Fatalf("request %s output %s", received, out.String())
	}
	server.Close()
	out.Reset()
	stderr.Reset()
	if code := runRemote(server.URL, []string{"run", "status", "run_proof", "--json"}, &out, &stderr); code == 0 || out.Len() != 0 {
		t.Fatalf("unavailable coordinator fell back: code %d output %s", code, out.String())
	}
}

func TestRemoteRejectsNonloopbackAndRedirects(t *testing.T) {
	for _, url := range []string{"http://example.com", "https://127.0.0.1", "http://127.0.0.1/private"} {
		var out, err bytes.Buffer
		if code := runRemote(url, []string{"run", "list"}, &out, &err); code != 2 {
			t.Fatalf("accepted %s", url)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://example.com", 302) }))
	defer server.Close()
	var out, err bytes.Buffer
	if code := runRemote(server.URL, []string{"run", "list"}, &out, &err); code == 0 {
		t.Fatal("redirect accepted")
	}
}

func TestRemoteRunInputsReadFilesAndLongLiteralBriefs(t *testing.T) {
	cwd := t.TempDir()
	brief := strings.Repeat("A concrete task with several words. ", 40)
	file := filepath.Join(cwd, "brief.md")
	if err := os.WriteFile(file, []byte(brief), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"data":{"board":{"rev":1}}}`)
			return
		}
		var got struct {
			Cwd    string
			Brief  string
			BeadID string
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		if got.Cwd != cwd || got.Brief != brief || got.BeadID != "form-proof" {
			t.Errorf("run inputs: %+v", got)
		}
		fmt.Fprint(w, `{"data":{"runId":"run_proof"}}`)
	}))
	defer server.Close()
	for _, input := range []string{brief, file} {
		var out, stderr bytes.Buffer
		if code := runRemote(server.URL, []string{"mission", "run", "proof", "--mission", "mis_proof", "--cwd", cwd, "--brief", input, "--bead", "form-proof"}, &out, &stderr); code != 0 {
			t.Fatalf("%d %s", code, stderr.String())
		}
	}
}

func TestRemoteGateVerdictSendsResponseText(t *testing.T) {
	answer := "1. Use Postgres.\n2. Ship on Friday."
	var got []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/formations/runs/run_proof/gates/gate_review/verdict" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		got = append(got, body)
		w.WriteHeader(202)
		fmt.Fprint(w, `{"data":{"runId":"run_proof"}}`)
	}))
	defer server.Close()
	for _, command := range [][]string{
		{"gate", "approve", "run_proof", "gate_review", "--requested-seq", "7", "--response", answer, "--json"},
		{"gate", "reject", "run_proof", "gate_review", "--requested-seq", "7", "--reason", answer},
	} {
		var out, stderr bytes.Buffer
		if code := runRemote(server.URL, command, &out, &stderr); code != 0 {
			t.Fatalf("%v: %d %s", command, code, stderr.String())
		}
	}
	if len(got) != 2 || got[0]["verdict"] != "pass" || got[1]["verdict"] != "fail" {
		t.Fatalf("verdict bodies = %+v", got)
	}
	for _, body := range got {
		if body["reason"] != answer || body["requestedSeq"] != float64(7) {
			t.Fatalf("verdict body = %+v, want response text as reason", body)
		}
	}
}

// authoringSides runs the same authoring commands offline and through a real
// coordinator, so remote output can be compared with offline output.
type authoringSide struct {
	name  string
	store *formations.Store
	run   func(args ...string) (string, string, int)
}

func newAuthoringSides(t *testing.T) (offline, remote authoringSide, server *httptest.Server) {
	t.Helper()
	runner := &fakeTmux{live: map[string]bool{}}
	offlineRoot := t.TempDir()
	t.Setenv("CHROTE_AGENTS_DIR", filepath.Join(offlineRoot, "agents"))
	offline = authoringSide{name: "offline", store: formations.NewStore(offlineRoot), run: func(args ...string) (string, string, int) {
		return runArchon(t, runner, append([]string{"--workspace", offlineRoot}, args...)...)
	}}
	remoteRoot := t.TempDir()
	c, err := coordinator.Open(remoteRoot, formations.NewPersonaStore(filepath.Join(remoteRoot, "agents")), func(*formations.Store) formations.FormationExecutor {
		return formations.NewUnavailableFormationExecutor("test")
	})
	if err != nil {
		t.Fatal(err)
	}
	server = httptest.NewServer(c.Handler())
	t.Cleanup(func() { server.Close(); c.Close() })
	remote = authoringSide{name: "remote", store: formations.NewStore(remoteRoot), run: func(args ...string) (string, string, int) {
		return runArchon(t, runner, append([]string{"--server", server.URL}, args...)...)
	}}
	return offline, remote, server
}

var (
	authoringIDPattern       = regexp.MustCompile(`\b([a-z]+)_[0-9A-HJKMNP-TV-Z]{26}\b`)
	authoringVolatilePattern = regexp.MustCompile(`"(etag|updatedAt|createdAt|editedAt)": "[^"]*"`)
	authoringTimePattern     = regexp.MustCompile(`\b\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?Z\b`)
)

// normalizeAuthoring numbers IDs by first appearance and hides ETags and times.
func normalizeAuthoring(text string) string {
	ids := map[string]string{}
	text = authoringIDPattern.ReplaceAllStringFunc(text, func(id string) string {
		if _, ok := ids[id]; !ok {
			ids[id] = fmt.Sprintf("%s#%d", id[:strings.Index(id, "_")], len(ids)+1)
		}
		return ids[id]
	})
	text = authoringVolatilePattern.ReplaceAllString(text, `"$1": "-"`)
	return authoringTimePattern.ReplaceAllString(text, "-")
}

type authoringStep struct {
	args func(board *formations.BoardDocument) []string
	// noteArgs, when set, builds the step from the side's notes instead.
	noteArgs func(board *formations.BoardDocument, notes *formations.BoardNotesDocument) []string
	// errorOnly compares only the JSON error code, boundary and selector.
	errorOnly bool
	// creates names the node kind the step adds; both sides must report its ID.
	creates string
}

// assertCreatedOutput checks that a create step printed {board, layout, <kind>}
// or "created <id>" for the node it added last.
func assertCreatedOutput(t *testing.T, side string, kind string, stdout string, board *formations.BoardDocument, jsonOut bool) {
	t.Helper()
	var id string
	switch kind {
	case "mission":
		id = board.Missions[len(board.Missions)-1].ID
	case "formation":
		id = board.Formations[len(board.Formations)-1].ID
	case "gate":
		id = board.Gates[len(board.Gates)-1].ID
	}
	if !jsonOut {
		if stdout != "created "+id+"\n" {
			t.Fatalf("%s %s create stdout = %q, want created %s", side, kind, stdout, id)
		}
		return
	}
	var result map[string]json.RawMessage
	var node struct{ ID string }
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || result["board"] == nil || result["layout"] == nil || json.Unmarshal(result[kind], &node) != nil || node.ID != id {
		t.Fatalf("%s %s create JSON has no board, layout and %s %s:\n%s", side, kind, kind, id, stdout)
	}
}

func fixed(args ...string) func(*formations.BoardDocument) []string {
	return func(*formations.BoardDocument) []string { return args }
}

func formationTitled(t *testing.T, board *formations.BoardDocument, title string) formations.FormationNode {
	t.Helper()
	for _, formation := range board.Formations {
		if formation.Title == title {
			return formation
		}
	}
	t.Fatalf("no formation %q in %+v", title, board.Formations)
	return formations.FormationNode{}
}

func gateTitled(t *testing.T, board *formations.BoardDocument, title string) formations.GateNode {
	t.Helper()
	for _, gate := range board.Gates {
		if gate.Title == title {
			return gate
		}
	}
	t.Fatalf("no gate %q in %+v", title, board.Gates)
	return formations.GateNode{}
}

// authoringScript uses every remote authoring command, including failures.
func authoringScript(t *testing.T, jsonOut bool) []authoringStep {
	worker := func(board *formations.BoardDocument) formations.FormationNode {
		return formationTitled(t, board, "Worker")
	}
	with := func(build func(*formations.BoardDocument) []string) func(*formations.BoardDocument) []string {
		return func(board *formations.BoardDocument) []string {
			args := build(board)
			if jsonOut {
				args = append(args, "--json")
			}
			return args
		}
	}
	steps := []authoringStep{
		{args: with(fixed("board", "new", "demo", "--title", "Demo"))},
		{args: with(fixed("agent", "new", "scout-x", "--kind", "scout", "--harness", "openai-codex", "--capable", "research"))},
		{args: with(fixed("agent", "edit", "scout-x", "--summary", "Finds things", "--add-capability", "inspect", "--display-name", "Scout X"))},
		{args: with(fixed("mission", "create", "demo", "--title", "Work", "--goal", "Do it", "--bead", "form-demo")), creates: "mission"},
		{args: with(fixed("formation", "create", "demo", "solo", "--title", "Worker")), creates: "formation"},
		{args: with(fixed("formation", "create", "demo", "--title", "Judge")), creates: "formation"},
		{args: with(fixed("formation", "rename", "demo", "Judge", "Critic"))},
		{args: with(fixed("formation", "add-input", "demo", "Worker", "--label", "Extra"))},
		{args: with(fixed("formation", "add-output", "demo", "Worker", "--label", "Report"))},
		{args: with(func(board *formations.BoardDocument) []string {
			return []string{"formation", "assign", "demo", "Worker", "--slot", worker(board).Slots[0].ID, "--agent", "scout-x", "--harness", "openai-codex"}
		})},
		{args: with(func(board *formations.BoardDocument) []string {
			return []string{"formation", "unassign", "demo", "Worker", "--slot", worker(board).Slots[0].ID}
		})},
		{args: with(func(board *formations.BoardDocument) []string {
			return []string{"formation", "assign", "demo", "Worker", "--slot", worker(board).Slots[0].ID, "--agent", "codex-builder", "--harness", "openai-codex"}
		})},
		{args: with(fixed("formation", "set-brief", "demo", "Worker", "--goal", "Produce the result", "--bead", "form-demo", "--file", "src/a.go", "--link", "https://example.com/spec"))},
		{args: with(fixed("formation", "set-brief", "demo", "Critic", "--goal", "Judge the result"))},
		{args: with(func(board *formations.BoardDocument) []string {
			return []string{"formation", "assign", "demo", "Critic", "--slot", formationTitled(t, board, "Critic").Slots[0].ID, "--agent", "codex-judge", "--harness", "openai-codex"}
		})},
		{args: with(fixed("formation", "set-type", "demo", "Critic", "peer"))},
		{args: with(func(board *formations.BoardDocument) []string {
			return []string{"formation", "set-type", "demo", "Critic", "solo", "--keep-slot", formationTitled(t, board, "Critic").Slots[0].ID}
		})},
		{args: with(fixed("formation", "set-type", "demo", "Critic", "solo", "--keep-slot", "Nobody")), errorOnly: true},
		{args: with(fixed("gate", "create", "demo", "--kinds", "formation", "--title", "Review", "--criterion", "The result satisfies the brief")), creates: "gate"},
		{args: with(func(board *formations.BoardDocument) []string {
			return []string{"gate", "judge", "demo", "Review", "--chain", formationTitled(t, board, "Critic").ID}
		})},
		{args: with(fixed("gate", "update", "demo", "Review", "--criterion", "Good enough"))},
		{args: with(fixed("gate", "judge", "demo", "Review", "--detach"))},
		{args: with(func(board *formations.BoardDocument) []string {
			return []string{"gate", "judge", "demo", "Review", "--chain", formationTitled(t, board, "Critic").ID}
		})},
		{args: with(func(board *formations.BoardDocument) []string {
			return []string{"mission", "wire", "demo", "Work", worker(board).ID + ":" + worker(board).Inputs[0].ID}
		})},
		{args: with(func(board *formations.BoardDocument) []string {
			return []string{"formation", "wire", "demo", worker(board).ID + ":" + worker(board).Outputs[0].ID, gateTitled(t, board, "Review").ID + ":in"}
		})},
		{args: with(func(board *formations.BoardDocument) []string {
			return []string{"formation", "unwire", "demo", worker(board).ID + ":" + worker(board).Outputs[0].ID, gateTitled(t, board, "Review").ID + ":in"}
		})},
		{args: with(func(board *formations.BoardDocument) []string {
			return []string{"formation", "wire", "demo", worker(board).ID + ":" + worker(board).Outputs[0].ID, gateTitled(t, board, "Review").ID + ":in"}
		})},
		{args: with(fixed("gate", "create", "demo", "--kinds", "human", "--title", "Signoff", "--criterion", "Operator signs off")), creates: "gate"},
		{args: with(fixed("gate", "update", "demo", "Signoff", "--title", "Sign-off", "--kinds", "human,code", "--check", "output_contains", "--check-version", "1", "--check-value", "done"))},
		{args: with(fixed("gate", "update", "demo", "Sign-off", "--clear-check", "--kinds", "human"))},
		{args: with(fixed("gate", "create", "demo", "--title", "Default")), creates: "gate"},
		{args: with(fixed("mission", "update", "demo", "Work", "--goal", "Do it well"))},
		{args: with(fixed("board", "note", "demo", "--text", "Board intent"))},
		{args: with(func(board *formations.BoardDocument) []string {
			return []string{"board", "note", "demo", "--node", worker(board).ID, "--text", "Worker intent"}
		})},
		{noteArgs: func(board *formations.BoardDocument, notes *formations.BoardNotesDocument) []string {
			return with(fixed("board", "note", "demo", "--node", worker(board).ID, "--text", "Reply"))(board)
		}},
		{noteArgs: func(board *formations.BoardDocument, notes *formations.BoardNotesDocument) []string {
			entries := workerNotes(notes, worker(board).ID)
			return with(fixed("board", "note", "demo", "--node", worker(board).ID, "--entry", entries[len(entries)-1].ID, "--text", "Edited reply"))(board)
		}},
		{noteArgs: func(board *formations.BoardDocument, notes *formations.BoardNotesDocument) []string {
			return with(fixed("board", "note", "demo", "--node", worker(board).ID, "--entry", workerNotes(notes, worker(board).ID)[0].ID, "--text", "Not mine", "--author", "human:ui"))(board)
		}, errorOnly: true},
		{noteArgs: func(board *formations.BoardDocument, notes *formations.BoardNotesDocument) []string {
			return with(fixed("board", "note", "demo", "--clear", "--entry", notes.Board[0].ID))(board)
		}},
		{args: with(fixed("board", "note", "demo", "--text", "Board intent"))},
		{args: with(fixed("board", "notes", "demo"))},
		{args: with(fixed("board", "arrange", "demo"))},
		{args: with(fixed("board", "validate", "demo"))},
		{args: with(fixed("board", "list"))},
		{args: with(fixed("board", "inspect", "demo"))},
		{args: with(fixed("formation", "list"))},
		{args: with(fixed("formation", "inspect", "demo"))},
		{args: with(fixed("mission", "list", "demo"))},
		{args: with(fixed("mission", "inspect", "demo", "Work"))},
		{args: with(fixed("agent", "list"))},
		{args: with(fixed("agent", "list", "--capable", "research", "--assignable"))},
		{args: with(fixed("agent", "inspect", "scout-x"))},
		{args: with(fixed("formation", "rename", "demo", "Nobody", "Ghost")), errorOnly: true},
		{args: with(fixed("gate", "update", "demo", "Nobody", "--title", "Ghost")), errorOnly: true},
		{args: with(fixed("mission", "wire", "demo", "Nobody", "x:y")), errorOnly: true},
		{args: with(fixed("board", "new", "demo")), errorOnly: true},
		{args: with(fixed("mission", "create", "demo", "--bead", "Home-123")), errorOnly: true},
		{args: with(fixed("formation", "set-brief", "demo", "Worker", "--bead", "chlab/123")), errorOnly: true},
		{args: with(fixed("gate", "create", "demo", "--command", "make test")), errorOnly: true},
		{args: with(fixed("gate", "create", "demo", "--check", "output_contains", "--check-version", "1", "--check-value", "done")), errorOnly: true},
		{args: with(fixed("formation", "create", "missing-board")), errorOnly: true},
		{args: with(fixed("board", "inspect", "missing-board")), errorOnly: true},
		{args: with(fixed("mission", "list", "missing-board")), errorOnly: true},
		{args: with(fixed("mission", "inspect", "demo", "Nobody")), errorOnly: true},
		{args: with(fixed("agent", "inspect", "nobody-here")), errorOnly: true},
	}
	return steps
}

func workerNotes(notes *formations.BoardNotesDocument, nodeID string) []formations.NoteEntry {
	for _, element := range notes.Elements {
		if element.NodeID == nodeID {
			return element.Entries
		}
	}
	return []formations.NoteEntry{{ID: "nte_missing"}}
}

func errorIdentity(t *testing.T, stderr string) string {
	t.Helper()
	var response archonErrorResponse
	if err := json.Unmarshal([]byte(stderr), &response); err != nil {
		return "text error"
	}
	return response.Code + "|" + response.Boundary + "|" + response.Selector
}

func TestRemoteAuthoringMatchesOfflineCommands(t *testing.T) {
	for _, jsonOut := range []bool{true, false} {
		t.Run(fmt.Sprintf("json=%t", jsonOut), func(t *testing.T) {
			offline, remote, _ := newAuthoringSides(t)
			for index, step := range authoringScript(t, jsonOut) {
				results := map[string][3]string{}
				var args []string
				for _, side := range []authoringSide{offline, remote} {
					board, _ := side.store.ReadBoard("demo")
					if step.noteArgs != nil {
						notes, _ := side.store.ReadBoardNotes("demo")
						if notes == nil {
							notes = &formations.BoardNotesDocument{}
						}
						args = step.noteArgs(board, notes)
					} else {
						args = step.args(board)
					}
					stdout, stderr, code := side.run(args...)
					if step.creates != "" && code == 0 {
						after, err := side.store.ReadBoard("demo")
						if err != nil {
							t.Fatalf("step %d %v read %s board: %v", index, args, side.name, err)
						}
						assertCreatedOutput(t, side.name, step.creates, stdout, after, jsonOut)
					}
					results[side.name] = [3]string{normalizeAuthoring(stdout), stderr, strconv.Itoa(code)}
				}
				off, rem := results["offline"], results["remote"]
				if off[2] != rem[2] {
					t.Fatalf("step %d %v exit offline %s remote %s\noffline stderr: %s\nremote stderr: %s", index, args, off[2], rem[2], off[1], rem[1])
				}
				if step.errorOnly {
					if off[2] == "0" {
						t.Fatalf("step %d %v unexpectedly succeeded", index, args)
					}
					if jsonOut && errorIdentity(t, off[1]) != errorIdentity(t, rem[1]) {
						t.Fatalf("step %d %v error offline %s remote %s", index, args, off[1], rem[1])
					}
					continue
				}
				if off[2] != "0" && !(args[0] == "board" && args[1] == "validate") {
					t.Fatalf("step %d %v failed on both sides: %s", index, args, off[1])
				}
				if off[0] != rem[0] {
					t.Fatalf("step %d %v output differs\noffline:\n%s\nremote:\n%s", index, args, off[0], rem[0])
				}
			}
			board, err := remote.store.ReadBoard("demo")
			// Mission to Worker, Worker to Review, and the Critic judge loop.
			if err != nil || len(board.Missions) != 1 || len(board.Formations) != 2 || len(board.Gates) != 3 || len(board.Connections) != 4 || board.UpdatedBy != "agent:archon" {
				t.Fatalf("remote board: %v missions %d formations %d gates %d connections %d by %s", err, len(board.Missions), len(board.Formations), len(board.Gates), len(board.Connections), board.UpdatedBy)
			}
			if gate := gateTitled(t, board, "Default"); strings.Join(gate.Kinds, ",") != "human" {
				t.Fatalf("remote gate created without --kinds = %+v, want kinds [human]", gate)
			}
			if notes, err := remote.store.ReadBoardNotes("demo"); err != nil || len(notes.Board) != 1 || notes.Board[0].Text != "Board intent" || len(notes.Elements) != 1 || len(notes.Elements[0].Entries) != 2 || notes.Elements[0].Entries[1].Text != "Edited reply" {
				t.Fatalf("remote notes = %+v, %v", notes, err)
			}
			if card, err := formations.NewPersonaStore(filepath.Join(filepath.Dir(remote.store.BoardPath("demo")), "..", "..", "agents")).ReadPersona("scout-x"); err != nil || card.DisplayName != "Scout X" || card.Summary != "Finds things" {
				t.Fatalf("remote agent card = %+v, %v", card, err)
			}
		})
	}
}

func flagNames(help string) []string {
	names := regexp.MustCompile(`(?m)^\s+-([a-z][a-z-]*)`).FindAllStringSubmatch(help, -1)
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name[1])
	}
	return result
}

func TestRemoteAuthoringCoversEveryCommandWithOfflineFlags(t *testing.T) {
	offline, remote, _ := newAuthoringSides(t)
	scripted := map[string]bool{}
	for _, step := range authoringScript(t, false) {
		board := &formations.BoardDocument{Formations: []formations.FormationNode{
			{ID: "fmn_x", Title: "Worker", Inputs: []formations.FormationPort{{ID: "port_in"}}, Outputs: []formations.FormationPort{{ID: "port_out"}}, Slots: []formations.FormationSlot{{ID: "slot_x"}}},
			{ID: "fmn_y", Title: "Critic", Slots: []formations.FormationSlot{{ID: "slot_y"}}},
		}, Gates: []formations.GateNode{{ID: "gate_x", Title: "Review"}}}
		var args []string
		if step.noteArgs != nil {
			args = step.noteArgs(board, &formations.BoardNotesDocument{Board: []formations.NoteEntry{{ID: "nte_x"}}})
		} else {
			args = step.args(board)
		}
		scripted[args[0]+" "+args[1]] = true
	}
	for command := range remoteAuthoringCommands {
		if !scripted[command] {
			t.Errorf("remote %q has no step in authoringScript", command)
		}
		words := strings.Fields(command)
		_, offlineHelp, _ := offline.run(words[0], words[1], "-h")
		_, remoteHelp, _ := remote.run(words[0], words[1], "-h")
		if got, want := flagNames(remoteHelp), flagNames(offlineHelp); len(want) == 0 || strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s flags: remote %v, offline %v", command, got, want)
		}
	}
}

func TestRemoteAuthoringRetriesAWriteRaceThenGivesUp(t *testing.T) {
	_, remote, daemon := newAuthoringSides(t)
	if _, stderr, code := remote.run("board", "new", "race"); code != 0 {
		t.Fatal(stderr)
	}
	target, err := url.Parse(daemon.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	for _, losses := range []int{remoteWriteAttempts - 1, remoteWriteAttempts} {
		patches := 0
		// The first writes lose to another editor, as if the cockpit wrote first.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPatch {
				patches++
				if patches <= losses {
					w.WriteHeader(http.StatusConflict)
					w.Write([]byte(`{"success":false,"error":{"code":"CONFLICT","message":"Formation definition changed; reload and retry"}}`))
					return
				}
			}
			proxy.ServeHTTP(w, r)
		}))
		var out, stderr bytes.Buffer
		code := runRemote(server.URL, []string{"formation", "create", "race", "--title", fmt.Sprintf("Worker %d", losses)}, &out, &stderr)
		server.Close()
		if losses < remoteWriteAttempts {
			if code != 0 || patches != losses+1 || !strings.HasPrefix(out.String(), "created fmn_") {
				t.Fatalf("losses %d: code %d patches %d out %q err %q", losses, code, patches, out.String(), stderr.String())
			}
		} else if code == 0 || patches != remoteWriteAttempts || !strings.Contains(stderr.String(), "coordinator HTTP 409") {
			t.Fatalf("losses %d: code %d patches %d err %q", losses, code, patches, stderr.String())
		}
	}
	board, err := remote.store.ReadBoard("race")
	if err != nil || len(board.Formations) != 1 {
		t.Fatalf("race board %+v, %v; want exactly one formation from the retried write", board, err)
	}
}

func TestRemoteFieldErrorsKeepOfflineCodes(t *testing.T) {
	for code, sentinel := range map[string]error{
		"INVALID_BEAD_ID":         formations.ErrInvalidBeadID,
		"INVALID_CONTROLLER_ROLE": formations.ErrInvalidControllerRole,
		"INVALID_PORT_DIRECTION":  formations.ErrInvalidPortDirection,
		"INVALID_AGENT_CARD":      formations.ErrInvalidAgentCard,
	} {
		offline := archonErrorCode(fmt.Errorf("%w: field", sentinel))
		remote := archonErrorCode(&remoteHTTPError{Status: 400, Code: code, Message: "field"})
		if offline != strings.ToLower(code) || remote != offline {
			t.Errorf("%s: offline code %q, remote code %q", code, offline, remote)
		}
	}
}
