import type { MenuItem } from './CanvasContextMenu'
import type { FormationPort, PortKind } from './formationsTypes'

export const PORT_KINDS: readonly PortKind[] = ['text', 'file', 'folder']

/** What a port carries; a port without a kind carries text. */
export const portKindOf = (port: Pick<FormationPort, 'kind'> | undefined): PortKind => port?.kind || 'text'

const CARRIED: Record<PortKind, string> = { text: 'text', file: 'a file', folder: 'a folder' }

/**
 * An output port's kind choices for its row menu: what it carries now as the
 * heading, then each other kind, as the End node offers its other outcome.
 */
export function portKindMenuItems(port: FormationPort, onSet: (kind: PortKind) => void): MenuItem[] {
  const current = portKindOf(port)
  return [
    { label: `Carries ${CARRIED[current]}`, head: true },
    ...PORT_KINDS.filter(kind => kind !== current).map(kind => ({ label: `Carry ${CARRIED[kind]} instead`, action: () => onSet(kind) })),
  ]
}
