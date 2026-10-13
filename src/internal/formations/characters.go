package formations

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// A Character is a saved staffing: a named, pictured combination of a role, a
// harness, a model and an effort that fills a slot in one pick. It is a stamp:
// the slot then owns its values, so editing or deleting a character changes
// no mission (archon-fh5c.7).
type Character struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Picture is a small image as a data URL; blank shows the name's initials.
	Picture string `json:"picture,omitempty"`
	// AgentID is the role; blank is a vanilla agent.
	AgentID string `json:"agentId,omitempty"`
	Harness string `json:"harness"`
	// Model blank is the harness default model.
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort"`
}

var ErrInvalidCharacter = errors.New("invalid character")

// characterPictureLimit bounds a picture's data URL; the dashboard sends a
// 96px image, far below it.
const characterPictureLimit = 96 * 1024

var characterPictureTypes = []string{"data:image/png;base64,", "data:image/jpeg;base64,", "data:image/webp;base64,"}

type charactersFile struct {
	Schema     int         `json:"schema"`
	Characters []Character `json:"characters"`
}

// CharacterStore keeps every character in characters.json beside the role cards.
type CharacterStore struct {
	AgentsDir string
	mu        sync.Mutex
}

func NewCharacterStore(agentsDir string) *CharacterStore {
	return &CharacterStore{AgentsDir: agentsDir}
}

func (s *CharacterStore) path() string {
	return filepath.Join(s.AgentsDir, "characters.json")
}

// List returns the characters by name.
func (s *CharacterStore) List() ([]Character, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

func (s *CharacterStore) read() ([]Character, error) {
	raw, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return []Character{}, nil
	}
	if err != nil {
		return nil, err
	}
	var file charactersFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidCharacter, s.path(), err)
	}
	if file.Schema != 1 {
		return nil, fmt.Errorf("%w: %s has schema %d, want 1", ErrUnsupportedSchema, s.path(), file.Schema)
	}
	if file.Characters == nil {
		file.Characters = []Character{}
	}
	return file.Characters, nil
}

// Create saves a new character; a blank id is made from the name.
func (s *CharacterStore) Create(character Character) (Character, error) {
	return s.save(character, true)
}

// Replace states an existing character in full.
func (s *CharacterStore) Replace(character Character) (Character, error) {
	return s.save(character, false)
}

func (s *CharacterStore) save(character Character, create bool) (Character, error) {
	character.Name = strings.TrimSpace(character.Name)
	character.Description = strings.TrimSpace(character.Description)
	character.ID = strings.TrimSpace(character.ID)
	if character.Name == "" {
		return Character{}, fmt.Errorf("%w: a character needs a name", ErrInvalidCharacter)
	}
	if character.ID == "" {
		character.ID = characterID(character.Name)
	}
	if err := validatePersonaID(character.ID); err != nil {
		return Character{}, fmt.Errorf("%w: id %q must be lowercase letters, digits and dashes", ErrInvalidCharacter, character.ID)
	}
	if character.AgentID != "" {
		if err := validatePersonaID(character.AgentID); err != nil {
			return Character{}, fmt.Errorf("%w: role %q is not a role id", ErrInvalidCharacter, character.AgentID)
		}
	}
	if err := validateRunSettings(fmt.Sprintf("character %q", character.Name), character.Harness, character.Model, character.Effort); err != nil {
		return Character{}, fmt.Errorf("%w: %s", ErrInvalidCharacter, strings.TrimPrefix(err.Error(), ErrInvalidSlotSettings.Error()+": "))
	}
	if character.Picture != "" {
		if len(character.Picture) > characterPictureLimit {
			return Character{}, fmt.Errorf("%w: the picture is larger than %d KB", ErrInvalidCharacter, characterPictureLimit/1024)
		}
		if !slices.ContainsFunc(characterPictureTypes, func(prefix string) bool { return strings.HasPrefix(character.Picture, prefix) }) {
			return Character{}, fmt.Errorf("%w: the picture must be a PNG, JPEG or WebP data URL", ErrInvalidCharacter)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	characters, err := s.read()
	if err != nil {
		return Character{}, err
	}
	at := slices.IndexFunc(characters, func(existing Character) bool { return existing.ID == character.ID })
	switch {
	case create && at >= 0:
		return Character{}, fmt.Errorf("%w: a character with the id %q exists; give this one another name or id", ErrAlreadyExists, character.ID)
	case create:
		characters = append(characters, character)
	case at < 0:
		return Character{}, fmt.Errorf("%w: character %q", ErrNotFound, character.ID)
	default:
		characters[at] = character
	}
	return character, s.write(characters)
}

// Delete removes a character; missions it once stamped keep their staffing.
func (s *CharacterStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	characters, err := s.read()
	if err != nil {
		return err
	}
	at := slices.IndexFunc(characters, func(existing Character) bool { return existing.ID == id })
	if at < 0 {
		return fmt.Errorf("%w: character %q", ErrNotFound, id)
	}
	return s.write(slices.Delete(characters, at, at+1))
}

func (s *CharacterStore) write(characters []Character) error {
	slices.SortFunc(characters, func(a, b Character) int {
		return strings.Compare(strings.ToLower(a.Name)+"\x00"+a.ID, strings.ToLower(b.Name)+"\x00"+b.ID)
	})
	raw, err := json.MarshalIndent(charactersFile{Schema: 1, Characters: characters}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.path(), append(raw, '\n'))
}

// characterID makes an id from a name: its letters and digits, dashed.
func characterID(name string) string {
	var id strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && id.Len() > 0 {
				id.WriteByte('-')
			}
			id.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	return id.String()
}
