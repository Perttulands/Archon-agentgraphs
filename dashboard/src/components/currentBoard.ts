/**
 * The one current board that Boards and Agents share. The address bar's
 * ?board= names it, so a reload, a view switch or a shared link keeps it; this
 * device also remembers the last board used, so a fresh open with no query
 * lands there instead of on whichever board sorts first.
 */

import { readRunLink, runLinkSearch } from './formationsRunDiscovery'

const STORAGE_KEY = 'archon.currentBoard.v1'

export function rememberedBoard(): string {
  try {
    return window.localStorage.getItem(STORAGE_KEY) || ''
  } catch {
    return ''
  }
}

export function rememberBoardOnDevice(slug: string): void {
  if (!slug) return
  try {
    window.localStorage.setItem(STORAGE_KEY, slug)
  } catch {
    // Storage may be unavailable; the address bar still carries the board.
  }
}

/**
 * Chooses the board to open: the address bar's board, then the remembered
 * board, then the first board. `missingLinked` names an address-bar board that
 * no longer exists, so the caller can say so.
 */
export function chooseCurrentBoard(slugs: string[], search: string): { slug: string; missingLinked: string } {
  const linked = readRunLink(search).board
  if (linked && slugs.includes(linked)) return { slug: linked, missingLinked: '' }
  const remembered = rememberedBoard()
  const slug = remembered && slugs.includes(remembered) ? remembered : slugs[0] || ''
  return { slug, missingLinked: linked }
}

/**
 * Makes `slug` the current board on this device and in the address bar. A
 * pinned run belongs to its board, so ?run= survives only while the board
 * stays the same.
 */
export function rememberCurrentBoard(slug: string): void {
  if (!slug) return
  rememberBoardOnDevice(slug)
  const link = readRunLink(window.location.search)
  const search = runLinkSearch(window.location.search, { board: slug, run: link.board === slug ? link.run : '' })
  if (search !== window.location.search) window.history.replaceState(window.history.state, '', `${window.location.pathname}${search}${window.location.hash}`)
}
