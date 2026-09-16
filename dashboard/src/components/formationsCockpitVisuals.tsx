/* Pure visual helpers for the Formations cockpit: type taglines, inline SVG
   glyphs, and agent initials/role/state. Extracted from FormationsCockpit so
   the component focuses on stateful canvas logic. The Agents view shares the
   roster grouping and seat layout so both tabs draw agents the same way. */
import { Fragment } from 'react'
import type { ReactNode } from 'react'
import type { AgentProjection, FormationNode, FormationSlot } from './formationsTypes'
import { harnessIcon } from './harnessIcons'

export function formationSummary(formation: FormationNode): string {
  return formation.brief?.goal?.replace(/\s+/g, ' ').trim() || 'Set a brief to describe this work.'
}

/* Castle wall with an opening: gates are checkpoints work must pass through. */
export const GATE_SVG = (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true">
    <path d="M4 20V6h3.5v2.5h3V6h3v2.5h3V6H20v14" />
    <path d="M9.5 20v-4a2.5 2.5 0 015 0v4" />
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
export function agentRole(agent: AgentProjection): string {
  return agent.harnessDefault || (agent.unbound ? 'unbound' : 'agent')
}
export function agentState(agent: AgentProjection): 'on' | 'idle' {
  return agent.liveness === 'live' || agent.assignable ? 'on' : 'idle'
}

export interface RosterSection<T> {
  id: 'codex' | 'claude' | 'other'
  label: string
  agents: T[]
}

/* Roster sidebars list Codex agents, then Claude, then everything else. */
export function groupRosterByHarness<T extends { harnessDefault?: string }>(agents: T[]): RosterSection<T>[] {
  const codex = agents.filter(agent => (agent.harnessDefault || '').toLowerCase().includes('codex'))
  const claude = agents.filter(agent => (agent.harnessDefault || '').toLowerCase().includes('claude'))
  const other = agents.filter(agent => !codex.includes(agent) && !claude.includes(agent))
  const sections: RosterSection<T>[] = [
    { id: 'codex', label: 'Codex', agents: codex },
    { id: 'claude', label: 'Claude', agents: claude },
    { id: 'other', label: 'Other', agents: other },
  ]
  return sections.filter(section => section.agents.length > 0)
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
  if (formation.type === 'flow') {
    return (
      <div className="flow-row">
        {slots.map((slot, index) => (
          <div key={slot.id} style={{ display: 'flex', alignItems: 'flex-start', gap: 6 }}>
            {seat(slot, index + 1)}
            {index < slots.length - 1 ? (
              <div className="flow-arrow"><svg viewBox="0 0 26 12" fill="none" stroke="currentColor" strokeWidth="1.6"><path d="M0 6h22" /><path d="M19 2l4 4-4 4" /></svg></div>
            ) : null}
          </div>
        ))}
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
  return <div className="solo-body">{slots[0] ? seat(slots[0]) : null}</div>
}
