package formations

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const (
	CurrentBoardNotesSchema = 2
	legacyBoardNotesSchema  = 1
	BoardNoteTarget         = "board"
	MaxBoardNoteBytes       = 64 * 1024
	maxBoardNotesBytes      = 512 * 1024

	NoteActionAppend = "append"
	NoteActionEdit   = "edit"
	NoteActionDelete = "delete"
)

var (
	ErrNoteTargetNotFound = errors.New("formation note target not found")
	ErrNoteTooLarge       = errors.New("formation note is too large")
	ErrNoteEntryNotFound  = errors.New("formation note entry not found")
	ErrNoteAuthorMismatch = errors.New("formation note entry belongs to another author")
	ErrInvalidNotePatch   = errors.New("invalid_note_patch")
)

var noteAuthorPattern = regexp.MustCompile(`^(human|agent):[A-Za-z0-9][A-Za-z0-9._@-]*$`)

// NoteEntry is one authored message in a board or element thread.
type NoteEntry struct {
	ID        string     `json:"id"`
	Author    string     `json:"author"`
	CreatedAt time.Time  `json:"createdAt"`
	EditedAt  *time.Time `json:"editedAt,omitempty"`
	Text      string     `json:"text"`
}

// ElementNote is the thread attached to one node, oldest entry first.
type ElementNote struct {
	NodeID  string      `json:"nodeId"`
	Entries []NoteEntry `json:"entries"`
}

type BoardNotesDocument struct {
	Schema    int           `json:"schema"`
	BoardID   string        `json:"boardId"`
	Rev       int           `json:"rev"`
	UpdatedAt time.Time     `json:"updatedAt"`
	UpdatedBy string        `json:"updatedBy,omitempty"`
	Board     []NoteEntry   `json:"board"`
	Elements  []ElementNote `json:"elements"`
	ETag      string        `json:"etag"`
	TOML      string        `json:"-"`
}

// BoardNotePatch appends to a thread by default. Edit and delete name an
// entry and apply only to the author's own entries, so a reply never erases
// someone else's words.
type BoardNotePatch struct {
	Target  string `json:"target"`
	Action  string `json:"action,omitempty"`
	EntryID string `json:"entryId,omitempty"`
	Text    string `json:"text"`
	Author  string `json:"author,omitempty"`
	// UpdatedBy names the author for clients written before note threads.
	UpdatedBy string `json:"updatedBy,omitempty"`
}

type NoteWriteOptions struct {
	ExpectedETag string
}

type boardNotesSource struct {
	Schema    int               `toml:"schema"`
	BoardID   string            `toml:"boardId"`
	Rev       int               `toml:"rev"`
	UpdatedAt time.Time         `toml:"updatedAt"`
	UpdatedBy string            `toml:"updatedBy"`
	Entries   []noteEntrySource `toml:"entry"`
	// Schema 1 held one text per target; it migrates to one human entry each.
	Board    string                    `toml:"board"`
	Elements []legacyElementNoteSource `toml:"element"`
}

type noteEntrySource struct {
	ID        string     `toml:"id"`
	Target    string     `toml:"target"`
	Author    string     `toml:"author"`
	CreatedAt time.Time  `toml:"createdAt"`
	EditedAt  *time.Time `toml:"editedAt"`
	Text      string     `toml:"text"`
}

type legacyElementNoteSource struct {
	NodeID string `toml:"nodeId"`
	Text   string `toml:"text"`
}

func (s *Store) NotesPath(slug string) string {
	return filepath.Join(s.workspaceRoot(), ".formations", notesDefinitionKind.directory, slug+notesDefinitionKind.suffix)
}

func (s *Store) ReadBoardNotes(slug string) (*BoardNotesDocument, error) {
	board, err := s.ReadBoard(slug)
	if err != nil {
		return nil, err
	}
	notesDefinition, err := s.openNotesDefinition(slug, false)
	if errors.Is(err, ErrNotFound) {
		return emptyBoardNotes(board.ID), nil
	}
	if err != nil {
		return nil, err
	}
	defer notesDefinition.close()
	raw, err := notesDefinition.readBytes()
	if errors.Is(err, ErrNotFound) {
		return emptyBoardNotes(board.ID), nil
	}
	if err != nil {
		return nil, err
	}
	notes, err := parseBoardNotes(raw)
	if err != nil {
		return nil, err
	}
	if notes.BoardID != board.ID {
		return emptyBoardNotes(board.ID), nil
	}
	return notes, nil
}

func (s *Store) UpdateBoardNote(slug string, patch BoardNotePatch, opts NoteWriteOptions) (*BoardNotesDocument, error) {
	if strings.TrimSpace(opts.ExpectedETag) == "" {
		return nil, ErrPreconditionRequired
	}
	patch.Target = strings.TrimSpace(patch.Target)
	if patch.Target == "" {
		patch.Target = BoardNoteTarget
	}
	if len([]byte(patch.Text)) > MaxBoardNoteBytes {
		return nil, ErrNoteTooLarge
	}
	author, err := validateNotePatch(&patch)
	if err != nil {
		return nil, err
	}

	var updated *BoardNotesDocument
	err = s.withBoardDefinitionLock(slug, func(boardDefinition *definitionFile) error {
		boardRaw, err := boardDefinition.readBytes()
		if err != nil {
			return err
		}
		board, err := parseBoard(boardRaw)
		if err != nil {
			return err
		}
		if patch.Target != BoardNoteTarget && !boardHasNoteTarget(board, patch.Target) {
			return ErrNoteTargetNotFound
		}

		return s.withNotesDefinitionLock(slug, func(notesDefinition *definitionFile) error {
			current := emptyBoardNotes(board.ID)
			exists, err := notesDefinition.exists()
			if err != nil {
				return err
			}
			if exists {
				raw, err := notesDefinition.readBytes()
				if err != nil {
					return err
				}
				persisted, err := parseBoardNotes(raw)
				if err != nil {
					return err
				}
				if persisted.BoardID != board.ID {
					if _, err := notesDefinition.archive(newPrefixedID("archive")); err != nil {
						return err
					}
					exists = false
				} else {
					current = persisted
				}
			}
			if exists {
				if opts.ExpectedETag == "*" || opts.ExpectedETag != current.ETag {
					return ErrConflict
				}
			} else if opts.ExpectedETag != "*" {
				return ErrConflict
			}

			next := cloneBoardNotes(current)
			next.Schema = CurrentBoardNotesSchema
			next.BoardID = board.ID
			next.Rev = current.Rev + 1
			next.UpdatedAt = s.now().UTC()
			next.UpdatedBy = author
			entries, err := applyNotePatch(next.thread(patch.Target), patch, author, next.UpdatedAt)
			if err != nil {
				return err
			}
			next.setThread(patch.Target, entries)
			if boardNotesSize(next) > maxBoardNotesBytes {
				return ErrNoteTooLarge
			}
			rendered := renderBoardNotes(next)
			if err := notesDefinition.writeAtomic(rendered); err != nil {
				return err
			}
			next.ETag = etag(rendered)
			next.TOML = ""
			updated = next
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func validateNotePatch(patch *BoardNotePatch) (string, error) {
	author := strings.TrimSpace(patch.Author)
	if author == "" {
		author = strings.TrimSpace(patch.UpdatedBy)
	}
	if !noteAuthorPattern.MatchString(author) {
		return "", fmt.Errorf("%w: note author %q must be human:<name> or agent:<name>", ErrInvalidNotePatch, author)
	}
	patch.Action = strings.TrimSpace(patch.Action)
	if patch.Action == "" {
		patch.Action = NoteActionAppend
	}
	patch.EntryID = strings.TrimSpace(patch.EntryID)
	switch patch.Action {
	case NoteActionAppend:
		if patch.EntryID != "" {
			return "", fmt.Errorf("%w: append creates a new entry and takes no entryId", ErrInvalidNotePatch)
		}
	case NoteActionEdit, NoteActionDelete:
		if patch.EntryID == "" {
			return "", fmt.Errorf("%w: %s needs the entryId of your own note", ErrInvalidNotePatch, patch.Action)
		}
	default:
		return "", fmt.Errorf("%w: unknown note action %q; use append, edit or delete", ErrInvalidNotePatch, patch.Action)
	}
	if patch.Action != NoteActionDelete && strings.TrimSpace(patch.Text) == "" {
		return "", fmt.Errorf("%w: %s needs note text; delete the entry to remove it", ErrInvalidNotePatch, patch.Action)
	}
	return author, nil
}

func applyNotePatch(entries []NoteEntry, patch BoardNotePatch, author string, now time.Time) ([]NoteEntry, error) {
	if patch.Action == NoteActionAppend {
		return append(entries, NoteEntry{ID: newPrefixedID("nte"), Author: author, CreatedAt: now, Text: patch.Text}), nil
	}
	for i, entry := range entries {
		if entry.ID != patch.EntryID {
			continue
		}
		if entry.Author != author {
			return nil, fmt.Errorf("%w: entry %q is by %s", ErrNoteAuthorMismatch, entry.ID, entry.Author)
		}
		if patch.Action == NoteActionDelete {
			return append(entries[:i:i], entries[i+1:]...), nil
		}
		edited := now
		entries[i].Text = patch.Text
		entries[i].EditedAt = &edited
		return entries, nil
	}
	return nil, fmt.Errorf("%w: %q on %s", ErrNoteEntryNotFound, patch.EntryID, patch.Target)
}

func (notes *BoardNotesDocument) thread(target string) []NoteEntry {
	if target == BoardNoteTarget {
		return append([]NoteEntry(nil), notes.Board...)
	}
	for _, element := range notes.Elements {
		if element.NodeID == target {
			return append([]NoteEntry(nil), element.Entries...)
		}
	}
	return nil
}

func (notes *BoardNotesDocument) setThread(target string, entries []NoteEntry) {
	if target == BoardNoteTarget {
		notes.Board = entries
		return
	}
	elements := make([]ElementNote, 0, len(notes.Elements)+1)
	for _, element := range notes.Elements {
		if element.NodeID != target {
			elements = append(elements, element)
		}
	}
	if len(entries) > 0 {
		elements = append(elements, ElementNote{NodeID: target, Entries: entries})
	}
	sort.Slice(elements, func(i, j int) bool { return elements[i].NodeID < elements[j].NodeID })
	notes.Elements = elements
}

func emptyBoardNotes(boardID string) *BoardNotesDocument {
	return &BoardNotesDocument{
		Schema:   CurrentBoardNotesSchema,
		BoardID:  boardID,
		Board:    []NoteEntry{},
		Elements: []ElementNote{},
		ETag:     "*",
	}
}

func parseBoardNotes(raw []byte) (*BoardNotesDocument, error) {
	var source boardNotesSource
	if err := toml.Unmarshal(raw, &source); err != nil {
		return nil, invalidDefinitionSource(err)
	}
	if source.Schema > CurrentBoardNotesSchema {
		return nil, fmt.Errorf("%w: notes schema %d", ErrUnsupportedSchema, source.Schema)
	}
	if source.Schema < legacyBoardNotesSchema || strings.TrimSpace(source.BoardID) == "" || source.Rev < 1 || source.UpdatedAt.IsZero() {
		return nil, invalidDefinitionSource(errors.New("invalid board notes metadata"))
	}
	notes := &BoardNotesDocument{
		Schema:    CurrentBoardNotesSchema,
		BoardID:   source.BoardID,
		Rev:       source.Rev,
		UpdatedAt: source.UpdatedAt.UTC(),
		UpdatedBy: source.UpdatedBy,
		Board:     []NoteEntry{},
		Elements:  []ElementNote{},
		ETag:      etag(raw),
	}
	var entries []noteEntrySource
	if source.Schema == legacyBoardNotesSchema {
		legacy, err := migrateLegacyNotes(source)
		if err != nil {
			return nil, err
		}
		entries = legacy
	} else {
		entries = source.Entries
	}
	seen := map[string]bool{}
	threads := map[string][]NoteEntry{}
	for _, entry := range entries {
		switch {
		case strings.TrimSpace(entry.ID) == "" || seen[entry.ID]:
			return nil, invalidDefinitionSource(fmt.Errorf("note entry id %q is missing or repeated", entry.ID))
		case strings.TrimSpace(entry.Target) == "":
			return nil, invalidDefinitionSource(fmt.Errorf("note entry %q has no target", entry.ID))
		case !noteAuthorPattern.MatchString(entry.Author):
			return nil, invalidDefinitionSource(fmt.Errorf("note entry %q has invalid author %q", entry.ID, entry.Author))
		case entry.CreatedAt.IsZero():
			return nil, invalidDefinitionSource(fmt.Errorf("note entry %q has no createdAt", entry.ID))
		case len([]byte(entry.Text)) > MaxBoardNoteBytes:
			return nil, ErrNoteTooLarge
		}
		seen[entry.ID] = true
		note := NoteEntry{ID: entry.ID, Author: entry.Author, CreatedAt: entry.CreatedAt.UTC(), Text: entry.Text}
		if entry.EditedAt != nil {
			edited := entry.EditedAt.UTC()
			note.EditedAt = &edited
		}
		threads[entry.Target] = append(threads[entry.Target], note)
	}
	for target, thread := range threads {
		notes.setThread(target, thread)
	}
	if boardNotesSize(notes) > maxBoardNotesBytes {
		return nil, ErrNoteTooLarge
	}
	return notes, nil
}

// migrateLegacyNotes turns each schema-1 text into one human entry, dated when
// the notes were last saved. The previous editor is kept when it was human.
func migrateLegacyNotes(source boardNotesSource) ([]noteEntrySource, error) {
	author := strings.TrimSpace(source.UpdatedBy)
	if !strings.HasPrefix(author, "human:") || !noteAuthorPattern.MatchString(author) {
		author = "human:operator"
	}
	var entries []noteEntrySource
	add := func(target, text string) {
		if text != "" {
			entries = append(entries, noteEntrySource{ID: "migrated-" + target, Target: target, Author: author, CreatedAt: source.UpdatedAt, Text: text})
		}
	}
	add(BoardNoteTarget, source.Board)
	seen := map[string]bool{}
	for _, note := range source.Elements {
		if strings.TrimSpace(note.NodeID) == "" {
			return nil, invalidDefinitionSource(errors.New("element note is missing nodeId"))
		}
		if seen[note.NodeID] {
			return nil, invalidDefinitionSource(fmt.Errorf("duplicate element note %q", note.NodeID))
		}
		seen[note.NodeID] = true
		add(note.NodeID, note.Text)
	}
	return entries, nil
}

func renderBoardNotes(notes *BoardNotesDocument) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "schema = %d\n", CurrentBoardNotesSchema)
	fmt.Fprintf(&b, "boardId = %s\n", renderString(notes.BoardID))
	fmt.Fprintf(&b, "rev = %d\n", notes.Rev)
	fmt.Fprintf(&b, "updatedAt = %s\n", renderString(notes.UpdatedAt.UTC().Format(time.RFC3339Nano)))
	fmt.Fprintf(&b, "updatedBy = %s\n", renderString(notes.UpdatedBy))
	renderThread := func(target string, entries []NoteEntry) {
		for _, entry := range entries {
			fmt.Fprintf(&b, "\n[[entry]]\nid = %s\ntarget = %s\nauthor = %s\n", renderString(entry.ID), renderString(target), renderString(entry.Author))
			fmt.Fprintf(&b, "createdAt = %s\n", renderString(entry.CreatedAt.UTC().Format(time.RFC3339Nano)))
			if entry.EditedAt != nil {
				fmt.Fprintf(&b, "editedAt = %s\n", renderString(entry.EditedAt.UTC().Format(time.RFC3339Nano)))
			}
			fmt.Fprintf(&b, "text = %s\n", renderString(entry.Text))
		}
	}
	renderThread(BoardNoteTarget, notes.Board)
	elements := append([]ElementNote(nil), notes.Elements...)
	sort.Slice(elements, func(i, j int) bool { return elements[i].NodeID < elements[j].NodeID })
	for _, element := range elements {
		renderThread(element.NodeID, element.Entries)
	}
	return []byte(b.String())
}

func cloneBoardNotes(notes *BoardNotesDocument) *BoardNotesDocument {
	clone := *notes
	clone.Board = append([]NoteEntry(nil), notes.Board...)
	clone.Elements = make([]ElementNote, 0, len(notes.Elements))
	for _, element := range notes.Elements {
		clone.Elements = append(clone.Elements, ElementNote{NodeID: element.NodeID, Entries: append([]NoteEntry(nil), element.Entries...)})
	}
	return &clone
}

func boardHasNoteTarget(board *BoardDocument, target string) bool {
	for _, mission := range board.Missions {
		if mission.ID == target {
			return true
		}
	}
	for _, formation := range board.Formations {
		if formation.ID == target {
			return true
		}
	}
	for _, gate := range board.Gates {
		if gate.ID == target {
			return true
		}
	}
	for _, tool := range board.Tools {
		if tool.ID == target {
			return true
		}
	}
	return false
}

func boardNotesSize(notes *BoardNotesDocument) int {
	total := 0
	for _, entry := range notes.Board {
		total += len([]byte(entry.Author)) + len([]byte(entry.Text))
	}
	for _, element := range notes.Elements {
		total += len([]byte(element.NodeID))
		for _, entry := range element.Entries {
			total += len([]byte(entry.Author)) + len([]byte(entry.Text))
		}
	}
	return total
}
