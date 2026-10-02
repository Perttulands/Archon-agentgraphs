/* Staffing is a sentence at the slot: "Worker 1 is [vanilla] on [Claude Code]
 * · [opus] · [low]". Each word is its own control. The first word is where the
 * operator types: free-order words ("cri ast", "sonnet high") fill the others
 * as they type, with an echo of how each was read. Enter staffs, Esc leaves the slot
 * as it was, and 1-6 set the effort. Clicking one word of a staffed slot opens
 * only that word, and the pick lands at once with its reason. This is variant
 * A, "Sentence", of the archon-n7u.56 prototypes (proto/staffing VariantA.tsx),
 * on the mission's own slots (archon-o7p.17). */
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { harnessName } from '../components/harnessIcons'
import type { Part } from './SlotFace'
import { staff, type StaffingHost } from './staffingActions'
import {
  allEfforts, applyParsed, effortRefusal, effortsFor, freshStaffing, matchRoles, modelWords, modelsOf, offCatalog, parseWords,
  policyLine, policyWords, roleName, roleSuggestion, withEffort, withFreeModel, withHarness, withModel, withRole,
  type EffortHold, type Outcome, type Parsed, type Staffing, type Suggestion,
} from './staffingModel'
import { placeDrop, viewportStage, type DropPlace, type StaffingStage } from './staffingPlacement'
import type { OpenSentence, StaffingStore } from './staffingStore'

const WIDTH = 440

/**
 * The window keeps one height from opening to closing, so it never jumps as
 * lists open and close in it: the sentence with room for the list it opens
 * with, or the role list when it opens on the whole sentence. That is about
 * eight lines of the list, or all of a shorter one, such as the six efforts.
 */
const LIST_LINES = 8

interface Row { id: string; label: string; hint?: string; tag?: string; disabled?: string; apply: () => void }

/** A list's lines of rows (a grid puts two rows on a line) and the pitch between them. */
function rowLines(listEl: HTMLElement): { pitch: number; lines: number } {
  const rows = [...listEl.querySelectorAll<HTMLElement>('.staffing-row')]
  const tops = [...new Set(rows.map(row => row.offsetTop))].sort((a, b) => a - b)
  const pitch = tops.length > 1 ? tops[1] - tops[0] : rows[0]?.offsetHeight || 22
  return { pitch: Math.max(1, pitch), lines: Math.max(1, tops.length) }
}

function partValue(staffing: Staffing, part: Part): string {
  return part === 'role' ? staffing.role : staffing[part]
}

export function SentenceWindow({ store, host, open, saved, stage = viewportStage }: {
  store: StaffingStore
  host: StaffingHost
  open: OpenSentence
  /** What the slot holds in the mission. */
  saved: Staffing | null
  stage?: StaffingStage
}) {
  const { ref } = open
  const { catalog } = host
  const current = store.current(ref.key, saved)
  const quick = Boolean(current) && Boolean(open.part)
  const [tokens, setTokens] = useState<Staffing>(() => current || freshStaffing(catalog, ref))
  const [tokenNote, setTokenNote] = useState<string | null>(null)
  const [tokenOffer, setTokenOffer] = useState<Suggestion | undefined>(undefined)
  // An effort chosen in this sentence is stated, as one typed is.
  const [effortStated, setEffortStated] = useState(false)
  // Whose effort a landing role keeps: one stated here, a staffed slot's own, or for an empty slot the policy's.
  const hold: EffortHold = effortStated ? 'stated' : current ? 'slot' : 'policy'
  const [text, setText] = useState('')
  const [list, setList] = useState<Part | null>(open.part)
  const [filter, setFilter] = useState('')
  // The highlighted row; null is the slot's current value, so a reflex Enter changes nothing.
  const [hi, setHi] = useState<number | null>(null)
  const [choosing, setChoosing] = useState(false)
  // Its height is measured once, out of sight, with the list it opens with; then it is placed below its row
  // (above where there is no room), after the canvas has panned to make the room where it can.
  const [height, setHeight] = useState<number | null>(null)
  const [placed, setPlaced] = useState<DropPlace | null>(null)
  const shownList: Part | null = height === null ? list || 'role' : list
  const rootRef = useRef<HTMLDivElement | null>(null)
  const inputRef = useRef<HTMLInputElement | null>(null)
  const committed = useRef(false)

  // Free-order words typed into the first word, read against the sentence as it stands.
  const parsed: Parsed | null = useMemo(() => (text.trim() ? parseWords(catalog, text, tokens) : null), [catalog, text, tokens])
  const blocking = Boolean(parsed) && parsed!.issues.some(issue => issue.blocking)
  const typed = useMemo(() => (parsed && !blocking ? applyParsed(catalog, ref, tokens, parsed, hold) : null), [blocking, catalog, hold, parsed, ref, tokens])
  const draft = typed && !typed.refused ? typed.next : tokens
  const note = typed ? typed.note || null : tokenNote
  const offer = typed ? typed.offer : tokenOffer

  // The slot shows the sentence as it is composed.
  useEffect(() => { store.setDraft(ref.key, draft) }, [draft, ref.key, store])
  useEffect(() => () => { if (!committed.current) store.setDraft(ref.key, undefined) }, [ref.key, store])
  useEffect(() => { (list || choosing ? rootRef.current : inputRef.current)?.focus({ preventScroll: true }) }, [list, choosing])

  const cancel = () => { store.setDraft(ref.key, undefined); store.setOpen(null) }
  const commit = (value: Staffing, why: string | null | undefined, offered?: Suggestion) => {
    committed.current = true
    // A landing on the policy's effort says why: the effort a new slot or a new role took from it.
    const suggestion = roleSuggestion(catalog, ref, catalog.roles.find(role => role.id === value.role))
    const decided = !current || current.effort !== value.effort || current.role !== value.role
    const policy = decided && suggestion && suggestion.effort === value.effort ? `Effort ${suggestion.effort}: ${suggestion.reason}.` : ''
    const words = [why || '', policy && !(why || '').includes(policy) ? policy : ''].filter(Boolean).join(' ')
    void staff(store, host, ref, saved, value, { note: words || undefined, offer: offered && offered.effort !== value.effort ? offered : undefined })
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
  const suggestion = roleSuggestion(catalog, ref, roleEntry)
  const efforts = allEfforts(catalog)

  /** A pick from a word's list: in a quick edit it lands at once, with the reason the full sentence gives. */
  const land = (outcome: Outcome) => {
    if (outcome.refused) { setTokenNote(outcome.refused); return }
    setFilter('')
    setHi(null)
    setText('')
    // The policy's offer stands until its effort is taken or the role changes.
    const standing = outcome.offer || (offer && outcome.next.role === draft.role && outcome.next.effort !== offer.effort ? offer : undefined)
    if (quick) { commit(outcome.next, outcome.note, standing); return }
    setTokens(outcome.next)
    setTokenNote(outcome.note || null)
    setTokenOffer(standing)
    setList(null)
  }

  const pickEffort = (effort: string) => { setEffortStated(true); land(withEffort(catalog, draft, effort)) }
  const landRole = (role: typeof roleEntry | null) => land(withRole(catalog, ref, draft, role || null, hold))

  const rows = useMemo<Row[]>(() => {
    if (choosing && parsed?.ambiguous) {
      return parsed.ambiguous.roles.map(role => ({
        id: `choice:${role.id}`, label: role.name, hint: role.summary, tag: roleSuggestion(catalog, ref, role)?.effort,
        apply: () => {
          const resolved = { ...parsed, ambiguous: undefined, roleTouched: true, result: { ...parsed.result, role: role.id } }
          const outcome = applyParsed(catalog, ref, tokens, resolved, hold)
          if (!outcome.refused) commit(outcome.next, outcome.note, outcome.offer)
        },
      }))
    }
    if (!shownList) return []
    const f = filter.toLowerCase()
    if (shownList === 'role') {
      // Roles staffed lately come first, then the rest by name.
      const recent = store.recentRoles.map(id => catalog.roles.find(role => role.id === id)).filter((role): role is NonNullable<typeof role> => Boolean(role))
      const roles = f ? matchRoles(catalog.roles, f) : [...recent, ...catalog.roles.filter(role => !store.recentRoles.includes(role.id))]
      const out: Row[] = []
      if (!f || 'vanilla'.startsWith(f)) out.push({ id: 'vanilla', label: 'vanilla', hint: 'no role: harness, model and effort only', apply: () => landRole(null) })
      for (const role of roles) {
        out.push({ id: role.id, label: role.name, hint: role.summary || role.kind, tag: roleSuggestion(catalog, ref, role)?.effort, apply: () => landRole(role) })
      }
      return out
    }
    if (shownList === 'harness') {
      return catalog.harnesses
        .map(harness => ({ id: harness.id, label: harnessName(harness.id) || harness.id, hint: (harness.models || []).map(model => model.id).join(', ') || 'its default model', apply: () => land(withHarness(catalog, draft, harness.id)) }))
        .filter(row => !f || row.label.toLowerCase().startsWith(f))
    }
    if (shownList === 'model') {
      const home = draft.harness
      const harnesses = [home, ...catalog.harnesses.map(harness => harness.id).filter(id => id !== home)]
      // A model outside the catalog is still the slot's own: it heads its harness's rows, as the current value.
      const own = draft.model && offCatalog(catalog, draft)
        ? [{ id: `${home}:${draft.model}`, label: draft.model, hint: 'not in the catalog; the harness decides', tag: 'warning', apply: () => land(withFreeModel(draft, draft.model)) }]
        : []
      const out: Row[] = harnesses.flatMap(harness => [
        ...(harness === home ? own : []),
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
  }, [choosing, parsed, shownList, filter, draft, catalog, ref, suggestion?.effort, tokens, hold, efforts]) // eslint-disable-line react-hooks/exhaustive-deps

  /** The row that holds the slot's value now: every list opens on it. */
  const isCurrent = (row: Row): boolean => {
    if (choosing || !shownList) return false
    if (shownList === 'role') return (draft.role || 'vanilla') === row.id
    if (shownList === 'model') return row.id === `${draft.harness}:${draft.model}`
    return row.id === partValue(draft, shownList)
  }
  const currentRow = rows.findIndex(isCurrent)
  const firstEnabled = Math.max(0, rows.findIndex(row => !row.disabled))
  const active = hi ?? (currentRow >= 0 && !filter ? currentRow : firstEnabled)

  const openList = (part: Part) => { setChoosing(false); setList(part); setFilter(''); setHi(null) }

  // A list shows only whole rows: its height is a whole number of rows that fit the window's room, it
  // scrolls a row at a time, and it opens with the highlighted row in its middle.
  const listRef = useRef<HTMLDivElement | null>(null)
  const opened = useRef<string>('')
  const [listHeight, setListHeight] = useState<number | null>(null)
  useLayoutEffect(() => {
    const win = rootRef.current
    const listEl = listRef.current
    if (!win) return
    if (height === null) {
      // Out of sight: the sentence with the list it opens with, about eight lines of it, is the window's height.
      if (listEl) {
        const { pitch, lines } = rowLines(listEl)
        listEl.style.height = `${Math.min(lines, LIST_LINES) * pitch}px`
      }
      setHeight(win.offsetHeight)
      if (listEl) listEl.style.height = ''
      return
    }
    if (!listEl) { if (listHeight !== null) setListHeight(null); return }
    // The list takes the whole rows that fit what the window's height leaves it.
    const box = listEl.parentElement!
    const room = box.clientHeight - [...box.children].reduce((sum, child) => sum + (child === listEl ? 0 : (child as HTMLElement).offsetHeight), 0) - 8
    const { pitch, lines } = rowLines(listEl)
    const fit = Math.max(1, Math.min(lines, Math.floor(room / pitch)))
    if (fit * pitch !== listHeight) setListHeight(fit * pitch)
  })
  useLayoutEffect(() => {
    if (height === null || placed) return
    const canPan = Boolean(stage.makeRoom) && Boolean(open.anchor.closest('.world'))
    const place = () => placeDrop(open.anchor.getBoundingClientRect(), { width: WIDTH, height }, stage.bounds(), canPan)
    const first = place()
    if (!first.pan) { setPlaced(first); return }
    let current = true
    // The canvas pans by exactly the room missing; the window fades in where it lands once the pan is over.
    void stage.makeRoom!(first.pan).then(() => { if (current) setPlaced(place()) })
    return () => { current = false }
  }, [height]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const listEl = listRef.current
    const row = rootRef.current?.querySelector<HTMLElement>('.staffing-row.hi')
    if (!listEl || !row || listHeight === null) return
    const fresh = opened.current !== `${list}:${choosing}`
    opened.current = `${list}:${choosing}`
    if (!listEl.contains(row)) return
    const { pitch } = rowLines(listEl)
    const shown = Math.round(listEl.clientHeight / pitch)
    if (fresh) listEl.scrollTop = Math.max(0, row.offsetTop - Math.floor((shown - 1) / 2) * pitch)
    else if (row.offsetTop < listEl.scrollTop) listEl.scrollTop = row.offsetTop
    else if (row.offsetTop + pitch > listEl.scrollTop + listEl.clientHeight) listEl.scrollTop = row.offsetTop + pitch - shown * pitch
  }, [active, list, choosing, listHeight])
  useEffect(() => {
    const listEl = listRef.current
    if (!listEl) return
    let pending = 0
    // A wheel notch moves two rows; a trackpad gathers its small steps into rows.
    const onWheel = (event: WheelEvent) => {
      event.preventDefault()
      pending += event.deltaMode === 1 ? event.deltaY * 40 : event.deltaY
      const steps = Math.trunc(pending / 50)
      if (!steps) return
      pending -= steps * 50
      const { pitch } = rowLines(listEl)
      listEl.scrollTop = Math.round((listEl.scrollTop + steps * pitch) / pitch) * pitch
    }
    // Any other scroll, such as a drag on the scrollbar or a press dragged inside the list, settles on a
    // whole row once it stops.
    let settle = 0
    const snap = () => {
      const { pitch } = rowLines(listEl)
      const snapped = Math.round(listEl.scrollTop / pitch) * pitch
      if (Math.abs(snapped - listEl.scrollTop) > 0.5) listEl.scrollTop = snapped
    }
    const onScroll = () => { window.clearTimeout(settle); settle = window.setTimeout(snap, 90) }
    listEl.addEventListener('wheel', onWheel, { passive: false })
    listEl.addEventListener('scroll', onScroll)
    window.addEventListener('pointerup', snap)
    return () => {
      window.clearTimeout(settle)
      listEl.removeEventListener('wheel', onWheel)
      listEl.removeEventListener('scroll', onScroll)
      window.removeEventListener('pointerup', snap)
    }
  }, [list, choosing, height])

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
        // In the role grid vanilla spans the first row, so down from it is the first role.
        const step = key === 'ArrowDown' ? (cols === 2 && rows[active]?.id === 'vanilla' ? 1 : cols) : key === 'ArrowUp' ? -cols : key === 'ArrowRight' ? 1 : -1
        let next = active + step
        if (!enabled.includes(next)) next = enabled[(enabled.indexOf(active) + (step > 0 ? 1 : -1) + enabled.length) % enabled.length] ?? 0
        setHi(Math.max(0, Math.min(rows.length - 1, next)))
        return
      }
      if (key === 'Enter') { event.preventDefault(); const row = rows[active]; if (row && !row.disabled) row.apply(); return }
      if (key === 'Backspace') { event.preventDefault(); if (filter) setFilter(filter.slice(0, -1)); else { setList(null); setChoosing(false) } return }
      if (key === 'Tab') { event.preventDefault(); setList(null); setChoosing(false); return }
      if (key.length === 1 && /\S/.test(key) && !event.ctrlKey && !event.metaKey) { event.preventDefault(); setFilter(filter + key); setHi(0) }
      return
    }
    if (key === 'Enter') {
      event.preventDefault()
      if (parsed?.ambiguous) { setChoosing(true); setHi(0); return }
      if (blocking) return
      commit(draft, note, offer)
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
  const grid = shownList === 'role' && !choosing
  // Until it is placed it waits out of sight, so it can already take focus and typing.
  const style: CSSProperties = placed
    ? { left: placed.left, top: placed.top, width: WIDTH, height: placed.height }
    : { left: -10000, top: 0, width: WIDTH, height: height ?? undefined }
  const option = (row: Row, index: number) => (
    <div
      key={row.id}
      role="option"
      aria-selected={index === active}
      aria-disabled={Boolean(row.disabled)}
      aria-current={isCurrent(row) || undefined}
      className={`staffing-row${index === active ? ' hi' : ''}${row.disabled ? ' disabled' : ''}${isCurrent(row) ? ' current' : ''}`}
      onPointerEnter={() => !row.disabled && setHi(index)}
      onClick={() => !row.disabled && row.apply()}
      data-row={row.id}
      title={row.hint}
    >
      <span className="staffing-row-l">{row.label}</span>
      {row.tag ? <span className={`staffing-tag${row.tag === 'suggested' ? ' sugg' : row.tag === 'warning' ? ' warn' : ''}`}>{row.tag}</span> : null}
      {grid ? null : <span className="staffing-row-h">{row.disabled || row.hint}</span>}
    </div>
  )
  return (
    <>
      <div className={`staffing-window${placed ? '' : ' placing'}${placed?.above ? ' above' : ''}`} style={style} ref={rootRef} tabIndex={-1} onKeyDown={onKeyDown} role="dialog" aria-label={`Staff ${ref.label}`} data-testid="staffing-sentence">
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
          {/* What the slot runs wraps as one group, so the effort never leaves its model. */}
          <span className="staffing-settings">
            <span className="staffing-lead">on</span>
            {token('harness', harnessName(draft.harness) || draft.harness)}
            <span className="staffing-sep">·</span>
            {token('model', modelWords(draft.model))}
            <span className="staffing-sep">·</span>
            {token('effort', draft.effort)}
          </span>
        </div>
        {parsed ? (
          <div className="staffing-read" data-testid="staffing-read">
            {parsed.read.map((read, index) => <span key={index} className="staffing-read-w"><b>{read.word}</b> {read.as}</span>)}
            {parsed.issues.map((issue, index) => <div key={`i${index}`} className="staffing-issue">{issue.text}{parsed.ambiguous && /could be/.test(issue.text) ? ' ↵ shows them.' : ''}</div>)}
            {parsed.alternatives.map((alternative, index) => (
              <button key={`a${index}`} type="button" className="staffing-alt" onClick={() => {
                const outcome = applyParsed(catalog, ref, tokens, { ...alternative, result: alternative.staffing, offCatalog: undefined }, hold)
                if (!outcome.refused) commit(outcome.next, outcome.note, outcome.offer)
              }}>instead: {alternative.label}</button>
            ))}
          </div>
        ) : null}
        {shownList || choosing ? (
          <div className="staffing-list-wrap">
            <div className="staffing-list-hd">{choosing ? `“${parsed?.ambiguous?.word}” could be:` : filter ? `filter: ${filter}` : shownList === 'role' ? 'roles: type to filter, 1–6 effort' : shownList === 'effort' && suggestion ? `effort: ${suggestion.effort} suggested, as ${suggestion.reason}` : shownList === 'effort' ? 'effort: ↵ takes the highlighted one' : 'type to filter'}</div>
            {/* vanilla stays above the scrolling grid, one click away whatever the grid shows. */}
            <div className="staffing-listbox" role="listbox" aria-label={choosing ? 'Roles that match' : `Choose ${shownList}`}>
              {grid && rows[0]?.id === 'vanilla' ? <div className="staffing-pinned">{option(rows[0], 0)}</div> : null}
              <div ref={listRef} className={grid ? 'staffing-list staffing-grid' : 'staffing-list'} style={listHeight === null ? undefined : { height: listHeight, flex: 'none' }}>
                {rows.map((row, index) => (grid && index === 0 && row.id === 'vanilla' ? null : option(row, index)))}
                {rows.length === 0 ? <div className="staffing-row disabled"><span className="staffing-row-h">Nothing matches “{filter}”. Esc leaves the slot as it was.</span></div> : null}
              </div>
            </div>
            {grid && rows[active]?.hint ? <div className="staffing-list-ft">{rows[active].hint}</div> : null}
          </div>
        ) : null}
        {/* While a list is open the policy speaks through its rows and header; a note still shows. */}
        <div className={`staffing-policy${(shownList || choosing) && !note ? ' quiet' : ''}`} data-testid="staffing-policy">
          {note ? <span className="staffing-note">{note}</span>
            : suggestion ? <span>Policy suggests <b>{suggestion.effort}</b>: {suggestion.reason}.</span>
            : <span>Policy: {policyLine(catalog)}.</span>}
          {offer && offer.effort !== draft.effort ? (
            <button type="button" className="staffing-offer" data-testid="staffing-window-offer" onClick={() => { land(withEffort(catalog, draft, offer.effort)); inputRef.current?.focus({ preventScroll: true }) }}>use {offer.effort}</button>
          ) : null}
          {offCatalog(catalog, draft) ? <div className="staffing-issue">{draft.model}: not in the catalog; the harness decides.</div> : null}
          {!effortsFor(catalog, draft.harness, draft.model).length ? <div className="staffing-issue">{harnessName(draft.harness) || draft.harness} is not a harness Archon starts.</div> : null}
        </div>
        {shownList || choosing ? null : <div className="staffing-keys">↵ staffs · Esc leaves it as it was · 1–6 effort · type words in any order</div>}
      </div>
    </>
  )
}
