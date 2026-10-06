package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

const missionReadFixture = `schema = 1
id = "brd_handover"
slug = "handover"
title = "Make a useful map"
rev = 7

[[inputCard]]
id = "inp_start"
title = "Map the supplied subject"
goal = "Produce a map that explains the choices"
inputs = [{ name = "subject", kind = "text", required = true, description = "What to map and why" }]

[[formation]]
id = "fmn_map"
type = "solo"
title = "Map"
[formation.brief]
goal = "Explain {subject}, keeping uncertain branches visible"
files = ["/reference/spec.md"]
[[formation.input]]
id = "map_in"
label = "Subject"
[[formation.output]]
id = "map_out"
label = "Map"
[[formation.slot]]
id = "mapper"
label = "Mapper"
agentId = "scout"
harness = "openai-codex"
effort = "low"
controller = true

[[gate]]
id = "gate_review"
title = "Review"
kinds = ["human"]
criterion = "The map is useful"

[[end]]
id = "end_done"
title = "Ready"
outcome = "done"

[[formation]]
id = "fmn_draft"
type = "solo"
title = "Unfinished draft"

[[connection]]
id = "edge_start"
from = "inp_start:out"
to = "fmn_map:map_in"
[[connection]]
id = "edge_review"
from = "fmn_map:map_out"
to = "gate_review:in"
[[connection]]
id = "edge_pass"
from = "gate_review:pass"
to = "end_done:in"
[[connection]]
id = "edge_back"
from = "gate_review:fail"
to = "fmn_map:map_in"
[[connection]]
id = "edge_dangling"
from = "fmn_draft:out"
to = "missing:in"
`

const missionReadNotes = `schema = 2
missionId = "brd_handover"
rev = 3
updatedAt = "2026-10-01T12:00:00Z"

[[entry]]
id = "nte_intent"
target = "mission"
author = "human:operator"
createdAt = "2026-10-01T12:00:00Z"
text = "Show uncertainty without inventing my choice"
[[entry]]
id = "nte_question"
target = "fmn_map"
author = "agent:manager"
createdAt = "2026-10-01T12:01:00Z"
text = "QUESTION: Which branch matters?"
[[entry]]
id = "nte_partial"
target = "fmn_map"
author = "human:operator"
createdAt = "2026-10-01T12:02:00Z"
editedAt = "2026-10-01T12:03:00Z"
text = "Keep both visible; this is not a settled pick"
[[entry]]
id = "nte_orphan"
target = "deleted_node"
author = "human:operator"
createdAt = "2026-10-01T12:04:00Z"
text = "Unicode α and code:\n` + "```" + `go\nkeep := true\n` + "```" + `\n  trailing space  "
`

func missionReadWorkspace(t *testing.T) (*formations.Store, *formations.BoardDocument, *formations.BoardNotesDocument) {
	t.Helper()
	t.Setenv("ARCHON_AGENTS_DIR", filepath.Join(t.TempDir(), "agents"))
	store := formations.NewStore(t.TempDir())
	writeArchonFile(t, store.BoardPath("handover"), missionReadFixture)
	writeArchonFile(t, store.NotesPath("handover"), missionReadNotes)
	board, err := store.ReadBoard("handover")
	if err != nil {
		t.Fatal(err)
	}
	notes, err := store.ReadBoardNotes("handover")
	if err != nil {
		t.Fatal(err)
	}
	return store, board, notes
}

func TestMissionInspectTextJoinsMissionAndIntent(t *testing.T) {
	store, board, notes := missionReadWorkspace(t)
	before := board.ETag
	out, stderr, code := runArchon(t, &fakeTmux{}, "--workspace", store.Workspace, "mission", "inspect", "handover")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect draft: code=%d stderr=%s", code, stderr)
	}
	for _, want := range []string{board.ID, "mission rev7/notes rev3", "What to map and why", "Explain {subject}, keeping uncertain branches visible", "/reference/spec.md", "mapper", "openai-codex", "low", "edge_back", "gate_review:fail", "edge_dangling", "missing:in", "Unfinished draft", "deleted_node"} {
		if !strings.Contains(out, want) {
			t.Errorf("hand-over omits %q", want)
		}
	}
	for _, entries := range append([][]formations.NoteEntry{notes.Board}, notes.Elements[0].Entries, notes.Elements[1].Entries) {
		for _, entry := range entries {
			if !strings.Contains(out, entry.ID) || !strings.Contains(out, entry.Author) || !strings.Contains(out, entry.Text) {
				t.Errorf("hand-over loses exact attributed note %+v", entry)
			}
		}
	}
	report := formations.ValidateRunAdmission(board, formations.NewPersonaStore(formations.AgentsDir(store.Workspace)), formations.RunAdmissionScope{})
	for _, finding := range append(report.Errors, report.Warnings...) {
		if !strings.Contains(out, finding.Code) || !strings.Contains(out, finding.Message) {
			t.Errorf("hand-over omits actual finding %+v", finding)
		}
	}
	if strings.Contains(out, "answered by") || strings.Contains(out, "answer to") {
		t.Fatal("inspect infers answer state from attributed prose")
	}
	after, _ := store.ReadBoard("handover")
	afterNotes, _ := store.ReadBoardNotes("handover")
	if after.ETag != before || afterNotes.ETag != notes.ETag {
		t.Fatal("inspection changed mission or notes")
	}
}

func TestMissionInspectTextRemoteMatchesOfflineAndRejectsMismatchedReads(t *testing.T) {
	store, board, notes := missionReadWorkspace(t)
	report := formations.ValidateRunAdmission(board, formations.NewPersonaStore(formations.AgentsDir(store.Workspace)), formations.RunAdmissionScope{})
	want, _, _ := runArchon(t, &fakeTmux{}, "--workspace", store.Workspace, "mission", "inspect", "handover")
	for _, problem := range []string{"", "notes-fetch", "notes-parse", "notes-identity", "validation-fetch", "validation-revision", "validation-etag"} {
		t.Run(problem, func(t *testing.T) {
			calls := map[string]int{}
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls[r.URL.Path]++
				mu.Unlock()
				data := map[string]any{}
				switch r.URL.Path {
				case "/api/missions/handover":
					data["mission"] = board
				case "/api/missions/handover/notes":
					if problem == "notes-fetch" {
						w.WriteHeader(500)
						return
					}
					if problem == "notes-parse" {
						w.Write([]byte(`{"data":{"notes":`))
						return
					}
					copy := *notes
					if problem == "notes-identity" {
						copy.BoardID = "brd_other"
					}
					data["notes"] = copy
				case "/api/missions/handover/validation":
					if problem == "validation-fetch" {
						w.WriteHeader(500)
						return
					}
					data["errors"], data["warnings"] = report.Errors, report.Warnings
					data["missionRev"], data["missionEtag"] = board.Rev, board.ETag
					if problem == "validation-revision" {
						data["missionRev"] = board.Rev + 1
					}
					if problem == "validation-etag" {
						data["missionEtag"] = "other"
					}
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"data": data})
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := runRemote(server.URL, []string{"mission", "inspect", "handover"}, &out, &stderr)
			if problem == "" {
				if code != 0 || stderr.Len() != 0 || out.String() != want {
					t.Fatalf("remote joined read: code=%d stderr=%s output differs=%v", code, &stderr, out.String() != want)
				}
				mu.Lock()
				defer mu.Unlock()
				for _, path := range []string{"/api/missions/handover", "/api/missions/handover/notes", "/api/missions/handover/validation"} {
					if calls[path] != 1 {
						t.Errorf("%s read %d times, want one", path, calls[path])
					}
				}
			} else if code == 0 || out.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("partial/mismatched read %s: code=%d stdout=%s stderr=%s", problem, code, &out, &stderr)
			}
		})
	}
}

func TestMissionInspectTextKeepsReadableDraftsAndLongThreads(t *testing.T) {
	store, _, _ := missionReadWorkspace(t)
	if err := os.Remove(store.NotesPath("handover")); err != nil {
		t.Fatal(err)
	}
	// Several Input cards and an implicit run input stay inspectable. No notes
	// is a real state, not an excuse to fall back to the old summary.
	raw := strings.Replace(missionReadFixture, `inputs = [{ name = "subject", kind = "text", required = true, description = "What to map and why" }]`, "", 1)
	raw += `
[[inputCard]]
id = "inp_second"
title = "Second unfinished entrance"
goal = "Still deciding"
[[formation]]
id = "fmn_judge"
type = "solo"
title = "Judge"
[[formation.input]]
id = "judge_in"
label = "Work"
[[formation.output]]
id = "judge_out"
label = "Verdict"
[[connection]]
id = "edge_judge"
from = "gate_review:judge"
to = "fmn_judge:judge_in"
[[connection]]
id = "edge_judge_return"
from = "fmn_judge:judge_out"
to = "gate_review:judge"
`
	writeArchonFile(t, store.BoardPath("handover"), raw)
	out, stderr, code := runArchon(t, &fakeTmux{}, "--workspace", store.Workspace, "mission", "inspect", "handover")
	for _, want := range []string{"(no mission notes)", "run input: brief", "inp_second", "Still deciding", "edge_judge", "edge_judge_return", "several_input_cards", "missing:in"} {
		if code != 0 || stderr != "" || !strings.Contains(out, want) {
			t.Errorf("readable draft missing %q: code=%d stderr=%s", want, code, stderr)
		}
	}
	if strings.Count(out, "Formation: Judge [fmn_judge]") != 1 {
		t.Fatal("judge loop duplicates its node")
	}
	var longNotes strings.Builder
	longNotes.WriteString("schema = 2\nmissionId = \"brd_handover\"\nrev = 4\nupdatedAt = \"2026-10-01T12:00:00Z\"\n")
	words := strings.Repeat("Uncertain α branch stays visible. ", 50)
	for i := 0; i < 20; i++ {
		raw += fmt.Sprintf("\n[[formation]]\nid = \"fmn_long_%d\"\ntype = \"solo\"\ntitle = \"Long node %d\"\n", i, i)
		fmt.Fprintf(&longNotes, "\n[[entry]]\nid = \"nte_long_%d\"\ntarget = \"fmn_long_%d\"\nauthor = \"human:operator\"\ncreatedAt = \"2026-10-01T12:00:00Z\"\ntext = %s\n", i, i, strconv.Quote(words+fmt.Sprintf("EXACT END %d  ", i)))
	}
	writeArchonFile(t, store.BoardPath("handover"), raw)
	writeArchonFile(t, store.NotesPath("handover"), longNotes.String())
	out, stderr, code = runArchon(t, &fakeTmux{}, "--workspace", store.Workspace, "mission", "inspect", "handover")
	if code != 0 || stderr != "" || len(out) < 8192 {
		t.Fatalf("large read: code=%d bytes=%d stderr=%s", code, len(out), stderr)
	}
	for i := 0; i < 20; i++ {
		if !strings.Contains(out, words+fmt.Sprintf("EXACT END %d  ", i)) || strings.Count(out, fmt.Sprintf("Formation: Long node %d [fmn_long_%d]", i, i)) != 1 {
			t.Errorf("large read truncated/repeated node or note %d", i)
		}
	}
}

func TestMissionInspectJSONStillReadsOnlyTheGraph(t *testing.T) {
	store, board, _ := missionReadWorkspace(t)
	want, stderr, code := runArchon(t, &fakeTmux{}, "--workspace", store.Workspace, "mission", "inspect", "handover", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("offline JSON: %d %s", code, stderr)
	}
	var decoded formations.BoardDocument
	if err := json.Unmarshal([]byte(want), &decoded); err != nil || decoded.ETag != board.ETag || decoded.TOML != "" {
		t.Fatalf("changed JSON contract: %v %+v", err, decoded)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/missions/handover" {
			t.Errorf("JSON inspection fetches extra context: %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"mission": board}})
	}))
	defer server.Close()
	var out, errors bytes.Buffer
	if code := runRemote(server.URL, []string{"mission", "inspect", "handover", "--json"}, &out, &errors); code != 0 || errors.Len() != 0 || out.String() != want {
		t.Fatalf("remote JSON changed: code=%d stderr=%s", code, &errors)
	}
}

func TestMissionInspectTextKeepsToolAndLimitConfiguration(t *testing.T) {
	store, _, _ := missionReadWorkspace(t)
	raw := missionReadFixture + `
[[tool]]
id = "tool_normalize"
title = "Normalize report"
profileId = "json.normalize"
profileVersion = "1"
[tool.params]
mode = "strict"
[[tool.input]]
id = "port_tool_in"
name = "report"
label = "Raw report"
direction = "input"
kind = "work"
acceptedMediaTypes = ["application/json"]
required = true
role = "data"
[[tool.output]]
id = "port_tool_out"
name = "normalized"
label = "Normalized report"
direction = "output"
kind = "work"
acceptedMediaTypes = ["application/json"]
[[limit]]
id = "lim_map"
title = "Drafting allowance"
target = "fmn_map"
rounds = 3
seconds = 600
warnSeconds = 30
tokens = 5000
`
	writeArchonFile(t, store.BoardPath("handover"), raw)
	out, stderr, code := runArchon(t, &fakeTmux{}, "--workspace", store.Workspace, "mission", "inspect", "handover")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect Tool/Limit: code=%d stderr=%s", code, stderr)
	}
	for _, want := range []string{"Tool: Normalize report [tool_normalize]", "profileId: json.normalize", "profileVersion: 1", `params: {"mode":"strict"}`, "Raw report", "port_tool_in", `"name":"report"`, `"direction":"input"`, `"kind":"work"`, `"acceptedMediaTypes":["application/json"]`, `"required":true`, `"role":"data"`, "Normalized report", "port_tool_out", `"name":"normalized"`, `"direction":"output"`, "Limit card: Drafting allowance [lim_map]", "target: fmn_map", "rounds: 3", "seconds: 600", "warnSeconds: 30", "tokens: 5000"} {
		if !strings.Contains(out, want) {
			t.Errorf("hand-over omits configured %q", want)
		}
	}
}

func TestMissionInspectTextRejectsCorruptOfflineNotes(t *testing.T) {
	store, _, _ := missionReadWorkspace(t)
	writeArchonFile(t, store.NotesPath("handover"), "schema = 2\n[[entry]\n")
	out, stderr, code := runArchon(t, &fakeTmux{}, "--workspace", store.Workspace, "mission", "inspect", "handover")
	if code == 0 || out != "" || stderr == "" {
		t.Fatalf("corrupt notes emitted a partial read: code=%d stdout=%s stderr=%s", code, out, stderr)
	}
}
