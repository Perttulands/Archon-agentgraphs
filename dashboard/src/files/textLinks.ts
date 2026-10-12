// Matcher adapted from CHROTE beads/beadIds.ts. Archon has no project catalog:
// match the ID shape for copying, without pretending to know its owning store.
import { copyTextToClipboard } from '../utils/clipboard'

// A one-word store prefix, a hyphen, a 3-5 character base-36 token, then every
// `.N` child segment: archon-fh5c, aqg-0gy.72.2. An ID may be followed by a
// hyphenated label (aqg-0gy.72.2-round1) but never starts inside a longer word.
const BEAD_ID = /(?<![\w/-])[a-z][a-z0-9]{1,7}-([a-z0-9]{3,5})((?:\.\d+)*)(?!\w|\.\d)/g

// Without a catalog of prefixes, "low-poly" has an ID's shape. A token of only
// letters that could be read aloud is taken for a word unless child segments
// follow it; that leaves a bare root such as archon-egg unlinked, which costs
// less than linking prose.
const wordLike = (token: string) => !/\d/.test(token) && /[aeiouy]/.test(token)

export function findBeadIds(text: string) {
  return Array.from(text.matchAll(BEAD_ID))
    .filter(match => match[2] || !wordLike(match[1]))
    .map(match => ({ id: match[0], index: match.index }))
}
export function openPath(path: string, anchor?: Element) {
  document.dispatchEvent(new CustomEvent('archon-open-path', { detail: { path, anchor } }))
}
export async function copyBeadId(id: string) { return copyTextToClipboard(id) }
