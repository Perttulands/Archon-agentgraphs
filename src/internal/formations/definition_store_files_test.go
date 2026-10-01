package formations

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefinitionWriterRepairsLockMode(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	writeFixture(t, store.BoardPath("private"), minimalBoard("private", 1))
	board, err := store.ReadBoard("private")
	if err != nil {
		t.Fatalf("read board precondition: %v", err)
	}
	lockPath := store.BoardPath("private") + ".lock"
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	title := "Updated"
	if _, err := store.UpdateBoardMetadata("private", BoardMetadataPatch{Title: &title}, WriteOptions{
		ExpectedETag: board.ETag,
		ExpectedRev:  board.Rev,
	}); err != nil {
		t.Fatalf("update with an existing lock: %v", err)
	}
	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stat repaired lock: %v", err)
	}
	if got := info.Mode().Perm(); got != sharedFileMode {
		t.Fatalf("repaired lock mode = %04o, want %04o", got, sharedFileMode)
	}
}

func TestLegacyMigrationRepairsValidatedDefinitionDirectoryMode(t *testing.T) {
	definitions := []struct {
		name string
		path func(*Store) string
		raw  string
		read func(*Store) error
	}{
		{
			name: "mission",
			path: func(store *Store) string { return store.BoardPath("legacy") },
			raw:  minimalBoard("legacy", 1),
			read: func(store *Store) error {
				_, err := store.ReadBoard("legacy")
				return err
			},
		},
		{
			name: "layout",
			path: func(store *Store) string { return store.LayoutPath("legacy") },
			raw: "schema = 1\n" +
				"missionId = \"brd_legacy\"\n" +
				"missionRev = 1\n" +
				"updatedAt = \"2026-07-18T12:00:00Z\"\n",
			read: func(store *Store) error {
				_, err := store.ReadLayout("legacy")
				return err
			},
		},
	}

	for _, definition := range definitions {
		definition := definition
		t.Run(definition.name, func(t *testing.T) {
			store := NewStore(t.TempDir())
			definitionPath := definition.path(store)
			writeFixture(t, definitionPath, definition.raw)
			definitionDirectory := filepath.Dir(definitionPath)
			formationsDirectory := filepath.Dir(definitionDirectory)
			for _, directory := range []string{formationsDirectory, definitionDirectory} {
				if err := os.Chmod(directory, 0o755); err != nil {
					t.Fatalf("set legacy directory mode for %s: %v", directory, err)
				}
			}

			formationsModeBefore := definitionPathModeForTest(t, formationsDirectory)
			formationsIdentityBefore := operativeFileIdentityForTest(t, formationsDirectory)
			definitionBefore := readFile(t, definitionPath)
			definitionModeBefore := definitionPathModeForTest(t, definitionPath)
			definitionIdentityBefore := operativeFileIdentityForTest(t, definitionPath)

			// Public definition readers deliberately open the existing hierarchy with
			// create=false. A successful validated file read may repair only the leaf.
			if err := definition.read(store); err != nil {
				t.Fatalf("read legacy %s: %v", definition.name, err)
			}
			if got := definitionPathModeForTest(t, formationsDirectory); got != formationsModeBefore {
				t.Errorf("legacy %s read changed .formations mode from %v to %v", definition.name, formationsModeBefore, got)
			}
			if got := operativeFileIdentityForTest(t, formationsDirectory); got != formationsIdentityBefore {
				t.Errorf("legacy %s read replaced .formations identity = %v, want %v", definition.name, got, formationsIdentityBefore)
			}
			if got := readFile(t, definitionPath); got != definitionBefore {
				t.Errorf("legacy %s read rewrote definition bytes:\n got %q\nwant %q", definition.name, got, definitionBefore)
			}
			if got := definitionPathModeForTest(t, definitionPath); got != definitionModeBefore {
				t.Errorf("legacy %s read changed definition mode from %v to %v", definition.name, definitionModeBefore, got)
			}
			if got := operativeFileIdentityForTest(t, definitionPath); got != definitionIdentityBefore {
				t.Errorf("legacy %s read replaced definition identity = %v, want %v", definition.name, got, definitionIdentityBefore)
			}
			if _, err := os.Lstat(definitionPath + ".lock"); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("legacy %s read created a definition lock: %v", definition.name, err)
			}
			if got := definitionPathModeForTest(t, definitionDirectory); !hasSharedDirMode(got) {
				t.Fatalf("migrated %s definition directory mode = %v, want shared setgid 0770", definition.name, got)
			}
		})
	}
}

func TestMissingDefinitionReadsDoNotRepairDirectoryMode(t *testing.T) {
	definitions := []struct {
		name      string
		directory string
		read      func(*Store) error
	}{
		{
			name:      "mission",
			directory: boardDefinitionKind.directory,
			read: func(store *Store) error {
				_, err := store.ReadBoard("missing")
				return err
			},
		},
		{
			name:      "layout",
			directory: layoutDefinitionKind.directory,
			read: func(store *Store) error {
				_, err := store.ReadLayout("missing")
				return err
			},
		},
	}

	for _, definition := range definitions {
		definition := definition
		t.Run(definition.name, func(t *testing.T) {
			store := NewStore(t.TempDir())
			formationsDirectory := filepath.Join(store.workspaceRoot(), ".archon")
			definitionDirectory := filepath.Join(formationsDirectory, definition.directory)
			if err := os.MkdirAll(definitionDirectory, 0o755); err != nil {
				t.Fatalf("create legacy definition directory: %v", err)
			}
			for _, directory := range []string{formationsDirectory, definitionDirectory} {
				if err := os.Chmod(directory, 0o755); err != nil {
					t.Fatalf("set legacy directory mode for %s: %v", directory, err)
				}
			}
			formationsModeBefore := definitionPathModeForTest(t, formationsDirectory)
			definitionDirectoryModeBefore := definitionPathModeForTest(t, definitionDirectory)

			if err := definition.read(store); !errors.Is(err, ErrNotFound) {
				t.Fatalf("missing %s read error = %v, want ErrNotFound", definition.name, err)
			}
			if got := definitionPathModeForTest(t, formationsDirectory); got != formationsModeBefore {
				t.Errorf("missing %s read changed .formations mode from %v to %v", definition.name, formationsModeBefore, got)
			}
			if got := definitionPathModeForTest(t, definitionDirectory); got != definitionDirectoryModeBefore {
				t.Errorf("missing %s read changed definition directory mode from %v to %v", definition.name, definitionDirectoryModeBefore, got)
			}
		})
	}
}

func TestListBoardsRejectsDefinitionShapedDirectory(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := os.MkdirAll(store.BoardPath("directory"), 0o755); err != nil {
		t.Fatalf("create definition-shaped directory: %v", err)
	}
	definitionDirectory := filepath.Dir(store.BoardPath("directory"))
	formationsDirectory := filepath.Dir(definitionDirectory)
	formationsModeBefore := definitionPathModeForTest(t, formationsDirectory)
	definitionDirectoryModeBefore := definitionPathModeForTest(t, definitionDirectory)

	if _, err := store.ListBoards(); err == nil {
		t.Fatal("list boards silently ignored definition-shaped directory")
	}
	if got := definitionPathModeForTest(t, formationsDirectory); got != formationsModeBefore {
		t.Errorf("wrong-type definition read changed .formations mode from %v to %v", formationsModeBefore, got)
	}
	if got := definitionPathModeForTest(t, definitionDirectory); got != definitionDirectoryModeBefore {
		t.Errorf("wrong-type definition read changed definition directory mode from %v to %v", definitionDirectoryModeBefore, got)
	}
}

func definitionPathModeForTest(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode()
}

func TestConfiguredWorkspaceSymlinkStillSupportsDefinitionPersistence(t *testing.T) {
	root := t.TempDir()
	actualWorkspace := filepath.Join(root, "actual-workspace")
	configuredWorkspace := filepath.Join(root, "configured-workspace")
	if err := os.MkdirAll(actualWorkspace, 0o755); err != nil {
		t.Fatalf("create actual workspace: %v", err)
	}
	if err := os.Symlink(actualWorkspace, configuredWorkspace); err != nil {
		t.Fatalf("symlink configured workspace: %v", err)
	}
	store := NewStore(configuredWorkspace)
	store.Now = fixedClock()

	created, err := store.CreateBoard(BoardCreateRequest{Slug: "linked", Title: "Linked workspace"})
	if err != nil {
		t.Fatalf("create board through configured workspace symlink: %v", err)
	}
	if created.Slug != "linked" || !strings.Contains(readFile(t, store.BoardPath("linked")), `title = "Linked workspace"`) {
		t.Fatalf("created board = %#v, want board persisted through configured workspace symlink", created)
	}
}

// A mission kept in a repository can be shared into the state directory as a
// symlink (archon-4m4j): it is read through the link and written through it,
// so the repository's file changes and the link stays a link.
func TestSymlinkedMissionIsReadAndWrittenThroughItsLink(t *testing.T) {
	root := t.TempDir()
	store := NewStore(filepath.Join(root, "workspace"))
	store.Now = fixedClock()
	repository := filepath.Join(root, "repository")
	shared := filepath.Join(repository, "shared.mission.toml")
	writeFixture(t, shared, minimalBoard("shared", 1))
	if err := os.Chmod(repository, 0o755); err != nil {
		t.Fatalf("chmod repository: %v", err)
	}
	link := store.BoardPath("shared")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("create missions directory: %v", err)
	}
	if err := os.Symlink(shared, link); err != nil {
		t.Fatalf("symlink mission: %v", err)
	}

	summaries, err := store.ListBoards()
	if err != nil || len(summaries) != 1 || summaries[0].Slug != "shared" {
		t.Fatalf("list = %+v (%v), want the linked mission", summaries, err)
	}
	board, err := store.ReadBoard("shared")
	if err != nil {
		t.Fatalf("read linked mission: %v", err)
	}
	title := "Shared from the repository"
	if _, err := store.UpdateBoardMetadata("shared", BoardMetadataPatch{Title: &title}, WriteOptions{
		ExpectedETag: board.ETag,
		ExpectedRev:  board.Rev,
	}); err != nil {
		t.Fatalf("update linked mission: %v", err)
	}
	assertSymlinkForTest(t, link)
	if got := readFile(t, shared); !strings.Contains(got, `title = "Shared from the repository"`) {
		t.Fatalf("repository mission = %q, want the update written through the link", got)
	}
	entries, err := os.ReadDir(repository)
	if err != nil || len(entries) != 1 {
		t.Fatalf("repository holds %v (%v), want only the mission: locks and temporaries stay in the state directory", entries, err)
	}
	if got := definitionPathModeForTest(t, repository).Perm(); got != 0o755 {
		t.Fatalf("repository mode = %04o, want it left at 0755", got)
	}

	updated, err := store.ReadBoard("shared")
	if err != nil {
		t.Fatalf("read updated mission: %v", err)
	}
	if _, err := store.DeleteBoard("shared", WriteOptions{ExpectedETag: updated.ETag, ExpectedRev: updated.Rev}); err != nil {
		t.Fatalf("delete linked mission: %v", err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("linked mission still listed after delete: %v", err)
	}
	if got := readFile(t, shared); !strings.Contains(got, title) {
		t.Fatalf("delete changed the repository's mission: %q", got)
	}
}

// Archon's state directories may live elsewhere behind a symlink, for example
// on another disk (archon-4m4j).
func TestSymlinkedDefinitionDirectoriesAreFollowed(t *testing.T) {
	root := t.TempDir()
	store := NewStore(filepath.Join(root, "workspace"))
	store.Now = fixedClock()
	elsewhere := filepath.Join(root, "other-disk", "missions")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatalf("create external missions directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(store.Workspace, ".archon"), 0o755); err != nil {
		t.Fatalf("create .archon: %v", err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(store.Workspace, ".archon", "missions")); err != nil {
		t.Fatalf("symlink missions directory: %v", err)
	}

	if _, err := store.CreateBoard(BoardCreateRequest{Slug: "moved", Title: "Moved"}); err != nil {
		t.Fatalf("create mission in a symlinked directory: %v", err)
	}
	if got := readFile(t, filepath.Join(elsewhere, "moved.mission.toml")); !strings.Contains(got, `title = "Moved"`) {
		t.Fatalf("mission written to %q, want it in the linked directory", got)
	}
	if _, err := store.ReadBoard("moved"); err != nil {
		t.Fatalf("read mission through a symlinked directory: %v", err)
	}
	assertSymlinkForTest(t, filepath.Join(store.Workspace, ".archon", "missions"))
}

func TestHardLinkedMissionIsRead(t *testing.T) {
	root := t.TempDir()
	store := NewStore(filepath.Join(root, "workspace"))
	other := filepath.Join(root, "other.mission.toml")
	writeFixture(t, other, minimalBoard("linked", 1))
	if err := os.MkdirAll(filepath.Dir(store.BoardPath("linked")), 0o755); err != nil {
		t.Fatalf("create missions directory: %v", err)
	}
	if err := os.Link(other, store.BoardPath("linked")); err != nil {
		t.Fatalf("hard link mission: %v", err)
	}
	if _, err := store.ReadBoard("linked"); err != nil {
		t.Fatalf("read hard-linked mission: %v", err)
	}
}

func TestStartRunSnapshotsASymlinkedMission(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	if _, err := personas.CreatePersona(CreatePersonaRequest{
		ID:           "scout",
		DisplayName:  "Scout",
		Kind:         "specialist",
		Capabilities: []string{"research"},
		Harness:      "openai-codex",
	}); err != nil {
		t.Fatalf("create persona: %v", err)
	}
	shared := filepath.Join(filepath.Dir(store.Workspace), "repository", "session-search.mission.toml")
	raw := s4RunBoardFixture()
	writeFixture(t, shared, raw)
	link := store.BoardPath("session-search")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("create missions directory: %v", err)
	}
	if err := os.Symlink(shared, link); err != nil {
		t.Fatalf("symlink mission: %v", err)
	}

	started, err := store.StartRun("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		ExpectedBoardETag: etag([]byte(raw)),
		ExpectedBoardRev:  7,
		Personas:          personas,
	})
	if err != nil {
		t.Fatalf("start a run of a symlinked mission: %v", err)
	}
	snapshot, err := store.ReadRunBoard(started.RunID)
	if err != nil || snapshot.Slug != "session-search" {
		t.Fatalf("run snapshot = %+v (%v), want the linked mission", snapshot, err)
	}
	assertSymlinkForTest(t, link)
}

func assertSymlinkForTest(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s has mode %v, want it still a symlink", path, info.Mode())
	}
}
