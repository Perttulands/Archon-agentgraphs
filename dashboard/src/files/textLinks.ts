// Matcher adapted from CHROTE beads/beadIds.ts. Archon has no project catalog:
// match the ID shape for copying, without pretending to know its owning store.
import { copyTextToClipboard } from '../utils/clipboard'
export function findBeadIds(text: string) {
  return Array.from(text.matchAll(/(?<![\w/-])[a-z][a-z0-9]*(?:-[a-z][a-z0-9]*)*-[a-z0-9]{3,6}(?:\.\d+)*(?![\w-])/g), match => ({ id: match[0], index: match.index }))
}
export function openPath(path: string, anchor?: Element) {
  document.dispatchEvent(new CustomEvent('archon-open-path', { detail: { path, anchor } }))
}
export async function copyBeadId(id: string) { return copyTextToClipboard(id) }
