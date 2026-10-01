/* A slot as a row: its harness mark, its label, and the caption that says what
 * it runs, "Critic Judge" over "Codex · gpt-6-astra · xhigh". The canvas and
 * the Agents view draw every slot this way (archon-vps.2), so harness, model
 * and effort read in plain words wherever a slot appears. */
import type { MouseEvent as ReactMouseEvent, ReactNode } from 'react'
import { harnessGlyph } from '../components/formationsCockpitVisuals'
import { harnessName, harnessShortName } from '../components/harnessIcons'
import { captionText, type Staffing } from './staffingModel'

export type Part = 'role' | 'harness' | 'model' | 'effort'

function partValue(staffing: Staffing, part: Part): string {
  return part === 'role' ? staffing.role : staffing[part]
}

/** "Critic Judge | Codex · gpt-6-astra · xhigh", or "Claude Code · opus · low" for a vanilla slot. */
export function staffingWords(staffing: Staffing, roleName: (id: string) => string): string {
  return `${staffing.role ? `${roleName(staffing.role)} | ` : ''}${captionText(staffing)}`
}

/**
 * The caption: the role on one line ("vanilla" when there is none) and
 * "<Harness> · <model> · <effort>" on the next, effort in bold, the harness in
 * its short name ("Claude", "Codex"). Each line has a fixed height, so a slot
 * never changes size with its words; only the model gives way when it is long. While a choice is
 * composed it shows the draft, its changed words marked; a landing replays the
 * confirmation.
 */
export function SlotCaption({ shown, saved, drafting, landed, roleName, onPart }: {
  shown: Staffing | null
  /** What the slot holds now; words that differ from it are marked while drafting. */
  saved: Staffing | null
  drafting: boolean
  /** Bumps when a staffing lands, replaying the confirmation. */
  landed?: number
  roleName: (id: string) => string
  onPart?: (part: Part, event: ReactMouseEvent<HTMLElement>) => void
}) {
  if (!shown) {
    return (
      <span className={`slot-caption empty${drafting ? ' pending' : ''}`} data-testid="slot-caption" data-staffing="">
        <span className="slot-cap-role slot-open">open slot</span>
        <span className="slot-cap-line"><span className="slot-add">+ Agent</span></span>
      </span>
    )
  }
  const changed = (part: Part) => drafting && (!saved || partValue(saved, part) !== partValue(shown, part))
  const word = (part: Part, text: string, extra = '') => (
    <span
      className={`slot-w slot-w-${part}${changed(part) ? ' changed' : ''}${onPart ? ' clickable' : ''}${extra}`}
      data-part={part}
      onClick={onPart ? event => onPart(part, event) : undefined}
    >{text}</span>
  )
  const words = staffingWords(shown, roleName)
  return (
    <span
      key={landed || 0}
      title={`${shown.role ? roleName(shown.role) : 'vanilla'}\n${captionText(shown)}`}
      className={`slot-caption${drafting ? ' pending' : ''}${landed ? ' landed' : ''}`}
      data-testid="slot-caption"
      data-staffing={words}
    >
      <span className="slot-cap-role">{shown.role ? word('role', roleName(shown.role)) : word('role', 'vanilla', ' vanilla')}</span>
      <span className="slot-cap-line">
        {word('harness', harnessShortName(shown.harness) || shown.harness || 'no harness')}
        <span className="slot-dot"> · </span>
        {/* On the card a blank model reads "default"; the sentence and tooltip say "default model". */}
        {word('model', shown.model || 'default', shown.model ? '' : ' default')}
        <span className="slot-dot"> · </span>
        {word('effort', shown.effort || 'no effort')}
      </span>
    </span>
  )
}

/** The inside of a slot: ring with the harness mark, label line, caption. Hosts wrap it in their own element. */
export function SlotFace({ label, badge, staffing, caption, marks, landed }: {
  label: string
  badge?: number
  staffing: Staffing | null
  caption: ReactNode
  /** Short words beside the label: a model outside the catalog, the policy's offer. */
  marks?: ReactNode
  landed?: number
}) {
  return (
    <>
      <span className="slot-ring">
        {badge ? <span className="badge">{badge}</span> : null}
        {staffing
          ? <span className={`face${landed ? ' landed' : ''}`} key={landed || 0} title={harnessName(staffing.harness) || staffing.harness}>{harnessGlyph(staffing.harness) ?? <span className="face-none">?</span>}</span>
          : <span className="plus">+</span>}
      </span>
      <span className="slot-text">
        <span className="slot-label-line">
          <span className="slot-label">{label}</span>
          {marks}
        </span>
        {caption}
      </span>
    </>
  )
}
