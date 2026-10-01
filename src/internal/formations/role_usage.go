package formations

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
)

// ErrRoleInUse marks a role a slot still names; deleting it would leave that
// slot pointing at a role that does not exist.
var ErrRoleInUse = errors.New("role_in_use")

// ErrBuiltinRole marks a built-in role, which has no card to delete.
var ErrBuiltinRole = errors.New("builtin_role")

// RoleUse is one slot that names a role.
type RoleUse struct {
	MissionID      string `json:"missionId"`
	MissionSlug    string `json:"missionSlug"`
	MissionTitle   string `json:"missionTitle"`
	FormationID    string `json:"formationId"`
	FormationTitle string `json:"formationTitle"`
	SlotID         string `json:"slotId"`
	SlotLabel      string `json:"slotLabel"`
}

// Words reads the use as the operator names it: "Delivery › Plan › Plan".
func (u RoleUse) Words() string {
	slot := u.SlotLabel
	if slot == "" {
		slot = u.SlotID
	}
	return u.MissionTitle + " › " + u.FormationTitle + " › " + slot
}

// RoleUsage lists every slot, in every mission, that names the role.
func (s *Store) RoleUsage(roleID string) ([]RoleUse, error) {
	missions, err := s.ListBoards()
	if err != nil {
		return nil, err
	}
	uses := []RoleUse{}
	for _, summary := range missions {
		mission, err := s.ReadBoard(summary.Slug)
		if err != nil {
			return nil, err
		}
		for _, formation := range mission.Formations {
			for _, slot := range formation.Slots {
				if slot.AgentID != roleID {
					continue
				}
				uses = append(uses, RoleUse{
					MissionID: mission.ID, MissionSlug: mission.Slug, MissionTitle: mission.Title,
					FormationID: formation.ID, FormationTitle: formation.Title,
					SlotID: slot.ID, SlotLabel: slot.Label,
				})
			}
		}
	}
	return uses, nil
}

// RoleUsageWords names the slots in one line for a refusal or a notice.
func RoleUsageWords(uses []RoleUse) string {
	words := make([]string, 0, len(uses))
	for _, use := range uses {
		words = append(words, use.Words())
	}
	return strings.Join(words, "; ")
}

// RoleInUseError names the slots that still use a role.
func RoleInUseError(roleID string, uses []RoleUse) error {
	slots := "slot"
	if len(uses) != 1 {
		slots = "slots"
	}
	return fmt.Errorf("%w: role %q staffs %d %s: %s; restaff them, or retire the role instead", ErrRoleInUse, roleID, len(uses), slots, RoleUsageWords(uses))
}

// DeletePersona removes a role's card from the agents directory. A built-in
// role has no card to delete; deleting a card that overrides one brings the
// built-in role back. The caller checks that no slot uses the role.
func (s *PersonaStore) DeletePersona(id, expectedETag string) (builtin bool, err error) {
	if err := validatePersonaID(id); err != nil {
		return false, err
	}
	_, builtin = builtinPresetPersona(id)
	err = s.withPersonaLock(id, func() error {
		raw, err := s.readPersonaRaw(id)
		if err != nil {
			if os.IsNotExist(err) {
				if builtin {
					return fmt.Errorf("%w: %q is a built-in role with no card to delete; retire it instead", ErrBuiltinRole, id)
				}
				return ErrNotFound
			}
			return err
		}
		if expectedETag == "" {
			return ErrPreconditionRequired
		}
		if expectedETag != etag(raw) {
			return ErrConflict
		}
		directory, err := s.openPersonaDirectory(false)
		if err != nil {
			return err
		}
		defer directory.Close()
		if err := syscall.Unlinkat(int(directory.Fd()), id+".toml"); err != nil {
			return err
		}
		// The lock is held through its open descriptor; its name goes with the card.
		_ = syscall.Unlinkat(int(directory.Fd()), id+".toml.lock")
		return directory.Sync()
	})
	return builtin, err
}
