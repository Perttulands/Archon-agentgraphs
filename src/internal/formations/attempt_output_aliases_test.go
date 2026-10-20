package formations

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// An output is independent evidence even when the seat wrote it directly at
// its eventual snapshot path. Read-only mode alone cannot isolate hardlinks.
func TestAcceptedOutputsDoNotShareMutableSourceFiles(t *testing.T) {
	for _, filesystem := range []string{"default", "copy-fallback"} {
		t.Run(filesystem, func(t *testing.T) {
			base := t.TempDir()
			if filesystem == "copy-fallback" {
				var err error
				base, err = os.MkdirTemp("/dev/shm", "archon-output-copy-")
				if err != nil {
					t.Skipf("tmpfs copy fallback unavailable: %v", err)
				}
				t.Cleanup(func() {
					if err := removeKept(base); err != nil {
						t.Error(err)
					}
				})
			}
			cloneSupported := outputTestCloneSupported(t, base)
			t.Logf("FICLONE supported: %v", cloneSupported)
			if filesystem == "copy-fallback" && cloneSupported {
				t.Fatal("copy fallback fixture unexpectedly supports FICLONE")
			}
			for _, kind := range []string{PortKindFolder, PortKindFile} {
				for _, location := range []string{"ordinary", "outputs", "destination", "outputs-root"} {
					if kind == PortKindFile && location == "outputs-root" {
						continue
					}
					t.Run(kind+"/"+location, func(t *testing.T) {
						workspace, err := os.MkdirTemp(base, "workspace-")
						if err != nil {
							t.Fatal(err)
						}
						thawAfter(t, workspace)
						store := NewStore(workspace)
						personas := NewPersonaStore(filepath.Join(workspace, "agents"))
						createS4Persona(t, personas, "scout")
						writeFixture(t, store.BoardPath("session-search"), draftLoopBoardFixture())
						started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas})
						if err != nil {
							t.Fatal(err)
						}
						dir, err := store.createAttemptArtifactDir(started.RunID, "fmn_work", 1)
						if err != nil {
							t.Fatal(err)
						}
						source := filepath.Join(workspace, "work", "result")
						if location == "outputs" {
							source = filepath.Join(dir, "outputs", "result")
						}
						if location == "outputs-root" {
							source = filepath.Join(dir, "outputs")
						}
						if location == "destination" {
							source = filepath.Join(dir, "outputs", "port_work_out", "result")
						}
						external := filepath.Join(workspace, "external")
						if err := os.MkdirAll(external, 0o700); err != nil {
							t.Fatal(err)
						}
						hardTarget := filepath.Join(external, "hard.txt")
						linkTarget := filepath.Join(external, "linked.txt")
						for _, p := range []string{hardTarget, linkTarget} {
							if err := os.WriteFile(p, []byte("original bytes"), 0o640); err != nil {
								t.Fatal(err)
							}
						}
						if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
							t.Fatal(err)
						}
						if kind == PortKindFolder {
							if err := os.Mkdir(source, 0o700); err != nil {
								t.Fatal(err)
							}
							if err := os.Link(hardTarget, filepath.Join(source, "hard.txt")); err != nil {
								t.Fatal(err)
							}
							if err := os.Symlink(linkTarget, filepath.Join(source, "linked.txt")); err != nil {
								t.Fatal(err)
							}
							if err := os.Symlink("hard.txt", filepath.Join(source, "internal.txt")); err != nil {
								t.Fatal(err)
							}
						} else if err := os.Link(hardTarget, source); err != nil {
							t.Fatal(err)
						}
						before, err := os.Stat(hardTarget)
						if err != nil {
							t.Fatal(err)
						}
						formation := FormationNode{ID: "fmn_work", Outputs: []FormationPort{{ID: "port_work_out", Kind: kind}}}
						accepted, err := store.acceptAttemptOutputs(context.Background(), started.RunID, formation, 1, map[string]FormationOutputPayload{"port_work_out": {Ref: source}})
						if err != nil {
							t.Fatal(err)
						}
						kept := accepted["port_work_out"].Ref
						if want := filepath.Join(dir, "outputs", "port_work_out", filepath.Base(source)); kept != want {
							t.Fatalf("kept=%s, want %s", kept, want)
						}
						after, err := os.Stat(hardTarget)
						if err != nil {
							t.Fatal(err)
						}
						if before.Mode() != after.Mode() {
							t.Fatalf("source mode changed: %v -> %v", before.Mode(), after.Mode())
						}
						preserved := kept
						names := []string{""}
						if kind == PortKindFolder {
							preserved = filepath.Join(kept, "hard.txt")
							names = []string{"hard.txt", "linked.txt", "internal.txt"}
						}
						snapshotInfo, err := os.Stat(preserved)
						if err != nil {
							t.Fatal(err)
						}
						if os.SameFile(after, snapshotInfo) {
							t.Fatal("snapshot retained source inode")
						}
						// Replace the symlink's original target, and rewrite the inode shared
						// with the original hardlink. Neither operation may change evidence.
						if err := os.WriteFile(hardTarget, []byte("changed through external hardlink"), 0o640); err != nil {
							t.Fatal(err)
						}
						if err := os.Remove(linkTarget); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(linkTarget, []byte("replacement symlink target"), 0o640); err != nil {
							t.Fatal(err)
						}
						for _, name := range names {
							path := filepath.Join(kept, name)
							content, err := os.ReadFile(path)
							if err != nil {
								t.Fatal(err)
							}
							wantHash := sha256.Sum256([]byte("original bytes"))
							if got := sha256.Sum256(content); got != wantHash {
								t.Fatalf("snapshot %s changed: %x != %x", name, got, wantHash)
							}
							rel, err := filepath.Rel(filepath.Join(workspace, ".archon", "artifacts", started.RunID), path)
							if err != nil {
								t.Fatal(err)
							}
							evidence, err := store.ReadRunArtifact(started.RunID, filepath.ToSlash(rel))
							if err != nil {
								t.Fatal(err)
							}
							if got := sha256.Sum256(evidence.Body); got != wantHash {
								t.Fatalf("evidence %s changed: %x", name, got)
							}
						}
						if kind == PortKindFolder {
							if link, err := os.Readlink(filepath.Join(kept, "internal.txt")); err != nil || link != "hard.txt" {
								t.Fatalf("internal link=%q, %v", link, err)
							}
						}
					})
				}
			}
		})
	}
}

func outputTestCloneSupported(t *testing.T, dir string) bool {
	t.Helper()
	in, err := os.CreateTemp(dir, "clone-source-")
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	defer os.Remove(in.Name())
	if _, err := in.WriteString("clone probe"); err != nil {
		t.Fatal(err)
	}
	out, err := os.CreateTemp(dir, "clone-target-")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	defer os.Remove(out.Name())
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, out.Fd(), ficlone, in.Fd())
	return errno == 0
}
