package formations

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestEvidenceArtifactsKeepEveryOutputBeforeOtherFiles(t *testing.T) {
	entries := []RunArtifactEntry{}
	for i := 0; i < 8715; i++ {
		entries = append(entries, RunArtifactEntry{Name: fmt.Sprintf("attempt/cache/%05d", i)})
	}
	for i := 0; i < 600; i++ {
		entries = append(entries, RunArtifactEntry{Name: fmt.Sprintf("attempt/outputs/site/%05d", i)})
	}
	entries = append(entries, RunArtifactEntry{Name: "attempt/outputs/zvideo/turntable.mp4"}, RunArtifactEntry{Name: "attempt/outputs/report.md"})
	outputs := []AttemptOutput{{Artifact: "attempt/outputs/site", Kind: "folder"}, {Artifact: "attempt/outputs/zvideo", Kind: "folder"}, {Artifact: "attempt/outputs/report.md", Kind: "file"}}
	selected, omitted := selectEvidenceArtifacts(entries, outputs)
	if len(selected) != EvidenceArtifactListMax || omitted != len(entries)-EvidenceArtifactListMax {
		t.Fatalf("selected=%d omitted=%d", len(selected), omitted)
	}
	for i, want := range []string{"attempt/outputs/site/00000", "attempt/outputs/zvideo/turntable.mp4", "attempt/outputs/report.md"} {
		if selected[i].Name != want {
			t.Fatalf("output %d=%s want %s", i, selected[i].Name, want)
		}
	}
	for _, entry := range selected {
		if len(entry.Name) > 14 && entry.Name[:14] == "attempt/cache/" {
			t.Fatalf("cache displaced output: %s", entry.Name)
		}
	}
}

func TestEvidenceArtifactsRetainOutputsBeyondListLimit(t *testing.T) {
	entries := []RunArtifactEntry{}
	outputs := []AttemptOutput{}
	for i := 0; i < EvidenceArtifactListMax+1; i++ {
		name := fmt.Sprintf("output/%04d", i)
		entries = append(entries, RunArtifactEntry{Name: name})
		outputs = append(outputs, AttemptOutput{Artifact: name})
	}
	selected, omitted := selectEvidenceArtifacts(entries, outputs)
	if len(selected) != len(entries) || omitted != 0 {
		t.Fatalf("outputs lost: selected=%d omitted=%d", len(selected), omitted)
	}
}

func TestAttemptFilesScanPastCacheAndCountOmissions(t *testing.T) {
	store, _, runID := startHumanGateRun(t)
	root := filepath.Join(store.Workspace, ".archon", "artifacts", runID)
	for _, sub := range []string{"fmn_work/1/cache", "fmn_work/1/outputs/report"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 600; i++ {
		if err := os.WriteFile(filepath.Join(root, "fmn_work/1/cache", fmt.Sprint(i)), []byte("cache"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	name := "fmn_work/1/outputs/report/result.md"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte("# Result"), 0644); err != nil {
		t.Fatal(err)
	}
	files, truncated, omitted, err := store.listAttemptFiles(runID, root, "fmn_work", 1, []AttemptOutput{{Artifact: "fmn_work/1/outputs/report", Kind: "folder"}})
	if err != nil || !truncated || omitted != 101 || len(files) != 500 || files[0].Name != name || files[0].Kind != "markdown" {
		t.Fatalf("files=%d first=%+v truncated=%v omitted=%d err=%v", len(files), files[0], truncated, omitted, err)
	}
	if err := store.AppendRunEvent(runID, RunEvent{Type: RunEventNodeOutput, NodeID: "fmn_work", Attempt: 1, Data: map[string]any{"outputs": map[string]any{"report": map[string]any{"kind": "folder", "ref": filepath.Join(root, "fmn_work/1/outputs/report")}}}}); err != nil {
		t.Fatal(err)
	}
	entries, truncated, omitted, err := store.ListRunArtifacts(runID)
	if err != nil || !truncated || omitted != 101 || len(entries) != 500 || entries[0].Name != name {
		t.Fatalf("global list=%d truncated=%v omitted=%d err=%v", len(entries), truncated, omitted, err)
	}
}

func TestArtifactListingSkipsAncestorSymlinkCycles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "result.md"), []byte("done"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"loop1", "loop2"} {
		if err := os.Symlink(root, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	entries := []RunArtifactEntry{}
	truncated := false
	if err := listRunArtifactsAt(directory, "", 1, &entries, &truncated); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "result.md" || truncated {
		t.Fatalf("cycle listing=%+v truncated=%v", entries, truncated)
	}
}

func TestEvidenceArtifactsKeepNestedDeclaredOutputs(t *testing.T) {
	entries := []RunArtifactEntry{}
	for i := 0; i < 600; i++ {
		entries = append(entries, RunArtifactEntry{Name: fmt.Sprintf("site/cache/%04d", i)})
	}
	entries = append(entries, RunArtifactEntry{Name: "site/turntable.mp4"}, RunArtifactEntry{Name: "site/viewer/model.glb"})
	outputs := []AttemptOutput{{Artifact: "site", Kind: "folder"}, {Artifact: "site/turntable.mp4", Kind: "file"}, {Artifact: "site/viewer", Kind: "folder"}}
	selected, _ := selectEvidenceArtifacts(entries, outputs)
	for i, want := range []string{"site/cache/0000", "site/turntable.mp4", "site/viewer/model.glb"} {
		if selected[i].Name != want {
			t.Fatalf("output %d=%s want %s", i, selected[i].Name, want)
		}
	}
}

func TestArtifactWalkStopsAcrossSiblingDirectories(t *testing.T) {
	root, shared := t.TempDir(), t.TempDir()
	for i := 0; i < 1000; i++ {
		if err := os.WriteFile(filepath.Join(shared, fmt.Sprint(i)), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Each alias lists another 1,000 files without requiring 51,000 disk files.
	for i := 0; i < 51; i++ {
		if err := os.Symlink(shared, filepath.Join(root, fmt.Sprintf("%02d", i))); err != nil {
			t.Fatal(err)
		}
	}
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	entries := []RunArtifactEntry{}
	truncated := false
	if err := listRunArtifactsAt(directory, "", 1, &entries, &truncated); err != nil {
		t.Fatal(err)
	}
	if !truncated || len(entries) != EvidenceArtifactWalkMax {
		t.Fatalf("walk: %d entries truncated=%v", len(entries), truncated)
	}
	selected, omitted := selectEvidenceArtifacts(entries, nil)
	if len(selected) != EvidenceArtifactListMax || omitted != EvidenceArtifactWalkMax-EvidenceArtifactListMax {
		t.Fatalf("list: %d entries, lower bound %d omitted", len(selected), omitted)
	}
}

func TestEvidenceArtifactsKeepReviewMediaBeforeFolderCap(t *testing.T) {
	entries := []RunArtifactEntry{}
	for i := 0; i < 600; i++ {
		entries = append(entries, RunArtifactEntry{Name: fmt.Sprintf("output/frame-%04d.png", i)})
	}
	names := []string{"output/z-contact-sheet.jpg", "output/z-final.png", "output/z-cited.png", "output/turntable.mp4", "output/rig.fbx"}
	for _, name := range names {
		entries = append(entries, RunArtifactEntry{Name: name})
	}
	selected, omitted := selectEvidenceArtifacts(entries, []AttemptOutput{{Artifact: "output", Kind: "folder"}}, "See /work/output/z-cited.png")
	seen := map[string]bool{}
	for _, entry := range selected {
		seen[entry.Name] = true
	}
	for _, name := range names {
		if !seen[name] {
			t.Fatalf("review media omitted: %s", name)
		}
	}
	if len(selected) != EvidenceArtifactListMax || omitted != len(entries)-len(selected) {
		t.Fatalf("count %d omitted %d", len(selected), omitted)
	}
}
