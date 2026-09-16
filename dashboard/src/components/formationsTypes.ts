export interface BoardSummary {
  id: string
  slug: string
  title: string
  rev: number
  etag: string
}

export interface BoardDeletion {
  id: string
  slug: string
  title: string
  archiveId: string
}

/** One authored message in a board or element note thread. */
export interface NoteEntry {
  id: string
  /** human:<name> or agent:<name> */
  author: string
  createdAt: string
  editedAt?: string
  text: string
}

export interface ElementNote {
  nodeId: string
  entries: NoteEntry[]
}

export interface BoardNotesDocument {
  schema: number
  boardId: string
  rev: number
  updatedAt: string
  updatedBy?: string
  board: NoteEntry[]
  elements: ElementNote[]
  etag: string
}

/** Appends by default; edit and delete name one of the caller's own entries. */
export type NotePatch =
  | { target: string; action: 'append'; text: string }
  | { target: string; action: 'edit'; entryId: string; text: string }
  | { target: string; action: 'delete'; entryId: string }

export interface FormationPort {
  id: string
  label: string
}

export interface FormationSlot {
  id: string
  label: string
  controller: boolean
  agentId?: string
  harness?: string
}

export interface FormationBrief {
  goal?: string
  beadId?: string
  files?: string[]
  links?: string[]
}

export interface FormationVerification {
  id?: string
  kinds?: string[]
  criterion?: string
  onFail?: string
}

export type FormationType = 'solo' | 'peer' | 'orchestrated'

export interface FormationNode {
  id: string
  /** A board saved before a type was retired can still carry it; the card
   *  shows it so the operator can change it. */
  type: FormationType | (string & {})
  title: string
  brief?: FormationBrief
  inputs: FormationPort[]
  outputs: FormationPort[]
  slots: FormationSlot[]
  verification?: FormationVerification
}

export type ToolParameterValue = string | boolean | number

interface ToolPortBase {
  id: string
  name: string
  label: string
  kind: 'work'
  acceptedMediaTypes: string[]
}

export type ToolPort = ToolPortBase & (
  | { direction: 'input'; required?: boolean; role?: 'data' }
  | { direction: 'output'; required?: never; role?: never }
)

export interface ToolNode {
  id: string
  title: string
  profileId: string
  profileVersion: string
  params: Record<string, ToolParameterValue>
  inputs: ToolPort[]
  outputs: ToolPort[]
}

export interface BoardConnection {
  id: string
  from: string
  to: string
}

export interface BoardDocument {
  id: string
  slug: string
  title: string
  rev: number
  etag: string
  missions?: MissionNode[]
  formations: FormationNode[]
  gates?: GateNode[]
  tools?: ToolNode[]
  connections: BoardConnection[]
}

/** A located problem a run would hit. Authoring accepts drafts; board
 * validation and run admission list these instead. nodeId names a node, a
 * connection, or nothing for board-level findings. */
export interface BoardFinding {
  code: string
  nodeId: string
  message: string
}

export interface BoardValidation {
  boardRev: number
  boardEtag: string
  errors: BoardFinding[]
  warnings: BoardFinding[]
}

export interface MissionNode {
  id: string
  title: string
  goal: string
  beadId: string
  /** What a run's brief should contain; Start mission shows it when set. */
  inputHint?: string
}

export interface GateNode {
  id: string
  title: string
  kinds: string[]
  criterion: string
  check?: string
  checkVersion?: string
  checkValue?: string
}

export interface CodeGateProfileDescriptor {
  profileId: string
  profileVersion: string
  displayName: string
  parameterName: string
  parameterLabel: string
}

export interface RunStatusProjection {
  cwd?: string
  beadId?: string
  runId: string
  status: string
  final: boolean
  boardSlug: string
  missionId: string
  eventCount: number
  waitingGates?: Array<{ gateId: string; requestedSeq: number }>
  resumeAllowed?: boolean
}

export interface RunEvent {
  seq: number
  type: string
  runId: string
  nodeId?: string
  gateId?: string
  attempt?: number
  data?: Record<string, unknown>
}

export interface OpenEscalation {
  runId: string
  seq: number
  nodeId?: string
  gateId?: string
  severity: string
  reason: string
  source: string
  trigger: string
  blocks: boolean
}

export interface RunStartResult {
  runId: string
  status: RunStatusProjection
}

export interface RunStatusResult {
  status: RunStatusProjection
}

export interface LayoutNode {
  id: string
  x: number
  y: number
}

export interface LayoutEdge {
  id: string
  lane: string
}

export interface LayoutDocument {
  boardId: string
  boardRev: number
  etag: string
  nodes: LayoutNode[]
  edges?: LayoutEdge[]
}

export interface ViewTransform {
  x: number
  y: number
  scale: number
}

export interface ContextMenuItem {
  label: string
  action?: () => void
  destructive?: boolean
  disabled?: boolean
}

export interface ContextMenuState {
  label: string
  x: number
  y: number
  items: ContextMenuItem[]
}

export interface AgentProjection {
  id: string
  displayName?: string
  kind?: string
  tags?: string[]
  harnessDefault?: string
  assignable: boolean
  unbound?: boolean
  liveness?: string
  preset?: boolean
  customized?: boolean
}

export interface PersonaHarnessVariant {
  id: string
  sessionStem?: string
  launch?: string
  source?: string
}

export interface PersonaCard {
  id: string
  displayName?: string
  kind: string
  summary?: string
  tags: string[]
  status?: string
  harnessDefault: string
  harnessVariants: PersonaHarnessVariant[]
  etag: string
  preset?: boolean
  customized?: boolean
}

export interface BriefDraft {
  goal: string
  beadId: string
  files: string
  links: string
}

export interface TerminalPopup {
  agentId: string
  title: string
  liveness: string
  x: number
  y: number
  width: number
  height: number
  focusedAt: number
  dragged?: boolean
  resized?: boolean
}

export type WireDragState =
  | { kind: 'new'; from: string }
  | { kind: 'reconnect-target'; connection: BoardConnection }
  | { kind: 'judge'; gateId: string }

export type UndoAction =
  | { kind: 'deleteFormation'; formationId: string }
  | { kind: 'deleteGate'; gateId: string }
  | { kind: 'deleteMission'; missionId: string }
  | { kind: 'assignSlot'; formationId: string; slotId: string; agentId: string; harness: string }
  | { kind: 'makeController'; formationId: string; slotId: string }
  | { kind: 'setBrief'; formationId: string; brief?: FormationBrief }
  | { kind: 'removePort'; formationId: string; portId: string }
  | { kind: 'unwireConnection'; from: string; to: string }
  | { kind: 'moveNode'; node: LayoutNode }

export type BoardUndoAction = Exclude<UndoAction, { kind: 'moveNode'; node: LayoutNode }>
