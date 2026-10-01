/* Mission inputs (archon-o7p.3): the named values a run supplies, declared on
 * the Input card. A mission that declares none takes one required text input,
 * brief, described by the Input card's input hint, as the daemon does. */
import type { BoardDocument, MissionInput, MissionNode } from './formationsTypes'

/** What the brief field asks for when the mission gives no input hint of its own. */
export const DEFAULT_BRIEF_HINT = 'The input this run works on: the request, sketch or task its first step receives. The mission stays reusable; each run takes its own brief.'

/** The inputs a run of this mission supplies. */
export function missionRunInputs(card: MissionNode | undefined): MissionInput[] {
  if (card?.inputs?.length) return card.inputs
  return [{ name: 'brief', kind: 'text', required: true, description: card?.inputHint || '' }]
}

/** The mission's Input card, the one a run starts from. */
export function inputCardOf(board: BoardDocument | null | undefined): MissionNode | undefined {
  return board?.inputCards?.[0]
}

/** A field label: the name with its underscores read as spaces, capitalised. */
export function inputLabel(name: string): string {
  const words = name.replace(/_/g, ' ')
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/** The required inputs left blank, in declared order. */
export function missingInputs(inputs: MissionInput[], values: Record<string, string>): string[] {
  return inputs.filter(input => input.required && !(values[input.name] || '').trim()).map(input => input.name)
}

/** The values a start sends: blank ones are left out, and paths are trimmed. */
export function suppliedInputs(inputs: MissionInput[], values: Record<string, string>): Record<string, string> {
  const supplied: Record<string, string> = {}
  for (const input of inputs) {
    const value = values[input.name] || ''
    if (!value.trim()) continue
    supplied[input.name] = input.kind === 'text' ? value : value.trim()
  }
  return supplied
}
