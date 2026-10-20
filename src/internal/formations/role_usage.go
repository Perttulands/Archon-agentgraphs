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

// ErrRoleUsageIncomplete refuses to establish absence from unreadable missions.
var ErrRoleUsageIncomplete = errors.New("role_usage_incomplete")

// RoleUsageReport keeps known uses visible even when some mission sources
// cannot be read. Only a complete report can establish that a role is unused.
type RoleUsageReport struct {
	Usage      []RoleUse    `json:"usage"`
	Complete   bool         `json:"complete"`
	Unreadable []Unreadable `json:"unreadable"`
}

func (r RoleUsageReport) IncompleteReason() string {
	entries := make([]string, 0, len(r.Unreadable))
	for _, entry := range r.Unreadable {
		entries = append(entries, entry.Name+": "+entry.Reason)
	}
	return strings.Join(entries, "; ")
}

func (r RoleUsageReport) CheckUnused(roleID string) error {
	if len(r.Usage) > 0 {
		return RoleInUseError(roleID, r.Usage)
	}
	if !r.Complete {
		return fmt.Errorf("%w: cannot establish that role %q is unused; unreadable missions: %s", ErrRoleUsageIncomplete, roleID, r.IncompleteReason())
	}
	return nil
}

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
func (s *Store) RoleUsage(roleID string) (*RoleUsageReport, error) {
	missions, err := s.ListBoards()
	if err != nil {
		return nil, err
	}
	report := &RoleUsageReport{Usage: []RoleUse{}, Complete: true, Unreadable: []Unreadable{}}
	for _, summary := range missions {
		if summary.Broken != "" {
			report.Unreadable = append(report.Unreadable, Unreadable{Name: summary.Slug, Reason: summary.Broken})
			continue
		}
		mission, err := s.ReadBoard(summary.Slug)
		if err != nil {
			report.Unreadable = append(report.Unreadable, Unreadable{Name: summary.Slug, Reason: err.Error()})
			continue
		}
		for _, formation := range mission.Formations {
			for _, slot := range formation.Slots {
				if slot.AgentID != roleID {
					continue
				}
				report.Usage = append(report.Usage, RoleUse{
					MissionID: mission.ID, MissionSlug: mission.Slug, MissionTitle: mission.Title,
					FormationID: formation.ID, FormationTitle: formation.Title,
					SlotID: slot.ID, SlotLabel: slot.Label,
				})
			}
		}
	}
	report.Complete = len(report.Unreadable) == 0
	return report, nil
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
		return s.removePersonaFile(id)
	})
	return builtin, err
}

// removePersonaFile unlinks a role's card and its lock; the caller holds the
// lock through its open descriptor.
func (s *PersonaStore) removePersonaFile(id string) error {
	directory, err := s.openPersonaDirectory(false)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := syscall.Unlinkat(int(directory.Fd()), id+".toml"); err != nil {
		return err
	}
	_ = syscall.Unlinkat(int(directory.Fd()), id+".toml.lock")
	return directory.Sync()
}
