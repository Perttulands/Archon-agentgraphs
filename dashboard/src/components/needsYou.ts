/* Runs that need the operator, across missions (archon-n7u.29): open runs
 * waiting at a human gate or blocked. The navigator lists them first and
 * the page title counts them. */
import type { BoardSummary, RunStatusProjection } from './formationsTypes'

/** How many runs of the mission with this identity need the operator. */
export function needsYouCount(runs: RunStatusProjection[], missionId: string): number {
  return runs.filter(run => run.missionId === missionId).length
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
