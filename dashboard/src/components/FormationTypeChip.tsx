/* Formation type chip and the type change choices it offers. Changing to solo
 * with more than one staffed slot offers one choice per staffed slot, because
 * the server never drops a bound agent without being told which slot stays. */
import type { MouseEvent as ReactMouseEvent } from 'react'
import type { FormationNode, FormationType } from './formationsTypes'
import '../styles/formations-node-editing.css'

export interface FormationTypeChoice {
  label: string
  type: FormationType
  keepSlotId?: string
}

const TYPE_LABELS: Record<FormationType, string> = { solo: 'Solo', peer: 'Peer', orchestrated: 'Orchestrated' }

export function formationTypeChoices(formation: FormationNode): FormationTypeChoice[] {
  const choices: FormationTypeChoice[] = []
  for (const type of Object.keys(TYPE_LABELS) as FormationType[]) {
    if (type === formation.type) continue
    const staffed = formation.slots.filter(slot => slot.agentId)
    if (type === 'solo' && staffed.length > 1) {
      for (const slot of staffed) {
        choices.push({ label: `Solo, keeping ${slot.label || slot.id} (${slot.agentId})`, type, keepSlotId: slot.id })
      }
      continue
    }
    choices.push({ label: TYPE_LABELS[type], type })
  }
  return choices
}

export function FormationTypeChip({ formation, onOpen }: {
  formation: FormationNode
  onOpen: (event: ReactMouseEvent<HTMLButtonElement>) => void
}) {
  return (
    <button
      type="button"
      className="ftype"
      data-testid={`formation-type-${formation.id}`}
      aria-label={`Change type of ${formation.title || 'formation'} (${formation.type})`}
      title="Change formation type"
      onPointerDown={event => event.stopPropagation()}
      onDoubleClick={event => event.stopPropagation()}
      onClick={event => { event.stopPropagation(); onOpen(event) }}
    >{formation.type}</button>
  )
}
