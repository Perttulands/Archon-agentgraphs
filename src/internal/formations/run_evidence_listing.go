package formations

import (
	"sort"
	"strings"
)

// Keep a representative of every declared file or folder before filling the
// remaining budget with output files and finally the seat's other artifacts.
// Folder chips open the complete directory, including entries not listed here.
// The omitted count is a lower bound: the walk may have stopped at its cap.
func selectEvidenceArtifacts(entries []RunArtifactEntry, outputs []AttemptOutput) ([]RunArtifactEntry, int) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	groups := make([][]RunArtifactEntry, len(outputs)+1)
	for _, entry := range entries {
		group := len(outputs)
		longest := -1
		for i, output := range outputs {
			if output.Artifact == "" {
				continue
			}
			if entry.Name == output.Artifact {
				group = i
				break
			}
			if strings.HasPrefix(entry.Name, output.Artifact+"/") && len(output.Artifact) > longest {
				group, longest = i, len(output.Artifact)
			}
		}
		groups[group] = append(groups[group], entry)
	}
	selected := make([]RunArtifactEntry, 0, min(len(entries), EvidenceArtifactListMax))
	for i := range outputs {
		if len(groups[i]) > 0 {
			selected = append(selected, groups[i][0])
			groups[i] = groups[i][1:]
		}
	}
	for _, group := range groups {
		remaining := max(0, EvidenceArtifactListMax-len(selected))
		selected = append(selected, group[:min(len(group), remaining)]...)
	}
	return selected, len(entries) - len(selected)
}
