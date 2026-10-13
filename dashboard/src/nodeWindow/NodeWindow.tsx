import { AuthoringReadOnly } from './EditableField'
import { NodeProblems } from '../components/formationsDrafts'
import type { BoardFinding } from '../components/formationsTypes'
import { useContext, useState, type MouseEvent as ReactMouseEvent } from 'react'
import { formationTypeChoices } from '../components/FormationTypeChip'
import { END_OUTCOMES, defaultEndTitle, endOutcomeMeaning } from '../components/endNode'
import { TOKENS_DEFINITION, durationInput, durationWords, limitCoverage, limitKnobWords, limitMeaning, limitsCovering, parseDuration, roundsProblem, timeProblem, tokenWords, tokensProblem, warnProblem } from '../components/limitCard'
import { GateKindChips, GateKindsFields, draftFromGate, type GateDraft } from '../components/GateEditorDialog'
import { isSafeBeadsIssueID } from '../components/formationsBeadId'
import { splitList } from '../components/formationsCockpitDom'
import type { NodeRunState } from '../components/formationsRunState'
import type {
  AgentProjection,
  BoardDocument,
  CodeGateProfileDescriptor,
  EndNode,
  EndOutcome,
  FormationBrief,
  FormationNode,
  FormationSlot,
  FormationType,
  GateNode,
  LimitNode,
  MissionNode,
  RunEvent,
} from '../components/formationsTypes'
import { fileAnchor, useFileWindows } from '../files/FileWindows'
import { ProducedFiles } from '../files/ProducedFiles'
import { referencedFileRequest } from '../files/fileWindowModel'
import { nodeFileRefs, relativeFileProblem, useFileProblems } from '../files/referencedFiles'
import FloatingWindow from '../windows/FloatingWindow'
import { nodeAnchor, nodeWindowKeepClear } from '../windows/cockpitScene'
import type { WindowRect } from '../windows/windowGeometry'
import { EditableField } from './EditableField'
import { MissionInputsField } from './MissionInputsField'
import { HumanChannelField } from '../humanChannel/HumanChannelField'
import { humanChannelField, humanChannelOf } from '../humanChannel/humanChannel'
import { buildFlow } from '../flow/flowModel'
import { judgeChain, nodeRoutes, nodeTitle } from './boardRoutes'
import { slotTitle, staffingSentence } from './staffing'
import { harnessName } from '../components/harnessIcons'
import type { Part } from '../staffing/SlotFace'
import type { Character } from '../characters/charactersApi'
import { modelWords, roleNamer, staffingOf } from '../staffing/staffingModel'
import SeatPane, { hasLiveSeat, useNodeSeats } from '../terminal/SeatPane'
import { GateRunFacts } from './GateRunFacts'
import './nodeWindow.css'

/**
 * An Input card, formation, gate, End node or Limit card opened from its card: every field read in full
 * and edited in place, its staffing and routes stated in words, and its run
 * state with a way into the evidence. Each save is one board change with its
 * own undo entry.
 */

export interface NodeWindowOps {
  rename: (nodeId: string, title: string) => Promise<boolean>
  updateInputCard: (missionId: string, fields: Partial<Pick<MissionNode, 'goal' | 'inputHint' | 'files' | 'humanChannel' | 'inputs'>>) => Promise<boolean>
  setBrief: (formationId: string, brief: FormationBrief) => Promise<boolean>
  changeType: (formation: FormationNode, type: FormationType, keepSlotId?: string) => void
  /** Opens the slot's staffing sentence dropping from the word that was clicked; a word opens only its own list. */
  /** Moves the run past a judge verdict Archon rejected: asks the judge again, or takes the verdict as a fail. */
  settleJudge: (takeFail: boolean) => Promise<void>
  staffSlot: (formation: FormationNode, slot: FormationSlot, part: Part | null, anchor: Element) => void
  /** Stamps a saved character onto the slot: its role, harness, model and effort, with no link kept. */
  stampSlot: (formation: FormationNode, slot: FormationSlot, character: Character) => void
  updateGate: (gate: GateNode, draft: GateDraft) => Promise<boolean>
  /** Sets an End node's outcome: done, or rejected, which fails the run. */
  setEndOutcome: (end: EndNode, outcome: EndOutcome) => Promise<boolean>
  setGateFiles: (gate: GateNode, files: string[]) => Promise<boolean>
  /** Changes what a Limit card covers ('' unwires it) or one knob (0 clears it); resolves true, or the server's refusal. */
  setLimit: (limit: LimitNode, change: LimitChange) => Promise<true | string>
  attachJudge: (gate: GateNode, chain: string[]) => void
  detachJudge: (gate: GateNode) => void
  openNode: (nodeId: string) => void
  /** Opens the node's note thread in its own note window. */
  openNotes: (nodeId: string) => void
  inspectEvidence: (nodeId: string) => void
}

/** One change to a Limit card: its target, or one knob in whole seconds or rounds. */
export type LimitChange = { target?: string; rounds?: number; seconds?: number; warnSeconds?: number; tokens?: number }

const BEAD_HINT = 'A Beads issue ID such as ctx-ug7.25, or blank.'
const beadProblem = (value: string) => (value && !isSafeBeadsIssueID(value) ? `Enter a Beads issue ID such as ctx-ug7.25, or leave it blank.` : '')

const RUN_STATE_WORDS: Record<NodeRunState, string> = {
  '': 'Not reached yet',
  running: 'Running',
  done: 'Done',
  blocked: 'Blocked',
  waiting: 'Waiting for a decision',
  failed: 'Failed',
}

type Located =
  | { kind: 'inputCard'; node: MissionNode }
  | { kind: 'formation'; node: FormationNode }
  | { kind: 'gate'; node: GateNode }
  | { kind: 'end'; node: EndNode }
  | { kind: 'limit'; node: LimitNode }

export function locateNode(board: Pick<BoardDocument, 'formations'> & Partial<Pick<BoardDocument, 'inputCards' | 'gates' | 'ends' | 'limits'>>, nodeId: string): Located | null {
  const mission = board.inputCards?.find(node => node.id === nodeId)
  if (mission) return { kind: 'inputCard', node: mission }
  const formation = board.formations.find(node => node.id === nodeId)
  if (formation) return { kind: 'formation', node: formation }
  const gate = board.gates?.find(node => node.id === nodeId)
  if (gate) return { kind: 'gate', node: gate }
  const end = board.ends?.find(node => node.id === nodeId)
  if (end) return { kind: 'end', node: end }
  const limit = board.limits?.find(node => node.id === nodeId)
  if (limit) return { kind: 'limit', node: limit }
  return null
}

const KIND_WORD = { inputCard: 'Input card', formation: 'Formation', gate: 'Gate', end: 'End node', limit: 'Limit card' } as const
const UNTITLED = { inputCard: 'Input', formation: 'Untitled formation', gate: 'Gate', end: 'End', limit: 'Limit' } as const

/** A time card's warning in a node's run: "#14 · attempt 2 · time warning pasted into Worker", naming the seat by its slot. */
export function limitWarningLine(event: RunEvent, slotName: (slotId: string) => string = slotId => slotId): string {
  const slot = typeof event.data?.slotId === 'string' ? event.data.slotId : ''
  return [`#${event.seq}`, event.attempt ? `attempt ${event.attempt}` : '', slot ? `time warning pasted into ${slotName(slot) || slot}` : 'time warning recorded'].filter(Boolean).join(' · ')
}

/** The window's accessible name, which its close button and handles repeat. */
export function nodeWindowLabel(located: Located): string {
  return `${KIND_WORD[located.kind]} · ${located.node.title || UNTITLED[located.kind]}`
}

export default function NodeWindow({ readOnly = false, runId, nodeId, board, agents, characters, profiles, noteCount, findings, anchor, runState, limitWarnings = [], onClose, ops, initiallyEditTitle = false, creating = false }: {
  readOnly?: boolean
  /** The run on the canvas; a step it seated shows its terminal, a gate what the run did there. */
  runId?: string
  initiallyEditTitle?: boolean
  creating?: boolean
  nodeId: string
  findings?: BoardFinding[]
  /** What the window opens beside; without it, the node's Flow row or card. */
  anchor?: WindowRect
  board: BoardDocument
  agents: AgentProjection[]
  /** The saved characters a slot can be stamped with. */
  characters?: Character[]
  profiles: CodeGateProfileDescriptor[]
  /** Entries in the node's note thread. */
  noteCount: number
  /** The node's state in the run on the canvas; undefined when the run has not reached it. */
  runState: NodeRunState | undefined
  /** The run's limit_warning events on this node: a time card's warning pasted into a seat. */
  limitWarnings?: RunEvent[]
  onClose: () => void
  ops: NodeWindowOps
}) {
  const located = locateNode(board, nodeId)
  const seats = useNodeSeats(located?.kind === 'formation' ? runId : undefined, nodeId)
  const [details, setDetails] = useState(false)
  if (!located) return null
  const withTerminal = hasLiveSeat(seats)
  // A step the run reached opens with a terminal's room, over the canvas; any other window opens beside its card.
  const seated = located.kind === 'formation' && Boolean(runId) && Boolean(runState)
  const flow = buildFlow(board)
  const steps = flow.numbers
  const label = nodeWindowLabel(located)
  const detail = located.kind === 'formation' ? located.node.type
    : located.kind === 'gate' ? located.node.kinds.map(kind => (kind === 'formation' ? 'judge' : kind)).join(', ') || 'no kind'
      : located.kind === 'end' ? located.node.outcome
        : ''
  const judged = flow.judgeOf.get(nodeId)
  const eyebrow = located.kind === 'inputCard' ? 'Input card'
    : steps.has(nodeId) ? `Step ${steps.get(nodeId)} · ${KIND_WORD[located.kind]}${detail ? ` · ${detail}` : ''}`
      : judged ? `Judge of ${steps.has(judged) ? `${steps.get(judged)} ` : ''}${nodeTitle(board, judged)} · ${detail}`
        : `${KIND_WORD[located.kind]}${detail ? ` · ${detail}` : ''}`
  return (
    <AuthoringReadOnly.Provider value={readOnly}>
    <FloatingWindow
      id={`node:${nodeId}`}
      kind="node"
      takeFocus={initiallyEditTitle}
      label={label}
      title={<><span className="nwin-kind">{KIND_WORD[located.kind]}</span> {located.node.title || UNTITLED[located.kind]}</>}
      defaultSize={seated ? { width: 860, height: 680 } : { width: 540, height: 620 }}
      overCanvas={seated}
      className={withTerminal ? 'nwin-terminal-window' : undefined}
      actions={runState !== undefined ? <ProducedFiles nodeId={nodeId} className="nwin-head-produced" /> : undefined}
      anchor={() => anchor || nodeAnchor(nodeId)}
      keepClear={() => nodeWindowKeepClear(nodeId, board.connections)}
      onClose={onClose}
    >
      <div className={`nwin${withTerminal ? ' with-terminal' : ''}${details ? ' details' : ''}`} data-testid={`node-window-${nodeId}`}>
        {!creating && located.kind === 'formation' ? <FormationSetup formation={located.node} agents={agents} characters={characters || NO_CHARACTERS} ops={ops} /> : null}
        {withTerminal ? (
          <button type="button" className="nwin-briefline" aria-expanded={details} onClick={() => setDetails(open => !open)}>
            <span>{(located.kind === 'formation' && located.node.brief?.goal || '').split('\n').find(line => line.trim()) || 'No brief'}</span>
          </button>
        ) : null}
        {!withTerminal || details ? <div className="nwin-fields">
        <div className="nwin-eyebrow">{eyebrow}</div>
        {located.kind === 'gate' && runId && runState !== undefined ? <GateRunFacts runId={runId} gateId={nodeId} onSettle={ops.settleJudge} /> : null}
        <NodeProblems findings={findings} />
        <EditableField initiallyEditing={initiallyEditTitle} label="Title" value={located.node.title} placeholder={UNTITLED[located.kind]} onSave={title => ops.rename(nodeId, title)} />
        {located.kind === 'inputCard' ? <MissionFields mission={located.node} ops={ops} /> : null}
        {!creating && located.kind === 'formation' ? <FormationFields formation={located.node} ops={ops} /> : null}
        {located.kind === 'gate' ? <GateFields gate={located.node} board={board} profiles={profiles} ops={ops} /> : null}
        {located.kind === 'end' ? <EndFields end={located.node} ops={ops} /> : null}
        {located.kind === 'limit' ? <LimitFields limit={located.node} board={board} steps={steps} ops={ops} /> : null}
        {!creating ? <>
        <section className="nwin-section" aria-label="Notes">
          <h3>Notes</h3>
          <div className="nwin-run">
            <span className="nwin-notes">{noteCount ? `${noteCount} ${noteCount === 1 ? 'entry' : 'entries'} in the thread` : 'No notes yet'}</span>
            <button type="button" className="nwin-action" disabled={readOnly} onClick={() => ops.openNotes(nodeId)}>{noteCount ? 'Open notes' : 'Add a note'}</button>
          </div>
        </section>
        <section className="nwin-section" aria-label="Connections">
          <h3>Connections</h3>
          <ul className="nwin-routes">
            {nodeRoutes(board, nodeId, steps).map(route => (
              <li key={`${route.kind}-${route.nodeId || 'end'}-${route.port || ''}`} className={`route-${route.kind}${route.back ? ' back' : ''}`}>
                {route.nodeId
                  ? <button type="button" className="nwin-route" onClick={() => ops.openNode(route.nodeId as string)}>{route.text}</button>
                  : <span className="nwin-route end">{route.text}</span>}
              </li>
            ))}
          </ul>
        </section>
        </> : <p className="nfield-note" role="status">Creating formation…</p>}
        {runState !== undefined ? (
          <section className="nwin-section" aria-label="Run">
            <h3>Run</h3>
            <div className="nwin-run">
              <span className={`nwin-state state-${runState || 'idle'}`}>{RUN_STATE_WORDS[runState]}</span>
              <button type="button" className="nwin-action" onClick={() => ops.inspectEvidence(nodeId)}>Open run evidence</button>
            </div>
            {limitWarnings.length ? (
              <ul className="nwin-list nwin-limit-warnings" data-testid={`limit-warnings-${nodeId}`}>
                {limitWarnings.map(event => <li key={event.seq}>{limitWarningLine(event, slotId => (located.kind === 'formation' ? located.node.slots.find(slot => slot.id === slotId)?.label || '' : ''))}</li>)}
              </ul>
            ) : null}
          </section>
        ) : null}
        </div> : null}
        {withTerminal && seats ? <SeatPane seats={seats} /> : null}
      </div>
    </FloatingWindow>
    </AuthoringReadOnly.Provider>
  )
}

function MissionFields({ mission, ops }: { mission: MissionNode; ops: NodeWindowOps }) {
  return (
    <>
      <EditableField label="Goal" value={mission.goal} multiline markdown placeholder="No goal yet. Say what the mission should achieve."
        onSave={goal => ops.updateInputCard(mission.id, { goal })} />
      <MissionInputsField inputs={mission.inputs} onSave={inputs => ops.updateInputCard(mission.id, { inputs })} />
      <EditableField label="Input hint" value={mission.inputHint || ''} multiline markdown
        placeholder="No input hint. Start mission explains what a brief is."
        hint="What a run's brief should contain, when the mission declares no inputs. Start mission shows it beside the brief."
        onSave={inputHint => ops.updateInputCard(mission.id, { inputHint })} />
      <FilesField files={mission.files} context={mission.title} onSave={files => ops.updateInputCard(mission.id, { files })} />
      <HumanChannelField channel={humanChannelOf(mission)}
        onSave={channel => ops.updateInputCard(mission.id, { humanChannel: humanChannelField(channel) })} />
    </>
  )
}

function FormationFields({ formation, ops }: { formation: FormationNode; ops: NodeWindowOps }) {
  const brief = formation.brief || {}
  const readOnly = useContext(AuthoringReadOnly)
  const saveBrief = (change: FormationBrief) => ops.setBrief(formation.id, {
    goal: brief.goal || '', beadId: brief.beadId || '', files: brief.files || [], links: brief.links || [], ...change,
  })
  const choices = formationTypeChoices(formation)
  return (
    <>
      <div className="nfield">
        <div className="nfield-head"><label className="nfield-label" htmlFor={`type-${formation.id}`}>Type</label></div>
        <select disabled={readOnly} id={`type-${formation.id}`} className="nwin-select" aria-label="Formation type" value=""
          onChange={event => {
            const choice = choices[Number(event.target.value)]
            if (choice) ops.changeType(formation, choice.type, choice.keepSlotId)
          }}>
          <option value="">{formation.type}</option>
          {choices.map((choice, index) => <option key={`${choice.type}-${choice.keepSlotId || ''}`} value={index}>{choice.label}</option>)}
        </select>
      </div>
      <EditableField label="Brief" value={brief.goal || ''} multiline markdown placeholder="No brief yet. Say what this step does and what it returns."
        onSave={goal => saveBrief({ goal })} />
      <EditableField label="Bead" value={brief.beadId || ''} placeholder="No Bead" hint={BEAD_HINT} validate={beadProblem}
        onSave={beadId => saveBrief({ beadId })} />
      <FilesField files={brief.files} context={formation.title} onSave={files => saveBrief({ files })} />
      <EditableField label="Links" value={(brief.links || []).join(', ')} placeholder="No links" hint="Separate links with commas."
        onSave={links => saveBrief({ links: splitList(links) })}>
        {brief.links?.length ? <ul className="nwin-list">{brief.links.map(link => <li key={link}>{link}</li>)}</ul> : null}
      </EditableField>
    </>
  )
}

/** Who does the step, first in its window: each slot with its role, harness, model and effort, each opening its own list. */
const NO_CHARACTERS: Character[] = []

function FormationSetup({ formation, agents, characters, ops }: { formation: FormationNode; agents: AgentProjection[]; characters: Character[]; ops: NodeWindowOps }) {
  return (
    <section className="nsetup" aria-label="Staffing">
      {formation.slots.length ? formation.slots.map(slot => (
        <SlotStaffing key={slot.id} formation={formation} slot={slot} agents={agents} characters={characters} ops={ops} />
      )) : <p className="nwin-empty">This formation has no slots.</p>}
    </section>
  )
}

/** Reference files, one per line when read and each opened in a file window, comma-separated when edited. */
function FilesField({ files, context, hint = 'Separate files with commas.', onSave }: {
  files: string[] | undefined
  /** The node the files belong to, named in each file window. */
  context: string
  hint?: string
  onSave: (files: string[]) => Promise<boolean>
}) {
  return (
    <EditableField label="Files" value={(files || []).join(', ')} placeholder="No files" hint={hint} validate={relativeFileProblem} onSave={value => onSave(splitList(value))}>
      {files?.length ? <FileList files={files} context={context} /> : null}
    </EditableField>
  )
}

function FileList({ files, context, label }: { files: string[]; context: string; label?: string }) {
  const fileWindows = useFileWindows()
  const problems = useFileProblems()
  return (
    <ul className="nwin-list" aria-label={label}>
      {files.map(file => (
        <li key={file}>
          {fileWindows
            ? <button type="button" className="nwin-route" aria-label={`Open file ${file}`} onClick={event => fileWindows.open(referencedFileRequest(file, context), fileAnchor(event.currentTarget))}>{file}</button>
            : file}
          {problems.has(file) ? <span className="nwin-file-problem">{problems.get(file)}</span> : null}
        </li>
      ))}
    </ul>
  )
}

/**
 * A slot's staffing in one row: each pick opens the staffing window dropping
 * from it, on that pick's list (archon-o7p.17).
 */
function SlotStaffing({ formation, slot, agents, characters, ops }: {
  formation: FormationNode
  slot: FormationSlot
  agents: AgentProjection[]
  characters: Character[]
  ops: NodeWindowOps
}) {
  const readOnly = useContext(AuthoringReadOnly)
  const staffing = staffingOf(slot)
  const words = staffingSentence(slot, roleNamer(agents))
  // The sentence window drops from the word clicked, as a dropdown does.
  const open = (part: Part | null) => (event: ReactMouseEvent<HTMLElement>) => ops.staffSlot(formation, slot, part, event.currentTarget)
  const pick = (part: Part, text: string) => (
    <button type="button" disabled={readOnly} className={`npick${part === 'effort' ? ' effort' : ''}`} aria-label={`Change the ${part} of ${slot.label || slot.id}: ${text}`} onClick={open(part)}>
      <small>{part}</small>{text}
    </button>
  )
  // A character fills the whole row in one pick; the slot keeps no link to it.
  const stamp = characters.length ? (
    <select className="npick stamp" disabled={readOnly} aria-label={`Staff ${slot.label || slot.id} as a character`} value=""
      onChange={event => {
        const character = characters.find(entry => entry.id === event.target.value)
        if (character) ops.stampSlot(formation, slot, character)
      }}>
      <option value="">character</option>
      {characters.map(character => <option key={character.id} value={character.id}>{character.name}</option>)}
    </select>
  ) : null
  if (!staffing) {
    return (
      <div className="nslot">
        <span className="nslot-who">{words}</span>
        <button type="button" disabled={readOnly} className="npick" aria-label={`Staff ${slot.label || slot.id}`} onClick={open(null)}>Staff it</button>
        {stamp}
      </div>
    )
  }
  return (
    <div className="nslot">
      <span className="nslot-who">{slotTitle(slot)}</span>
      {pick('role', staffing.role ? roleNamer(agents)(staffing.role) : 'vanilla')}
      {pick('harness', harnessName(staffing.harness) || staffing.harness || 'no harness')}
      {pick('model', modelWords(staffing.model))}
      {pick('effort', staffing.effort || 'no effort')}
      {stamp}
    </div>
  )
}

/** An End node's outcome, chosen in place, and what it means for the run. */
function EndFields({ end, ops }: { end: EndNode; ops: NodeWindowOps }) {
  const readOnly = useContext(AuthoringReadOnly)
  return (
    <div className="nfield">
      <div className="nfield-head"><span className="nfield-label" id={`outcome-${end.id}`}>Outcome</span></div>
      <div className="nwin-outcomes" role="radiogroup" aria-labelledby={`outcome-${end.id}`}>
        {END_OUTCOMES.map(outcome => (
          <button key={outcome} type="button" disabled={readOnly} role="radio" aria-checked={end.outcome === outcome}
            className={`nwin-outcome end-${outcome}${end.outcome === outcome ? ' on' : ''}`}
            onClick={() => { if (end.outcome !== outcome) void ops.setEndOutcome(end, outcome) }}>
            {defaultEndTitle(outcome)}
          </button>
        ))}
      </div>
      <p className="nfield-note">{endOutcomeMeaning(end.outcome)}</p>
    </div>
  )
}

/**
 * A Limit card's target, rounds, time and warning, edited in place, and what it does to a run
 * in words. A refusal from the server reads under the field it came from.
 */
function LimitFields({ limit, board, steps, ops }: { limit: LimitNode; board: BoardDocument; steps: ReadonlyMap<string, number>; ops: NodeWindowOps }) {
  const [targetError, setTargetError] = useState('')
  const [saving, setSaving] = useState(false)
  const coverage = limitCoverage(board, limit)
  const others = limit.target ? limitsCovering(board, limit.target).filter(other => other.id !== limit.id) : []
  const stepName = (formation: FormationNode) => `${steps.has(formation.id) ? `${steps.get(formation.id)} ` : ''}${formation.title || formation.id}`
  const changeTarget = async (target: string) => {
    if (target === limit.target) return
    setSaving(true)
    const saved = await ops.setLimit(limit, { target })
    setSaving(false)
    setTargetError(saved === true ? '' : saved)
  }
  return (
    <>
      <div className="nfield">
        <div className="nfield-head"><label className="nfield-label" htmlFor={`limit-target-${limit.id}`}>Covers</label></div>
        <select id={`limit-target-${limit.id}`} className="nwin-select" aria-label="Covers" value={coverage.kind === 'none' || coverage.kind === 'missing' ? '' : limit.target}
          disabled={saving} onChange={event => void changeTarget(event.target.value)}>
          <option value="">{coverage.kind === 'missing' ? 'A step that is gone' : 'Nothing yet'}</option>
          {(board.inputCards || []).map(card => <option key={card.id} value={card.id}>Input card — the whole mission</option>)}
          {[...board.formations].sort((a, b) => (steps.get(a.id) ?? Infinity) - (steps.get(b.id) ?? Infinity)).map(formation => (
            <option key={formation.id} value={formation.id}>{stepName(formation)}</option>
          ))}
        </select>
        {targetError ? <p className="nfield-note error" role="alert">{targetError}</p> : null}
        {others.length ? <p className="nfield-note error">{`${coverage.kind === 'mission' ? 'The Input card' : coverage.kind === 'step' ? coverage.node.title : limit.target} has another Limit card, ${others.map(other => other.title).join(', ')}: keep one.`}</p> : null}
      </div>
      <EditableField label="Rounds" value={limit.rounds ? String(limit.rounds) : ''} placeholder="No rounds set"
        hint="A step's runs, a peer step's journal messages, or the whole mission's step runs. Leave it blank for no limit."
        validate={roundsProblem} onSave={value => ops.setLimit(limit, { rounds: value ? Number(value) : 0 })}>
        {limit.rounds ? limitKnobWords(board, { ...limit, seconds: undefined, tokens: undefined }) : undefined}
      </EditableField>
      <EditableField label="Time" value={durationInput(limit.seconds)} placeholder="No time set"
        hint="How long the work may run, such as 45s, 30m or 1h30m. Waiting on a human gate does not count. Leave it blank for no time limit."
        validate={timeProblem} onSave={value => ops.setLimit(limit, { seconds: parseDuration(value) || 0 })}>
        {limit.seconds ? `${durationWords(limit.seconds)} of work` : undefined}
      </EditableField>
      <EditableField label="Warning" value={durationInput(limit.warnSeconds)} placeholder={limit.seconds ? 'No warning' : 'No warning; it needs time'}
        hint="How much time is left when Archon pastes a warning into the seats, such as 5m. It must be shorter than the time. Leave it blank for no warning."
        validate={value => warnProblem(value, limit.seconds)} onSave={value => ops.setLimit(limit, { warnSeconds: parseDuration(value) || 0 })}>
        {limit.warnSeconds ? `warns at ${durationWords(limit.warnSeconds)} left` : undefined}
      </EditableField>
      <EditableField label="Tokens" value={limit.tokens ? String(limit.tokens) : ''} placeholder="No tokens set"
        hint={`How many tokens the work may spend. ${TOKENS_DEFINITION} Leave it blank for no limit.`}
        validate={tokensProblem} onSave={value => ops.setLimit(limit, { tokens: value ? Number(value) : 0 })}>
        {limit.tokens ? tokenWords(limit.tokens) : undefined}
      </EditableField>
      <p className="nfield-note limit-meaning" data-testid={`limit-meaning-${limit.id}`}>{limitMeaning(board, limit)}</p>
    </>
  )
}

function GateFields({ gate, board, profiles, ops }: { gate: GateNode; board: BoardDocument; profiles: CodeGateProfileDescriptor[]; ops: NodeWindowOps }) {
  const readOnly = useContext(AuthoringReadOnly)
  const chain = judgeChain(board, gate.id)
  const judged = gate.kinds.includes('formation') || chain.length > 0
  // A judge's brief files show on the gate too, unless the gate names them itself.
  const judgeFiles = nodeFileRefs(board, gate.id).filter(file => file.judge)
  const judges = [...new Set(judgeFiles.map(file => file.owner))]
  return (
    <>
      <GateKindsEditor gate={gate} profiles={profiles} hasJudgeChain={chain.length > 0} ops={ops} />
      <EditableField label="Criterion" value={gate.criterion} multiline markdown placeholder="No criterion yet. Say what passes."
        onSave={criterion => ops.updateGate(gate, { ...draftFromGate(gate), criterion })} />
      <FilesField files={gate.files} context={gate.title} hint="Separate files with commas. A rubric belongs here." onSave={files => ops.setGateFiles(gate, files)} />
      {judged ? (
        <div className="nfield">
          <div className="nfield-head"><label className="nfield-label" htmlFor={`judge-${gate.id}`}>Judge</label></div>
          <div className="nslot-controls">
            <select disabled={readOnly} id={`judge-${gate.id}`} aria-label="Judge formation" value={chain.length === 1 ? chain[0] : ''}
              onChange={event => { if (event.target.value) ops.attachJudge(gate, [event.target.value]) }}>
              <option value="">{chain.length > 1 ? `A chain of ${chain.length} formations` : 'Choose a judge formation'}</option>
              {board.formations.map(formation => <option key={formation.id} value={formation.id}>{formation.title || formation.id}</option>)}
            </select>
            {chain.length ? <button type="button" className="nwin-action" disabled={readOnly} onClick={() => ops.detachJudge(gate)}>Detach judge</button> : null}
          </div>
          {judges.map(judge => (
            <div key={judge} className="nwin-judge-files">
              <span className="nfield-note">From {judge}'s brief</span>
              <FileList files={judgeFiles.filter(file => file.owner === judge).map(file => file.ref)} context={`${judge} (judge)`} label={`Judge ${judge}'s brief files`} />
            </div>
          ))}
        </div>
      ) : null}
    </>
  )
}

function GateKindsEditor({ gate, profiles, hasJudgeChain, ops }: { gate: GateNode; profiles: CodeGateProfileDescriptor[]; hasJudgeChain: boolean; ops: NodeWindowOps }) {
  const readOnly = useContext(AuthoringReadOnly)
  const [draft, setDraft] = useState<GateDraft | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const check = gate.kinds.includes('code')
    ? gate.check ? `${gate.check}@${gate.checkVersion || '?'}${gate.checkValue ? ` · ${gate.checkValue}` : ''}` : 'check not chosen'
    : ''
  const save = async () => {
    if (!draft) return
    setSaving(true)
    let saved: boolean
    try { saved = await ops.updateGate(gate, { ...draftFromGate(gate), kinds: draft.kinds, profileKey: draft.profileKey, checkValue: draft.checkValue }) }
    catch (reason) { setError(reason instanceof Error ? reason.message : String(reason)); return }
    finally { setSaving(false) }
    if (saved) { setDraft(null); setError('') } else setError('The kinds were not saved.')
  }
  return (
    <div className={`nfield${draft ? ' editing' : ''}`}>
      <div className="nfield-head">
        <span className="nfield-label">Kinds</span>
        {draft ? null : <button type="button" className="nfield-edit" disabled={readOnly} aria-label="Edit kinds" onClick={() => setDraft(draftFromGate(gate))}>Edit</button>}
      </div>
      {draft ? (
        <form className="nfield-form gate-editor" onSubmit={event => { event.preventDefault(); void save() }}
          onKeyDown={event => { if (event.key === 'Escape') { event.preventDefault(); setDraft(null) } }}>
          <GateKindsFields draft={draft} onChange={update => setDraft(current => (current ? update(current) : current))} profiles={profiles}
            hasJudgeChain={hasJudgeChain} disabled={saving} idPrefix={`node-${gate.id}`} />
          {error ? <p className="nfield-note error" role="alert">{error}</p> : null}
          <div className="nfield-actions">
            <button type="button" onClick={() => setDraft(null)} disabled={saving}>Cancel</button>
            <button type="submit" className="primary" aria-label="Save kinds" disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
          </div>
        </form>
      ) : (
        <div className="nfield-value">
          <GateKindChips gateId={`window-${gate.id}`} kinds={gate.kinds} />
          {check ? <div className="nwin-check">Code check: {check}</div> : null}
        </div>
      )}
    </div>
  )
}
