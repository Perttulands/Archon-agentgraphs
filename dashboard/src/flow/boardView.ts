/**
 * Which way each board is shown in Boards, Canvas or Flow, remembered on this
 * device per board.
 */

export type BoardView = 'canvas' | 'flow'

const STORAGE_KEY = 'archon.boardView.v1'

function readViews(): Record<string, unknown> {
  try {
    const parsed: unknown = JSON.parse(window.localStorage.getItem(STORAGE_KEY) || '{}')
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed as Record<string, unknown> : {}
  } catch {
    return {}
  }
}

export function readBoardView(slug: string): BoardView {
  return slug && readViews()[slug] === 'flow' ? 'flow' : 'canvas'
}

export function writeBoardView(slug: string, view: BoardView): void {
  if (!slug) return
  const views = readViews()
  if (view === 'flow') views[slug] = 'flow'
  else delete views[slug]
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(views))
  } catch {
    // Private mode and quota failures only lose the preference.
  }
}
