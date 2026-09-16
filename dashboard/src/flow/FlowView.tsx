import { useEffect, useMemo, useRef, type ReactNode } from 'react'
import { noteAuthor } from '../components/NoteThread'
import RunPoint from '../components/RunPoint'
import type { NodeRunState, RunPoint as RunPointModel } from '../components/formationsRunState'
import type { AgentProjection, BoardDocument, FormationNode, MissionNode, NoteEntry } from '../components/formationsTypes'
import { fileAnchor, useFileWindows } from '../files/FileWindows'
import { ProducedFiles } from '../files/ProducedFiles'
import { referencedFileRequest } from '../files/fileWindowModel'
import { staffingSentence } from '../nodeWindow/staffing'
import { usePersonaCards } from '../nodeWindow/usePersonaCards'
import { buildFlow, type FlowStep, type FlowTarget, type GateDecider } from './flowModel'
import './flow.css'

/**
 * A board as a numbered sequence, read top to bottom: each mission's goal and
 * input, then its steps with gates inline, judges nested under their gate, and
 * with a run selected, each step's state beside it. Every step, note and file
 * opens in its floating window.
 */

export interface FlowRun {
  runId: string
  states: ReadonlyMap<string, NodeRunState>
  attempts: ReadonlyMap<string, number>
  point: RunPointModel | null
  pointTitle: string
}

const STATE_WORDS: Record<NodeRunState, string> = {
  '': 'not reached',
  running: 'running',
  done: 'done',
  blocked: 'blocked',
  waiting: 'waiting for you',
  failed: 'failed',
}

const DECIDER_WORDS: Record<GateDecider, string> = { you: 'you', judge: 'a judge', code: 'a code check' }

export function deciderWords(deciders: GateDecider[]): string {
  const words = deciders.map(decider => DECIDER_WORDS[decider])
  if (!words.length) return 'Decided by no one yet'
  return `Decided by ${words.length > 1 ? `${words.slice(0, -1).join(', ')} and ${words[words.length - 1]}` : words[0]}`
}

/** A long text's opening, cut at a word, for a row; the window has the rest. */
export function summary(text: string, limit = 220): string {
  const flat = text.replace(/\s+/g, ' ').trim()
  if (flat.length <= limit) return flat
  const cut = flat.slice(0, limit)
  return `${cut.slice(0, Math.max(cut.lastIndexOf(' '), limit - 30)).trim()}…`
}

export default function FlowView({ board, agents, notes, run, answerPanel, onOpenNode, onOpenNotes, onStartMission }: {
  board: BoardDocument
  agents: AgentProjection[]
  /** Each node's note thread. */
  notes: ReadonlyMap<string, NoteEntry[]>
  run: FlowRun | null
  /** The pending human gate's answer panel, shown in that gate's row. */
  answerPanel: { gateId: string; panel: ReactNode } | null
  onOpenNode: (nodeId: string) => void
  onOpenNotes: (nodeId: string) => void
  onStartMission: (mission: MissionNode) => void
}) {
  const flow = useMemo(() => buildFlow(board), [board])
  const cards = usePersonaCards(board.formations.flatMap(formation => formation.slots.map(slot => slot.agentId || '')))
  const root = useRef<HTMLDivElement>(null)

  // The canvas zooms on the wheel; in Flow the wheel scrolls the list.
  useEffect(() => {
    const element = root.current
    if (!element) return
    const keep = (event: Event) => event.stopPropagation()
    element.addEventListener('wheel', keep)
    return () => element.removeEventListener('wheel', keep)
  }, [])

  const staffing = (formation: FormationNode) => formation.slots
    .map(slot => staffingSentence(slot, agents.find(agent => agent.id === slot.agentId), slot.agentId ? cards.get(slot.agentId) : undefined))
    .join(' ')

  const context = { flow, run, notes, answerPanel, staffing, onOpenNode, onOpenNotes }
  return (
    <div
      ref={root}
      className={`flow${run ? ' with-run' : ''}`}
      data-testid="flow-view"
      // The canvas under the Flow view never sees its presses or menus.
      onPointerDown={event => event.stopPropagation()}
      onContextMenu={event => event.stopPropagation()}
    >
      {flow.sections.map(section => (
        <section key={section.mission?.id || 'unreached'} className="flow-section" aria-label={section.mission ? `Mission ${section.mission.title}` : 'Steps no mission reaches'}>
          {section.mission ? (
            <header className="flow-mission" data-flow-node={section.mission.id}>
              <div className="flow-mission-head">
                <button type="button" className="flow-title" onClick={() => onOpenNode(section.mission!.id)}>
                  <span className="flow-kicker">◆ Mission</span> {section.mission.title || 'Untitled mission'}
                </button>
                <button type="button" className="flow-action" onClick={() => onStartMission(section.mission!)}>Start mission</button>
              </div>
              <p className="flow-text">{summary(section.mission.goal, 480) || <span className="placeholder">No goal yet.</span>}</p>
              <p className="flow-line"><span className="flow-label">Input</span>{section.mission.inputHint ? summary(section.mission.inputHint, 320) : 'The brief you give when you start the mission.'}</p>
              <FileChips files={section.mission.files} context={section.mission.title} />
              <NoteLine nodeId={section.mission.id} notes={notes} onOpenNotes={onOpenNotes} />
              {section.start.length ? null : <p className="flow-line warn">Wire the mission to its first step.</p>}
            </header>
          ) : (
            <header className="flow-mission unreached"><h2>Not reached from a mission</h2></header>
          )}
          <ol className="flow-steps">
            {section.steps.map(step => <FlowRow key={step.id} step={step} {...context} />)}
          </ol>
        </section>
      ))}
    </div>
  )
}

function FlowRow({ step, run, notes, answerPanel, staffing, onOpenNode, onOpenNotes }: {
  step: FlowStep
  run: FlowRun | null
  notes: ReadonlyMap<string, NoteEntry[]>
  answerPanel: { gateId: string; panel: ReactNode } | null
  staffing: (formation: FormationNode) => string
  onOpenNode: (nodeId: string) => void
  onOpenNotes: (nodeId: string) => void
}) {
  const state = run ? run.states.get(step.id) ?? '' : undefined
  const title = step.node.title || (step.kind === 'gate' ? 'Gate' : 'Untitled step')
  return (
    <li className={`flow-step flow-${step.kind}${state ? ` state-${state}` : ''}`} data-flow-node={step.id} data-testid={`flow-step-${step.id}`}>
      <div className="flow-body">
        <div className="flow-step-head">
          <span className="flow-number" aria-hidden="true">{step.number}</span>
          <button type="button" className="flow-title" aria-label={`${step.number} ${title}`} onClick={() => onOpenNode(step.id)}>{title}</button>
          <span className="flow-kind">{step.kind === 'formation' ? step.node.type : step.kind === 'gate' ? 'gate' : 'tool'}</span>
        </div>
        {step.kind === 'formation' ? (
          <>
            <p className="flow-text">{summary(step.node.brief?.goal || '') || <span className="placeholder">No brief yet.</span>}</p>
            <p className="flow-line"><span className="flow-label">Staffing</span>{staffing(step.node) || 'No slots.'}</p>
            <Routes label="Next" targets={step.next} onOpenNode={onOpenNode} />
            <FileChips files={step.node.brief?.files} context={title} />
          </>
        ) : null}
        {step.kind === 'gate' ? (
          <>
            <p className="flow-line"><span className="flow-label">Decider</span>{deciderWords(step.deciders)}</p>
            <p className="flow-text">{summary(step.node.criterion) || <span className="placeholder">No criterion yet.</span>}</p>
            <Routes label="Pass" targets={step.pass} onOpenNode={onOpenNode} />
            <Routes label="Fail" targets={step.fail} onOpenNode={onOpenNode} />
            <FileChips files={step.node.files} context={title} />
            {step.judges.length ? (
              <ul className="flow-judges" aria-label={`Judges of ${title}`}>
                {step.judges.map(judge => (
                  <li key={judge.id} className="flow-judge" data-flow-node={judge.id}>
                    <button type="button" className="flow-title" aria-label={`Judge ${judge.title}`} onClick={() => onOpenNode(judge.id)}>
                      <span className="flow-kicker">Judge</span> {judge.title || 'Untitled judge'}
                    </button>
                    <p className="flow-line">{staffing(judge)}</p>
                  </li>
                ))}
              </ul>
            ) : null}
          </>
        ) : null}
        {step.kind === 'tool' ? (
          <>
            <p className="flow-line"><span className="flow-label">Tool</span>{step.node.profileId}@{step.node.profileVersion}</p>
            <Routes label="Next" targets={step.next} onOpenNode={onOpenNode} />
          </>
        ) : null}
        <NoteLine nodeId={step.id} notes={notes} onOpenNotes={onOpenNotes} />
        {answerPanel && answerPanel.gateId === step.id ? <div className="flow-answer">{answerPanel.panel}</div> : null}
      </div>
      {run ? (
        <div className="flow-status" aria-label={`Run state of ${title}`}>
          <span className={`flow-state state-${state || 'idle'}`}>{STATE_WORDS[state || '']}</span>
          {(run.attempts.get(step.id) || 0) > 1 ? <span className="flow-attempt">attempt {run.attempts.get(step.id)}</span> : null}
          {run.point && run.point.nodeId === step.id && run.point.kind !== 'running'
            ? <RunPoint runId={run.runId} point={run.point} title={run.pointTitle} onLocate={onOpenNode} />
            : null}
          <ProducedFiles nodeId={step.id} className="flow-produced" />
        </div>
      ) : null}
    </li>
  )
}

function Routes({ label, targets, onOpenNode }: { label: string; targets: FlowTarget[]; onOpenNode: (nodeId: string) => void }) {
  return (
    <p className={`flow-line flow-routes route-${label.toLowerCase()}`}>
      <span className="flow-label">{label}</span>
      {targets.map((target, index) => (
        <span key={target.kind === 'step' ? target.nodeId : target.kind} className="flow-route">
          {index ? ', ' : null}
          {target.kind === 'step' ? (
            <button type="button" className="flow-link" onClick={() => onOpenNode(target.nodeId)}>
              {target.back ? '↺ back to ' : '→ '}{target.number !== null ? `${target.number} ` : ''}{target.title}
            </button>
          ) : target.kind === 'end' ? '→ run ends' : '→ the run blocks'}
        </span>
      ))}
    </p>
  )
}

function NoteLine({ nodeId, notes, onOpenNotes }: { nodeId: string; notes: ReadonlyMap<string, NoteEntry[]>; onOpenNotes: (nodeId: string) => void }) {
  const entries = notes.get(nodeId) || []
  const latest = entries[entries.length - 1]
  if (!latest) return null
  const author = noteAuthor(latest.author)
  return (
    <p className="flow-line">
      <span className="flow-label">Note</span>
      <button type="button" className={`flow-note note-${author.kind}`} onClick={() => onOpenNotes(nodeId)}>
        <span className="flow-note-author">{author.name}</span> {summary(latest.text, 160)}
        {entries.length > 1 ? <span className="flow-note-more"> · {entries.length} entries</span> : null}
      </button>
    </p>
  )
}

function FileChips({ files, context }: { files: string[] | undefined; context: string }) {
  const fileWindows = useFileWindows()
  if (!files?.length) return null
  return (
    <p className="flow-line">
      <span className="flow-label">Files</span>
      {files.map(file => fileWindows
        ? <button key={file} type="button" className="flow-file" onClick={event => fileWindows.open(referencedFileRequest(file, context), fileAnchor(event.currentTarget))}>{file.split('/').pop() || file}</button>
        : <span key={file} className="flow-file">{file}</span>)}
    </p>
  )
}
