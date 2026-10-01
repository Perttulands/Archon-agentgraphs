/* Staffing is a sentence at the slot: "Worker 1 is [vanilla] on [Claude Code]
 * · [opus] · [low]". Each word is its own control. The first word is where he
 * types: free-order words ("cri ast", "sonnet high") fill the others as he
 * types, with an echo of how each was read. Enter staffs, Esc leaves the slot
 * as it was, and 1-6 set the effort. Clicking one word of a staffed slot opens
 * only that word, and the pick lands at once with its reason. This is variant
 * A, "Sentence", of the archon-n7u.56 prototypes (proto/staffing VariantA.tsx),
 * on the mission's own slots (archon-o7p.17). */
import { useEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { harnessName } from '../components/harnessIcons'
import type { Part } from './SlotFace'
import { staff, type StaffingHost } from './staffingActions'
import {
  allEfforts, applyParsed, effortRefusal, effortsFor, freshStaffing, matchRoles, modelWords, modelsOf, offCatalog, parseWords,
  policyLine, policyWords, roleName, roleSuggestion, withEffort, withFreeModel, withHarness, withModel, withRole,
  type Outcome, type Parsed, type Staffing, type Suggestion,
} from './staffingModel'
import type { OpenSentence, StaffingStore } from './staffingStore'

const WIDTH = 470
/** The tallest the window gets, a role grid of the whole catalog; it is placed so this always fits. */
const MAX_HEIGHT = 520

interface Row { id: string; label: string; hint?: string; tag?: string; disabled?: string; apply: () => void }

type Rect = { left: number; top: number; right: number; bottom: number }
const overlaps = (a: Rect, b: Rect) => a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom

/** What the window opens beside: the slot's card, or the node window or inspector that holds the sentence. */
function besideOf(anchor: Element): DOMRect {
  return (anchor.closest('.formation, .fwin, .agx-inspector') || anchor).getBoundingClientRect()
}

/** The next empty slot after this one on the canvas: the one he is likely to staff next. */
function nextEmptyRect(key: string): Rect | null {
  const slots = [...document.querySelectorAll<HTMLElement>('.world .slot[data-slot-key]')]
  const index = slots.findIndex(slot => slot.dataset.slotKey === key)
  const next = [...slots.slice(index + 1), ...slots.slice(0, Math.max(0, index))].find(slot => slot.classList.contains('empty'))
  return next ? besideOf(next) : null
}

/** Placed once, tall enough for its largest list, beside what it staffs, on the side that keeps the next empty slot clear. */
function placeWindow(anchor: Element, key: string): CSSProperties {
  const vw = window.innerWidth
  const vh = window.innerHeight
  const slot = anchor.getBoundingClientRect()
  const beside = besideOf(anchor)
  const top = Math.max(56, Math.min(slot.top - 8, vh - MAX_HEIGHT - 12))
  const next = nextEmptyRect(key)
  const sides = [beside.right + 12, beside.left - 12 - WIDTH]
  const box = (left: number): Rect => ({ left, top, right: left + WIDTH, bottom: top + MAX_HEIGHT })
  const onScreen = (left: number) => left >= 8 && left + WIDTH <= vw - 8
  const left = sides.find(side => onScreen(side) && !(next && overlaps(box(side), next)))
    ?? sides.find(onScreen)
    ?? Math.max(8, Math.min(beside.right + 12, vw - WIDTH - 8))
  return { left, top, width: WIDTH, maxHeight: MAX_HEIGHT }
}

function partValue(staffing: Staffing, part: Part): string {
  return part === 'role' ? staffing.role : staffing[part]
}

export function SentenceWindow({ store, host, open, saved }: {
  store: StaffingStore
  host: StaffingHost
  open: OpenSentence
  /** What the slot holds in the mission. */
  saved: Staffing | null
}) {
  const { ref } = open
  const { catalog } = host
  const current = store.current(ref.key, saved)
  const quick = Boolean(current) && Boolean(open.part)
  const [tokens, setTokens] = useState<Staffing>(() => current || freshStaffing(catalog, ref))
  const [tokenNote, setTokenNote] = useState<string | null>(null)
  const [tokenOffer, setTokenOffer] = useState<Suggestion | undefined>(undefined)
  // Whether he chose the effort by hand in this sentence, or before it.
  const [byHand, setByHand] = useState(() => Boolean(current) && store.effortByHand(ref.key, current?.effort || ''))
  const [text, setText] = useState('')
  const [list, setList] = useState<Part | null>(open.part)
  const [filter, setFilter] = useState('')
  const [hi, setHi] = useState(0)
  const [choosing, setChoosing] = useState(false)
  const [style] = useState(() => placeWindow(open.anchor, ref.key))
  const rootRef = useRef<HTMLDivElement | null>(null)
  const inputRef = useRef<HTMLInputElement | null>(null)
  const committed = useRef(false)

  // Free-order words typed into the first word, read against the sentence as it stands.
  const parsed: Parsed | null = useMemo(() => (text.trim() ? parseWords(catalog, text, tokens) : null), [catalog, text, tokens])
  const blocking = Boolean(parsed) && parsed!.issues.some(issue => issue.blocking)
  const typed = useMemo(() => (parsed && !blocking ? applyParsed(catalog, ref, tokens, parsed, byHand) : null), [blocking, byHand, catalog, parsed, ref, tokens])
  const draft = typed && !typed.refused ? typed.next : tokens
  const note = typed ? typed.note || null : tokenNote
  const offer = typed ? typed.offer : tokenOffer
  const draftByHand = byHand || Boolean(typed?.effortTyped)

  // The slot shows the sentence as it is composed.
  useEffect(() => { store.setDraft(ref.key, draft) }, [draft, ref.key, store])
  useEffect(() => () => { if (!committed.current) store.setDraft(ref.key, undefined) }, [ref.key, store])
  useEffect(() => { (list || choosing ? rootRef.current : inputRef.current)?.focus({ preventScroll: true }) }, [list, choosing])

  const cancel = () => { store.setDraft(ref.key, undefined); store.setOpen(null) }
  const commit = (value: Staffing, why: string | null | undefined, meta: { effortByHand: boolean; offer?: Suggestion }) => {
    committed.current = true
    // A landing the policy decided always says why: the effort a new slot or a new role took from it.
    const suggestion = roleSuggestion(ref, catalog.roles.find(role => role.id === value.role))
    const decided = !current || current.effort !== value.effort || current.role !== value.role
    const policy = decided && !meta.effortByHand && suggestion && suggestion.effort === value.effort ? `Effort ${suggestion.effort}: ${suggestion.reason}.` : ''
    const words = [why || '', policy && !(why || '').includes(policy) ? policy : ''].filter(Boolean).join(' ')
    void staff(store, host, ref, saved, value, { note: words || undefined, effortByHand: meta.effortByHand, offer: meta.offer })
    store.setOpen(null)
  }

  // A press anywhere else leaves the slot as it was.
  useEffect(() => {
    const onDown = (event: PointerEvent) => {
      const target = event.target as Element | null
      if (target?.closest('.staffing-window') || (target && open.anchor.contains(target))) return
      cancel()
    }
    window.addEventListener('pointerdown', onDown, true)
    return () => window.removeEventListener('pointerdown', onDown, true)
  })

  const roleEntry = catalog.roles.find(role => role.id === draft.role)
  const suggestion = roleSuggestion(ref, roleEntry)
  const efforts = allEfforts(catalog)

  /** A pick from a word's list: in a quick edit it lands at once, with the reason the full sentence gives. */
  const land = (outcome: Outcome, pickedEffort = false) => {
    if (outcome.refused) { setTokenNote(outcome.refused); return }
    setFilter('')
    setHi(0)
    setText('')
    const nextByHand = pickedEffort || byHand
    if (quick) { commit(outcome.next, outcome.note, { effortByHand: nextByHand, offer: outcome.offer }); return }
    setTokens(outcome.next)
    setTokenNote(outcome.note || null)
    setTokenOffer(outcome.offer)
    setByHand(nextByHand)
    setList(null)
  }

  const pickEffort = (effort: string) => land(withEffort(catalog, draft, effort), true)
  const landRole = (role: typeof roleEntry | null) => land(withRole(catalog, ref, draft, role || null, draftByHand))

  const rows = useMemo<Row[]>(() => {
    if (choosing && parsed?.ambiguous) {
      return parsed.ambiguous.roles.map(role => ({
        id: `choice:${role.id}`, label: role.name, hint: role.summary, tag: roleSuggestion(ref, role)?.effort,
        apply: () => {
          const resolved = { ...parsed, ambiguous: undefined, roleTouched: true, result: { ...parsed.result, role: role.id } }
          const outcome = applyParsed(catalog, ref, tokens, resolved, byHand)
          if (!outcome.refused) commit(outcome.next, outcome.note, { effortByHand: byHand || outcome.effortTyped, offer: outcome.offer })
        },
      }))
    }
    if (!list) return []
    const f = filter.toLowerCase()
    if (list === 'role') {
      // Roles staffed lately come first, then the rest by name.
      const recent = store.recentRoles.map(id => catalog.roles.find(role => role.id === id)).filter((role): role is NonNullable<typeof role> => Boolean(role))
      const roles = f ? matchRoles(catalog.roles, f) : [...recent, ...catalog.roles.filter(role => !store.recentRoles.includes(role.id))]
      const out: Row[] = []
      if (!f || 'vanilla'.startsWith(f)) out.push({ id: 'vanilla', label: 'vanilla', hint: 'no role: harness, model and effort only', apply: () => landRole(null) })
      for (const role of roles) {
        out.push({ id: role.id, label: role.name, hint: role.summary || role.kind, tag: roleSuggestion(ref, role)?.effort, apply: () => landRole(role) })
      }
      return out
    }
    if (list === 'harness') {
      return catalog.harnesses
        .map(harness => ({ id: harness.id, label: harnessName(harness.id) || harness.id, hint: (harness.models || []).map(model => model.id).join(', ') || 'its default model', apply: () => land(withHarness(catalog, draft, harness.id)) }))
        .filter(row => !f || row.label.toLowerCase().startsWith(f))
    }
    if (list === 'model') {
      const home = draft.harness
      const harnesses = [home, ...catalog.harnesses.map(harness => harness.id).filter(id => id !== home)]
      const out: Row[] = harnesses.flatMap(harness => [
        ...modelsOf(catalog, harness).map(model => ({
          id: `${harness}:${model.id}`, label: model.id, hint: harness === home ? harnessName(harness) : `${harnessName(harness)} (switches harness)`,
          apply: () => land(withModel(catalog, draft, harness, model.id)),
        })),
        ...(harness === home ? [{ id: `${harness}:`, label: 'default model', hint: `whatever ${harnessName(harness)} runs by default`, apply: () => land(withModel(catalog, draft, harness, '')) }] : []),
      ]).filter(row => !f || row.label.startsWith(f) || row.label.split('-').some(part => part.startsWith(f)))
      if (f && !out.length && /[-.\d]/.test(f)) out.push({ id: 'free', label: f, hint: 'use it: not in the catalog; the harness decides', tag: 'warning', apply: () => land(withFreeModel(draft, f)) })
      return out
    }
    return efforts.map(effort => {
      const refusal = effortRefusal(catalog, draft.harness, draft.model, effort)
      return { id: effort, label: effort, hint: policyWords(catalog, effort), tag: suggestion?.effort === effort ? 'suggested' : undefined, disabled: refusal || undefined, apply: () => pickEffort(effort) }
    }).filter(row => !f || row.label.startsWith(f))
  }, [choosing, parsed, list, filter, draft, catalog, ref, suggestion?.effort, tokens, byHand, efforts]) // eslint-disable-line react-hooks/exhaustive-deps

  // The effort list opens on the policy's suggestion, so Enter takes it; otherwise on the current effort.
  useEffect(() => {
    if (list !== 'effort') return
    const suggested = rows.findIndex(row => row.tag === 'suggested' && !row.disabled)
    setHi(suggested >= 0 ? suggested : Math.max(0, rows.findIndex(row => row.id === draft.effort)))
  }, [list]) // eslint-disable-line react-hooks/exhaustive-deps

  const openList = (part: Part) => { setChoosing(false); setList(part); setFilter(''); setHi(0) }

  const onKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    event.stopPropagation()
    const key = event.key
    const inInput = event.target === inputRef.current
    if (key === 'Escape') { event.preventDefault(); cancel(); return }
    const digit = Number(key)
    // 1-6 set the effort in one input whenever no words are being typed.
    if (digit >= 1 && digit <= efforts.length && !event.ctrlKey && !event.metaKey && !(inInput && text) && !filter && !choosing) {
      event.preventDefault()
      pickEffort(efforts[digit - 1])
      return
    }
    if (list || choosing) {
      const enabled = rows.map((row, index) => (row.disabled ? -1 : index)).filter(index => index >= 0)
      const cols = list === 'role' && !choosing ? 2 : 1
      if (key === 'ArrowDown' || key === 'ArrowUp' || (cols === 2 && (key === 'ArrowLeft' || key === 'ArrowRight'))) {
        event.preventDefault()
        const step = key === 'ArrowDown' ? cols : key === 'ArrowUp' ? -cols : key === 'ArrowRight' ? 1 : -1
        let next = hi + step
        if (!enabled.includes(next)) next = enabled[(enabled.indexOf(hi) + (step > 0 ? 1 : -1) + enabled.length) % enabled.length] ?? 0
        setHi(Math.max(0, Math.min(rows.length - 1, next)))
        return
      }
      if (key === 'Enter') { event.preventDefault(); const row = rows[hi]; if (row && !row.disabled) row.apply(); return }
      if (key === 'Backspace') { event.preventDefault(); if (filter) setFilter(filter.slice(0, -1)); else { setList(null); setChoosing(false) } return }
      if (key === 'Tab') { event.preventDefault(); setList(null); setChoosing(false); return }
      if (key.length === 1 && /\S/.test(key) && !event.ctrlKey && !event.metaKey) { event.preventDefault(); setFilter(filter + key); setHi(0) }
      return
    }
    if (key === 'Enter') {
      event.preventDefault()
      if (parsed?.ambiguous) { setChoosing(true); setHi(0); return }
      if (blocking) return
      commit(draft, note, { effortByHand: draftByHand, offer })
      return
    }
    if (key === 'ArrowDown' && inInput && !text) { event.preventDefault(); openList('role') }
  }

  const token = (part: Exclude<Part, 'role'>, value: string) => (
    <button
      type="button"
      tabIndex={-1}
      className={`staffing-token${list === part ? ' open' : ''}${!current || partValue(current, part) !== partValue(draft, part) ? ' changed' : ''}${part === 'model' && offCatalog(catalog, draft) ? ' warn' : ''}`}
      data-token={part}
      aria-label={`${part}: ${value}`}
      onClick={() => (list === part ? setList(null) : openList(part))}
    >{value}</button>
  )

  const roleWords = draft.role ? roleName(catalog, draft.role) : 'vanilla'
  return (
    <div className="staffing-window" style={style} ref={rootRef} tabIndex={-1} onKeyDown={onKeyDown} role="dialog" aria-label={`Staff ${ref.label}`} data-testid="staffing-sentence">
      <div className="staffing-sent">
        <span className="staffing-lead">{ref.label} is</span>
        <input
          ref={inputRef}
          className={`staffing-first${text ? ' typing' : ''}${list === 'role' ? ' open' : ''}${!current || current.role !== draft.role ? ' changed' : ''}`}
          value={text}
          size={Math.max(7, (text || roleWords).length)}
          placeholder={roleWords}
          spellCheck={false}
          aria-label="Role, or type role, harness, model and effort in any order"
          onChange={event => { setText(event.target.value); setChoosing(false) }}
          onClick={() => { if (!text) openList('role') }}
          data-token="role"
        />
        <span className="staffing-lead">on</span>
        {token('harness', harnessName(draft.harness) || draft.harness)}
        <span className="staffing-sep">·</span>
        {token('model', modelWords(draft.model))}
        <span className="staffing-sep">·</span>
        {token('effort', draft.effort)}
      </div>
      {parsed ? (
        <div className="staffing-read" data-testid="staffing-read">
          {parsed.read.map((read, index) => <span key={index} className="staffing-read-w"><b>{read.word}</b> {read.as}</span>)}
          {parsed.issues.map((issue, index) => <div key={`i${index}`} className="staffing-issue">{issue.text}{parsed.ambiguous && /could be/.test(issue.text) ? ' ↵ shows them.' : ''}</div>)}
          {parsed.alternatives.map((alternative, index) => (
            <button key={`a${index}`} type="button" className="staffing-alt" onClick={() => {
              const outcome = applyParsed(catalog, ref, tokens, { ...alternative, result: alternative.staffing, offCatalog: undefined }, byHand)
              if (!outcome.refused) commit(outcome.next, outcome.note, { effortByHand: byHand || outcome.effortTyped, offer: outcome.offer })
            }}>instead: {alternative.label}</button>
          ))}
        </div>
      ) : null}
      {list || choosing ? (
        <div className="staffing-list-wrap">
          <div className="staffing-list-hd">{choosing ? `“${parsed?.ambiguous?.word}” could be:` : filter ? `filter: ${filter}` : list === 'role' ? 'roles: type to filter, 1–6 effort' : list === 'effort' ? 'effort: ↵ takes the highlighted one' : 'type to filter'}</div>
          <div className={list === 'role' && !choosing ? 'staffing-list staffing-grid' : 'staffing-list'} role="listbox" aria-label={choosing ? 'Roles that match' : `Choose ${list}`}>
            {rows.map((row, index) => (
              <div
                key={row.id}
                role="option"
                aria-selected={index === hi}
                aria-disabled={Boolean(row.disabled)}
                className={`staffing-row${index === hi ? ' hi' : ''}${row.disabled ? ' disabled' : ''}${list && list !== 'role' && partValue(draft, list) === (row.id.split(':').pop() || row.id) ? ' current' : ''}${list === 'role' && (draft.role || 'vanilla') === row.id ? ' current' : ''}`}
                onPointerEnter={() => !row.disabled && setHi(index)}
                onClick={() => !row.disabled && row.apply()}
                data-row={row.id}
                title={row.hint}
              >
                <span className="staffing-row-l">{row.label}</span>
                {row.tag ? <span className={`staffing-tag${row.tag === 'suggested' ? ' sugg' : row.tag === 'warning' ? ' warn' : ''}`}>{row.tag}</span> : null}
                {list === 'role' && !choosing ? null : <span className="staffing-row-h">{row.disabled || row.hint}</span>}
              </div>
            ))}
            {rows.length === 0 ? <div className="staffing-row disabled"><span className="staffing-row-h">Nothing matches “{filter}”. Esc leaves the slot as it was.</span></div> : null}
          </div>
          {list === 'role' && !choosing && rows[hi]?.hint ? <div className="staffing-list-ft">{rows[hi].hint}</div> : null}
        </div>
      ) : null}
      <div className="staffing-policy" data-testid="staffing-policy">
        {note ? <span className="staffing-note">{note}</span>
          : suggestion ? <span>Policy suggests <b>{suggestion.effort}</b>: {suggestion.reason}.</span>
          : <span>Policy: {policyLine(catalog)}.</span>}
        {offCatalog(catalog, draft) ? <div className="staffing-issue">{draft.model}: not in the catalog; the harness decides.</div> : null}
        {!effortsFor(catalog, draft.harness, draft.model).length ? <div className="staffing-issue">{harnessName(draft.harness) || draft.harness} is not a harness Archon starts.</div> : null}
      </div>
      <div className="staffing-keys">↵ staffs · Esc leaves it as it was · 1–6 effort · type roles, models, efforts in any order</div>
    </div>
  )
}
