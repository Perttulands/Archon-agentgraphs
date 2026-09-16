import { useState } from 'react'
import { formationTypeChoices } from '../components/FormationTypeChip'
import { GateKindChips, GateKindsFields, draftFromGate, type GateDraft } from '../components/GateEditorDialog'
import { isSafeBeadsIssueID } from '../components/formationsBeadId'
import { splitList } from '../components/formationsCockpitDom'
import type { NodeRunState } from '../components/formationsRunState'
import type {
  AgentProjection,
  BoardDocument,
  CodeGateProfileDescriptor,
  FormationBrief,
  FormationNode,
  FormationSlot,
  FormationType,
  GateNode,
  MissionNode,
  PersonaCard,
} from '../components/formationsTypes'
import { useFileWindows } from '../files/FileWindows'
import { ProducedFiles } from '../files/ProducedFiles'
import { referencedFileRequest } from '../files/fileWindowModel'
import FloatingWindow from '../windows/FloatingWindow'
import { EditableField } from './EditableField'
import { judgeChain, nodeRoutes, stepNumbers } from './boardRoutes'
import { usePersonaCards } from './usePersonaCards'
import './nodeWindow.css'

/**
 * A mission, formation or gate opened from its card: every field read in full
 * and edited in place, its staffing and routes stated in words, and its run
 * state with a way into the evidence. Each save is one board change with its
 * own undo entry.
 */

export interface NodeWindowOps {
  rename: (nodeId: string, title: string) => Promise<boolean>
  updateMission: (missionId: string, fields: Partial<Pick<MissionNode, 'goal' | 'beadId' | 'inputHint' | 'files'>>) => Promise<boolean>
  setBrief: (formationId: string, brief: FormationBrief) => Promise<boolean>
  changeType: (formation: FormationNode, type: FormationType, keepSlotId?: string) => void
  assignSlot: (formation: FormationNode, slot: FormationSlot, agentId: string, harness: string) => void
  updateGate: (gate: GateNode, draft: GateDraft) => Promise<boolean>
  setGateFiles: (gate: GateNode, files: string[]) => Promise<boolean>
  attachJudge: (gate: GateNode, chain: string[]) => void
  detachJudge: (gate: GateNode) => void
  openNode: (nodeId: string) => void
  /** Opens the node's note thread in its own note window. */
  openNotes: (nodeId: string) => void
  inspectEvidence: (nodeId: string) => void
}

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
  | { kind: 'mission'; node: MissionNode }
  | { kind: 'formation'; node: FormationNode }
  | { kind: 'gate'; node: GateNode }

export function locateNode(board: Pick<BoardDocument, 'formations'> & Partial<Pick<BoardDocument, 'missions' | 'gates'>>, nodeId: string): Located | null {
  const mission = board.missions?.find(node => node.id === nodeId)
  if (mission) return { kind: 'mission', node: mission }
  const formation = board.formations.find(node => node.id === nodeId)
  if (formation) return { kind: 'formation', node: formation }
  const gate = board.gates?.find(node => node.id === nodeId)
  if (gate) return { kind: 'gate', node: gate }
  return null
}

const KIND_WORD = { mission: 'Mission', formation: 'Formation', gate: 'Gate' } as const
const UNTITLED = { mission: 'Untitled mission', formation: 'Untitled formation', gate: 'Gate' } as const

/** Where a node's card sits on screen, so its window opens beside it. */
function cardRect(nodeId: string) {
  const card = document.querySelector(`[data-node="${nodeId.replace(/["\\]/g, '\\$&')}"]`)
  if (!card) return null
  const { left, top, width, height } = card.getBoundingClientRect()
  return { left, top, width, height }
}

/** The window's accessible name, which its close button and handles repeat. */
export function nodeWindowLabel(located: Located): string {
  return `${KIND_WORD[located.kind]} · ${located.node.title || UNTITLED[located.kind]}`
}

export default function NodeWindow({ nodeId, board, agents, profiles, noteCount, runState, onClose, ops }: {
  nodeId: string
  board: BoardDocument
  agents: AgentProjection[]
  profiles: CodeGateProfileDescriptor[]
  /** Entries in the node's note thread. */
  noteCount: number
  /** The node's state in the run on the canvas; undefined when the run has not reached it. */
  runState: NodeRunState | undefined
  onClose: () => void
  ops: NodeWindowOps
}) {
  const located = locateNode(board, nodeId)
  if (!located) return null
  const steps = stepNumbers(board)
  const label = nodeWindowLabel(located)
  const detail = located.kind === 'formation' ? located.node.type
    : located.kind === 'gate' ? located.node.kinds.map(kind => (kind === 'formation' ? 'judge' : kind)).join(', ') || 'no kind'
      : ''
  return (
    <FloatingWindow
      id={`node:${nodeId}`}
      kind="node"
      label={label}
      title={<><span className="nwin-kind">{KIND_WORD[located.kind]}</span> {located.node.title || UNTITLED[located.kind]}</>}
      defaultSize={{ width: 540, height: 620 }}
      anchor={() => cardRect(nodeId)}
      onClose={onClose}
    >
      <div className="nwin" data-testid={`node-window-${nodeId}`}>
        <div className="nwin-eyebrow">Step {steps.get(nodeId) ?? '?'} · {KIND_WORD[located.kind]}{detail ? ` · ${detail}` : ''}</div>
        <EditableField label="Title" value={located.node.title} placeholder={UNTITLED[located.kind]} onSave={title => ops.rename(nodeId, title)} />
        {located.kind === 'mission' ? <MissionFields mission={located.node} ops={ops} /> : null}
        {located.kind === 'formation' ? <FormationFields formation={located.node} agents={agents} ops={ops} /> : null}
        {located.kind === 'gate' ? <GateFields gate={located.node} board={board} profiles={profiles} ops={ops} /> : null}
        <section className="nwin-section" aria-label="Notes">
          <h3>Notes</h3>
          <div className="nwin-run">
            <span className="nwin-notes">{noteCount ? `${noteCount} ${noteCount === 1 ? 'entry' : 'entries'} in the thread` : 'No notes yet'}</span>
            <button type="button" className="nwin-action" onClick={() => ops.openNotes(nodeId)}>{noteCount ? 'Open notes' : 'Add a note'}</button>
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
        {runState !== undefined ? (
          <section className="nwin-section" aria-label="Run">
            <h3>Run</h3>
            <div className="nwin-run">
              <span className={`nwin-state state-${runState || 'idle'}`}>{RUN_STATE_WORDS[runState]}</span>
              <button type="button" className="nwin-action" onClick={() => ops.inspectEvidence(nodeId)}>Open run evidence</button>
            </div>
            <ProducedFiles nodeId={nodeId} className="nwin-produced" />
          </section>
        ) : null}
      </div>
    </FloatingWindow>
  )
}

function MissionFields({ mission, ops }: { mission: MissionNode; ops: NodeWindowOps }) {
  return (
    <>
      <EditableField label="Goal" value={mission.goal} multiline markdown placeholder="No goal yet. Say what the mission should achieve."
        onSave={goal => ops.updateMission(mission.id, { goal })} />
      <EditableField label="Input hint" value={mission.inputHint || ''} multiline markdown
        placeholder="No input hint. Start mission explains what a brief is."
        hint="What a run's brief should contain. Start mission shows it beside the brief."
        onSave={inputHint => ops.updateMission(mission.id, { inputHint })} />
      <EditableField label="Bead" value={mission.beadId} placeholder="No Bead" hint={BEAD_HINT} validate={beadProblem}
        onSave={beadId => ops.updateMission(mission.id, { beadId })} />
      <FilesField files={mission.files} context={mission.title} onSave={files => ops.updateMission(mission.id, { files })} />
    </>
  )
}

function FormationFields({ formation, agents, ops }: { formation: FormationNode; agents: AgentProjection[]; ops: NodeWindowOps }) {
  const brief = formation.brief || {}
  const saveBrief = (change: FormationBrief) => ops.setBrief(formation.id, {
    goal: brief.goal || '', beadId: brief.beadId || '', files: brief.files || [], links: brief.links || [], ...change,
  })
  const choices = formationTypeChoices(formation)
  const cards = usePersonaCards(formation.slots.map(slot => slot.agentId || ''))
  return (
    <>
      <div className="nfield">
        <div className="nfield-head"><label className="nfield-label" htmlFor={`type-${formation.id}`}>Type</label></div>
        <select id={`type-${formation.id}`} className="nwin-select" aria-label="Formation type" value=""
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
      <section className="nwin-section" aria-label="Staffing">
        <h3>Staffing</h3>
        {formation.slots.length ? formation.slots.map(slot => (
          <SlotStaffing key={slot.id} formation={formation} slot={slot} agents={agents} card={slot.agentId ? cards.get(slot.agentId) : undefined} ops={ops} />
        )) : <p className="nwin-empty">This formation has no slots.</p>}
      </section>
    </>
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
  const fileWindows = useFileWindows()
  return (
    <EditableField label="Files" value={(files || []).join(', ')} placeholder="No files" hint={hint} onSave={value => onSave(splitList(value))}>
      {files?.length ? (
        <ul className="nwin-list">
          {files.map(file => (
            <li key={file}>
              {fileWindows
                ? <button type="button" className="nwin-route" aria-label={`Open file ${file}`} onClick={() => fileWindows.open(referencedFileRequest(file, context))}>{file}</button>
                : file}
            </li>
          ))}
        </ul>
      ) : null}
    </EditableField>
  )
}

/** "Worker 1 is Codex builder (codex) on openai-codex, model gpt-5, medium effort." A persona that sets neither uses its harness's defaults, and says so. */
export function staffingSentence(slot: FormationSlot, agent: AgentProjection | undefined, card: PersonaCard | undefined): string {
  const role = `${slot.label || slot.id}${slot.controller ? ' (controller)' : ''}`
  if (!slot.agentId) return `${role} is not staffed.`
  const name = card?.displayName || agent?.displayName || slot.agentId
  const harness = slot.harness || card?.harnessDefault || agent?.harnessDefault || ''
  const variant = card?.harnessVariants.find(item => item.id === harness)
  const parts = [
    `${role} is ${name}${name === slot.agentId ? '' : ` (${slot.agentId})`}`,
    harness ? `on ${harness}` : 'with no harness',
  ]
  if (!harness) return `${parts.join(' ')}.`
  const model = variant?.model ? `model ${variant.model}` : 'default model'
  const effort = variant?.effort ? `${variant.effort} effort` : 'default effort'
  return `${parts.join(' ')}, ${model}, ${effort}.`
}

function SlotStaffing({ formation, slot, agents, card, ops }: {
  formation: FormationNode
  slot: FormationSlot
  agents: AgentProjection[]
  card: PersonaCard | undefined
  ops: NodeWindowOps
}) {
  const agent = agents.find(candidate => candidate.id === slot.agentId)
  const choices = agents.filter(candidate => candidate.assignable || candidate.id === slot.agentId)
  const harnesses = card?.harnessVariants.map(variant => variant.id) || []
  const slotName = slot.label || slot.id
  return (
    <div className="nslot">
      <p className="nslot-words">{staffingSentence(slot, agent, card)}</p>
      <div className="nslot-controls">
        <select aria-label={`Persona for ${slotName}`} value={slot.agentId || ''}
          onChange={event => {
            const chosen = agents.find(candidate => candidate.id === event.target.value)
            ops.assignSlot(formation, slot, chosen?.id || '', chosen?.harnessDefault || '')
          }}>
          <option value="">Not staffed</option>
          {slot.agentId && !agent ? <option value={slot.agentId}>{slot.agentId}</option> : null}
          {choices.map(candidate => <option key={candidate.id} value={candidate.id}>{candidate.displayName || candidate.id}</option>)}
        </select>
        {slot.agentId && harnesses.length > 1 ? (
          <select aria-label={`Harness for ${slotName}`} value={slot.harness || card?.harnessDefault || ''}
            onChange={event => ops.assignSlot(formation, slot, slot.agentId as string, event.target.value)}>
            {harnesses.map(harness => <option key={harness} value={harness}>{harness}</option>)}
          </select>
        ) : null}
      </div>
    </div>
  )
}

function GateFields({ gate, board, profiles, ops }: { gate: GateNode; board: BoardDocument; profiles: CodeGateProfileDescriptor[]; ops: NodeWindowOps }) {
  const chain = judgeChain(board, gate.id)
  const judged = gate.kinds.includes('formation') || chain.length > 0
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
            <select id={`judge-${gate.id}`} aria-label="Judge formation" value={chain.length === 1 ? chain[0] : ''}
              onChange={event => { if (event.target.value) ops.attachJudge(gate, [event.target.value]) }}>
              <option value="">{chain.length > 1 ? `A chain of ${chain.length} formations` : 'Choose a judge formation'}</option>
              {board.formations.map(formation => <option key={formation.id} value={formation.id}>{formation.title || formation.id}</option>)}
            </select>
            {chain.length ? <button type="button" className="nwin-action" onClick={() => ops.detachJudge(gate)}>Detach judge</button> : null}
          </div>
        </div>
      ) : null}
    </>
  )
}

function GateKindsEditor({ gate, profiles, hasJudgeChain, ops }: { gate: GateNode; profiles: CodeGateProfileDescriptor[]; hasJudgeChain: boolean; ops: NodeWindowOps }) {
  const [draft, setDraft] = useState<GateDraft | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const check = gate.kinds.includes('code')
    ? gate.check ? `${gate.check}@${gate.checkVersion || '?'}${gate.checkValue ? ` · ${gate.checkValue}` : ''}` : 'check not chosen'
    : ''
  const save = async () => {
    if (!draft) return
    setSaving(true)
    const saved = await ops.updateGate(gate, { ...draftFromGate(gate), kinds: draft.kinds, profileKey: draft.profileKey, checkValue: draft.checkValue })
    setSaving(false)
    if (saved) { setDraft(null); setError('') } else setError('The kinds were not saved.')
  }
  return (
    <div className={`nfield${draft ? ' editing' : ''}`}>
      <div className="nfield-head">
        <span className="nfield-label">Kinds</span>
        {draft ? null : <button type="button" className="nfield-edit" aria-label="Edit kinds" onClick={() => setDraft(draftFromGate(gate))}>Edit</button>}
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
