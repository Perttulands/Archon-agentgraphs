/* Runs that need the operator, across missions (archon-n7u.29): open runs
 * waiting at a human gate or blocked. The mission picker counts them per
 * mission, the page title counts them all, and the run bar offers the next. */
import type { BoardSummary, RunStatusProjection } from './formationsTypes'

/** How many runs of the mission with this identity need the operator. */
export function needsYouCount(runs: RunStatusProjection[], missionId: string): number {
  return runs.filter(run => run.missionId === missionId).length
}

/** The mission picker's label: its title, and how many of its runs need you. */
export function missionPickLabel(summary: BoardSummary, runs: RunStatusProjection[]): string {
  if (summary.broken) return `${summary.slug} · cannot be read`
  const count = needsYouCount(runs, summary.id)
  const title = summary.title || summary.slug
  return count ? `${title} · ${count} need${count === 1 ? 's' : ''} you` : title
}

/** The runs of missions that still exist; a deleted mission's runs are not shown. */
export function runsOfLiveMissions(runs: RunStatusProjection[], missions: BoardSummary[]): RunStatusProjection[] {
  const live = new Set(missions.map(mission => mission.id))
  return runs.filter(run => !run.missionId || live.has(run.missionId))
}

/** The page title: how many runs need you, then the mission. */
export function pageTitle(runs: RunStatusProjection[], missionTitle: string): string {
  return `${runs.length ? `(${runs.length}) ` : ''}${missionTitle ? `${missionTitle} · ` : ''}Archon`
}

/**
 * The runs, other than the one shown, that need you, the most urgent first:
 * waiting at a gate before blocked, the longest waiting first. Runs of missions
 * that no longer exist are left out.
 */
export function otherRunsNeedingYou(runs: RunStatusProjection[], shownRunId: string, missions: BoardSummary[]): RunStatusProjection[] {
  const since = (run: RunStatusProjection) => run.waitingGates?.[0]?.requestedAt || run.updatedAt || run.startedAt || ''
  return runsOfLiveMissions(runs, missions)
    .filter(run => run.runId !== shownRunId)
    .sort((a, b) => Number(b.status === 'waiting_human') - Number(a.status === 'waiting_human') || since(a).localeCompare(since(b)))
}
