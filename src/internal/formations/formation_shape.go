package formations

import "fmt"

// formationSlotShapeFinding checks authored shape without normalizing a snapshot.
// Solo's controller flag is legal; only peer and orchestrated constrain it.
func formationSlotShapeFinding(formation FormationNode) *BoardFinding {
	problem := func(code, format string, args ...any) *BoardFinding {
		return &BoardFinding{Code: code, NodeID: formation.ID, Message: fmt.Sprintf(format, args...)}
	}
	if len(formation.Slots) == 0 {
		return problem(FindingFormationWithoutSlots, "formation %q has no slots; add a slot and staff it", formation.ID)
	}
	controllers := 0
	for _, slot := range formation.Slots {
		if slot.Controller {
			controllers++
		}
	}
	switch formation.Type {
	case FormationTypeSolo:
		if len(formation.Slots) != 1 {
			return problem(FindingInvalidFormationShape, "solo formation %q needs exactly one slot; it has %d", formation.ID, len(formation.Slots))
		}
	case FormationTypePeer:
		if len(formation.Slots) < 2 || controllers != 0 {
			return problem(FindingInvalidFormationShape, "peer formation %q needs at least two slots and no controllers; it has %d slots and %d controllers", formation.ID, len(formation.Slots), controllers)
		}
	case FormationTypeOrchestrated:
		if controllers != 1 {
			return problem(FindingOrchestratedController, "orchestrated formation %q needs exactly one controller slot; it has %d", formation.ID, controllers)
		}
		if len(formation.Slots) < 2 {
			return problem(FindingOrchestratedController, "orchestrated formation %q needs a worker slot beside its controller", formation.ID)
		}
	}
	return nil
}
