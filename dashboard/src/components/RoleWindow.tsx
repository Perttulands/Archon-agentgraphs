/* A role opened from the rail, in a floating window beside it: its role text,
 * and the slots of this mission it staffs, each with what that slot runs
 * (archon-n7u.38). A role carries no harness, model or effort; its slots do. */
import FloatingWindow from '../windows/FloatingWindow'
import '../styles/formations-roster.css'
import type { WindowRect } from '../windows/windowGeometry'
import { captionText, staffingOf } from '../staffing/staffingModel'
import { agentRole } from './formationsCockpitVisuals'
import type { AgentProjection, FormationNode } from './formationsTypes'

export function roleWindowId(roleId: string): string {
  return `role:${roleId}`
}

export default function RoleWindow({ role, formations, anchor, onOpenNode, onEdit, onClose }: {
  role: AgentProjection
  /** The mission's formations, to find the slots this role staffs. */
  formations: FormationNode[]
  /** The rail row it opened from. */
  anchor?: () => WindowRect | null
  onOpenNode: (formationId: string) => void
  onEdit: (trigger: HTMLElement) => void
  onClose: () => void
}) {
  const name = role.displayName || role.id
  const uses = formations.flatMap(formation => formation.slots
    .filter(slot => slot.agentId === role.id)
    .map(slot => ({ formation, slot, staffing: staffingOf(slot) })))
  return (
    <FloatingWindow
      id={roleWindowId(role.id)}
      kind="role"
      title={`Role · ${name}`}
      label={`role ${name}`}
      defaultSize={{ width: 400, height: 420 }}
      anchor={anchor}
      anchorKind="control"
      className="role-window"
      onClose={onClose}
    >
      <div className="role-window-body">
        <p className="role-window-kind">{[agentRole(role), role.preset ? (role.customized ? 'custom preset' : 'preset') : ''].filter(Boolean).join(' · ')}</p>
        <p className={`role-window-summary${role.summary ? '' : ' placeholder'}`}>{role.summary || 'This role has no summary yet.'}</p>
        {role.tags?.length ? <p className="role-window-tags">{role.tags.join(' · ')}</p> : null}
        <section aria-label="Slots this role staffs">
          <h3>In this mission</h3>
          {uses.length ? (
            <ul className="role-window-uses">
              {uses.map(({ formation, slot, staffing }) => (
                <li key={`${formation.id}:${slot.id}`}>
                  <button type="button" className="role-window-open" aria-label={`Open ${formation.title || formation.id}`} onClick={() => onOpenNode(formation.id)}>
                    {formation.title || formation.id} · {slot.label || slot.id}
                  </button>
                  {staffing ? <span className="role-window-runs">{captionText(staffing)}</span> : null}
                </li>
              ))}
            </ul>
          ) : (
            <p className="role-window-none">Not in use. Drag it onto a slot, or pick it in a slot&rsquo;s sentence.</p>
          )}
        </section>
        <div className="role-window-actions">
          <button type="button" className="role-window-edit" onClick={event => onEdit(event.currentTarget)}>Edit role</button>
        </div>
      </div>
    </FloatingWindow>
  )
}
