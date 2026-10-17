/** Canvas or Flow is one preference for this browser, across missions and runs. */
export type BoardView = 'canvas' | 'flow'

const STORAGE_KEY = 'archon.missionView.v2'
const OLD_STORAGE_KEY = 'archon.missionView.v1'

/** Migrate once using the first opened mission's old preference. */
export function readBoardView(initialSlug: string): BoardView {
  try {
    const saved = window.localStorage.getItem(STORAGE_KEY)
    if (saved === 'canvas' || saved === 'flow') {
      window.localStorage.removeItem(OLD_STORAGE_KEY)
      return saved
    }
    let old: unknown
    try { old = JSON.parse(window.localStorage.getItem(OLD_STORAGE_KEY) || '{}') } catch { old = null }
    const view = old && typeof old === 'object' && !Array.isArray(old)
      && (old as Record<string, unknown>)[initialSlug] === 'flow' ? 'flow' : 'canvas'
    window.localStorage.setItem(STORAGE_KEY, view)
    window.localStorage.removeItem(OLD_STORAGE_KEY)
    return view
  } catch {
    return 'canvas'
  }
}

export function writeBoardView(view: BoardView): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, view)
  } catch {
    // Private mode and quota failures only lose the preference.
  }
}
