/* Gate editor — one dialog creates and edits a gate. A gate combines human,
 * judge (formation) and code kinds; code fields appear only with the code kind
 * and stay optional, because authoring saves drafts. */
import { useState } from 'react'
import type { CodeGateProfileDescriptor, GateNode } from './formationsTypes'
import '../styles/formations-gate-editor.css'

export type GateKind = 'code' | 'formation' | 'human'

export interface GateDraft {
  title: string
  kinds: GateKind[]
  criterion: string
  /** `${profileId}@${profileVersion}`, or '' to choose the check later. */
  profileKey: string
  checkValue: string
}

/** The updateGate/createGate fields a draft describes. */
export interface GateFields {
  title: string
  kinds: GateKind[]
  criterion: string
  check: string
  checkVersion: string
  checkValue: string
}

const KIND_ORDER: GateKind[] = ['human', 'formation', 'code']
const KIND_LABEL: Record<GateKind, string> = { human: 'Human', formation: 'Judge', code: 'Code' }
const KIND_HINT: Record<GateKind, string> = {
  human: 'An operator approves or rejects.',
  formation: 'Formations wired to the judge socket decide.',
  code: 'A registered check reads the routed output.',
}

const profileKeyOf = (profile: Pick<CodeGateProfileDescriptor, 'profileId' | 'profileVersion'>) => `${profile.profileId}@${profile.profileVersion}`

export function gateKindLabel(kind: string): string {
  return kind === 'formation' ? 'judge' : kind
}

export function draftFromGate(gate: GateNode): GateDraft {
  return {
    title: gate.title,
    kinds: KIND_ORDER.filter(kind => gate.kinds.includes(kind)),
    criterion: gate.criterion,
    profileKey: gate.check ? `${gate.check}@${gate.checkVersion || ''}` : '',
    checkValue: gate.checkValue || '',
  }
}

export function newGateDraft(profiles: CodeGateProfileDescriptor[]): GateDraft {
  return { title: 'Review gate', kinds: ['code'], criterion: '', profileKey: profiles[0] ? profileKeyOf(profiles[0]) : '', checkValue: '' }
}

/** Explicit fields for a draft: code fields are blank unless code is chosen. */
export function gateFieldsFromDraft(draft: GateDraft): GateFields {
  const code = draft.kinds.includes('code') && draft.profileKey !== ''
  const at = draft.profileKey.lastIndexOf('@')
  return {
    title: draft.title.trim(),
    kinds: KIND_ORDER.filter(kind => draft.kinds.includes(kind)),
    criterion: draft.criterion.trim(),
    check: code ? draft.profileKey.slice(0, at) : '',
    checkVersion: code ? draft.profileKey.slice(at + 1) : '',
    checkValue: code ? draft.checkValue.trim() : '',
  }
}

/** The stored fields of a gate, for undo. A gate without code keeps no check. */
export function gateFieldsFromGate(gate: GateNode): GateFields {
  const code = gate.kinds.includes('code')
  return {
    title: gate.title,
    kinds: gate.kinds.filter((kind): kind is GateKind => KIND_ORDER.includes(kind as GateKind)),
    criterion: gate.criterion,
    check: code ? gate.check || '' : '',
    checkVersion: code ? gate.checkVersion || '' : '',
    checkValue: code ? gate.checkValue || '' : '',
  }
}

export function GateKindChips({ gateId, kinds }: { gateId: string; kinds: string[] }) {
  return (
    <span className="gkinds" data-testid={`gate-kinds-${gateId}`}>
      {kinds.length ? kinds.map(kind => <span key={kind} className={`gkind gkind-${kind}`}>{gateKindLabel(kind)}</span>) : <span className="gkind gkind-none">no kind</span>}
    </span>
  )
}

export function GateEditorDialog({ mode, initial, profiles, hasJudgeChain, saving, onSave, onClose }: {
  mode: 'create' | 'edit'
  initial: GateDraft
  profiles: CodeGateProfileDescriptor[]
  hasJudgeChain: boolean
  saving: boolean
  onSave: (draft: GateDraft) => void
  onClose: () => void
}) {
  const [draft, setDraft] = useState<GateDraft>(initial)
  const profile = profiles.find(candidate => profileKeyOf(candidate) === draft.profileKey)
  const unknownProfile = draft.profileKey !== '' && !profile
  const toggle = (kind: GateKind) => setDraft(current => ({
    ...current,
    kinds: current.kinds.includes(kind) ? current.kinds.filter(item => item !== kind) : KIND_ORDER.filter(item => item === kind || current.kinds.includes(item)),
  }))
  const heading = mode === 'create' ? 'Create gate' : 'Edit gate'
  return (
    <div className="pop gate-editor" role="dialog" aria-label={heading} onPointerDown={event => event.stopPropagation()}>
      <div className="pop-head">
        <span className="pt">{heading}</span>
        <button className="x" type="button" aria-label="Close gate editor" disabled={saving} onClick={onClose}>x</button>
      </div>
      <form className="pop-body" onSubmit={event => { event.preventDefault(); onSave(draft) }}>
        <label htmlFor="cockpit-gate-title">Title</label>
        <input id="cockpit-gate-title" className="f" aria-label="Gate title" value={draft.title} disabled={saving}
          onChange={event => setDraft(current => ({ ...current, title: event.target.value }))} />

        <fieldset className="gate-kinds" disabled={saving}>
          <legend>Kinds</legend>
          {KIND_ORDER.map(kind => {
            const checked = draft.kinds.includes(kind)
            return (
              <label key={kind} className={`gate-kind${checked ? ' on' : ''}`} title={KIND_HINT[kind]}>
                <input type="checkbox" aria-label={`${KIND_LABEL[kind]} kind`} checked={checked}
                  disabled={checked && draft.kinds.length === 1} onChange={() => toggle(kind)} />
                <span>{KIND_LABEL[kind]}</span>
              </label>
            )
          })}
        </fieldset>
        {draft.kinds.includes('formation') && !hasJudgeChain ? (
          <p className="field-note">Attach a judge formation from the gate's judge socket. A run needs one.</p>
        ) : null}
        {!draft.kinds.includes('formation') && hasJudgeChain ? (
          <p className="field-note warn" role="note">Saving detaches the current judge chain.</p>
        ) : null}

        {draft.kinds.includes('code') ? (
          <>
            <label htmlFor="cockpit-gate-profile">Evaluator profile</label>
            <select id="cockpit-gate-profile" className="legacy-select" aria-label="Evaluator profile" value={draft.profileKey} disabled={saving}
              onChange={event => setDraft(current => ({ ...current, profileKey: event.target.value }))}>
              <option value="">Choose later</option>
              {unknownProfile ? <option value={draft.profileKey}>{draft.profileKey} (unregistered)</option> : null}
              {profiles.map(candidate => (
                <option key={profileKeyOf(candidate)} value={profileKeyOf(candidate)}>
                  {candidate.displayName} · {profileKeyOf(candidate)}
                </option>
              ))}
            </select>
            <label htmlFor="cockpit-gate-value">{profile?.parameterLabel || 'Value'}</label>
            <input id="cockpit-gate-value" className="f" aria-label={profile?.parameterLabel || 'Value'} value={draft.checkValue}
              placeholder="Optional while drafting" disabled={saving || !draft.profileKey}
              onChange={event => setDraft(current => ({ ...current, checkValue: event.target.value }))} />
          </>
        ) : null}

        <label htmlFor="cockpit-gate-criterion">Criterion</label>
        <textarea id="cockpit-gate-criterion" aria-label="Gate criterion" value={draft.criterion} disabled={saving}
          onChange={event => setDraft(current => ({ ...current, criterion: event.target.value }))} />

        <div className="pop-actions">
          <button className="cancel" type="button" aria-label="Cancel gate editing" disabled={saving} onClick={onClose}>Cancel</button>
          <button className="save" type="submit" disabled={saving}>
            {saving ? 'Saving…' : mode === 'create' ? 'Create gate' : 'Save gate'}
          </button>
        </div>
      </form>
    </div>
  )
}
