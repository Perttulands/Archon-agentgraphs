import { useEffect, useRef, useState } from 'react'
import { gateKindLabel } from './GateEditorDialog'
import { HARNESS_ICONS, type HarnessId } from './harnessIcons'
import { harnessName } from './formationsCockpitVisuals'
import '../styles/formations-wires.css'

// The canvas notation in words: what each wire, gate kind, run ring and harness
// glyph means. Wires and rings are drawn with the canvas's own classes, so the
// legend cannot drift from what the board shows.

const WIRES: Array<{ className: string; text: string; label?: string }> = [
  { className: 'wire', text: 'An output feeds the next step' },
  { className: 'wire pass', text: 'A gate passes the work on', label: '→ Next step' },
  { className: 'wire fail', text: 'A gate sends failed work elsewhere', label: '→ Fallback' },
  { className: 'wire fail loop', text: 'A gate sends work back to an earlier step', label: '↺ Earlier step' },
  { className: 'wire judge', text: 'A judge chain: the gate asks a formation to decide' },
  { className: 'wire flowing', text: 'Work moving now' },
]

const GATE_KINDS: Array<{ kind: string; text: string }> = [
  { kind: 'code', text: 'A check on the output text decides' },
  { kind: 'formation', text: 'A judge formation decides' },
  { kind: 'human', text: 'You decide' },
]

const RUN_STATES: Array<{ className: string; text: string; chip?: string }> = [
  { className: 'running', text: 'Running' },
  { className: 'waiting', text: 'Waiting for your answer' },
  { className: 'blocked', text: 'Blocked or failed; the run bar says why', chip: 'blocked' },
]

const HARNESSES: HarnessId[] = ['claude-code', 'codex', 'opencode', 'pi', 'hermes', 'terminal']

export default function CanvasLegend() {
  const [open, setOpen] = useState(false)
  const anchor = useRef<HTMLDivElement | null>(null)
  const button = useRef<HTMLButtonElement | null>(null)

  useEffect(() => {
    if (!open) return
    const close = (event: Event) => {
      if (event instanceof KeyboardEvent) {
        if (event.key !== 'Escape') return
        button.current?.focus()
      } else if (anchor.current?.contains(event.target as Node)) {
        return
      }
      setOpen(false)
    }
    document.addEventListener('pointerdown', close, true)
    document.addEventListener('keydown', close)
    return () => {
      document.removeEventListener('pointerdown', close, true)
      document.removeEventListener('keydown', close)
    }
  }, [open])

  return (
    <div className="canvas-legend-anchor" ref={anchor}>
      <button ref={button} type="button" className="newbtn" aria-expanded={open} aria-controls="canvas-legend" onClick={() => setOpen(value => !value)}>Legend</button>
      {open ? (
        <section id="canvas-legend" className="canvas-legend" role="dialog" aria-label="Canvas legend">
          <h3>Wires</h3>
          <ul>
            {WIRES.map(wire => (
              <li key={wire.className}>
                <svg className="wires legend-wire" width="118" height="22" aria-hidden="true">
                  <path className={wire.className} d="M4,15 L114,15" />
                  {wire.label ? <text className={`wire-label ${wire.className.replace('wire ', '')}`} x="6" y="9">{wire.label}</text> : null}
                </svg>
                <span>{wire.text}</span>
              </li>
            ))}
          </ul>
          <h3>Gate kinds</h3>
          <ul>
            {GATE_KINDS.map(gate => (
              <li key={gate.kind}><span className="legend-mark"><span className={`gkind gkind-${gate.kind}`}>{gateKindLabel(gate.kind)}</span></span><span>{gate.text}</span></li>
            ))}
          </ul>
          <h3>Run states</h3>
          <ul>
            {RUN_STATES.map(state => (
              <li key={state.className}>
                <span className="legend-mark"><span className={`legend-ring ${state.className}`}>{state.chip ? <span className="run-chip blocked">{state.chip}</span> : null}</span></span>
                <span>{state.text}</span>
              </li>
            ))}
            <li><span className="legend-mark legend-pause">paused</span><span>A pause after your answer, before the run resumes</span></li>
          </ul>
          <h3>Harnesses</h3>
          <ul className="legend-harnesses">
            {HARNESSES.map(id => (
              <li key={id}><span className="legend-glyph" aria-hidden="true">{HARNESS_ICONS[id]}</span><span>{id === 'terminal' ? 'Another harness' : harnessName(id)}</span></li>
            ))}
          </ul>
        </section>
      ) : null}
    </div>
  )
}
