/* Pure visual helpers for the Archon cockpit: type taglines, inline SVG
   glyphs, and agent initials/role/state. Extracted from FormationsCockpit so
   the component focuses on stateful canvas logic. The Agents view shares the
   roster grouping and seat layout so both tabs draw agents the same way. */
import { Fragment } from 'react'
import type { ReactNode } from 'react'
import type { NodeRunState } from './formationsRunState'
import type { AgentProjection, BoardConnection, FormationNode, FormationSlot } from './formationsTypes'
import { harnessIcon } from './harnessIcons'

export function formationSummary(formation: FormationNode): string {
  return formation.brief?.goal?.replace(/\s+/g, ' ').trim() || 'Set a brief to describe this work.'
}

/** What feeds an input, in words: "from Map the territory, Framing review (fail)". */
export function inputFeedLabel(incoming: BoardConnection[], titleOf: (nodeId: string) => string): string {
  if (!incoming.length) return ''
  return `from ${incoming.map(connection => {
    const [nodeId, port] = connection.from.split(':')
    return port === 'fail' || port === 'judge' ? `${titleOf(nodeId)} (${port})` : titleOf(nodeId)
  }).join(', ')}`
}

/* Castle wall with an opening: gates are checkpoints work must pass through. */
export const GATE_SVG = (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true">
    <path d="M4 20V6h3.5v2.5h3V6h3v2.5h3V6H20v14" />
    <path d="M9.5 20v-4a2.5 2.5 0 015 0v4" />
  </svg>
)

/** An End node's mark: the final-state bullseye, a ring around a filled stop. */
export const END_SVG = (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
    <circle cx="12" cy="12" r="8.5" />
    <circle cx="12" cy="12" r="4.2" fill="currentColor" stroke="none" />
  </svg>
)

/* Harness product marks live in the shared library; this wrapper keeps the
   cockpit's call sites stable. */
export function harnessGlyph(harness: string | undefined | null): JSX.Element | null {
  return harnessIcon(harness)
}

export const PLAY_SVG = (
  <svg viewBox="0 0 24 24" fill="currentColor"><path d="M6 4l14 8-14 8z" /></svg>
)

export function initials(id: string): string {
  const cleaned = id.replace(/[^a-zA-Z0-9]/g, '')
  return (cleaned.slice(0, 2) || '?').toUpperCase()
}
/** What a roster row is: an unbound session, or a role of its kind. Roles carry no harness. */
export function agentRole(agent: AgentProjection): string {
  if (agent.unbound) return 'unbound'
  return agent.kind || 'role'
}
export function agentState(agent: AgentProjection): 'on' | 'idle' {
  return agent.liveness === 'live' || agent.assignable ? 'on' : 'idle'
}

export interface OutputRowStatus {
  label: string
  tone: 'idle' | 'running' | 'review' | 'done' | 'blocked'
}

/* A formation card's first OUT row says what the selected run has done with
   the node. Without a run there is nothing to report yet. */
export function outputRowStatus(runSelected: boolean, state: NodeRunState | undefined, hasOutput: boolean): OutputRowStatus {
  if (!runSelected) return { label: 'no output yet', tone: 'idle' }
  switch (state) {
    case 'running':
      return { label: 'running', tone: 'running' }
    case 'waiting':
      return { label: 'waiting', tone: 'review' }
    case 'blocked':
      return { label: 'blocked', tone: 'blocked' }
    case 'failed':
      return { label: 'failed', tone: 'blocked' }
    case 'done':
      return { label: hasOutput ? 'output ready' : 'done', tone: 'done' }
    default:
      return hasOutput ? { label: 'output ready', tone: 'done' } : { label: 'not reached', tone: 'idle' }
  }
}

/* Both rosters count roles the same way: how many there are, and how many staff
   a slot of this mission (every formation on it), with the live sessions where
   the view shows them. */
export function rolesInUseLabel(roles: number | string, inUse: number, live = 0): string {
  return [`${roles} role${roles === 1 ? '' : 's'}`, `${inUse} in use`, live ? `${live} live` : ''].filter(Boolean).join(' · ')
}

/** How many slots of these formations each role staffs. */
export function roleUses(formations: FormationNode[]): Map<string, number> {
  const uses = new Map<string, number>()
  for (const formation of formations) {
    for (const slot of formation.slots) if (slot.agentId) uses.set(slot.agentId, (uses.get(slot.agentId) || 0) + 1)
  }
  return uses
}

/** "in 2 slots": a role in use, said in words rather than by dimming it. */
export function inSlotsWords(count: number): string {
  return count ? `in ${count} slot${count === 1 ? '' : 's'}` : ''
}

/* Rosters list roles by name; roles carry no harness, so nothing groups them by one. */
export function byRoleName<T extends { id: string; displayName?: string }>(a: T, b: T): number {
  return (a.displayName || a.id).localeCompare(b.displayName || b.id)
}

/* Seat arrangement for each formation type. Callers draw the seats: the
   canvas card makes them drag handles, the Agents card makes them buttons. */
export function FormationSeats({ formation, renderSlot }: {
  formation: FormationNode
  renderSlot: (slot: FormationSlot, badge?: number) => ReactNode
}) {
  const slots = formation.slots
  const seat = (slot: FormationSlot, badge?: number) => <Fragment key={slot.id}>{renderSlot(slot, badge)}</Fragment>
  if (formation.type === 'peer') {
    return (
      <div className="huddle"><span className="hl">peers · no hierarchy</span>
        <div className="peers-row">{slots.map(slot => seat(slot))}</div>
      </div>
    )
  }
  if (formation.type === 'orchestrated') {
    const ctrl = slots.find(slot => slot.controller) || slots[0]
    const workers = slots.filter(slot => slot !== ctrl)
    return (
      <div className="orch">
        <div className="ctrl-wrap">{ctrl ? seat(ctrl) : null}</div>
        <div className="pool"><span className="pl">open slots</span>{workers.map(slot => seat(slot))}</div>
      </div>
    )
  }
  // Solo: every slot stays visible so none is hidden.
  return <div className="solo-body">{slots.map(slot => seat(slot))}</div>
}
