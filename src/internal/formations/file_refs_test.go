package formations

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func writeFileRef(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A referenced file opens wherever its absolute path points, as in CHROTE:
// no roots, and symlinks and hard links are followed. Secrets in text are
// redacted as in run evidence.
func TestReferencedFilesOpenAnyAbsolutePath(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "project")
	elsewhere := filepath.Join(base, "elsewhere")
	writeFileRef(t, filepath.Join(project, "docs", "rubric.md"), "# Rubric\n\napi_key = hunter2\n")
	writeFileRef(t, filepath.Join(project, "logo.png"), "\x89PNG\r\n")
	writeFileRef(t, filepath.Join(project, "design.pdf"), "%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	writeFileRef(t, filepath.Join(elsewhere, "notes.md"), "elsewhere\n")
	if err := os.Symlink(filepath.Join(elsewhere, "notes.md"), filepath.Join(project, "docs", "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(project, "jump")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(elsewhere, "notes.md"), filepath.Join(project, "hardlink.md")); err != nil {
		t.Fatal(err)
	}

	preview, err := PreviewReferencedFile(filepath.Join(project, "docs", "rubric.md"))
	if err != nil {
		t.Fatalf("preview rubric: %v", err)
	}
	if preview.Path != filepath.Join(project, "docs", "rubric.md") || preview.Name != "rubric.md" || preview.Kind != "markdown" || preview.Text == nil ||
		!strings.Contains(preview.Text.Text, "# Rubric") || strings.Contains(preview.Text.Text, "hunter2") {
		t.Fatalf("rubric preview = %+v", preview)
	}
	for _, ref := range []string{
		filepath.Join(elsewhere, "notes.md"),
		filepath.Join(project, "docs", "link.md"),
		filepath.Join(project, "jump", "notes.md"),
		filepath.Join(project, "hardlink.md"),
		project + "/../elsewhere/notes.md",
	} {
		if got, err := PreviewReferencedFile(ref); err != nil || got.Text == nil || got.Text.Text != "elsewhere\n" {
			t.Errorf("preview %q = %+v, %v", ref, got, err)
		}
		if got, err := ReadReferencedFile(ref); err != nil || string(got.Body) != "elsewhere\n" {
			t.Errorf("read %q = %+v, %v", ref, got, err)
		}
	}
	if image, err := ReadReferencedFile(filepath.Join(project, "logo.png")); err != nil || image.ContentType != "image/png" {
		t.Fatalf("image read = %+v, %v", image, err)
	}
	if pdf, err := PreviewReferencedFile(filepath.Join(project, "design.pdf")); err != nil || pdf.Kind != "pdf" || pdf.Text != nil {
		t.Fatalf("pdf preview = %+v, %v", pdf, err)
	}
	if raw, err := ReadReferencedFile(filepath.Join(project, "docs", "rubric.md")); err != nil || raw.ContentType != "text/plain; charset=utf-8" || strings.Contains(string(raw.Body), "hunter2") {
		t.Fatalf("raw rubric = %+v, %v", raw, err)
	}

	fifo := filepath.Join(project, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for ref, want := range map[string]error{
		"docs/rubric.md":                         ErrRelativeFileRef,
		filepath.Join(project, "docs"):           ErrNotFound,
		fifo:                                     ErrNotFound,
		filepath.Join(project, "missing.md"):     os.ErrNotExist,
		"":                                       ErrNotFound,
		filepath.Join(project, "docs") + "\x00x": ErrNotFound,
	} {
		if _, err := PreviewReferencedFile(ref); !errors.Is(err, want) {
			t.Errorf("preview %q error = %v, want %v", ref, err, want)
		}
		if _, err := ReadReferencedFile(ref); !errors.Is(err, want) {
			t.Errorf("read %q error = %v, want %v", ref, err, want)
		}
	}

	large := filepath.Join(project, "large.log")
	file, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(EvidenceArtifactRawMaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := ReadReferencedFile(large); !errors.Is(err, ErrEvidenceTooLarge) {
		t.Fatalf("raw read past the cap = %v, want ErrEvidenceTooLarge", err)
	}
	long := filepath.Join(project, "long.md")
	writeFileRef(t, long, strings.Repeat("A line of the rubric.\n", EvidenceArtifactPreviewMaxBytes/10))
	if preview, err := PreviewReferencedFile(long); err != nil || preview.Text == nil || !preview.Text.Truncated || len(preview.Text.Text) > EvidenceArtifactPreviewMaxBytes {
		t.Fatalf("preview past the cap = %v, want a truncated preview", err)
	}
}

// A relative reference file has no base, so authoring refuses it where it is
// written and saves nothing (archon-ka59).
func TestAuthoringRefusesRelativeFileReferences(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	if _, err := store.CreateBoard(BoardCreateRequest{Slug: "refs", Title: "Refs"}); err != nil {
		t.Fatal(err)
	}
	current := func() WriteOptions {
		t.Helper()
		board, err := store.ReadBoard("refs")
		if err != nil {
			t.Fatal(err)
		}
		return WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}
	}
	mission, err := store.CreateMission("refs", MissionCreateRequest{Title: "Work"}, current())
	if err != nil {
		t.Fatal(err)
	}
	gate, err := store.CreateGate("refs", GateCreateRequest{Title: "Review"}, current())
	if err != nil {
		t.Fatal(err)
	}
	formation, err := store.CreateFormation("refs", FormationCreateRequest{Type: FormationTypeSolo, Title: "Draft"}, current())
	if err != nil {
		t.Fatal(err)
	}
	relative := []string{"/work/plan.md", "docs/rubric.md"}
	before := readFile(t, store.BoardPath("refs"))
	writes := map[string]func() error{
		"mission create": func() error {
			_, err := store.CreateMission("refs", MissionCreateRequest{Title: "Second", Files: relative}, current())
			return err
		},
		"mission update": func() error {
			_, err := store.UpdateMission("refs", MissionUpdateRequest{MissionID: mission.Mission.ID, Files: &relative}, current())
			return err
		},
		"gate create": func() error {
			_, err := store.CreateGate("refs", GateCreateRequest{Title: "Other", Files: relative}, current())
			return err
		},
		"gate update": func() error {
			_, err := store.UpdateGate("refs", GateUpdateRequest{GateID: gate.Gate.ID, Files: &relative}, current())
			return err
		},
		"formation brief": func() error {
			_, err := store.SetFormationBrief("refs", FormationBriefRequest{FormationID: formation.Formation.ID, Goal: "Draft it", Files: relative}, current())
			return err
		},
	}
	for name, write := range writes {
		err := write()
		if !errors.Is(err, ErrRelativeFileRef) || !strings.Contains(err.Error(), `file "docs/rubric.md" is relative: use an absolute path`) {
			t.Fatalf("%s with a relative file = %v, want the refusal", name, err)
		}
		if got := readFile(t, store.BoardPath("refs")); got != before {
			t.Fatalf("%s saved a refused write:\n%s", name, got)
		}
	}
}

func TestMissionAndGateFileReferencesRoundTrip(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	if _, err := store.CreateBoard(BoardCreateRequest{Slug: "refs", Title: "Refs"}); err != nil {
		t.Fatal(err)
	}
	current := func() WriteOptions {
		t.Helper()
		board, err := store.ReadBoard("refs")
		if err != nil {
			t.Fatal(err)
		}
		return WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}
	}
	mission, err := store.CreateMission("refs", MissionCreateRequest{Title: "Work", Files: []string{" /work/docs/brief.md ", "", "/srv/project/plan.md"}}, current())
	if err != nil {
		t.Fatal(err)
	}
	gate, err := store.CreateGate("refs", GateCreateRequest{Title: "Review", Files: []string{"/work/rubrics/quality.md"}}, current())
	if err != nil {
		t.Fatal(err)
	}
	bare, err := store.CreateGate("refs", GateCreateRequest{Title: "Plain"}, current())
	if err != nil {
		t.Fatal(err)
	}
	raw := readFile(t, store.BoardPath("refs"))
	if strings.Count(raw, "files = ") != 2 {
		t.Fatalf("board keeps a files key only where references exist:\n%s", raw)
	}
	check := func(label string, missionFiles, gateFiles []string) {
		t.Helper()
		board, err := store.ReadBoard("refs")
		if err != nil {
			t.Fatal(err)
		}
		gotMission, _ := findMission(board, mission.Mission.ID)
		gotGate, _ := findGate(board.Gates, gate.Gate.ID)
		gotBare, _ := findGate(board.Gates, bare.Gate.ID)
		if !equalStrings(gotMission.Files, missionFiles) || !equalStrings(gotGate.Files, gateFiles) || len(gotBare.Files) != 0 {
			t.Fatalf("%s: mission files %q, gate files %q, plain gate files %q; want %q and %q", label, gotMission.Files, gotGate.Files, gotBare.Files, missionFiles, gateFiles)
		}
	}
	check("created", []string{"/work/docs/brief.md", "/srv/project/plan.md"}, []string{"/work/rubrics/quality.md"})

	replaced := []string{"/work/docs/next.md"}
	if _, err := store.UpdateMission("refs", MissionUpdateRequest{MissionID: mission.Mission.ID, Files: &replaced}, current()); err != nil {
		t.Fatal(err)
	}
	gateFiles := []string{"/work/rubrics/quality.md", "/work/rubrics/style.md"}
	if _, err := store.UpdateGate("refs", GateUpdateRequest{GateID: gate.Gate.ID, Files: &gateFiles}, current()); err != nil {
		t.Fatal(err)
	}
	check("replaced", replaced, gateFiles)

	title := "Work again"
	if _, err := store.UpdateMission("refs", MissionUpdateRequest{MissionID: mission.Mission.ID, Title: &title}, current()); err != nil {
		t.Fatal(err)
	}
	check("unrelated update keeps files", replaced, gateFiles)

	cleared := []string{""}
	if _, err := store.UpdateMission("refs", MissionUpdateRequest{MissionID: mission.Mission.ID, Files: &cleared}, current()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateGate("refs", GateUpdateRequest{GateID: gate.Gate.ID, Files: &[]string{}}, current()); err != nil {
		t.Fatal(err)
	}
	check("cleared", nil, nil)
	if strings.Contains(readFile(t, store.BoardPath("refs")), "files = ") {
		t.Fatalf("cleared references left a files key:\n%s", readFile(t, store.BoardPath("refs")))
	}
}

// Validation warns about each reference file that names nothing Archon can
// open, with its path, so the cockpit can flag that file's chip
// (archon-n7u.26). An existing file raises nothing.
func TestValidationFlagsMissingAndRelativeFileReferences(t *testing.T) {
	root := t.TempDir()
	present := filepath.Join(root, "brief.md")
	writeFixture(t, present, "# Brief\n")
	missing := filepath.Join(root, "rubric.md")
	store := NewStore(filepath.Join(root, "state"))
	writeFixture(t, store.BoardPath("refs"), `schema = 1
id = "brd_refs"
slug = "refs"
title = "Refs"
rev = 1

[[inputCard]]
id = "inp_refs"
title = "Deliver"
files = ["`+present+`"]

[[formation]]
id = "fmn_draft"
type = "solo"
title = "Draft"
[formation.brief]
goal = "Draft it"
files = ["notes/plan.md"]

[[gate]]
id = "gte_review"
title = "Review"
kinds = ["human"]
files = ["`+missing+`"]
`)
	board, err := store.ReadBoard("refs")
	if err != nil {
		t.Fatal(err)
	}
	var got []BoardFinding
	for _, finding := range ValidateBoard(board).Warnings {
		if finding.Path != "" {
			got = append(got, finding)
		}
	}
	want := []BoardFinding{
		{Code: FindingRelativeFile, NodeID: "fmn_draft", Path: "notes/plan.md", Message: "Draft's file notes/plan.md is relative: use an absolute path"},
		{Code: FindingMissingFile, NodeID: "gte_review", Path: missing, Message: "Review's file " + missing + " does not exist"},
	}
	if len(got) != len(want) {
		t.Fatalf("file findings = %+v, want %+v", got, want)
	}
	for _, finding := range want {
		found := false
		for _, candidate := range got {
			found = found || candidate == finding
		}
		if !found {
			t.Fatalf("file findings = %+v, want %+v among them", got, finding)
		}
	}
}
