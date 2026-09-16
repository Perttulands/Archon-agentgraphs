import type { RunStatusProjection } from './formationsTypes'

// A board's runs come from the daemon, so runs started by the CLI, an agent or
// another browser appear too. The address bar carries ?board=&run= so a link
// from a notification opens that run and a reload keeps it.

export interface RunLink {
  board: string
  run: string
}

export function readRunLink(search: string): RunLink {
  const params = new URLSearchParams(search)
  return { board: params.get('board') || '', run: params.get('run') || '' }
}

/** Returns the search string for a board and run, keeping unrelated parameters. */
export function runLinkSearch(search: string, link: RunLink): string {
  const params = new URLSearchParams(search)
  if (link.board) params.set('board', link.board)
  else params.delete('board')
  if (link.run) params.set('run', link.run)
  else params.delete('run')
  const next = params.toString()
  return next ? `?${next}` : ''
}

const attentionOrder: Record<string, number> = { waiting_human: 0, running: 1, blocked: 2 }

/** Non-final runs, the one needing attention first: waiting, running, blocked; newest first within each. */
export function openRunsByAttention(runs: RunStatusProjection[]): RunStatusProjection[] {
  return runs
    .filter(run => !run.final)
    .sort((a, b) => (attentionOrder[a.status] ?? 3) - (attentionOrder[b.status] ?? 3) || (a.runId < b.runId ? 1 : a.runId > b.runId ? -1 : 0))
}

/**
 * Chooses the run a board shows. A run the operator chose (by link or picker)
 * stays; otherwise the open run needing attention most; otherwise the run
 * already shown on this board, so a finished run stays visible.
 */
export function chooseBoardRun(input: {
  slug: string
  runs: RunStatusProjection[]
  pinnedRunId: string
  current: RunStatusProjection | null
}): string {
  if (input.pinnedRunId) return input.pinnedRunId
  const open = openRunsByAttention(input.runs)
  if (open.length) return open[0].runId
  if (input.current && (!input.current.boardSlug || input.current.boardSlug === input.slug)) return input.current.runId
  return ''
}

export function runChoiceLabel(run: RunStatusProjection): string {
  return [run.status, `…${run.runId.slice(-6)}`, run.beadId].filter(Boolean).join(' · ')
}
