package formations

import (
	"errors"
	"strings"
	"testing"
)

func noteTexts(entries []NoteEntry) []string {
	texts := make([]string, 0, len(entries))
	for _, entry := range entries {
		texts = append(texts, entry.Author+": "+entry.Text)
	}
	return texts
}

func elementThread(notes *BoardNotesDocument, nodeID string) []NoteEntry {
	for _, element := range notes.Elements {
		if element.NodeID == nodeID {
			return element.Entries
		}
	}
	return nil
}

func TestBoardNotesRoundTripWithoutChangingExecutableBoard(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	writeFixture(t, store.BoardPath("session-search"), notesBoardFixture())
	boardBefore := readFile(t, store.BoardPath("session-search"))

	empty, err := store.ReadBoardNotes("session-search")
	if err != nil {
		t.Fatalf("read empty notes: %v", err)
	}
	if empty.ETag != "*" || empty.Rev != 0 || empty.BoardID != "brd_notes" || len(empty.Board) != 0 || len(empty.Elements) != 0 {
		t.Fatalf("empty notes = %+v, want synthetic empty document", empty)
	}

	boardText := "Shared plan\n\n- preserve the API\n- ship tests"
	afterBoard, err := store.UpdateBoardNote("session-search", BoardNotePatch{
		Target: BoardNoteTarget,
		Text:   boardText,
		Author: "human:operator",
	}, NoteWriteOptions{ExpectedETag: empty.ETag})
	if err != nil {
		t.Fatalf("write board note: %v", err)
	}
	if afterBoard.Rev != 1 || len(afterBoard.Board) != 1 || afterBoard.Board[0].Text != boardText || afterBoard.ETag == "" || afterBoard.ETag == "*" {
		t.Fatalf("board notes = %+v, want persisted multiline board note", afterBoard)
	}

	elementText := "Builder: keep this formation narrow."
	afterElement, err := store.UpdateBoardNote("session-search", BoardNotePatch{
		Target:    "fmn_frame",
		Text:      elementText,
		UpdatedBy: "agent:archon",
	}, NoteWriteOptions{ExpectedETag: afterBoard.ETag})
	if err != nil {
		t.Fatalf("write element note: %v", err)
	}
	if afterElement.Rev != 2 || afterElement.UpdatedBy != "agent:archon" || len(afterElement.Elements) != 1 || afterElement.Elements[0].NodeID != "fmn_frame" ||
		strings.Join(noteTexts(afterElement.Elements[0].Entries), "|") != "agent:archon: "+elementText {
		t.Fatalf("element notes = %+v, want an agent-authored fmn_frame entry", afterElement)
	}
	if got := readFile(t, store.BoardPath("session-search")); got != boardBefore {
		t.Fatalf("notes changed executable board bytes:\n%s", got)
	}

	reread, err := store.ReadBoardNotes("session-search")
	if err != nil {
		t.Fatalf("reread notes: %v", err)
	}
	if reread.Board[0].Text != boardText || reread.Board[0].Author != "human:operator" || !reread.Board[0].CreatedAt.Equal(fixedClock()()) ||
		elementThread(reread, "fmn_frame")[0].Text != elementText || reread.TOML != "" || reread.ETag != afterElement.ETag {
		t.Fatalf("reread notes = %+v, want exact entries without raw TOML", reread)
	}
}

func TestBoardNoteThreadsAppendAndLimitEditsToTheAuthor(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	writeFixture(t, store.BoardPath("session-search"), notesBoardFixture())
	write := func(patch BoardNotePatch) (*BoardNotesDocument, error) {
		t.Helper()
		current, err := store.ReadBoardNotes("session-search")
		if err != nil {
			t.Fatal(err)
		}
		return store.UpdateBoardNote("session-search", patch, NoteWriteOptions{ExpectedETag: current.ETag})
	}

	operator, err := write(BoardNotePatch{Target: "fmn_frame", Text: "Map the territory first", Author: "human:ui"})
	if err != nil {
		t.Fatal(err)
	}
	operatorEntry := operator.Elements[0].Entries[0]
	reply, err := write(BoardNotePatch{Target: "fmn_frame", Text: "Agreed; I staffed a scout", Author: "agent:archon"})
	if err != nil {
		t.Fatal(err)
	}
	agentEntry := elementThread(reply, "fmn_frame")[1]
	if got := strings.Join(noteTexts(elementThread(reply, "fmn_frame")), "|"); got != "human:ui: Map the territory first|agent:archon: Agreed; I staffed a scout" {
		t.Fatalf("thread after reply = %q, want the operator's words kept", got)
	}
	if agentEntry.ID == operatorEntry.ID || !strings.HasPrefix(agentEntry.ID, "nte_") {
		t.Fatalf("entry ids = %q and %q, want distinct nte_ ids", operatorEntry.ID, agentEntry.ID)
	}

	if _, err := write(BoardNotePatch{Target: "fmn_frame", Action: NoteActionEdit, EntryID: operatorEntry.ID, Text: "overwrite", Author: "agent:archon"}); !errors.Is(err, ErrNoteAuthorMismatch) {
		t.Fatalf("agent editing the operator's entry = %v, want ErrNoteAuthorMismatch", err)
	}
	if _, err := write(BoardNotePatch{Target: "fmn_frame", Action: NoteActionDelete, EntryID: operatorEntry.ID, Author: "agent:archon"}); !errors.Is(err, ErrNoteAuthorMismatch) {
		t.Fatalf("agent deleting the operator's entry = %v, want ErrNoteAuthorMismatch", err)
	}
	edited, err := write(BoardNotePatch{Target: "fmn_frame", Action: NoteActionEdit, EntryID: agentEntry.ID, Text: "Agreed; a scout and a planner", Author: "agent:archon"})
	if err != nil {
		t.Fatal(err)
	}
	if thread := elementThread(edited, "fmn_frame"); thread[1].Text != "Agreed; a scout and a planner" || thread[1].EditedAt == nil || thread[1].ID != agentEntry.ID || thread[0].Text != "Map the territory first" {
		t.Fatalf("edited thread = %+v", thread)
	}
	deleted, err := write(BoardNotePatch{Target: "fmn_frame", Action: NoteActionDelete, EntryID: agentEntry.ID, Author: "agent:archon"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(noteTexts(elementThread(deleted, "fmn_frame")), "|"); got != "human:ui: Map the territory first" {
		t.Fatalf("thread after deleting the reply = %q", got)
	}
	cleared, err := write(BoardNotePatch{Target: "fmn_frame", Action: NoteActionDelete, EntryID: operatorEntry.ID, Author: "human:ui"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.Elements) != 0 {
		t.Fatalf("deleting the last entry left the element thread: %+v", cleared.Elements)
	}

	before := readFile(t, store.NotesPath("session-search"))
	for name, patch := range map[string]BoardNotePatch{
		"blank author":       {Target: BoardNoteTarget, Text: "x"},
		"unscoped author":    {Target: BoardNoteTarget, Text: "x", Author: "operator"},
		"blank append":       {Target: BoardNoteTarget, Text: "  ", Author: "human:ui"},
		"append with entry":  {Target: BoardNoteTarget, Text: "x", EntryID: "nte_x", Author: "human:ui"},
		"edit without entry": {Target: BoardNoteTarget, Action: NoteActionEdit, Text: "x", Author: "human:ui"},
		"unknown action":     {Target: BoardNoteTarget, Action: "replace", Text: "x", Author: "human:ui"},
		"missing entry":      {Target: BoardNoteTarget, Action: NoteActionDelete, EntryID: "nte_missing", Author: "human:ui"},
	} {
		_, err := write(patch)
		want := ErrInvalidNotePatch
		if name == "missing entry" {
			want = ErrNoteEntryNotFound
		}
		if !errors.Is(err, want) {
			t.Errorf("%s error = %v, want %v", name, err, want)
		}
		if after := readFile(t, store.NotesPath("session-search")); after != before {
			t.Fatalf("%s changed the notes file", name)
		}
	}
}

func TestBoardNotesMigrateSchemaOneTextsToHumanEntries(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	writeFixture(t, store.BoardPath("session-search"), notesBoardFixture()+`
[[formation]]
id = "fmn_peers"
type = "peer"
title = "Peers"
`)
	// The shape of the Wayfinding notes at rev 3: a board note and two element
	// notes, last saved from the cockpit.
	legacy := `schema = 1
boardId = "brd_notes"
rev = 3
updatedAt = "2026-09-16T12:39:30.091445373Z"
updatedBy = "human:ui"
board = "The purpose is a workflow that clarifies a goal\nand avoids typical AI shortcomings"

[[element]]
nodeId = "fmn_peers"
text = "Two peers reason about the goal and surface at most 10 questions"

[[element]]
nodeId = "fmn_frame"
text = "Start by mapping the territory"
`
	writeFixture(t, store.NotesPath("session-search"), legacy)

	notes, err := store.ReadBoardNotes("session-search")
	if err != nil {
		t.Fatalf("legacy notes do not load: %v", err)
	}
	wantThread := func(entries []NoteEntry, id, text string) {
		t.Helper()
		if len(entries) != 1 || entries[0].ID != id || entries[0].Author != "human:ui" || entries[0].Text != text || !entries[0].CreatedAt.Equal(notes.UpdatedAt) || entries[0].EditedAt != nil {
			t.Fatalf("migrated thread = %+v, want one human:ui entry %q", entries, text)
		}
	}
	if notes.Schema != CurrentBoardNotesSchema || notes.Rev != 3 || notes.ETag != etag([]byte(legacy)) {
		t.Fatalf("migrated metadata = %+v", notes)
	}
	wantThread(notes.Board, "migrated-board", "The purpose is a workflow that clarifies a goal\nand avoids typical AI shortcomings")
	wantThread(elementThread(notes, "fmn_frame"), "migrated-fmn_frame", "Start by mapping the territory")
	wantThread(elementThread(notes, "fmn_peers"), "migrated-fmn_peers", "Two peers reason about the goal and surface at most 10 questions")
	if got := readFile(t, store.NotesPath("session-search")); got != legacy {
		t.Fatal("reading legacy notes rewrote the file")
	}

	replied, err := store.UpdateBoardNote("session-search", BoardNotePatch{Target: "fmn_peers", Text: "I will cap the questions at 10", Author: "agent:archon"}, NoteWriteOptions{ExpectedETag: notes.ETag})
	if err != nil {
		t.Fatalf("reply on migrated notes: %v", err)
	}
	reread, err := store.ReadBoardNotes("session-search")
	if err != nil || reread.ETag != replied.ETag {
		t.Fatalf("reread after migration write: %v", err)
	}
	if got := strings.Join(noteTexts(elementThread(reread, "fmn_peers")), "|"); got != "human:ui: Two peers reason about the goal and surface at most 10 questions|agent:archon: I will cap the questions at 10" {
		t.Fatalf("migrated thread after reply = %q", got)
	}
	if reread.Board[0].Text != notes.Board[0].Text || elementThread(reread, "fmn_frame")[0].ID != "migrated-fmn_frame" {
		t.Fatalf("migration changed other threads: %+v", reread)
	}
	raw := readFile(t, store.NotesPath("session-search"))
	if !strings.HasPrefix(raw, "schema = 2\n") || strings.Contains(raw, "[[element]]") {
		t.Fatalf("write did not store schema 2 entries:\n%s", raw)
	}

	agentLegacy := strings.Replace(legacy, `updatedBy = "human:ui"`, `updatedBy = "agent:archon"`, 1)
	writeFixture(t, store.NotesPath("session-search"), agentLegacy)
	if notes, err := store.ReadBoardNotes("session-search"); err != nil || notes.Board[0].Author != "human:operator" {
		t.Fatalf("legacy notes last saved by an agent = %+v (%v), want human:operator entries", notes, err)
	}
}

func TestBoardNotesRejectConcurrentWritesWithStaleETag(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	writeFixture(t, store.BoardPath("session-search"), notesBoardFixture())
	seed, err := store.UpdateBoardNote("session-search", BoardNotePatch{Target: BoardNoteTarget, Text: "seed", Author: "human:ui"}, NoteWriteOptions{ExpectedETag: "*"})
	if err != nil {
		t.Fatal(err)
	}
	// Two writers read the same revision; the second must reload, not clobber.
	first, err := store.UpdateBoardNote("session-search", BoardNotePatch{Target: BoardNoteTarget, Text: "operator reply", Author: "human:ui"}, NoteWriteOptions{ExpectedETag: seed.ETag})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBoardNote("session-search", BoardNotePatch{Target: BoardNoteTarget, Text: "agent reply", Author: "agent:archon"}, NoteWriteOptions{ExpectedETag: seed.ETag}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second writer on a stale ETag = %v, want ErrConflict", err)
	}
	if _, err := store.UpdateBoardNote("session-search", BoardNotePatch{Target: BoardNoteTarget, Action: NoteActionDelete, EntryID: seed.Board[0].ID, Author: "human:ui"}, NoteWriteOptions{ExpectedETag: seed.ETag}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale delete = %v, want ErrConflict", err)
	}
	retried, err := store.UpdateBoardNote("session-search", BoardNotePatch{Target: BoardNoteTarget, Text: "agent reply", Author: "agent:archon"}, NoteWriteOptions{ExpectedETag: first.ETag})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(noteTexts(retried.Board), "|"); got != "human:ui: seed|human:ui: operator reply|agent:archon: agent reply" {
		t.Fatalf("thread after retry = %q", got)
	}
}

func TestBoardNotesAreBoundToStableBoardIDAcrossSlugReuse(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	original, err := store.CreateBoard(BoardCreateRequest{Slug: "reused", Title: "Original", UpdatedBy: "human"})
	if err != nil {
		t.Fatalf("create original board: %v", err)
	}
	written, err := store.UpdateBoardNote("reused", BoardNotePatch{Target: BoardNoteTarget, Text: "original secret", Author: "human:operator"}, NoteWriteOptions{ExpectedETag: "*"})
	if err != nil {
		t.Fatalf("write original note: %v", err)
	}
	if _, err := store.DeleteBoard("reused", WriteOptions{ExpectedETag: original.ETag, ExpectedRev: original.Rev}); err != nil {
		t.Fatalf("archive original board: %v", err)
	}
	recreated, err := store.CreateBoard(BoardCreateRequest{Slug: "reused", Title: "Replacement", UpdatedBy: "human"})
	if err != nil {
		t.Fatalf("create replacement board: %v", err)
	}
	if recreated.ID == original.ID {
		t.Fatalf("replacement board reused stable id %q", recreated.ID)
	}
	empty, err := store.ReadBoardNotes("reused")
	if err != nil {
		t.Fatalf("read replacement notes: %v", err)
	}
	if len(empty.Board) != 0 || len(empty.Elements) != 0 || empty.ETag != "*" || empty.BoardID != recreated.ID {
		t.Fatalf("replacement leaked archived notes = %+v (old notes %+v)", empty, written)
	}
	updated, err := store.UpdateBoardNote("reused", BoardNotePatch{Target: BoardNoteTarget, Text: "replacement", Author: "human:operator"}, NoteWriteOptions{ExpectedETag: "*"})
	if err != nil {
		t.Fatalf("write replacement note: %v", err)
	}
	if updated.BoardID != recreated.ID || len(updated.Board) != 1 || updated.Board[0].Text != "replacement" {
		t.Fatalf("replacement notes = %+v", updated)
	}
}

func TestBoardNotesRejectUnknownElementAndStaleETagWithoutClobber(t *testing.T) {
	store := NewStore(t.TempDir())
	writeFixture(t, store.BoardPath("session-search"), notesBoardFixture())
	empty, err := store.ReadBoardNotes("session-search")
	if err != nil {
		t.Fatalf("read empty notes: %v", err)
	}

	if _, err := store.UpdateBoardNote("session-search", BoardNotePatch{Target: "fmn_missing", Text: "nope", Author: "human:ui"}, NoteWriteOptions{ExpectedETag: empty.ETag}); !errors.Is(err, ErrNoteTargetNotFound) {
		t.Fatalf("unknown target error = %v, want ErrNoteTargetNotFound", err)
	}
	current, err := store.UpdateBoardNote("session-search", BoardNotePatch{Target: BoardNoteTarget, Text: "current", Author: "human:ui"}, NoteWriteOptions{ExpectedETag: empty.ETag})
	if err != nil {
		t.Fatalf("write current note: %v", err)
	}
	if _, err := store.UpdateBoardNote("session-search", BoardNotePatch{Target: BoardNoteTarget, Text: "stale", Author: "human:ui"}, NoteWriteOptions{ExpectedETag: empty.ETag}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale note error = %v, want ErrConflict", err)
	}
	after, err := store.ReadBoardNotes("session-search")
	if err != nil {
		t.Fatalf("read after stale write: %v", err)
	}
	if len(after.Board) != 1 || after.Board[0].Text != "current" || after.ETag != current.ETag {
		t.Fatalf("stale write clobbered notes = %+v", after)
	}
}

func TestBoardNotesRejectOversizedText(t *testing.T) {
	store := NewStore(t.TempDir())
	writeFixture(t, store.BoardPath("session-search"), notesBoardFixture())

	_, err := store.UpdateBoardNote("session-search", BoardNotePatch{
		Target: BoardNoteTarget,
		Text:   strings.Repeat("x", MaxBoardNoteBytes+1),
		Author: "human:ui",
	}, NoteWriteOptions{ExpectedETag: "*"})
	if !errors.Is(err, ErrNoteTooLarge) {
		t.Fatalf("oversized note error = %v, want ErrNoteTooLarge", err)
	}
}

func notesBoardFixture() string {
	return `schema = 1
id = "brd_notes"
slug = "session-search"
title = "Session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"

[[formation]]
id = "fmn_frame"
type = "solo"
title = "Frame"

[[formation.slot]]
id = "slot_frame"
label = "Builder"
`
}
