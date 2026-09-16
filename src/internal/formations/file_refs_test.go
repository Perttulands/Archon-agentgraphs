package formations

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestFileRootsServeReferencesOnlyUnderTheirRoots(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "project")
	notes := filepath.Join(base, "notes")
	outside := filepath.Join(base, "outside")
	writeFileRef(t, filepath.Join(project, "docs", "rubric.md"), "# Rubric\n\napi_key = hunter2\n")
	writeFileRef(t, filepath.Join(project, "logo.png"), "\x89PNG\r\n")
	writeFileRef(t, filepath.Join(project, "design.pdf"), "%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	writeFileRef(t, filepath.Join(notes, "context.txt"), "notes root\n")
	writeFileRef(t, filepath.Join(outside, "secret.md"), "outside\n")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(project, "docs", "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(outside, "secret.md"), filepath.Join(project, "hardlink.md")); err != nil {
		t.Fatal(err)
	}
	roots, err := NewFileRoots([]string{project, notes})
	if err != nil {
		t.Fatal(err)
	}

	preview, err := roots.Preview(filepath.Join(project, "docs", "rubric.md"))
	if err != nil {
		t.Fatalf("preview rubric: %v", err)
	}
	if preview.Path != filepath.Join(project, "docs", "rubric.md") || preview.Name != "rubric.md" || preview.Kind != "markdown" || preview.Text == nil ||
		!strings.Contains(preview.Text.Text, "# Rubric") || strings.Contains(preview.Text.Text, "hunter2") {
		t.Fatalf("rubric preview = %+v", preview)
	}
	if relative, err := roots.Preview("context.txt"); err != nil || relative.Path != filepath.Join(notes, "context.txt") || relative.Text.Text != "notes root\n" {
		t.Fatalf("relative reference under the second root = %+v, %v", relative, err)
	}
	if image, err := roots.Read(filepath.Join(project, "logo.png")); err != nil || image.ContentType != "image/png" {
		t.Fatalf("image read = %+v, %v", image, err)
	}
	if pdf, err := roots.Preview("design.pdf"); err != nil || pdf.Kind != "pdf" || pdf.Text != nil {
		t.Fatalf("pdf preview = %+v, %v", pdf, err)
	}
	if pdf, err := roots.Read("design.pdf"); err != nil || pdf.ContentType != "application/pdf" || string(pdf.Body) != "%PDF-1.7\n%\xe2\xe3\xcf\xd3\n" {
		t.Fatalf("pdf read = %+v, %v", pdf, err)
	}
	if raw, err := roots.Read("docs/rubric.md"); err != nil || raw.ContentType != "text/plain; charset=utf-8" || strings.Contains(string(raw.Body), "hunter2") {
		t.Fatalf("raw rubric = %+v, %v", raw, err)
	}

	for ref, want := range map[string]error{
		filepath.Join(outside, "secret.md"):           ErrFileOutsideRoots,
		"/etc/passwd":                                 ErrFileOutsideRoots,
		project + "/../outside/secret.md":             ErrNotFound,
		"../outside/secret.md":                        ErrNotFound,
		"docs/../../outside/secret.md":                ErrNotFound,
		filepath.Join(project, "docs", "link.md"):     ErrNotFound,
		filepath.Join(project, "escape", "secret.md"): ErrNotFound,
		filepath.Join(project, "hardlink.md"):         ErrNotFound,
		filepath.Join(project, "docs"):                ErrNotFound,
		project:                                       ErrNotFound,
		"missing.md":                                  ErrNotFound,
		"":                                            ErrNotFound,
	} {
		if _, err := roots.Preview(ref); !errors.Is(err, want) {
			t.Errorf("preview %q error = %v, want %v", ref, err, want)
		}
		if _, err := roots.Read(ref); !errors.Is(err, want) {
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
	if _, err := roots.Read(large); !errors.Is(err, ErrEvidenceTooLarge) {
		t.Fatalf("raw read past the cap = %v, want ErrEvidenceTooLarge", err)
	}
	long := filepath.Join(project, "long.md")
	writeFileRef(t, long, strings.Repeat("A line of the rubric.\n", EvidenceArtifactPreviewMaxBytes/10))
	if preview, err := roots.Preview(long); err != nil || preview.Text == nil || !preview.Text.Truncated || len(preview.Text.Text) > EvidenceArtifactPreviewMaxBytes {
		t.Fatalf("preview past the cap = %v, want a truncated preview", err)
	}

	none, err := NewFileRoots(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := none.Preview("docs/rubric.md"); !errors.Is(err, ErrFileOutsideRoots) {
		t.Fatalf("relative reference with no roots = %v, want ErrFileOutsideRoots", err)
	}
	for _, bad := range []string{"relative/root", project + "/../project", filepath.Join(project, "docs", "rubric.md"), filepath.Join(base, "missing")} {
		if _, err := NewFileRoots([]string{bad}); err == nil {
			t.Errorf("NewFileRoots(%q) accepted a bad root", bad)
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
