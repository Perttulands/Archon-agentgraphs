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
	mission, err := store.CreateMission("refs", MissionCreateRequest{Title: "Work", Files: []string{" docs/brief.md ", "", "/srv/project/plan.md"}}, current())
	if err != nil {
		t.Fatal(err)
	}
	gate, err := store.CreateGate("refs", GateCreateRequest{Title: "Review", Files: []string{"rubrics/quality.md"}}, current())
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
		compat, err := parseBoardCompatibility([]byte(readFile(t, store.BoardPath("refs"))))
		if err != nil {
			t.Fatal(err)
		}
		compatMission, _ := findMission(compat, mission.Mission.ID)
		compatGate, _ := findGate(compat.Gates, gate.Gate.ID)
		if !equalStrings(compatMission.Files, missionFiles) || !equalStrings(compatGate.Files, gateFiles) {
			t.Fatalf("%s: compatibility parse lost file references: %q %q", label, compatMission.Files, compatGate.Files)
		}
	}
	check("created", []string{"docs/brief.md", "/srv/project/plan.md"}, []string{"rubrics/quality.md"})

	replaced := []string{"docs/next.md"}
	if _, err := store.UpdateMission("refs", MissionUpdateRequest{MissionID: mission.Mission.ID, Files: &replaced}, current()); err != nil {
		t.Fatal(err)
	}
	gateFiles := []string{"rubrics/quality.md", "rubrics/style.md"}
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
