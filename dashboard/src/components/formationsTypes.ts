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

/** One authored message in a mission or element note thread. */
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
  missionId: string
  rev: number
  updatedAt: string
  updatedBy?: string
  mission: NoteEntry[]
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

/** The `addPort` direction, in the server's vocabulary (FormationPortInput/Output). */
export type FormationPortDirection = 'input' | 'output'

export interface FormationSlot {
  id: string
  label: string
  controller: boolean
  /** The slot's optional role (persona id); a slot without one is a vanilla agent. */
  agentId?: string
  /** What the slot's seat runs. A blank model is the harness default. */
  harness?: string
  model?: string
  effort?: string
}

export interface FormationBrief {
  goal?: string
  beadId?: string
  files?: string[]
  links?: string[]
}

export type FormationType = 'solo' | 'peer' | 'orchestrated'

export interface FormationExecutionPolicy {
  /** Positive seconds for the whole formation invocation; an omitted policy means no time limit. */
  timeoutSeconds: number
}

export interface FormationNode {
  id: string
  type: FormationType
  title: string
  brief?: FormationBrief
  execution?: FormationExecutionPolicy
  inputs: FormationPort[]
  outputs: FormationPort[]
  slots: FormationSlot[]
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
  inputCards?: MissionNode[]
  formations: FormationNode[]
  gates?: GateNode[]
  tools?: ToolNode[]
  ends?: EndNode[]
  limits?: LimitNode[]
  connections: BoardConnection[]
}

/** A located problem a run would hit. Authoring accepts drafts; mission
 * validation and run admission list these instead. nodeId names a node, a
 * connection, or nothing for mission-level findings. */
export interface BoardFinding {
  code: string
  nodeId: string
  message: string
}

export interface BoardValidation {
  missionRev: number
  missionEtag: string
  errors: BoardFinding[]
  warnings: BoardFinding[]
}

export interface MissionNode {
  id: string
  title: string
  goal: string
  /** What a run's brief should contain; Start mission shows it when set. */
  inputHint?: string
  /** Reference file paths; the cockpit opens absolute ones. */
  files?: string[]
  /** How its human gates reach the operator (ADR-0019): absent or notify notifies, session asks the agents. A patch sends '' to clear it. */
  humanChannel?: '' | 'notify' | 'session'
}

export interface GateNode {
  id: string
  title: string
  kinds: string[]
  criterion: string
  check?: string
  checkVersion?: string
  checkValue?: string
  /** Reference file paths, such as the gate's rubric. */
  files?: string[]
}

export type EndOutcome = 'done' | 'rejected'

/** Ends a path on purpose (archon-o7p.10). Its only port is `in`, which takes any number of routes. */
export interface EndNode {
  id: string
  title: string
  outcome: EndOutcome
}

/**
 * Caps the rounds of the step it covers, or of the whole mission when it covers
 * the Input card (archon-o7p.8). It has no ports: `target` names what it covers,
 * '' while it is wired to nothing, and `rounds` is absent while it sets none.
 */
export interface LimitNode {
  id: string
  title: string
  target: string
  rounds?: number
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
  missionSlug: string
  inputCardId: string
  eventCount: number
  waitingGates?: WaitingGate[]
  resumeAllowed?: boolean
  /** grant while the run is blocked at a spent Limit card: it resumes only with one more round granted. */
  resumePolicy?: 'grant'
  /** The channel frozen from the run's mission (ADR-0019); formation-only runs notify. */
  humanChannel?: 'notify' | 'session'
  /** Seats kept after their formation finished, to answer human gate asks. */
  onCallSeats?: OnCallSeat[]
  /** Who failed or canceled a final run; why is in its run evidence problems. */
  endedBy?: string
}

/** A seat that received a human gate's ask on a session-channel run. */
export interface AskedSeat {
  nodeId: string
  slotId: string
  createdSeq: number
  deliveredSeq?: number
}

export interface WaitingGate {
  gateId: string
  requestedSeq: number
  /** Seats the ask was delivered to; empty until one receives it. */
  askedSeats?: AskedSeat[]
  /** Why the ask went to the notify command instead of the agents. */
  fallbackReason?: string
}

export interface OnCallSeat {
  nodeId: string
  slotId: string
  createdSeq: number
  keptSeq: number
  /** Pending asks delivered to this seat. */
  waitingOn: Array<{ gateId: string; requestedSeq: number }>
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
  missionId: string
  missionRev: number
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
  model?: string
  effort?: string
  source?: string
  /** Derived by the daemon on read: the effort seats run at (unset is medium). */
  effectiveEffort?: string
  /** Derived: the efforts this harness accepts; absent when Archon cannot start it. */
  efforts?: string[]
  /** Derived: the command a seat for this variant runs, from the seat launcher itself. */
  seatLaunch?: string
  seatLaunchError?: string
}

/** A harness whose seats Archon starts from model and effort (GET /api/agents data.harnesses). */
export interface LaunchableHarness {
  id: string
  executable: string
  efforts: string[]
  defaultEffort: string
}

/** One line of the effort policy the roster serves: which effort suits which work. */
export interface EffortPolicyEntry {
  effort: string
  use: string
}

/** One variant's model and effort in a persona patch; an empty string clears the setting. */
export interface VariantSettingsPatch {
  id: string
  model?: string
  effort?: string
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

