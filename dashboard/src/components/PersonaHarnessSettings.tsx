import type { PersonaHarnessVariant, VariantSettingsPatch } from './formationsTypes'
import '../styles/persona-harness.css'

// A persona's harness variants decide what each seat runs: the harness CLI,
// its model and its effort. The Agents inspector, the persona editor and the
// New agent form all read and edit them through these pieces, and the command
// shown is the one the daemon's seat launcher renders, never a local copy.

export const HARNESS_DEFAULT_MODEL = 'harness default'
export const DEFAULT_EFFORT = 'medium'

/**
 * Model and effort being edited (blank means the harness default model and
 * medium), or, for a harness Archon cannot start, the launch string that
 * `archon agent spawn` runs.
 */
export interface VariantDraft {
  model: string
  effort: string
  launch: string
}

export function variantDraft(variant: PersonaHarnessVariant): VariantDraft {
  return { model: variant.model || '', effort: normalEffort(variant.effort), launch: variant.launch || '' }
}

/** Seats start from model and effort only for a harness Archon renders. */
export function isLaunchable(variant: PersonaHarnessVariant): boolean {
  return Boolean(variant.efforts?.length)
}

/** An unset effort and an explicit medium run the same; both read as the default. */
function normalEffort(effort: string | undefined): string {
  const value = (effort || '').trim()
  return value === DEFAULT_EFFORT ? '' : value
}

/** The patch for what changed, or null when the draft matches the card. */
export function variantChanges(variant: PersonaHarnessVariant, draft: VariantDraft): VariantSettingsPatch | null {
  const patch: VariantSettingsPatch = { id: variant.id }
  if (!isLaunchable(variant)) {
    const launch = draft.launch.trim()
    if (launch !== (variant.launch || '')) patch.launch = launch
    return patch.launch === undefined ? null : patch
  }
  const model = draft.model.trim()
  if (model !== (variant.model || '')) patch.model = model
  if (normalEffort(draft.effort) !== normalEffort(variant.effort)) patch.effort = normalEffort(draft.effort)
  return patch.model === undefined && patch.effort === undefined ? null : patch
}

export function EffortSelect({ id, efforts, value, onChange, disabled, label = 'Effort', className }: {
  id: string
  className?: string
  efforts: string[]
  value: string
  onChange: (effort: string) => void
  disabled?: boolean
  label?: string
}) {
  const current = normalEffort(value)
  // A hand-edited card may hold an effort the harness does not take; show it as it is.
  const unknown = current && !efforts.includes(current) ? current : ''
  return (
    <select id={id} className={className} aria-label={label} value={current} disabled={disabled} onChange={event => onChange(event.target.value)}>
      <option value="">{DEFAULT_EFFORT} (default)</option>
      {efforts.filter(effort => effort !== DEFAULT_EFFORT).map(effort => <option key={effort} value={effort}>{effort}</option>)}
      {unknown ? <option value={unknown}>{unknown} (not accepted by this harness)</option> : null}
    </select>
  )
}

/**
 * Model and effort inputs for one variant. A harness Archon cannot start takes
 * neither; its launch string is what `archon agent spawn` runs, so it stays
 * editable there.
 */
export function VariantSettingsFields({ idPrefix, harness, efforts, draft, onDraft, disabled }: {
  idPrefix: string
  harness: string
  efforts?: string[]
  draft: VariantDraft
  onDraft: (draft: VariantDraft) => void
  disabled?: boolean
}) {
  if (!efforts?.length) {
    return (
      <div className="ph-fields">
        <p className="ph-none ph-wide">Archon cannot start {harness} seats, so it takes no model or effort. <code>archon agent spawn</code> runs this launch command.</p>
        <label htmlFor={`${idPrefix}-launch`}>Launch</label>
        <input
          id={`${idPrefix}-launch`}
          aria-label={`${harness} launch command (archon agent spawn)`}
          value={draft.launch}
          spellCheck={false}
          disabled={disabled}
          onChange={event => onDraft({ ...draft, launch: event.target.value })}
        />
      </div>
    )
  }
  return (
    <div className="ph-fields">
      <label htmlFor={`${idPrefix}-model`}>Model</label>
      <input
        id={`${idPrefix}-model`}
        aria-label={`${harness} model`}
        value={draft.model}
        placeholder={HARNESS_DEFAULT_MODEL}
        spellCheck={false}
        disabled={disabled}
        onChange={event => onDraft({ ...draft, model: event.target.value })}
      />
      <label htmlFor={`${idPrefix}-effort`}>Effort</label>
      <EffortSelect
        id={`${idPrefix}-effort`}
        label={`${harness} effort`}
        efforts={efforts}
        value={draft.effort}
        disabled={disabled}
        onChange={effort => onDraft({ ...draft, effort })}
      />
    </div>
  )
}

/** What a slot starts as when this role is dragged onto it (the daemon's rendering), and why a legacy launch string is not it. Each slot's own settings decide what its seat runs. */
export function SeatLaunch({ variant, label = 'A slot this role is dragged onto starts as' }: { variant: PersonaHarnessVariant; label?: string }) {
  const launchable = isLaunchable(variant)
  return (
    <div className="ph-launch">
      {variant.seatLaunch ? (
        <>
          <span className="ph-launch-label">{label}</span>
          <code data-testid={`seat-launch-${variant.id}`}>{variant.seatLaunch}</code>
        </>
      ) : null}
      {variant.seatLaunchError ? <p className="ph-warn">{seatLaunchProblem(variant)}</p> : null}
      {variant.launch && launchable ? (
        <p className="ph-legacy">
          This card also holds a legacy launch string, <code>{variant.launch}</code>. Seats do not use it: each slot's own harness, model and effort decide what its
          seat runs.
        </p>
      ) : null}
    </div>
  )
}

function seatLaunchProblem(variant: PersonaHarnessVariant): string {
  const error = variant.seatLaunchError || ''
  if (error.startsWith('unsupported seat harness')) return `Archon cannot start ${variant.id} seats.`
  if (error.includes('not on PATH')) return `The daemon cannot find the ${variant.id} CLI on its PATH, so these seats would not start: ${error}`
  return error
}

/** "claude-opus-5 · effort low", or the defaults in words. */
export function variantSettingsSummary(variant: PersonaHarnessVariant): string {
  if (!variant.efforts?.length) return 'no model or effort'
  const effort = variant.effectiveEffort || variant.effort || DEFAULT_EFFORT
  return `${variant.model || HARNESS_DEFAULT_MODEL} · effort ${effort}${variant.effort ? '' : ' (default)'}`
}
