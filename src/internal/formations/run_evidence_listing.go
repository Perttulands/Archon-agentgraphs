package formations

import (
	"path"
	"sort"
	"strings"
	"unicode"
)

// Keep a representative of every declared file or folder before filling the
// remaining budget with output files and finally the seat's other artifacts.
// Folder chips open the complete directory, including entries not listed here.
// The omitted count is a lower bound: the walk may have stopped at its cap.
func selectEvidenceArtifacts(entries []RunArtifactEntry, outputs []AttemptOutput, evidence ...string) ([]RunArtifactEntry, int) {
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
	priority := func(name string, finals map[string]bool) int {
		base := strings.ToLower(path.Base(name))
		ext := path.Ext(base)
		if _, image := evidenceImageTypes[ext]; image {
			for _, cited := range evidence {
				for _, word := range strings.FieldsFunc(cited, func(r rune) bool {
					return unicode.IsSpace(r) || strings.ContainsRune("\"'<>()[\x60]", r)
				}) {
					if strings.HasSuffix(strings.TrimRight(word, ".,;:!?"), "/"+path.Base(name)) {
						return 0
					}
				}
			}
			if strings.Contains(base, "contact") || strings.Contains(base, "sheet") {
				return 1
			}
		}
		switch ext {
		case ".mp4", ".webm", ".glb", ".gltf", ".fbx":
			return 1
		}
		if finals[name] {
			return 2
		}
		return 3
	}
	for _, group := range groups {
		finals := make(map[string]bool)
		for _, entry := range group {
			base := strings.ToLower(path.Base(entry.Name))
			_, image := evidenceImageTypes[path.Ext(base)]
			if len(finals) < 3 && image && strings.Contains(strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(" "+base+" "), " final ") && priority(entry.Name, nil) == 3 {
				finals[entry.Name] = true
			}
		}
		// Keep reviewable media in large output folders before filling the cap
		// with frames, textures and scratch files. Preserve lexical ties.
		sort.SliceStable(group, func(i, j int) bool { return priority(group[i].Name, finals) < priority(group[j].Name, finals) })
		remaining := max(0, EvidenceArtifactListMax-len(selected))
		selected = append(selected, group[:min(len(group), remaining)]...)
	}
	return selected, len(entries) - len(selected)
}
