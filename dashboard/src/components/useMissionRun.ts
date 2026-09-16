import { useEffect, useState } from 'react'
import { fetchRunProblems } from '../evidence/runEvidenceApi'
import { fetchBoardRuns, fetchRunEvents } from './formationsApi'
import { openRunsByAttention } from './formationsRunDiscovery'
import type { RunEvent, RunStatusProjection } from './formationsTypes'

// The Agents tab shows a mission's run read-only. Runs come from the daemon, as
// on Boards, so a run started by the CLI or another browser appears; Boards is
// where the operator acts on it.

export interface MissionRunState {
  run: RunStatusProjection | null
  /** Open runs of this mission; more than one means Boards offers a picker. */
  openCount: number
  events: RunEvent[]
  /** The recorded reason for the latest block. */
  blockReason: string
}

const noRun: MissionRunState = { run: null, openCount: 0, events: [], blockReason: '' }

/** The mission's open run needing attention most, otherwise its newest run. */
export function chooseMissionRun(runs: RunStatusProjection[], missionId: string): { run: RunStatusProjection | null; openCount: number } {
  const mine = runs.filter(run => run.missionId === missionId)
  const open = openRunsByAttention(mine)
  if (open.length) return { run: open[0], openCount: open.length }
  const newest = [...mine].sort((a, b) => (a.runId < b.runId ? 1 : a.runId > b.runId ? -1 : 0))[0] || null
  return { run: newest, openCount: 0 }
}

/** The ?board=&run= link that opens a run on the Boards tab. */
export function boardsRunHref(board: string, runId: string): string {
  const params = new URLSearchParams({ board })
  if (runId) params.set('run', runId)
  return `?${params.toString()}`
}

async function latestBlockReason(run: RunStatusProjection, events: RunEvent[]): Promise<string> {
  const block = [...events].reverse().find(event => event.type === 'run_blocked')
  if (!block) return ''
  try {
    return (await fetchRunProblems(run.runId)).find(problem => problem.seq === block.seq)?.reason.text || ''
  } catch {
    return ''
  }
}

export function useMissionRun(board: string, missionId: string, pollMs = 5000): MissionRunState {
  const [state, setState] = useState<MissionRunState>(noRun)
  useEffect(() => {
    setState(noRun)
    if (!board || !missionId) return
    let cancelled = false
    const refresh = async () => {
      let runs: RunStatusProjection[]
      try {
        runs = await fetchBoardRuns(board)
      } catch {
        return // keep what is shown; the next poll retries
      }
      const { run, openCount } = chooseMissionRun(runs, missionId)
      if (!run) {
        if (!cancelled) setState(noRun)
        return
      }
      const events = await fetchRunEvents(run.runId).catch(() => [] as RunEvent[])
      const blockReason = run.status === 'blocked' ? await latestBlockReason(run, events) : ''
      if (!cancelled) setState({ run, openCount, events, blockReason })
    }
    void refresh()
    const timer = window.setInterval(() => { void refresh() }, pollMs)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [board, missionId, pollMs])
  return state
}
