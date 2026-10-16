import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { fetchRunAttempts, type StepAttempt, type StepAttempts } from '../evidence/runEvidenceApi'
import FloatingWindow from '../windows/FloatingWindow'
import type { FrameSize } from '../windows/floatingWindowSize'
import AttemptColumn, { type AttemptView } from './AttemptColumn'
import type { ImageZoom } from './AttemptImage'
import {
  OUTCOME_WORDS,
  attemptOutcome,
  attemptsWindowId,
  comparePair,
  isImagePath,
  openingAttempt,
  settledAttempt,
  stepAttempt,
  type AttemptOutcome,
  type AttemptsPick,
} from './attemptsModel'
import './attempts.css'

// A step's attempts in one floating window (archon-7963). The strip shows
// every attempt in order, each with what it produced and what its gates said,
// so the loop reads left to right; side by side puts two of them in frames of
// one size. The arrow keys step between attempts, C switches the view, and an
// image click zooms every attempt's image of that output to the same spot.
// A pending best-of-N pick (archon-gihe) is answered here too: the candidates
// stand out, the judge's choice is marked, and one button picks.


/** Room for each attempt's column, up to most of the screen. */
export function attemptsWindowSize(count: number, viewport: FrameSize): FrameSize {
  return {
    width: Math.round(Math.min(viewport.width * .94, Math.max(480, 40 + Math.max(1, count) * 400))),
    height: Math.round(viewport.height * .86),
  }
}

const PIP_GLYPH: Record<AttemptOutcome, string> = {
  picked: '✓', pass: '✓', fail: '✕', waiting: '?', judging: '…', running: '…', stopped: '–', done: '',
}

const outcomeWord = (outcome: AttemptOutcome) => OUTCOME_WORDS[outcome].replace(/^[✓✕] /, '')

/** The first output of an attempt that is an image, which the keyboard's zoom shows. */
function imageGroup(attempt: StepAttempt | undefined): string {
  const output = attempt?.outputs.find(candidate => candidate.fileKind === 'image' || (!candidate.fileKind && candidate.kind === 'file' && isImagePath(candidate.artifact || candidate.path || '')))
  return output?.portId || ''
}

// Keys typed into a field, or that move or size the window, are not the attempts'.
const OWN_KEYS = 'input, textarea, select, [contenteditable="true"], .fwin-title, [role="separator"]'

export default function AttemptsWindow({ runId, nodeId, stepTitle, count, version, live, waitingGates, pick, onClose }: {
  runId: string
  nodeId: string
  stepTitle: string
  /** How many attempts the run has recorded, for the opening size. */
  count: number
  /** Changes whenever the run records something, so the attempts are read again. */
  version: string
  /** Whether the run can still add attempts. */
  live: boolean
  waitingGates: ReadonlySet<string>
  pick?: AttemptsPick | null
  onClose: () => void
}) {
  const [step, setStep] = useState<StepAttempts | null>(null)
  const [error, setError] = useState('')
  const [poll, setPoll] = useState(0)
  const [focus, setFocus] = useState<number | null>(null)
  const [mode, setMode] = useState<'strip' | 'compare'>('strip')
  const [pair, setPair] = useState<[number, number]>([0, 0])
  const [side, setSide] = useState<0 | 1>(1)
  const [zoom, setZoom] = useState<ImageZoom | null>(null)
  const [now, setNow] = useState(() => new Date())
  const [reason, setReason] = useState('')
  const [picking, setPicking] = useState(false)
  const [pickError, setPickError] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const latestSeen = useRef<number | null>(null)
  const [size] = useState(() => attemptsWindowSize(count, { width: window.innerWidth, height: window.innerHeight }))

  useEffect(() => {
    let cancelled = false
    fetchRunAttempts(runId, nodeId)
      .then(evidence => {
        if (cancelled) return
        setStep(evidence.steps.find(candidate => candidate.nodeId === nodeId) || { nodeId, title: stepTitle, outputs: [], attempts: [] })
        setError('')
      })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : 'the daemon did not answer') })
    return () => { cancelled = true }
  }, [nodeId, poll, runId, stepTitle, version])

  const attempts = useMemo(() => step?.attempts || [], [step])
  const numbers = useMemo(() => attempts.map(attempt => attempt.attempt), [attempts])
  const running = attempts.some(attempt => attempt.status === 'running')
  // A running attempt keeps writing files and its clock keeps going.
  useEffect(() => {
    if (!live || !running) return
    const clock = window.setInterval(() => setNow(new Date()), 1000)
    const files = window.setInterval(() => setPoll(value => value + 1), 4000)
    return () => { window.clearInterval(clock); window.clearInterval(files) }
  }, [live, running])

  const candidates = useMemo(() => (pick ? new Set(pick.candidates) : null), [pick])
  const recommended = pick?.recommended && numbers.includes(pick.recommended.attempt) ? pick.recommended : undefined
  const choices = useMemo(() => (candidates ? numbers.filter(number => candidates.has(number)) : numbers), [candidates, numbers])

  // Open on the judge's pick, the settled attempt or the latest; then follow
  // new attempts as they start, unless the operator stepped back to an older one.
  useEffect(() => {
    if (!numbers.length) return
    const latest = numbers[numbers.length - 1]
    const before = latestSeen.current
    latestSeen.current = latest
    setFocus(current => {
      if (current === null || !numbers.includes(current)) {
        const opening = openingAttempt(attempts, recommended?.attempt)
        return candidates && opening !== null && !candidates.has(opening) ? choices[choices.length - 1] ?? opening : opening
      }
      return current === before && latest !== current ? latest : current
    })
  }, [attempts, candidates, choices, numbers, recommended?.attempt])

  const compare = mode === 'compare' && numbers.length > 1
  const chosen = compare ? pair[side] : focus
  const settled = settledAttempt(attempts)

  const showCompare = useCallback((on: boolean) => {
    if (on && focus !== null) {
      const among = candidates && candidates.has(focus) && choices.length > 1 ? choices : numbers
      setPair(comparePair(among, focus))
      setSide(among.indexOf(focus) === 0 ? 0 : 1)
    }
    if (!on && pair[side]) setFocus(pair[side])
    setMode(on ? 'compare' : 'strip')
  }, [candidates, choices, focus, numbers, pair, side])

  const choose = useCallback((attempt: number) => {
    if (!compare) { setFocus(attempt); return }
    setPair(current => (side === 0 ? [attempt, current[1]] : [current[0], attempt]))
  }, [compare, side])

  // The focused column comes into view as the keys step to it.
  useEffect(() => {
    if (compare || focus === null) return
    const strip = scrollRef.current
    const column = strip?.querySelector<HTMLElement>(`[data-attempt="${focus}"]`)
    if (!strip || !column) return
    const box = column.getBoundingClientRect()
    const view = strip.getBoundingClientRect()
    const left = box.left < view.left ? box.left - view.left - 12 : box.right > view.right ? Math.min(box.left - view.left - 12, box.right - view.right + 12) : 0
    if (left) strip.scrollBy?.({ left, behavior: 'smooth' })
  }, [compare, focus])

  // Asking for the pick again brings the window's keys back to it.
  const focusRequest = pick?.focusRequest || 0
  useEffect(() => {
    if (focusRequest) rootRef.current?.focus({ preventScroll: true })
  }, [focusRequest])

  const onKeyDown = (event: ReactKeyboardEvent<HTMLElement>) => {
    if ((event.target as HTMLElement).closest(OWN_KEYS) || event.altKey || event.ctrlKey || event.metaKey) return
    const at = chosen ?? numbers[numbers.length - 1]
    const go = (attempt: number | undefined) => {
      if (attempt === undefined) return
      event.preventDefault()
      choose(attempt)
    }
    switch (event.key) {
      case 'ArrowLeft': go(stepAttempt(numbers, at, -1)); return
      case 'ArrowRight': go(stepAttempt(numbers, at, 1)); return
      case 'Home': go(numbers[0]); return
      case 'End': go(numbers[numbers.length - 1]); return
      case 'c': case 'C':
        if (numbers.length < 2) return
        event.preventDefault()
        showCompare(!compare)
        return
      case 'z': case 'Z': {
        const group = imageGroup(attempts.find(attempt => attempt.attempt === at))
        if (!zoom && !group) return
        event.preventDefault()
        setZoom(zoom ? null : { group, x: .5, y: .5, factor: 0 })
        return
      }
      case 'Escape':
        if (!zoom) return
        event.preventDefault()
        setZoom(null)
        return
      default:
        if (/^[1-9]$/.test(event.key) && numbers.includes(Number(event.key))) go(Number(event.key))
    }
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!pick || chosen === null || !candidates?.has(chosen) || picking) return
    setPicking(true)
    setPickError('')
    const failure = await pick.onPick(chosen, reason.trim())
    // A recorded pick ends the request, and the window with it.
    if (failure) {
      setPicking(false)
      setPickError(failure)
    }
  }

  const view: AttemptView = { runId, stepTitle, waitingGates, zoom, onZoom: setZoom, compare, now }
  const column = (attempt: StepAttempt, focused: boolean, onFocus: () => void) => (
    <AttemptColumn key={attempt.attempt} attempt={attempt} view={view} onFocus={onFocus}
      marks={{
        focused,
        settled: attempt.attempt === settled,
        candidate: candidates ? candidates.has(attempt.attempt) : undefined,
        recommended: recommended?.attempt === attempt.attempt && !attempt.picked ? recommended.reason : undefined,
      }} />
  )
  const shown = compare ? pair.map(number => attempts.find(attempt => attempt.attempt === number)) : []
  const label = pick ? `Pick the best attempt of ${stepTitle}` : `Attempts of ${stepTitle}`
  const pickable = Boolean(pick && chosen !== null && candidates?.has(chosen))

  return (
    <FloatingWindow
      id={attemptsWindowId(nodeId)}
      kind="attempts"
      label={label}
      className={`attempts-window${pick ? ' picking' : ''}`}
      title={pick
        ? <><span className="attempts-kicker">Needs your pick</span> {stepTitle}</>
        : <>{stepTitle} <span className="attempts-count">· {numbers.length || count} attempt{(numbers.length || count) === 1 ? '' : 's'}</span></>}
      defaultSize={size}
      overCanvas
      takeFocus={!pick || focusRequest > 0}
      onKeyDown={onKeyDown}
      onClose={onClose}
      actions={numbers.length ? (
        <span className="attempts-actions">
          {zoom ? <button type="button" className="attempts-action" aria-pressed="true" onClick={() => setZoom(null)}>Fit <kbd>Z</kbd></button> : null}
          {numbers.length > 1 ? (
            <button type="button" className="attempts-action" aria-pressed={compare} data-testid="attempts-compare" onClick={() => showCompare(!compare)}>
              Side by side <kbd>C</kbd>
            </button>
          ) : null}
        </span>
      ) : null}
    >
      <div className="attempts" ref={rootRef} tabIndex={-1} data-testid="attempts-window">
        {numbers.length > 1 ? (
          <nav className="attempts-bar" aria-label={`Attempts of ${stepTitle}`}>
            {attempts.map(attempt => {
              const outcome = attemptOutcome(attempt, waitingGates)
              const onSide = compare ? pair.indexOf(attempt.attempt) : -1
              const current = attempt.attempt === chosen
              return (
                <button key={attempt.attempt} type="button"
                  className={`attempts-pip outcome-${outcome}${attempt.attempt === settled ? ' settled' : ''}${current ? ' current' : ''}${onSide >= 0 ? ' shown' : ''}${candidates && !candidates.has(attempt.attempt) ? ' excluded' : ''}`}
                  aria-pressed={current}
                  aria-label={`Attempt ${attempt.attempt}, ${outcomeWord(outcome)}`}
                  title={`Attempt ${attempt.attempt} · ${outcomeWord(outcome)}`}
                  data-testid={`attempts-pip-${attempt.attempt}`}
                  onClick={() => choose(attempt.attempt)}>
                  {attempt.attempt}{PIP_GLYPH[outcome] ? <span className="attempts-pip-glyph" aria-hidden="true">{PIP_GLYPH[outcome]}</span> : null}
                </button>
              )
            })}
            <span className="attempts-keys" aria-hidden="true"><kbd>←</kbd><kbd>→</kbd></span>
          </nav>
        ) : null}
        <div className={`attempts-scroll${compare ? ' compare' : ''}`} ref={scrollRef}>
          {error && !step ? <p className="attempts-message" role="alert">The attempts cannot be read: {error}</p>
            : !step ? <p className="attempts-message" role="status">Reading attempts…</p>
              : !attempts.length ? <p className="attempts-message">No attempt has started yet.</p>
                : compare ? (
                  <div className="attempts-grid compare">
                    {shown.map((attempt, index) => attempt ? column(attempt, side === index, () => setSide(index as 0 | 1)) : null)}
                  </div>
                ) : (
                  <div className="attempts-grid strip">
                    {attempts.map(attempt => column(attempt, attempt.attempt === focus && numbers.length > 1, () => setFocus(attempt.attempt)))}
                  </div>
                )}
        </div>
        {pick ? (
          <form className="attempts-pick" onSubmit={event => void submit(event)} data-testid="attempts-pick">
            <span className="attempts-pick-gate">{pick.gateTitle}</span>
            <input className="attempts-pick-reason" value={reason} onChange={event => setReason(event.target.value)}
              placeholder="Why this attempt" aria-label="Why this attempt" disabled={picking} />
            {pickError ? <span className="attempts-pick-error" role="alert">{pickError}</span> : null}
            <button type="submit" className="attempts-pick-go" disabled={!pickable || picking} data-testid="attempts-pick-go">
              {picking ? 'Picking…' : chosen === null ? 'Pick' : pickable ? `Pick attempt ${chosen}` : `Attempt ${chosen} is not a candidate`}
            </button>
          </form>
        ) : null}
      </div>
    </FloatingWindow>
  )
}
