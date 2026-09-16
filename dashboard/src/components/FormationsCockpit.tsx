import { StartMissionDialog, type RunInputs } from "./StartMissionDialog"
/* FormationsCockpit — spatial board editor for Archon.
 *
 * Ported from the D7 prototype (Perttus_vision_for_agent_orchestration/03-formations.{html,js}):
 * left Agent Roster (drag an agent into a slot to staff it), an infinite pan/zoom
 * world canvas, compact typed formation cards with circular slot-spheres, mission and
 * gate cards, and SVG wires. Reuses the sound file/model/API plumbing
 * (formationsApi/Types/Canvas/RunState) — only the broken form-style view layer is replaced.
 *
 * This pass implements: roster + drag-to-staff, typed cards with slot spheres,
 * mission/gate cards, wires, port-drag wiring, pan/zoom/fit, card drag-to-reposition,
 * and run buttons with honest per-node run-state projection. Wire reconnect/delete,
 * context menus, on-canvas editors/terminals, and undo are tracked for follow passes
 * (bead home-f7as).
 */
import { type CSSProperties, MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent, lazy, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import {
  ApiRequestError,
  abortRunRequest,
  createBoard,
  deleteBoard,
  fetchAgents,
  fetchBoardChanged,
  fetchBoardNotes,
  fetchBoardSummaries,
  fetchBoardValidation,
  fetchBoardWithLayout,
  fetchCodeGateProfiles,
  fetchRunEscalations,
  fetchRunEvents,
  fetchBoardRuns,
  fetchRunStatus,
  missingLayoutForBoard,
  patchBoardNote,
  patchBoardDocument,
  patchBoardLayout,
  recordGateVerdict,
  resumeRunRequest,
  startRun,
} from './formationsApi'
import {
  activeRunStorageKey,
  openHumanGateId,
  projectNodeStates,
  runCurrentPoint,
  runStatusFromResponse,
  upsertRunEvent,
} from './formationsRunState'
import { chooseBoardRun, openRunsByAttention, readRunLink, runChoiceLabel, runLinkSearch } from './formationsRunDiscovery'
import { clampScale, displayLayoutFor, fallbackNodePosition, freeGridPosition, snapToGrid, zoomTransform } from './formationsCanvas'
import { FormationSeats, GATE_SVG, PLAY_SVG, slotTooltip, formationSummary, agentRole, agentState, groupRosterByHarness, harnessGlyph, initials, inputFeedLabel, outputRowStatus, rosterCountLabel } from './formationsCockpitVisuals'
import { useEscapeKey } from './useEscapeKey'
import { ROSTER_MAX_WIDTH, ROSTER_MIN_WIDTH, useRosterPanel } from './useRosterPanel'
const FloatingPeek = lazy(() => import('../terminal/FloatingPeek'))
const RunEvidence = lazy(() => import('../evidence/RunEvidence'))
const NodeWindow = lazy(() => import('../nodeWindow/NodeWindow'))
import DismissiblePanel from './DismissiblePanel'
import PersonaEditorDialog from './PersonaEditorDialog'
import HumanGateAnswerPanel, { type GateDecision } from './HumanGateAnswerPanel'
import RunPoint from './RunPoint'
import CanvasLegend from './CanvasLegend'
import { evidenceNamesForBoard } from '../evidence/evidenceNames'
import { FileWindowsLayer, FileWindowsProvider } from '../files/FileWindows'
import { ProducedFiles, RunProduced, RunProducedProvider } from '../files/ProducedFiles'
import { ReferencedFiles, type HiddenReferencedFile } from '../files/ReferencedFiles'
import { nodeFileRefs } from '../files/referencedFiles'
import { summarizeProduced, useRunProduced } from '../files/produced'
import { useHumanGateUpstream } from './useHumanGateUpstream'
import { connectionKind, findInputPortAt, findOutputPortAt, isTextEditingTarget, laneYFrom } from './formationsCockpitDom'
import { gateWireNeedsLabel, loopConnectionIds, routeJudgeWire, routeLoopWires, routeOrthoWire, type LoopWire } from './formationsRouting'
import type { ObstacleRect } from './formationsRouting'
import { findAddedByID } from './formationsBoardModel'
import { NOTE_CARDS, NoteLayer, NOTES_MODES, noteWindowAnchor, readNotesMode, sameNoteAnchors, writeNotesMode, type NoteAnchor, type NotesMode } from './NoteLayer'
import NoteWindow, { BOARD_NOTE_TARGET, noteWindowId } from './NoteWindow'
import { FormationTypeChip, formationTypeChoices } from './FormationTypeChip'
import { MissionEditorDialog } from './MissionEditorDialog'
import type { MissionDraft } from './MissionEditorDialog'
import { AdmissionFindingsPanel, DraftMarker, findingsByNode, unresolvedFindings } from './formationsDrafts'
import { GateEditorDialog, GateKindChips, gateFieldsFromDraft, gateFieldsFromGate, gateKindLabel, newGateDraft } from './GateEditorDialog'
import type { GateDraft, GateFields } from './GateEditorDialog'
import { createFormationsInteractionOwner } from './formationsInteraction'
import type { FormationsInteractionOwner } from './formationsInteraction'
import { WindowManagerProvider, useWindowManager } from '../windows/WindowManager'
import type { NodeWindowOps } from '../nodeWindow/NodeWindow'
import { cockpitWorkspace } from '../windows/cockpitWorkspace'
import type {
  AgentProjection,
  BoardConnection,
  BoardDocument,
  BoardFinding,
  BoardNotesDocument,
  BoardSummary,
  BoardValidation,
  CodeGateProfileDescriptor,
  FormationBrief,
  FormationNode,
  FormationSlot,
  FormationType,
  GateNode,
  LayoutDocument,
  LayoutNode,
  MissionNode,
  NoteEntry,
  NotePatch,
  OpenEscalation,
  RunEvent,
  RunStatusProjection,
  ViewTransform,
} from './formationsTypes'




type DragStaff = { agentId: string; harness: string; fromSlot?: { formationId: string; slotId: string }; startX: number; startY: number; moved: boolean }
type DragNode = { id: string; pointerId: number; startX: number; startY: number; originX: number; originY: number; moved: boolean }
type WireLabel = { x: number; y: number; text: string; anchor: 'start' | 'end' }
/** A loop is a fail wire back to an earlier step, drawn dashed in its own channel. */
type WirePath = { id: string; d: string; kind: 'wire' | 'pass' | 'fail' | 'judge'; flowing: boolean; loop?: boolean; label?: WireLabel }
type WireDrag =
  | { kind: 'new'; from: string; wireKind: WirePath['kind'] }
  | { kind: 'reconnect-target'; connection: BoardConnection }
  | { kind: 'reconnect-source'; connection: BoardConnection }
  | { kind: 'judge'; gate: GateNode; moved: boolean; startX: number; startY: number }
type LaneDrag = { connectionId: string; previousLane: string; moved: boolean }
type MenuItem = { label: string; action?: () => void; destructive?: boolean; disabled?: boolean; head?: boolean }
type MenuState = { label: string; x: number; y: number; items: MenuItem[] }
type LegacyVerificationState = {
  boardSlug: string
  boardRev: number
  boardETag: string
  formationId: string
  verificationId: string
  title: string
  kinds: string[]
  criterion: string
  onFail: string
  replacementGateIds: string[]
  replacementGateId: string
  pending: boolean
  error: string
}
type MissionEditorState = { initial: MissionDraft; x: number; y: number }
type GateEditorState = {
  initial: GateDraft
  x: number
  y: number
  saving: boolean
}
type BoardDialogState = {
  mode: 'create' | 'rename' | 'delete'
  title: string
  target?: Pick<BoardDocument, 'id' | 'slug' | 'etag' | 'rev'>
  saving: boolean
  error: string
}
type CockpitUndo =
  | { kind: 'clearBrief'; formationId: string }
  | { kind: 'setBrief'; formationId: string; brief: FormationBrief }
  | { kind: 'wireConnection'; from: string; to: string }
  | { kind: 'unwireConnection'; from: string; to: string }
  | { kind: 'rewireConnection'; from: string; previousTo: string; to: string }
  | { kind: 'rewireSource'; previousFrom: string; from: string; to: string }
  | { kind: 'deleteFormation'; id: string }
  | { kind: 'deleteGate'; id: string }
  | { kind: 'updateGate'; gateId: string; fields: Partial<GateFields> & { files?: string[] }; chain: string[] }
  | { kind: 'updateFormation'; id: string; title: string }
  | { kind: 'setFormationType'; id: string; type: FormationNode['type']; slots: FormationSlot[] }
  | { kind: 'updateMission'; id: string; fields: Partial<Pick<MissionNode, 'title' | 'goal' | 'beadId' | 'inputHint' | 'files'>> }
  | { kind: 'deleteMission'; id: string }
  | { kind: 'assignSlot'; formationId: string; slotId: string; agentId: string; harness: string }
  | { kind: 'moveNode'; id: string; x: number; y: number }
  | { kind: 'moveNodes'; nodes: { id: string; x: number; y: number }[] }
  | { kind: 'setLane'; edgeId: string; lane: string }
  | { kind: 'setGateJudge'; gateId: string; chain: string[] }
  | { kind: 'detachGateJudge'; gateId: string }
  | { kind: 'removePort'; formationId: string; portId: string }
  | { kind: 'makeController'; formationId: string; slotId: string }

export default function FormationsCockpit({ active = true }: { active?: boolean } = {}) {
  const [boards, setBoards] = useState<BoardSummary[]>([])
  const [selectedSlug, setSelectedSlug] = useState('')
  const [board, setBoard] = useState<BoardDocument | null>(null)
  const [layout, setLayout] = useState<LayoutDocument | null>(null)
  const [agents, setAgents] = useState<AgentProjection[]>([])
  const [view, setView] = useState<ViewTransform>({ x: 40, y: 40, scale: 1 })
  const [error, setError] = useState('')
  const [validation, setValidation] = useState<BoardValidation | null>(null)
  const [admissionFindings, setAdmissionFindings] = useState<BoardFinding[]>([])
  const [activeRun, setActiveRun] = useState<RunStatusProjection | null>(null)
  // A link's ?board=&run= and the run picker pin a run to its board.
  const initialRunLink = useRef(readRunLink(window.location.search)).current
  const [pinnedRun, setPinnedRun] = useState({ slug: initialRunLink.board, runId: initialRunLink.run })
  const [boardRuns, setBoardRuns] = useState<RunStatusProjection[]>([])
  // Link problems outlive the board loads that clear ordinary errors.
  const [linkError, setLinkError] = useState('')
  const activeRunRef = useRef<RunStatusProjection | null>(null)
  activeRunRef.current = activeRun
  const [runEvents, setRunEvents] = useState<RunEvent[]>([])
  const [escalations, setEscalations] = useState<OpenEscalation[]>([])
  const [inspectedNodeId, setInspectedNodeId] = useState<string | null>(null)
  // Formations whose terminal Peek is open; '' is the run-wide Peek.
  const [peeks, setPeeks] = useState<string[]>([])
  // The card the run bar's phrase last located, marked briefly on the canvas.
  const [locatedNodeId, setLocatedNodeId] = useState('')
  // Missions, formations and gates open in node windows, oldest first.
  const [nodeWindows, setNodeWindows] = useState<string[]>([])
  const [ghost, setGhost] = useState<{ x: number; y: number; agentId: string; harness?: string } | null>(null)
  const [hoverSlot, setHoverSlot] = useState<string | null>(null)
  const [dragPos, setDragPos] = useState<{ id: string; x: number; y: number } | null>(null)
  const [wires, setWires] = useState<WirePath[]>([])
  const [geometryTick, setGeometryTick] = useState(0)
  const [tempWire, setTempWire] = useState<{ ax: number; ay: number; bx: number; by: number; kind: WirePath['kind']; moving: 'a' | 'b' } | null>(null)
  const [hoverPort, setHoverPort] = useState<string | null>(null)
  const [gateGhost, setGateGhost] = useState<{ x: number; y: number } | null>(null)
  const [menu, setMenu] = useState<MenuState | null>(null)
  const [missionEditor, setMissionEditor] = useState<MissionEditorState | null>(null)
  const [missionEditorSaving, setMissionEditorSaving] = useState(false)
  const [gateProfiles, setGateProfiles] = useState<CodeGateProfileDescriptor[]>([])
  const [gateEditor, setGateEditor] = useState<GateEditorState | null>(null)
  const roster = useRosterPanel()
  const [boardDialog, setBoardDialog] = useState<BoardDialogState | null>(null)
  const [agentEditor, setAgentEditor] = useState<{ agent: AgentProjection; trigger: HTMLElement | null } | null>(null)
  const [notesMode, setNotesMode] = useState<NotesMode>(() => readNotesMode())
  const [notes, setNotes] = useState<BoardNotesDocument | null>(null)
  // Open note windows by thread target (a node ID or the board), in opening order.
  const [noteWindows, setNoteWindows] = useState<string[]>([])
  // Unsaved text per thread; closing a note window keeps its draft.
  const [noteDrafts, setNoteDrafts] = useState<Record<string, string>>({})
  const [noteSaving, setNoteSaving] = useState('')
  const [noteErrors, setNoteErrors] = useState<Record<string, string>>({})
  const [notesConflict, setNotesConflict] = useState(false)
  // The entry being edited per thread; its text is in that thread's draft.
  const [noteEditing, setNoteEditing] = useState<Record<string, string>>({})
  const [noteAnchors, setNoteAnchors] = useState<ReadonlyMap<string, NoteAnchor>>(() => new Map())
  const noteAnchorsRef = useRef(noteAnchors)
  const [legacyVerification, setLegacyVerification] = useState<LegacyVerificationState | null>(null)
  const legacyVerificationOpen = legacyVerification !== null
  const [inspectedToolId, setInspectedToolId] = useState<string | null>(null)
  useEscapeKey(active && inspectedToolId !== null, () => setInspectedToolId(null))
  const [hiddenWireId, setHiddenWireId] = useState<string | null>(null)
  const [judgeHover, setJudgeHover] = useState<string | null>(null)
  const [laneDraft, setLaneDraft] = useState<{ connectionId: string; y: number } | null>(null)

  const boardRef = useRef<BoardDocument | null>(null)
  const layoutRef = useRef<LayoutDocument | null>(null)
  const notesRef = useRef<BoardNotesDocument | null>(null)
  const noteDraftsRef = useRef<Record<string, string>>({})
  const viewportRef = useRef<HTMLDivElement | null>(null)
  const windows = useWindowManager(() => cockpitWorkspace(viewportRef.current))
  const { focus: focusWindow } = windows
  // Each Peek is its own window; asking for one already open raises it.
  const openPeek = useCallback((nodeId: string) => {
    setPeeks(current => current.includes(nodeId) ? current : [...current, nodeId])
    focusWindow(`peek:${nodeId}`)
  }, [focusWindow])
  // Each note thread opens in its own window; opening one already open raises it.
  const openNoteWindow = useCallback((target: string) => {
    setNoteWindows(current => current.includes(target) ? current : [...current, target])
    focusWindow(noteWindowId(target))
  }, [focusWindow])
  const openNodeWindow = useCallback((nodeId: string) => {
    setNodeWindows(current => current.includes(nodeId) ? current : [...current, nodeId])
    focusWindow(`node:${nodeId}`)
  }, [focusWindow])
  const worldRef = useRef<HTMLDivElement | null>(null)
  const viewRef = useRef<ViewTransform>(view)
  const interactionOwnerRef = useRef<FormationsInteractionOwner | null>(null)
  if (!interactionOwnerRef.current) interactionOwnerRef.current = createFormationsInteractionOwner()
  const interactionOwner = interactionOwnerRef.current
  const undoStack = useRef<CockpitUndo[]>([])
  const fittedBoardRef = useRef<string | null>(null)
  const judgeHoverRef = useRef<string | null>(null)
  const openJudgePickerRef = useRef<((gate: GateNode, x: number, y: number) => void) | null>(null)
  const legacyVerificationTriggerRef = useRef<HTMLElement | null>(null)
  const legacyVerificationInitialFocusRef = useRef<HTMLButtonElement | null>(null)
  const legacyVerificationWasOpenRef = useRef(false)
  const legacyVerificationPendingRef = useRef(false)
  const legacyVerificationRequestRef = useRef<symbol | null>(null)
  const boardDialogReturnFocusRef = useRef<HTMLElement | null>(null)

  const closeBoardDialog = useCallback(() => {
    const trigger = boardDialogReturnFocusRef.current
    boardDialogReturnFocusRef.current = null
    setBoardDialog(null)
    window.setTimeout(() => { if (trigger?.isConnected) trigger.focus() }, 0)
  }, [])

  viewRef.current = view
  useEffect(() => { boardRef.current = board }, [board])
  useEffect(() => { layoutRef.current = layout }, [layout])
  useEffect(() => { notesRef.current = notes }, [notes])
  useEffect(() => { judgeHoverRef.current = judgeHover }, [judgeHover])

  useEffect(() => {
    if (!boardDialog) return
    const closeDialog = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || boardDialog.saving) return
      event.preventDefault()
      closeBoardDialog()
    }
    window.addEventListener('keydown', closeDialog)
    return () => window.removeEventListener('keydown', closeDialog)
  }, [boardDialog, closeBoardDialog])

  useEffect(() => {
    legacyVerificationRequestRef.current = null
    legacyVerificationPendingRef.current = false
    setLegacyVerification(null)
    setInspectedToolId(null)
    setInspectedNodeId(null)
    setEscalations([])
    setValidation(null)
    setAdmissionFindings([])
  }, [selectedSlug])

  useLayoutEffect(() => {
    if (legacyVerificationOpen) {
      legacyVerificationWasOpenRef.current = true
      legacyVerificationInitialFocusRef.current?.focus({ preventScroll: true })
      return
    }
    if (!legacyVerificationWasOpenRef.current) return
    legacyVerificationWasOpenRef.current = false
    const trigger = legacyVerificationTriggerRef.current
    legacyVerificationTriggerRef.current = null
    if (trigger?.isConnected) trigger.focus({ preventScroll: true })
  }, [legacyVerificationOpen])

  // ----- data loading -----
  useEffect(() => {
    let cancelled = false
    fetchBoardSummaries()
      .then(list => {
        if (cancelled) return
        setBoards(list)
        if (list[0]) {
          const linked = list.some(item => item.slug === initialRunLink.board) ? initialRunLink.board : ''
          if (initialRunLink.board && !linked) {
            setLinkError(`Board "${initialRunLink.board}" from the link was not found`)
            setPinnedRun({ slug: '', runId: '' })
          }
          setSelectedSlug(current => current || linked || list[0].slug)
          return
        }
        boardRef.current = null
        layoutRef.current = null
        setBoards([])
        setSelectedSlug('')
        setBoard(null)
        setLayout(null)
        setActiveRun(null)
        setRunEvents([])
        setEscalations([])
      })
      .catch(err => !cancelled && setError(err instanceof Error ? err.message : 'Failed to load boards'))
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    let cancelled = false
    fetchCodeGateProfiles()
      .then(profiles => {
        if (!cancelled) setGateProfiles(profiles)
      })
      .catch(err => !cancelled && setError(err instanceof Error ? err.message : 'Failed to load code Gate profiles'))
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    if (!selectedSlug) return
    let cancelled = false
    fetchBoardWithLayout(selectedSlug)
      .then(({ board: nextBoard, layout: nextLayout }) => {
        if (cancelled) return
        setBoard(nextBoard)
        setLayout(nextLayout)
        setError('')
      })
      .catch(err => !cancelled && setError(err instanceof Error ? err.message : 'Failed to load board'))
    return () => { cancelled = true }
  }, [selectedSlug])

  useEffect(() => {
    notesRef.current = null
    noteDraftsRef.current = {}
    setNotes(null)
    setNoteWindows([])
    setNoteDrafts({})
    setNoteErrors({})
    setNotesConflict(false)
    setNoteEditing({})
  }, [selectedSlug])

  const noteWindowsOpen = noteWindows.length > 0
  useEffect(() => {
    if (!selectedSlug) return
    let cancelled = false
    const loadNotes = async () => {
      try {
        const next = await fetchBoardNotes(selectedSlug)
        if (cancelled) return
        // Replies are separate entries, so new entries from others never touch a draft.
        notesRef.current = next
        setNotes(next)
      } catch (err) {
        if (!cancelled) setNoteErrors(current => ({ ...current, [BOARD_NOTE_TARGET]: err instanceof Error ? err.message : 'Failed to load board notes' }))
      }
    }
    if (!notesRef.current) void loadNotes()
    // Open note windows follow replies closely; stickies on the canvas refresh slowly.
    const interval = noteWindowsOpen ? 3000 : notesMode !== 'hidden' ? 15000 : 0
    const timer = active && interval ? window.setInterval(() => { void loadNotes() }, interval) : 0
    return () => {
      cancelled = true
      if (timer) window.clearInterval(timer)
    }
  }, [active, noteWindowsOpen, notesMode, selectedSlug])

  useEffect(() => {
    // Paused while the tab is hidden (keep-alive); reactivation re-runs this
    // effect and refreshes immediately via the leading checkChanges() call.
    if (!active || !selectedSlug || !board?.etag) return
    let cancelled = false
    const checkChanges = async () => {
      try {
        const changed = await fetchBoardChanged(selectedSlug, board.etag)
        if (cancelled || !changed) return
        const { board: nextBoard, layout: nextLayout } = await fetchBoardWithLayout(selectedSlug)
        if (cancelled) return
        setBoard(nextBoard)
        setLayout(nextLayout)
        setError('')
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to check board changes')
      }
    }
    void checkChanges()
    const timer = window.setInterval(() => { void checkChanges() }, 600)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [active, board?.etag, selectedSlug])

  useEffect(() => {
    // Draft markers follow each board revision. A rejected run keeps its nodes
    // marked until validation stops reporting them.
    if (!selectedSlug || !board?.etag) return
    let cancelled = false
    fetchBoardValidation(selectedSlug)
      .then(report => {
        if (cancelled) return
        setValidation(report)
        setAdmissionFindings(current => unresolvedFindings(current, [...report.errors, ...report.warnings]))
      })
      .catch(() => { if (!cancelled) setValidation(null) })
    return () => { cancelled = true }
  }, [board?.etag, selectedSlug])

  useEffect(() => {
    if (!active) return
    let cancelled = false
    const load = () => fetchAgents().then(list => !cancelled && setAgents(list)).catch(() => undefined)
    load()
    const timer = window.setInterval(load, 8000)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [active])

  // ----- run discovery -----
  // The daemon lists every run of the board, so runs started outside this
  // browser appear. A run this browser started earlier is the last fallback.
  useEffect(() => {
    if (!selectedSlug) return
    if (activeRunRef.current?.boardSlug && activeRunRef.current.boardSlug !== selectedSlug) {
      setActiveRun(null)
      setRunEvents([])
    }
    setBoardRuns(runs => (runs.some(run => run.boardSlug !== selectedSlug) ? [] : runs))
    const pinnedRunId = pinnedRun.slug === selectedSlug ? pinnedRun.runId : ''
    let cancelled = false
    const discover = async () => {
      let runs: RunStatusProjection[] = []
      try {
        runs = await fetchBoardRuns(selectedSlug)
      } catch {
        /* keep the shown run; the next poll retries */
      }
      if (cancelled) return
      setBoardRuns(runs)
      const stored = window.localStorage.getItem(activeRunStorageKey(selectedSlug)) || ''
      const runId = chooseBoardRun({ slug: selectedSlug, runs, pinnedRunId, current: activeRunRef.current }) || stored
      if (!runId || runId === activeRunRef.current?.runId) return
      const dropPin = (message: string) => {
        setPinnedRun({ slug: '', runId: '' })
        setLinkError(message)
      }
      try {
        const status = runStatusFromResponse(await fetchRunStatus(runId))
        if (cancelled) return
        if (status.boardSlug && status.boardSlug !== selectedSlug) {
          if (runId === pinnedRunId) dropPin(`Run ${runId} belongs to board "${status.boardSlug}", not "${selectedSlug}"`)
          return
        }
        try {
          const events = await fetchRunEvents(runId)
          if (!cancelled) {
            setRunEvents(events)
            setActiveRun(status)
          }
        } catch (err) {
          if (!cancelled) {
            setRunEvents([])
            setActiveRun(status)
            setError(err instanceof Error ? err.message : 'Failed to load run events')
          }
        }
        try {
          const open = await fetchRunEscalations(runId)
          if (!cancelled) setEscalations(open)
        } catch {
          /* escalations are best-effort; never block showing a run */
        }
        if (status.final && runId === stored) window.localStorage.removeItem(activeRunStorageKey(selectedSlug))
      } catch (err) {
        if (cancelled) return
        if (runId === pinnedRunId) dropPin(`Run ${runId} from the link was not found`)
        else setError(err instanceof Error ? err.message : 'Failed to load run')
      }
    }
    void discover()
    const timer = active ? window.setInterval(discover, 5000) : undefined
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [active, pinnedRun, selectedSlug])

  // The address bar names the board and a pinned run, so a reload keeps them.
  useEffect(() => {
    if (!selectedSlug) return
    const search = runLinkSearch(window.location.search, { board: selectedSlug, run: pinnedRun.slug === selectedSlug ? pinnedRun.runId : '' })
    if (search !== window.location.search) window.history.replaceState(window.history.state, '', `${window.location.pathname}${search}${window.location.hash}`)
  }, [pinnedRun, selectedSlug])

  // ----- run polling -----
  useEffect(() => {
    if (!activeRun?.runId || activeRun.final) return
    let cancelled = false
    const tick = async () => {
      try {
        const status = runStatusFromResponse(await fetchRunStatus(activeRun.runId))
        if (cancelled) return
        const events = await fetchRunEvents(activeRun.runId)
        if (cancelled) return
        setRunEvents(prev => events.reduce((acc, event) => upsertRunEvent(acc, event), prev))
        setActiveRun(status)
        try {
          const open = await fetchRunEscalations(activeRun.runId)
          if (!cancelled) setEscalations(open)
        } catch {
          /* escalations are best-effort; a transient failure must not drop the run poll */
        }
        if (status.final && selectedSlug) window.localStorage.removeItem(activeRunStorageKey(selectedSlug))
      } catch {
        /* transient */
      }
    }
    void tick()
    const timer = window.setInterval(tick, 1200)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [activeRun?.runId, activeRun?.final, selectedSlug])

  // ----- positioned nodes -----
  const layoutByNode = useMemo(() => {
    const map = new Map<string, LayoutNode>()
    layout?.nodes?.forEach(node => map.set(node.id, node))
    return map
  }, [layout])

  const displayLayoutByNode = useMemo(() => {
    if (!board) return layoutByNode
    return displayLayoutFor(board, layoutByNode)
  }, [board, layoutByNode])

  const positionOf = useCallback((id: string, index: number): { x: number; y: number } => {
    if (dragPos && dragPos.id === id) return { x: dragPos.x, y: dragPos.y }
    const node = displayLayoutByNode.get(id)
    if (node) return { x: node.x, y: node.y }
    return fallbackNodePosition(index)
  }, [displayLayoutByNode, dragPos])

  const placementForNewNode = useCallback((x: number, y: number) => (
    freeGridPosition({ x, y }, [...displayLayoutByNode.values()])
  ), [displayLayoutByNode])

  const nodeStates = useMemo(() => projectNodeStates(runEvents, activeRun), [runEvents, activeRun])
  const runPoint = useMemo(() => runCurrentPoint(runEvents, activeRun), [runEvents, activeRun])
  const outputNodeIds = useMemo(() => new Set(runEvents.filter(event => event.type === 'node_output' && event.nodeId).map(event => event.nodeId)), [runEvents])
  // What the run's steps produced, for their cards, node windows and the run bar.
  const stepIds = useMemo(() => new Set((board?.formations || []).map(formation => formation.id)), [board?.formations])
  const runProduced = useRunProduced(activeRun?.runId || '', runEvents, stepIds, Boolean(activeRun?.final))
  const producedValue = useMemo(() => activeRun ? {
    runId: activeRun.runId,
    byNode: new Map(runProduced.produced.map(step => [step.nodeId, step])),
    summary: summarizeProduced(board, runProduced.produced, runProduced.artifacts, activeRun.final),
    names: evidenceNamesForBoard(board),
  } : null, [activeRun, board, runProduced])
  const inspectedTool = useMemo(
    () => (board?.tools || []).find(tool => tool.id === inspectedToolId) || null,
    [board?.tools, inspectedToolId],
  )
  useEffect(() => {
    if (inspectedToolId && !inspectedTool) setInspectedToolId(null)
  }, [inspectedTool, inspectedToolId])

  // ----- wire geometry: measure rendered port centers in world coords -----
  // Reads board/layout/view STATE directly (not refs) so the measurement runs in the
  // same commit the cards are laid out in; reading refs here would lag a frame and drop wires.
  useLayoutEffect(() => {
    const world = worldRef.current
    if (!world || !board) { setWires([]); return }
    const worldRect = world.getBoundingClientRect()
    const scale = view.scale || 1
    const cssEscape = (value: string) => value.replace(/["\\]/g, '\\$&')
    const centerOfEl = (el: HTMLElement | null): { x: number; y: number } | null => {
      if (!el) return null
      const r = el.getBoundingClientRect()
      return { x: (r.left + r.width / 2 - worldRect.left) / scale, y: (r.top + r.height / 2 - worldRect.top) / scale }
    }
    // Judge endpoints (`<gateId>:judge`) anchor on the gate's top socket, which is
    // not a data-port element; everything else resolves by exact port endpoint.
    const endpointEl = (endpoint: string, direction: 'out' | 'in'): HTMLElement | null => {
      const [nodeId, portId] = endpoint.split(':')
      if (portId === 'judge') return world.querySelector<HTMLElement>(`[data-gate-judge-socket="${cssEscape(nodeId)}"]`)
      return world.querySelector<HTMLElement>(`[data-port-${direction}="${cssEscape(endpoint)}"]`)
    }
    // Card boxes in world coordinates double as routing obstacles and judge-bracket anchors.
    const obstacles: ObstacleRect[] = Array.from(world.querySelectorAll<HTMLElement>('[data-node]')).map(el => {
      const r = el.getBoundingClientRect()
      return {
        id: el.dataset.node || '',
        x: (r.left - worldRect.left) / scale,
        y: (r.top - worldRect.top) / scale,
        width: r.width / scale,
        height: r.height / scale,
      }
    })
    const cardRect = (id: string): ObstacleRect | null => obstacles.find(rect => rect.id === id) || null
    const layoutEdges = layout?.edges || []
    const laneFor = (connectionId: string): number | null => {
      if (laneDraft && laneDraft.connectionId === connectionId) return laneDraft.y
      return laneYFrom(layoutEdges.find(edge => edge.id === connectionId)?.lane)
    }
    const titleOf = (nodeId: string) => [...(board.missions || []), ...board.formations, ...(board.gates || []), ...(board.tools || [])]
      .find(node => node.id === nodeId)?.title || nodeId
    const loopIds = loopConnectionIds(board.connections || [])
    const loops: LoopWire[] = []
    const paths: WirePath[] = []
    for (const conn of board.connections || []) {
      if (conn.id === hiddenWireId) continue // being reconnected — drawn as the temp wire instead
      const [fromNode, fromPort] = conn.from.split(':')
      const [toNode] = conn.to.split(':')
      const a = centerOfEl(endpointEl(conn.from, 'out'))
      const b = centerOfEl(endpointEl(conn.to, 'in'))
      if (!a || !b) continue
      const kind = connectionKind(conn)
      let d: string
      let flowing: boolean
      if (kind === 'judge') {
        const direction = fromPort === 'judge' ? 'send' : 'return'
        const gateId = fromPort === 'judge' ? fromNode : toNode
        const judgeId = fromPort === 'judge' ? toNode : fromNode
        const judgeRect = cardRect(judgeId)
        d = routeJudgeWire(a, b, { direction, nodeRect: judgeRect })
        // Judge wires pulse only while their gate evaluates (reference drawWires).
        flowing = nodeStates.get(gateId) === 'running'
        if (direction === 'send' && judgeRect) {
          paths.push({ id: conn.id, d, kind, flowing, label: { x: judgeRect.x + 4, y: judgeRect.y - 9, text: `judges ${titleOf(gateId)}`, anchor: 'start' } })
          continue
        }
      } else if (loopIds.has(conn.id) && laneFor(conn.id) === null) {
        // Routed with the other loops below, so parallel loops take separate channels.
        loops.push({ id: conn.id, source: a, target: b })
        paths.push({ id: conn.id, d: '', kind, loop: true, flowing: nodeStates.get(toNode) === 'running', label: { x: 0, y: 0, text: `↺ ${titleOf(toNode)}`, anchor: 'end' } })
        continue
      } else {
        d = routeOrthoWire(a, b, {
          fromId: fromNode,
          toId: toNode,
          obstacles,
          frozen: !!dragPos,
          laneY: laneFor(conn.id),
        })
        flowing = nodeStates.get(fromNode) === 'running' || nodeStates.get(toNode) === 'running'
      }
      // A gate's pass and fail wires name where they go, beside the gate, unless they run into the next card.
      const label = (kind === 'pass' || kind === 'fail') && (loopIds.has(conn.id) || gateWireNeedsLabel(a, b))
        ? { x: a.x + 12, y: a.y - 7, text: `${loopIds.has(conn.id) ? '↺' : '→'} ${titleOf(toNode)}`, anchor: 'start' as const }
        : undefined
      paths.push({ id: conn.id, d, kind, flowing, loop: loopIds.has(conn.id), label })
    }
    const loopRoutes = routeLoopWires(loops, obstacles, { frozen: !!dragPos })
    setWires(paths.map(path => {
      const route = path.loop ? loopRoutes.get(path.id) : undefined
      return route && path.label ? { ...path, d: route.d, label: { ...path.label, x: route.label.x, y: route.label.y } } : path
    }))
  }, [board, layout, view, dragPos, agents, nodeStates, hiddenWireId, laneDraft, geometryTick])

  // Cards ease into place (left/top transitions) and FIT glides the world
  // transform; wires are measured from the DOM mid-flight, so re-measure once
  // motion settles or the endpoints stay visually stale.
  useEffect(() => {
    const world = worldRef.current
    if (!world) return
    const onTransitionEnd = (event: TransitionEvent) => {
      if (event.propertyName === 'left' || event.propertyName === 'top' || event.propertyName === 'transform') {
        setGeometryTick(tick => tick + 1)
      }
    }
    world.addEventListener('transitionend', onTransitionEnd)
    return () => world.removeEventListener('transitionend', onTransitionEnd)
  }, [])

  // ----- mutations -----
  const patchBoard = useCallback(async (patch: Record<string, unknown>): Promise<{ board: BoardDocument; layout: LayoutDocument | null } | null> => {
    const current = boardRef.current
    if (!current) return null
    try {
      const result = await patchBoardDocument(current.slug, current.etag, current.rev, patch)
      boardRef.current = result.board
      setBoard(result.board)
      if (result.layout) {
        layoutRef.current = result.layout
        setLayout(result.layout)
      }
      setError('')
      return { board: result.board, layout: result.layout ?? null }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Update failed')
      return null
    }
  }, [])

  const blockBoardExitForDirtyNotes = useCallback(() => {
    const dirty = Object.keys(noteDraftsRef.current).find(target => noteDraftsRef.current[target])
    if (!dirty) return false
    openNoteWindow(dirty)
    setNoteErrors(current => ({ ...current, [dirty]: 'Save the current notes before leaving this board.' }))
    return true
  }, [openNoteWindow])

  const selectBoard = useCallback((slug: string) => {
    if (boardDialog || slug === boardRef.current?.slug || blockBoardExitForDirtyNotes()) return
    setLinkError('')
    setSelectedSlug(slug)
  }, [blockBoardExitForDirtyNotes, boardDialog])

  const openCreateBoard = useCallback((trigger?: HTMLElement) => {
    if (blockBoardExitForDirtyNotes()) return
    boardDialogReturnFocusRef.current = trigger || null
    setBoardDialog({ mode: 'create', title: '', saving: false, error: '' })
  }, [blockBoardExitForDirtyNotes])

  const openRenameBoard = useCallback((trigger?: HTMLElement) => {
    const current = boardRef.current
    if (!current) return
    boardDialogReturnFocusRef.current = trigger || null
    setBoardDialog({ mode: 'rename', title: current.title, target: { id: current.id, slug: current.slug, etag: current.etag, rev: current.rev }, saving: false, error: '' })
  }, [])

  const openDeleteBoard = useCallback((trigger?: HTMLElement) => {
    if (blockBoardExitForDirtyNotes()) return
    const current = boardRef.current
    if (!current) return
    boardDialogReturnFocusRef.current = trigger || null
    setBoardDialog({ mode: 'delete', title: current.title, target: { id: current.id, slug: current.slug, etag: current.etag, rev: current.rev }, saving: false, error: '' })
  }, [blockBoardExitForDirtyNotes])

  const saveBoardName = useCallback(async () => {
    if (!boardDialog || boardDialog.mode === 'delete') return
    // A blank name saves too: the server names new boards "Untitled board".
    const title = boardDialog.title.trim()
    if (boardDialog.mode === 'create' && blockBoardExitForDirtyNotes()) {
      closeBoardDialog()
      return
    }
    setBoardDialog(current => current ? { ...current, saving: true, error: '' } : current)
    try {
      if (boardDialog.mode === 'create') {
        const created = await createBoard(title)
        const createdLayout = missingLayoutForBoard(created)
        boardRef.current = created
        layoutRef.current = createdLayout
        setBoard(created)
        setLayout(createdLayout)
        setBoards(current => [...current.filter(item => item.slug !== created.slug), {
          id: created.id,
          slug: created.slug,
          title: created.title,
          rev: created.rev,
          etag: created.etag,
        }].sort((a, b) => a.slug.localeCompare(b.slug)))
        setSelectedSlug(created.slug)
      } else {
        const target = boardDialog.target
        if (!target) throw new Error('Board target is missing; close and retry')
        const result = await patchBoardDocument(target.slug, target.etag, target.rev, { title })
        if (boardRef.current?.id === target.id) {
          boardRef.current = result.board
          setBoard(result.board)
        }
        setBoards(items => items.map(item => item.slug === result.board.slug ? {
          ...item,
          title: result.board.title,
          rev: result.board.rev,
          etag: result.board.etag,
        } : item))
      }
      closeBoardDialog()
      setError('')
    } catch (err) {
      setBoardDialog(current => current ? {
        ...current,
        saving: false,
        error: err instanceof Error ? err.message : 'Board update failed',
      } : current)
    }
  }, [blockBoardExitForDirtyNotes, boardDialog, closeBoardDialog])

  const archiveSelectedBoard = useCallback(async () => {
    const target = boardDialog?.target
    if (!target || boardDialog?.mode !== 'delete') return
    if (blockBoardExitForDirtyNotes()) {
      closeBoardDialog()
      return
    }
    setBoardDialog(dialog => dialog ? { ...dialog, saving: true, error: '' } : dialog)
    try {
      await deleteBoard(target.slug, target.etag, target.rev)
      const remaining = await fetchBoardSummaries()
      setBoards(remaining)
      if (boardRef.current?.id === target.id) {
        boardRef.current = null
        layoutRef.current = null
        setBoard(null)
        setLayout(null)
        setSelectedSlug(remaining[0]?.slug || '')
        setActiveRun(null)
        setRunEvents([])
        setEscalations([])
      }
      closeBoardDialog()
      setError('')
    } catch (err) {
      setBoardDialog(dialog => dialog ? {
        ...dialog,
        saving: false,
        error: err instanceof Error ? err.message : 'Board deletion failed',
      } : dialog)
    }
  }, [blockBoardExitForDirtyNotes, boardDialog, closeBoardDialog])

  const patchLayoutEdge = useCallback(async (edgeId: string, lane: string) => {
    const currentBoard = boardRef.current
    const currentLayout = layoutRef.current
    if (!currentBoard || !currentLayout) return
    try {
      const next = await patchBoardLayout(currentBoard.slug, currentLayout.etag, { edges: [{ id: edgeId, lane }] })
      layoutRef.current = next
      setLayout(next)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update wire routing')
    }
  }, [])

  const persistPositions = useCallback(async (nodes: { id: string; x: number; y: number }[]) => {
    const currentBoard = boardRef.current
    const currentLayout = layoutRef.current
    if (!currentBoard || !currentLayout || !nodes.length) return
    try {
      const next = await patchBoardLayout(currentBoard.slug, currentLayout.etag, { nodes })
      layoutRef.current = next
      setLayout(next)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save layout')
    }
  }, [])

  const persistPosition = useCallback(async (id: string, x: number, y: number) => {
    await persistPositions([{ id, x, y }])
  }, [persistPositions])

  const closeMenu = useCallback(() => setMenu(null), [])

  const openMenu = useCallback((event: ReactMouseEvent<Element> | ReactPointerEvent<Element>, label: string, items: MenuItem[]) => {
    event.preventDefault()
    event.stopPropagation()
    setMenu({ label, x: event.clientX, y: event.clientY, items })
  }, [])

  // A card's +N file chip lists the files that did not fit, beside the chip.
  const openReferencedFilesMenu = useCallback((hidden: HiddenReferencedFile[], anchor: DOMRect) => {
    setMenu({ label: 'Referenced files', x: anchor.left, y: anchor.bottom + 4, items: hidden.map(file => ({ label: file.label, action: file.open })) })
  }, [])

  // Node windows save one field at a time; each save is one undo entry.
  const saveBrief = useCallback(async (formationId: string, brief: FormationBrief): Promise<boolean> => {
    const previous = boardRef.current?.formations.find(formation => formation.id === formationId)?.brief
    const result = await patchBoard({
      setBrief: { formationId, goal: brief.goal || '', beadId: brief.beadId || '', files: brief.files || [], links: brief.links || [] },
    })
    if (!result) return false
    undoStack.current.push(previous ? { kind: 'setBrief', formationId, brief: previous } : { kind: 'clearBrief', formationId })
    return true
  }, [patchBoard])

  const performUndo = useCallback(async () => {
    const action = undoStack.current.pop()
    if (!action) return
    const retry = () => undoStack.current.push(action)
    if (action.kind === 'rewireSource') {
      // Undo of a source-end reconnect is two sequential ops: drop the new wire,
      // restore the previous one.
      const removed = await patchBoard({ unwireConnection: { from: action.from, to: action.to } })
      if (!removed) { retry(); return }
      await patchBoard({ wireConnection: { from: action.previousFrom, to: action.to } })
      return
    }
    if (action.kind === 'moveNode') {
      await persistPosition(action.id, action.x, action.y)
      return
    }
    if (action.kind === 'moveNodes') {
      await persistPositions(action.nodes)
      return
    }
    if (action.kind === 'setLane') {
      await patchLayoutEdge(action.edgeId, action.lane)
      return
    }
    if (action.kind === 'updateGate') {
      // Restore the fields, then any judge chain the edit detached.
      const restored = await patchBoard({ updateGate: { id: action.gateId, ...action.fields } })
      if (!restored) { retry(); return }
      if (action.chain.length) await patchBoard({ setGateJudge: { gateId: action.gateId, chain: action.chain } })
      return
    }
    let patch: Record<string, unknown>
    switch (action.kind) {
      case 'clearBrief':
        patch = { clearBrief: { formationId: action.formationId } }
        break
      case 'setBrief':
        patch = {
          setBrief: {
            formationId: action.formationId,
            goal: action.brief.goal || '',
            beadId: action.brief.beadId || '',
            files: action.brief.files || [],
            links: action.brief.links || [],
          },
        }
        break
      case 'wireConnection':
        patch = { wireConnection: { from: action.from, to: action.to } }
        break
      case 'unwireConnection':
        patch = { unwireConnection: { from: action.from, to: action.to } }
        break
      case 'rewireConnection':
        patch = { rewireConnection: { from: action.from, previousTo: action.previousTo, to: action.to } }
        break
      case 'deleteFormation':
        patch = { deleteFormation: { id: action.id } }
        break
      case 'deleteGate':
        patch = { deleteGate: { id: action.id } }
        break
      case 'deleteMission':
        patch = { deleteMission: { id: action.id } }
        break
      case 'updateFormation':
        patch = { updateFormation: { id: action.id, title: action.title } }
        break
      case 'setFormationType':
        patch = { setFormationType: { id: action.id, type: action.type, slots: action.slots } }
        break
      case 'updateMission':
        patch = { updateMission: { id: action.id, ...action.fields } }
        break
      case 'assignSlot':
        patch = { assignSlot: { formationId: action.formationId, slotId: action.slotId, agentId: action.agentId, harness: action.harness } }
        break
      case 'setGateJudge':
        patch = { setGateJudge: { gateId: action.gateId, chain: action.chain } }
        break
      case 'detachGateJudge':
        patch = { detachGateJudge: { gateId: action.gateId } }
        break
      case 'removePort':
        patch = { removePort: { formationId: action.formationId, portId: action.portId } }
        break
      case 'makeController':
        patch = { makeController: { formationId: action.formationId, slotId: action.slotId } }
        break
    }
    const result = await patchBoard(patch)
    if (!result) retry()
  }, [patchBoard, patchLayoutEdge, persistPosition, persistPositions])

  useEffect(() => {
    if (!active) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (!(event.ctrlKey || event.metaKey) || event.shiftKey || event.key.toLowerCase() !== 'z') return
      if (isTextEditingTarget(event.target)) return
      event.preventDefault()
      void performUndo()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [active, performUndo])

  // Context menus dismiss on outside click, Escape, or scroll (reference behavior).
  useEffect(() => {
    if (!menu) return
    const onPointerDown = (event: Event) => {
      const target = event.target as HTMLElement | null
      if (!target?.closest?.('.ctxmenu')) setMenu(null)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (active && event.key === 'Escape') setMenu(null)
    }
    window.addEventListener('pointerdown', onPointerDown, true)
    window.addEventListener('wheel', onPointerDown, { capture: true, passive: true })
    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('pointerdown', onPointerDown, true)
      window.removeEventListener('wheel', onPointerDown, true)
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [active, menu])

  const wire = useCallback((from: string, to: string) => {
    if (!from || !to || from.split(':')[0] === to.split(':')[0]) return
    undoStack.current.push({ kind: 'unwireConnection', from, to })
    void patchBoard({ wireConnection: { from, to } })
  }, [patchBoard])

  const rewireTarget = useCallback((connection: BoardConnection, to: string) => {
    if (!to || connection.to === to || connection.from.split(':')[0] === to.split(':')[0]) return
    undoStack.current.push({ kind: 'rewireConnection', from: connection.from, previousTo: to, to: connection.to })
    void patchBoard({ rewireConnection: { from: connection.from, previousTo: connection.to, to } })
  }, [patchBoard])

  const removeWire = useCallback((connection: BoardConnection) => {
    undoStack.current.push({ kind: 'wireConnection', from: connection.from, to: connection.to })
    void patchBoard({ unwireConnection: { from: connection.from, to: connection.to } })
  }, [patchBoard])

  const createGateAt = useCallback((worldX: number, worldY: number) => {
    setGateEditor({ initial: newGateDraft(gateProfiles), x: worldX, y: worldY, saving: false })
  }, [gateProfiles])

  const closeGateEditor = useCallback(() => {
    if (gateEditor?.saving) return
    setGateEditor(null)
  }, [gateEditor?.saving])

  useEffect(() => {
    if (!active || !gateEditor || gateEditor.saving) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      closeGateEditor()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [active, closeGateEditor, gateEditor])

  const assignSlot = useCallback((formation: FormationNode, slot: FormationSlot, agentId: string, harness: string) => {
    // An unchanged assignment writes nothing, so it cannot churn the board revision.
    if ((slot.agentId || '') === agentId && (slot.harness || '') === harness) return
    undoStack.current.push({ kind: 'assignSlot', formationId: formation.id, slotId: slot.id, agentId: slot.agentId || '', harness: slot.harness || '' })
    void patchBoard({ assignSlot: { formationId: formation.id, slotId: slot.id, agentId, harness } })
  }, [patchBoard])

  const unassignSlot = useCallback((formation: FormationNode, slot: FormationSlot) => {
    if (!slot.agentId) return
    assignSlot(formation, slot, '', '')
  }, [assignSlot])

  const makeControllerOp = useCallback((formation: FormationNode, slot: FormationSlot) => {
    const previous = formation.slots.find(item => item.controller)
    if (previous && previous.id !== slot.id) {
      undoStack.current.push({ kind: 'makeController', formationId: formation.id, slotId: previous.id })
    }
    void patchBoard({ makeController: { formationId: formation.id, slotId: slot.id } })
  }, [patchBoard])

  const createFormationAt = useCallback(async (type: FormationType, title: string, x: number, y: number): Promise<FormationNode | null> => {
    const placement = placementForNewNode(x, y)
    const before = boardRef.current
    const result = await patchBoard({ createFormation: { type, title, x: placement.x, y: placement.y } })
    if (!before || !result) return null
    const created = findAddedByID(before.formations || [], result.board.formations || [])
    if (created) undoStack.current.push({ kind: 'deleteFormation', id: created.id })
    return created ?? null
  }, [patchBoard, placementForNewNode])

  const createMissionAt = useCallback((x: number, y: number) => {
    setMissionEditor({ initial: { title: 'New mission', goal: '', beadId: '' }, x, y })
    setMissionEditorSaving(false)
  }, [])

  const closeMissionEditor = useCallback(() => {
    if (missionEditorSaving) return
    setMissionEditor(null)
  }, [missionEditorSaving])

  const saveMissionEditor = useCallback(async (draft: MissionDraft) => {
    const before = boardRef.current
    if (!missionEditor || missionEditorSaving || !before) return
    setMissionEditorSaving(true)
    const placement = placementForNewNode(missionEditor.x, missionEditor.y)
    const result = await patchBoard({
      createMission: { ...draft, title: draft.title || 'New mission', x: placement.x, y: placement.y },
    })
    setMissionEditorSaving(false)
    if (!result) return
    const created = findAddedByID(before.missions || [], result.board.missions || [])
    if (created) undoStack.current.push({ kind: 'deleteMission', id: created.id })
    setMissionEditor(null)
  }, [missionEditor, missionEditorSaving, patchBoard, placementForNewNode])

  // A rename keeps the node's ID, ports, edges, layout and notes.
  const renameNode = useCallback(async (nodeId: string, title: string): Promise<boolean> => {
    const current = boardRef.current
    if (!current) return false
    const formation = current.formations.find(node => node.id === nodeId)
    const mission = current.missions?.find(node => node.id === nodeId)
    const gate = current.gates?.find(node => node.id === nodeId)
    const previous = formation?.title ?? mission?.title ?? gate?.title
    if (previous === undefined) return false
    if (previous === title) return true
    if (formation) {
      if (!await patchBoard({ updateFormation: { id: nodeId, title } })) return false
      undoStack.current.push({ kind: 'updateFormation', id: nodeId, title: previous })
    } else if (mission) {
      if (!await patchBoard({ updateMission: { id: nodeId, title } })) return false
      undoStack.current.push({ kind: 'updateMission', id: nodeId, fields: { title: previous } })
    } else if (gate) {
      if (!await patchBoard({ updateGate: { id: nodeId, title } })) return false
      undoStack.current.push({ kind: 'updateGate', gateId: nodeId, fields: gateFieldsFromGate(gate), chain: [] })
    }
    return true
  }, [patchBoard])

  const updateMissionFields = useCallback(async (missionId: string, fields: Partial<Pick<MissionNode, 'goal' | 'beadId' | 'inputHint' | 'files'>>): Promise<boolean> => {
    const previous = boardRef.current?.missions?.find(mission => mission.id === missionId)
    if (!previous) return false
    if (!await patchBoard({ updateMission: { id: missionId, ...fields } })) return false
    // An absent field is restored as empty, which clears it.
    undoStack.current.push({ kind: 'updateMission', id: missionId, fields: Object.fromEntries(Object.keys(fields).map(key => [key, previous[key as keyof typeof fields] ?? (key === 'files' ? [] : '')])) })
    return true
  }, [patchBoard])

  const renderNodeTitle = (title: string, className: string, fallback: string, Tag: 'div' | 'span') => (
    <Tag className={`${className}${title ? '' : ' untitled'}`}>{title || fallback}</Tag>
  )

  useEffect(() => {
    if (!active || !missionEditor || missionEditorSaving) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      closeMissionEditor()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [active, closeMissionEditor, missionEditor, missionEditorSaving])

  const deleteFormationOp = useCallback((formation: FormationNode) => {
    void patchBoard({ deleteFormation: { id: formation.id } })
  }, [patchBoard])

  const deleteGateOp = useCallback((gate: GateNode) => {
    void patchBoard({ deleteGate: { id: gate.id } })
  }, [patchBoard])

  const deleteMissionOp = useCallback((mission: MissionNode) => {
    void patchBoard({ deleteMission: { id: mission.id } })
  }, [patchBoard])

  const addPortOp = useCallback(async (formation: FormationNode, direction: 'in' | 'out') => {
    const before = boardRef.current?.formations.find(item => item.id === formation.id)
    const result = await patchBoard({ addPort: { formationId: formation.id, direction, label: direction === 'in' ? 'Input' : 'Output' } })
    if (!before || !result) return
    const after = result.board.formations.find(item => item.id === formation.id)
    if (!after) return
    const created = direction === 'in'
      ? findAddedByID(before.inputs || [], after.inputs || [])
      : findAddedByID(before.outputs || [], after.outputs || [])
    if (created) undoStack.current.push({ kind: 'removePort', formationId: formation.id, portId: created.id })
  }, [patchBoard])

  const removePortOp = useCallback((formation: FormationNode, portId: string) => {
    void patchBoard({ removePort: { formationId: formation.id, portId } })
  }, [patchBoard])

  const closeLegacyVerification = useCallback(() => {
    if (legacyVerificationPendingRef.current) return
    setLegacyVerification(null)
  }, [])

  useEffect(() => {
    if (!active || !legacyVerificationOpen) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      closeLegacyVerification()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [active, closeLegacyVerification, legacyVerificationOpen])

  const removeLegacyVerification = useCallback(async () => {
    if (!legacyVerification?.replacementGateId || legacyVerificationPendingRef.current) return
    const current = boardRef.current
    if (!current || current.slug !== legacyVerification.boardSlug || current.rev !== legacyVerification.boardRev || current.etag !== legacyVerification.boardETag) {
      setError('Board changed while legacy verification was open. Reopen migration to continue.')
      setLegacyVerification(open => open ? { ...open, error: 'Could not remove legacy verification. Reopen migration from the current board and try again.' } : open)
      return
    }
    legacyVerificationPendingRef.current = true
    const request = Symbol('remove-legacy-verification')
    legacyVerificationRequestRef.current = request
    setLegacyVerification(open => open ? { ...open, pending: true, error: '' } : open)
    const result = await patchBoard({
      removeVerification: {
        formationId: legacyVerification.formationId,
        replacementGateId: legacyVerification.replacementGateId,
      },
    })
    if (legacyVerificationRequestRef.current !== request) return
    legacyVerificationRequestRef.current = null
    legacyVerificationPendingRef.current = false
    if (result) {
      setLegacyVerification(null)
      return
    }
    setLegacyVerification(open => open ? {
      ...open,
      pending: false,
      error: 'Could not remove legacy verification. Review the current board before trying again.',
    } : open)
  }, [legacyVerification, patchBoard])

  const openLegacyVerification = useCallback((formation: FormationNode, trigger?: HTMLElement) => {
    const current = boardRef.current
    if (!formation.verification || !current) return
    const outputEndpoints = new Set(formation.outputs.map(output => `${formation.id}:${output.id}`))
    const replacementGateIds = (current.gates || [])
      .filter(gate => current.connections.some(connection => outputEndpoints.has(connection.from) && connection.to === `${gate.id}:in`))
      .map(gate => gate.id)
    legacyVerificationTriggerRef.current = trigger || (document.activeElement instanceof HTMLElement ? document.activeElement : null)
    legacyVerificationRequestRef.current = null
    legacyVerificationPendingRef.current = false
    setMenu(null)
    setLegacyVerification({
      boardSlug: current.slug,
      boardRev: current.rev,
      boardETag: current.etag,
      formationId: formation.id,
      verificationId: formation.verification.id || '',
      title: formation.title,
      kinds: formation.verification.kinds?.length ? [...formation.verification.kinds] : [],
      criterion: formation.verification.criterion || '',
      onFail: formation.verification.onFail || '',
      replacementGateIds,
      replacementGateId: '',
      pending: false,
      error: '',
    })
  }, [])

  /** Reconstruct the gate's judge chain from persisted `<gateId>:judge` edges. */
  const judgeChainOf = useCallback((gateId: string): string[] => {
    const connections = boardRef.current?.connections || []
    const socket = `${gateId}:judge`
    const send = connections.find(connection => connection.from === socket)
    if (!send) return []
    const start = send.to.split(':')[0]
    const visit = (node: string, acc: string[]): string[] | null => {
      const nextAcc = [...acc, node]
      for (const connection of connections) {
        if (connection.from.split(':')[0] !== node) continue
        if (connection.to === socket) return nextAcc
        const next = connection.to.split(':')[0]
        if (nextAcc.includes(next)) continue
        const found = visit(next, nextAcc)
        if (found) return found
      }
      return null
    }
    return visit(start, []) || [start]
  }, [])

  const attachJudge = useCallback((gate: GateNode, chain: string[]) => {
    const previous = judgeChainOf(gate.id)
    undoStack.current.push(previous.length
      ? { kind: 'setGateJudge', gateId: gate.id, chain: previous }
      : { kind: 'detachGateJudge', gateId: gate.id })
    void patchBoard({ setGateJudge: { gateId: gate.id, chain } })
  }, [judgeChainOf, patchBoard])

  // Detaching may change the kinds (a judge-only gate becomes human), so undo
  // restores the gate's fields before its chain.
  const detachJudge = useCallback((gate: GateNode) => {
    undoStack.current.push({ kind: 'updateGate', gateId: gate.id, fields: gateFieldsFromGate(gate), chain: judgeChainOf(gate.id) })
    void patchBoard({ detachGateJudge: { gateId: gate.id } })
  }, [judgeChainOf, patchBoard])

  /** Drop on empty canvas / picker "new judge": create the formation, then wire it as judge. */
  const createJudgeFor = useCallback(async (gate: GateNode, type: FormationType, title: string, x: number, y: number) => {
    const created = await createFormationAt(type, title, x, y)
    if (created) attachJudge(gate, [created.id])
  }, [attachJudge, createFormationAt])

  // Drafts save: missing code fields or a missing judge chain become run findings.
  const saveGateEditor = useCallback(async (draft: GateDraft) => {
    const before = boardRef.current
    if (!gateEditor || gateEditor.saving || !before) return
    const fields = gateFieldsFromDraft(draft)
    setGateEditor(current => current ? { ...current, saving: true } : null)
    const placement = placementForNewNode(gateEditor.x, gateEditor.y)
    const result = await patchBoard({ createGate: { ...fields, title: fields.title || 'Review gate', x: placement.x, y: placement.y } })
    setGateEditor(current => current ? { ...current, saving: false } : null)
    if (!result) return
    const gate = findAddedByID(before.gates || [], result.board.gates || [])
    if (gate) undoStack.current.push({ kind: 'deleteGate', id: gate.id })
    setGateEditor(null)
  }, [gateEditor, patchBoard, placementForNewNode])

  const setGateFiles = useCallback(async (gateId: string, files: string[]): Promise<boolean> => {
    const previous = boardRef.current?.gates?.find(gate => gate.id === gateId)
    if (!previous) return false
    if (!await patchBoard({ updateGate: { id: gateId, files } })) return false
    undoStack.current.push({ kind: 'updateGate', gateId, fields: { files: previous.files || [] }, chain: [] })
    return true
  }, [patchBoard])

  // Dropping the judge kind detaches the chain, so undo restores the chain after the fields.
  const updateGateFields = useCallback(async (gateId: string, draft: GateDraft): Promise<boolean> => {
    const previous = boardRef.current?.gates?.find(gate => gate.id === gateId)
    if (!previous) return false
    const { kinds, ...rest } = gateFieldsFromDraft(draft)
    const chain = previous.kinds.includes('formation') && !kinds.includes('formation') ? judgeChainOf(previous.id) : []
    if (!await patchBoard({ updateGate: { id: previous.id, ...rest, ...(kinds.length ? { kinds } : {}) } })) return false
    undoStack.current.push({ kind: 'updateGate', gateId: previous.id, fields: gateFieldsFromGate(previous), chain })
    return true
  }, [judgeChainOf, patchBoard])

  const rewireSource = useCallback(async (connection: BoardConnection, newFrom: string) => {
    if (!newFrom || newFrom === connection.from || newFrom.split(':')[0] === connection.to.split(':')[0]) return
    const removed = await patchBoard({ unwireConnection: { from: connection.from, to: connection.to } })
    if (!removed) return
    const added = await patchBoard({ wireConnection: { from: newFrom, to: connection.to } })
    if (added) undoStack.current.push({ kind: 'rewireSource', previousFrom: connection.from, from: newFrom, to: connection.to })
  }, [patchBoard])

  const [startMission, setStartMission] = useState<MissionNode | null>(null)

  const runMission = useCallback(async (mission: MissionNode, inputs: RunInputs) => {
    const current = boardRef.current
    if (!current) return
      const result = await startRun(current.etag, { ...inputs, board: current.slug, missionId: mission.id, expectedRev: current.rev, actor: 'agent:ui' })
        .catch(err => {
          if (err instanceof ApiRequestError && err.findings.length) setAdmissionFindings(err.findings)
          throw err
        })
      setAdmissionFindings([])
      const status = { ...result.status, runId: result.status.runId || result.runId }
      const events = await fetchRunEvents(status.runId)
      setRunEvents(events)
      setActiveRun(status)
      setEscalations([])
      setPinnedRun({ slug: current.slug, runId: status.runId })
      if (status.final) window.localStorage.removeItem(activeRunStorageKey(current.slug))
      else window.localStorage.setItem(activeRunStorageKey(current.slug), status.runId)
      setError('')
  }, [])

  const runFormation = useCallback(async (formation: FormationNode) => {
    const current = boardRef.current
    if (!current) return
    try {
      const result = await startRun(current.etag, { board: current.slug, formationId: formation.id, expectedRev: current.rev, actor: 'agent:ui' })
      setAdmissionFindings([])
      const status = { ...result.status, runId: result.status.runId || result.runId }
      const events = await fetchRunEvents(status.runId)
      setRunEvents(events)
      setActiveRun(status)
      setEscalations([])
      setPinnedRun({ slug: current.slug, runId: status.runId })
      if (status.final) window.localStorage.removeItem(activeRunStorageKey(current.slug))
      else window.localStorage.setItem(activeRunStorageKey(current.slug), status.runId)
      setError('')
    } catch (err) {
      if (err instanceof ApiRequestError && err.findings.length) {
        setAdmissionFindings(err.findings)
        setError('')
        return
      }
      setError(err instanceof Error ? err.message : 'Failed to start run')
    }
  }, [])

  const refreshRunEvents = useCallback(async (runId: string) => {
    const events = await fetchRunEvents(runId)
    setRunEvents(events)
  }, [])

  const abortActiveRun = useCallback(async () => {
    if (!activeRun?.runId || activeRun.final) return
    try {
      const status = runStatusFromResponse(await abortRunRequest(activeRun.runId, { reason: 'operator stop', requestedBy: 'agent:ui' }))
      setActiveRun(status)
      await refreshRunEvents(activeRun.runId)
      if (status.final && selectedSlug) window.localStorage.removeItem(activeRunStorageKey(selectedSlug))
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to abort run')
    }
  }, [activeRun, refreshRunEvents, selectedSlug])

  const resumeActiveRun = useCallback(async () => {
    if (!activeRun?.runId || activeRun.final || !activeRun.resumeAllowed) return
    try {
      const status = runStatusFromResponse(await resumeRunRequest(activeRun.runId, {
        actor: 'agent:ui',
        mode: 'reattach',
        reason: 'operator resume',
      }))
      setActiveRun(status)
      await refreshRunEvents(activeRun.runId)
      if (selectedSlug) {
        if (status.final) window.localStorage.removeItem(activeRunStorageKey(selectedSlug))
        else window.localStorage.setItem(activeRunStorageKey(selectedSlug), status.runId)
      }
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to resume run')
    }
  }, [activeRun, refreshRunEvents, selectedSlug])

  // The response is the verdict reason: approve routes it with the gate input,
  // send back returns it as feedback.
  const recordHumanGateVerdict = useCallback(async (gateId: string, requestedSeq: number, verdict: GateDecision, response: string) => {
    if (!activeRun?.runId || activeRun.final) return false
    try {
      const status = runStatusFromResponse(await recordGateVerdict(activeRun.runId, gateId, {
        actor: 'agent:ui',
        verdict,
        requestedSeq,
        reason: response,
      }))
      setActiveRun(status)
      await refreshRunEvents(activeRun.runId)
      if (selectedSlug) {
        if (status.final) window.localStorage.removeItem(activeRunStorageKey(selectedSlug))
        else window.localStorage.setItem(activeRunStorageKey(selectedSlug), status.runId)
      }
      setError('')
      return true
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to record human verdict')
      return false
    }
  }, [activeRun, refreshRunEvents, selectedSlug])

  const createSolo = useCallback(() => {
    // Land only the new card near the viewport center on free grid space.
    // Existing authored coordinates remain untouched.
    const rect = viewportRef.current?.getBoundingClientRect()
    const v = viewRef.current
    const center = rect
      ? { x: (rect.width / 2 - v.x) / (v.scale || 1) - 150, y: (rect.height / 2 - v.y) / (v.scale || 1) - 120 }
      : { x: 200, y: 200 }
    const placement = placementForNewNode(center.x, center.y)
    void patchBoard({
      createFormation: {
        type: 'solo',
        title: 'New formation',
        x: placement.x,
        y: placement.y,
      },
    })
  }, [patchBoard, placementForNewNode])

  // ----- pan + zoom -----
  const onViewportPointerDown = useCallback((event: ReactPointerEvent) => {
    if (event.button !== 0) return
    const target = event.target as HTMLElement
    if (target.closest('.formation,.gatecard,.missioncard,.toolcard,.zoomctl,.run-banner')) return
    const pan = { startX: event.clientX, startY: event.clientY, originX: viewRef.current.x, originY: viewRef.current.y }
    interactionOwner.begin({
      kind: 'pan',
      pointerId: event.pointerId,
      project: pointer => {
        setView(current => ({ ...current, x: pan.originX + (pointer.clientX - pan.startX), y: pan.originY + (pointer.clientY - pan.startY) }))
        viewportRef.current?.classList.add('panning')
      },
      finalize: () => undefined,
      cancel: () => viewportRef.current?.classList.remove('panning'),
    })
  }, [interactionOwner])

  // Native non-passive listener: React's onWheel is passive in the embedded
  // dashboard, so preventDefault there cannot stop the page from scrolling.
  useEffect(() => {
    const viewport = viewportRef.current
    if (!viewport) return
    const onWheel = (event: WheelEvent) => {
      event.preventDefault()
      const rect = viewport.getBoundingClientRect()
      const cursor = { x: event.clientX - rect.left, y: event.clientY - rect.top }
      setView(current => zoomTransform(current, event.deltaY < 0 ? 1.12 : 0.892, cursor))
    }
    viewport.addEventListener('wheel', onWheel, { passive: false })
    return () => viewport.removeEventListener('wheel', onWheel)
  }, [])

  const zoomBy = useCallback((factor: number) => {
    const rect = viewportRef.current?.getBoundingClientRect()
    const cursor = rect ? { x: rect.width / 2, y: rect.height / 2 } : undefined
    setView(current => zoomTransform(current, factor, cursor))
  }, [])

  const fitView = useCallback((options?: { smooth?: boolean }) => {
    const currentBoard = boardRef.current
    const rect = viewportRef.current?.getBoundingClientRect()
    const world = worldRef.current
    if (!currentBoard || !rect || !world) return
    // Cards carry data-node; class selectors also match chips inside cards,
    // such as a gate's "formation" kind, which have no position of their own.
    const cards = Array.from(world.querySelectorAll<HTMLElement>('[data-node]'))
      .map(card => ({ card, x: Number.parseFloat(card.style.left), y: Number.parseFloat(card.style.top) }))
      .filter(({ x, y }) => Number.isFinite(x) && Number.isFinite(y))
    if (!cards.length) { setView({ x: 40, y: 40, scale: 1 }); return }
    let minX = 1e9, minY = 1e9, maxX = -1e9, maxY = -1e9
    cards.forEach(({ card, x, y }) => {
      // Arrange animates left/top, and a previous Fit may still be zooming.
      // Inline positions are the authored destinations, while bounding rects
      // describe an intermediate frame at an intermediate camera scale.
      minX = Math.min(minX, x); minY = Math.min(minY, y)
      maxX = Math.max(maxX, x + card.offsetWidth); maxY = Math.max(maxY, y + card.offsetHeight)
    })
    const narrow = window.innerWidth <= 768
    const pad = narrow ? 24 : 64
    const fittedScale = clampScale(Math.min(rect.width / (maxX - minX + pad * 2), rect.height / (maxY - minY + pad * 2)), 1)
    // Narrow screens are a readable pan/zoom surface, not a miniature overview.
    // Keep cards at authored size and focus the leading edge of the saved board.
    const next = narrow ? 1 : fittedScale
    // The FIT button glides via the .world.smooth transition (reference feel);
    // the initial auto-fit on board load snaps so geometry is stable immediately.
    if (options?.smooth) {
      world.classList.add('smooth')
      window.setTimeout(() => world.classList.remove('smooth'), 420)
    }
    setView({
      scale: next,
      x: narrow ? pad - minX * next : (rect.width - (maxX - minX) * next) / 2 - minX * next,
      y: narrow ? pad - minY * next : (rect.height - (maxY - minY) * next) / 2 - minY * next,
    })
  }, [])

  // Centre a card in the viewport at the current zoom and mark it briefly.
  const locateNode = useCallback((nodeId: string) => {
    const rect = viewportRef.current?.getBoundingClientRect()
    const world = worldRef.current
    const card = Array.from(world?.querySelectorAll<HTMLElement>('[data-node]') || []).find(el => el.dataset.node === nodeId)
    if (!rect || !world || !card) return
    const x = Number.parseFloat(card.style.left)
    const y = Number.parseFloat(card.style.top)
    if (!Number.isFinite(x) || !Number.isFinite(y)) return
    world.classList.add('smooth')
    window.setTimeout(() => world.classList.remove('smooth'), 420)
    setView(current => ({
      ...current,
      x: rect.width / 2 - (x + card.offsetWidth / 2) * current.scale,
      y: rect.height / 2 - (y + card.offsetHeight / 2) * current.scale,
    }))
    setLocatedNodeId(nodeId)
  }, [])

  useEffect(() => {
    if (!locatedNodeId) return
    const timer = window.setTimeout(() => setLocatedNodeId(''), 1600)
    return () => window.clearTimeout(timer)
  }, [locatedNodeId])

  // The run bar's phrase centres its node, then opens the node's window beside the centred card.
  const locateAndOpenNode = useCallback((nodeId: string) => {
    locateNode(nodeId)
    const current = boardRef.current
    if (!current || ![...(current.missions || []), ...current.formations, ...(current.gates || [])].some(node => node.id === nodeId)) return
    window.setTimeout(() => openNodeWindow(nodeId), 450)
  }, [locateNode, openNodeWindow])

  useLayoutEffect(() => {
    if (!board || !layout || fittedBoardRef.current === board.slug) return
    const frame = window.requestAnimationFrame(() => {
      fitView()
      fittedBoardRef.current = board.slug
    })
    return () => window.cancelAnimationFrame(frame)
  }, [board, layout, fitView])

  const arrangeBoard = useCallback(async () => {
    const currentBoard = boardRef.current
    const currentLayout = layoutRef.current
    if (!currentBoard || !currentLayout) return
    const previous = currentLayout.nodes.map(node => ({ id: node.id, x: node.x, y: node.y }))
    undoStack.current.push({ kind: 'moveNodes', nodes: previous })
    try {
      const next = await patchBoardLayout(currentBoard.slug, currentLayout.etag, { arrange: true })
      layoutRef.current = next
      setLayout(next)
      setError('')
    } catch (err) {
      undoStack.current.pop()
      setError(err instanceof Error ? err.message : 'Failed to arrange layout')
    }
  }, [])

  const screenToWorld = useCallback((clientX: number, clientY: number) => {
    const rect = viewportRef.current?.getBoundingClientRect()
    const v = viewRef.current
    if (!rect) return { x: 0, y: 0 }
    return { x: (clientX - rect.left - v.x) / (v.scale || 1), y: (clientY - rect.top - v.y) / (v.scale || 1) }
  }, [])

  // ----- one global owner for pan, card/staff/wire/gate/lane interactions -----
  useLayoutEffect(() => {
    const onMove = (event: globalThis.PointerEvent) => { interactionOwner.project(event) }
    const onUp = (event: globalThis.PointerEvent) => { interactionOwner.finalize(event) }
    const onCancel = (event: globalThis.PointerEvent) => { interactionOwner.cancel(event.pointerId) }
    const onLostOwnership = () => { interactionOwner.cancel() }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    window.addEventListener('pointercancel', onCancel)
    window.addEventListener('lostpointercapture', onCancel)
    window.addEventListener('blur', onLostOwnership)
    return () => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
      window.removeEventListener('pointercancel', onCancel)
      window.removeEventListener('lostpointercapture', onCancel)
      window.removeEventListener('blur', onLostOwnership)
      interactionOwner.cancel()
    }
  }, [interactionOwner])

  const beginStaff = useCallback((event: ReactPointerEvent, agentId: string, harness: string, fromSlot?: DragStaff['fromSlot']) => {
    if (event.button !== 0) return
    event.stopPropagation()
    const staff: DragStaff = { agentId, harness, fromSlot, startX: event.clientX, startY: event.clientY, moved: false }
    interactionOwner.begin({
      kind: 'staff',
      pointerId: event.pointerId,
      project: pointer => {
        if (!staff.moved && Math.abs(pointer.clientX - staff.startX) + Math.abs(pointer.clientY - staff.startY) >= 6) staff.moved = true
        setGhost({ x: pointer.clientX, y: pointer.clientY, agentId: staff.agentId, harness: staff.harness })
        const el = document.elementFromPoint(pointer.clientX, pointer.clientY) as HTMLElement | null
        const slotEl = el?.closest<HTMLElement>('.slot')
        setHoverSlot(slotEl ? `${slotEl.dataset.fid}:${slotEl.dataset.sid}` : null)
      },
      finalize: pointer => {
        if (!staff.moved) return // a click staffs nothing
        const el = document.elementFromPoint(pointer.clientX, pointer.clientY) as HTMLElement | null
        const slotEl = el?.closest<HTMLElement>('.slot')
        if (slotEl && slotEl.dataset.fid && slotEl.dataset.sid) {
          const f = boardRef.current?.formations.find(item => item.id === slotEl.dataset.fid)
          const s = f?.slots.find(item => item.id === slotEl.dataset.sid)
          if (f && s) assignSlot(f, s, staff.agentId, staff.harness)
        } else if (staff.fromSlot && staff.moved) {
          // Dragging an agent out of its slot onto anything that isn't a slot unassigns it (reference).
          const f = boardRef.current?.formations.find(item => item.id === staff.fromSlot?.formationId)
          const s = f?.slots.find(item => item.id === staff.fromSlot?.slotId)
          if (f && s) unassignSlot(f, s)
        }
      },
      cancel: () => {
        setGhost(null)
        setHoverSlot(null)
      },
    })
    setGhost({ x: event.clientX, y: event.clientY, agentId, harness })
  }, [assignSlot, interactionOwner, unassignSlot])

  const beginNodeDrag = useCallback((event: ReactPointerEvent, id: string, index: number) => {
    if (event.button !== 0) return
    const target = event.target as HTMLElement
    if (target.closest('.port,.frun,.mrun,.note-pin')) return
    event.stopPropagation()
    const pos = positionOf(id, index)
    const drag: DragNode = { id, pointerId: event.pointerId, startX: event.clientX, startY: event.clientY, originX: pos.x, originY: pos.y, moved: false }
    interactionOwner.begin({
      kind: 'node',
      pointerId: event.pointerId,
      project: pointer => {
        // 3px movement threshold so plain clicks don't jiggle cards (reference feel).
        if (!drag.moved && Math.abs(pointer.clientX - drag.startX) + Math.abs(pointer.clientY - drag.startY) < 3) return
        if (!drag.moved) worldRef.current?.classList.add('nodedrag')
        drag.moved = true
        const scale = viewRef.current.scale || 1
        const nx = drag.originX + (pointer.clientX - drag.startX) / scale
        const ny = drag.originY + (pointer.clientY - drag.startY) / scale
        setDragPos({ id: drag.id, x: Math.round(nx), y: Math.round(ny) })
      },
      finalize: pointer => {
        if (!drag.moved) {
          // A click without a drag opens the node where it can be read and edited.
          const current = boardRef.current
          if (current && [...(current.missions || []), ...current.formations, ...(current.gates || [])].some(node => node.id === drag.id)) openNodeWindow(drag.id)
          return
        }
        const scale = viewRef.current.scale || 1
        undoStack.current.push({ kind: 'moveNode', id: drag.id, x: drag.originX, y: drag.originY })
        // Release snaps to the visible dot grid so hand-placed cards line up.
        void persistPosition(drag.id, snapToGrid(drag.originX + (pointer.clientX - drag.startX) / scale), snapToGrid(drag.originY + (pointer.clientY - drag.startY) / scale))
      },
      cancel: () => {
        worldRef.current?.classList.remove('nodedrag')
        setDragPos(null)
      },
    })
  }, [interactionOwner, openNodeWindow, persistPosition, positionOf])

  const endpointWorldCenter = useCallback((endpoint: string, direction: 'out' | 'in') => {
    const world = worldRef.current
    if (!world) return null
    const escaped = endpoint.replace(/["\\]/g, '\\$&')
    const [nodeId, portId] = endpoint.split(':')
    const el = portId === 'judge'
      ? world.querySelector<HTMLElement>(`[data-gate-judge-socket="${nodeId.replace(/["\\]/g, '\\$&')}"]`)
      : world.querySelector<HTMLElement>(`[data-port-${direction}="${escaped}"]`)
    if (!el) return null
    const wr = world.getBoundingClientRect()
    const r = el.getBoundingClientRect()
    const s = viewRef.current.scale || 1
    return { x: (r.left + r.width / 2 - wr.left) / s, y: (r.top + r.height / 2 - wr.top) / s }
  }, [])

  const portWorldCenter = useCallback((endpoint: string) => endpointWorldCenter(endpoint, 'out'), [endpointWorldCenter])

  const ownWireDrag = useCallback((event: ReactPointerEvent<Element> | ReactMouseEvent<Element>, active: WireDrag) => {
    const pointerId = 'pointerId' in event ? event.pointerId : 1
    interactionOwner.begin({
      kind: 'wire',
      pointerId,
      project: pointer => {
        const w = screenToWorld(pointer.clientX, pointer.clientY)
        if (active.kind === 'judge') {
          if (!active.moved && Math.abs(pointer.clientX - active.startX) + Math.abs(pointer.clientY - active.startY) < 4) return
          active.moved = true
          setTempWire(prev => (prev ? { ...prev, ax: w.x, ay: w.y } : prev))
          const card = (document.elementFromPoint(pointer.clientX, pointer.clientY) as HTMLElement | null)?.closest<HTMLElement>('.formation')
          setJudgeHover(card?.dataset.node && card.dataset.node !== active.gate.id ? card.dataset.node : null)
          return
        }
        setTempWire(prev => (prev ? (prev.moving === 'a' ? { ...prev, ax: w.x, ay: w.y } : { ...prev, bx: w.x, by: w.y }) : prev))
        if (active.kind === 'reconnect-source') {
          const outPort = findOutputPortAt(pointer.clientX, pointer.clientY)
          setHoverPort(outPort?.dataset.portOut ?? null)
        } else {
          const inPort = findInputPortAt(pointer.clientX, pointer.clientY)
          setHoverPort(inPort?.dataset.portIn ?? (inPort?.dataset.gateJudgeSocket ? `${inPort.dataset.gateJudgeSocket}:judge` : null))
        }
      },
      finalize: pointer => {
        const hoveredJudge = judgeHoverRef.current
        if (active.kind === 'judge') {
          const gate = active.gate
          if (!active.moved) {
            openJudgePickerRef.current?.(gate, pointer.clientX, pointer.clientY)
          } else if (hoveredJudge) {
            attachJudge(gate, [hoveredJudge])
          } else {
            // Dropped on empty canvas inside the viewport → spawn a judge there (reference just-works).
            const rect = viewportRef.current?.getBoundingClientRect()
            const overCard = (pointer.target as HTMLElement | null)?.closest?.('.formation,.gatecard,.missioncard,.toolcard,.ctxmenu,.pop')
            const inView = rect && pointer.clientX > rect.left && pointer.clientX < rect.right && pointer.clientY > rect.top && pointer.clientY < rect.bottom
            if (!overCard && inView) {
              const w = screenToWorld(pointer.clientX, pointer.clientY)
              void createJudgeFor(gate, 'solo', 'Judge', w.x - 100, w.y - 58)
            }
          }
        } else if (active.kind === 'reconnect-source') {
          const outPort = findOutputPortAt(pointer.clientX, pointer.clientY)
          if (outPort?.dataset.portOut) void rewireSource(active.connection, outPort.dataset.portOut)
        } else {
          const target = findInputPortAt(pointer.clientX, pointer.clientY)
          const judgeGateId = target?.dataset.gateJudgeSocket
          if (judgeGateId) {
            // Dropping an output onto a gate's judge socket makes it the judge (reference setJudgeReturn).
            const gate = (boardRef.current?.gates || []).find(item => item.id === judgeGateId)
            const fromEndpoint = active.kind === 'new' ? active.from : active.connection.from
            const fromNodeId = fromEndpoint.split(':')[0]
            const isFormation = boardRef.current?.formations.some(item => item.id === fromNodeId)
            if (gate && isFormation) {
              if (active.kind === 'reconnect-target') removeWire(active.connection)
              attachJudge(gate, [fromNodeId])
            }
          } else if (target?.dataset.portIn) {
            if (active.kind === 'new') wire(active.from, target.dataset.portIn)
            else rewireTarget(active.connection, target.dataset.portIn)
          }
        }
      },
      cancel: () => {
        setTempWire(null)
        setHoverPort(null)
        setHiddenWireId(null)
        setJudgeHover(null)
      },
    })
  }, [attachJudge, createJudgeFor, interactionOwner, removeWire, rewireSource, rewireTarget, screenToWorld, wire])

  const beginWire = useCallback((event: ReactPointerEvent, endpoint: string, kind: WirePath['kind']) => {
    if (event.button !== 0) return
    event.stopPropagation()
    ownWireDrag(event, { kind: 'new', from: endpoint, wireKind: kind })
    const start = portWorldCenter(endpoint)
    const w = screenToWorld(event.clientX, event.clientY)
    setTempWire(start ? { ax: start.x, ay: start.y, bx: w.x, by: w.y, kind, moving: 'b' } : { ax: w.x, ay: w.y, bx: w.x, by: w.y, kind, moving: 'b' })
  }, [ownWireDrag, portWorldCenter, screenToWorld])

  const beginReconnect = useCallback((event: ReactPointerEvent<HTMLElement> | ReactMouseEvent<HTMLElement>, connection: BoardConnection) => {
    if (event.button !== 0) return
    if (interactionOwner.projection()?.kind === 'wire') return
    event.stopPropagation()
    ownWireDrag(event, { kind: 'reconnect-target', connection })
    setHiddenWireId(connection.id)
    const kind = connectionKind(connection)
    const start = endpointWorldCenter(connection.from, 'out')
    const w = screenToWorld(event.clientX, event.clientY)
    setTempWire(start
      ? { ax: start.x, ay: start.y, bx: w.x, by: w.y, kind, moving: 'b' }
      : { ax: w.x, ay: w.y, bx: w.x, by: w.y, kind, moving: 'b' })
  }, [endpointWorldCenter, interactionOwner, ownWireDrag, screenToWorld])

  const beginReconnectSource = useCallback((event: ReactPointerEvent<Element> | ReactMouseEvent<Element>, connection: BoardConnection) => {
    if (event.button !== 0) return
    if (interactionOwner.projection()?.kind === 'wire') return
    event.stopPropagation()
    ownWireDrag(event, { kind: 'reconnect-source', connection })
    setHiddenWireId(connection.id)
    const kind = connectionKind(connection)
    const fixed = endpointWorldCenter(connection.to, 'in')
    const w = screenToWorld(event.clientX, event.clientY)
    setTempWire(fixed
      ? { ax: w.x, ay: w.y, bx: fixed.x, by: fixed.y, kind, moving: 'a' }
      : { ax: w.x, ay: w.y, bx: w.x, by: w.y, kind, moving: 'a' })
  }, [endpointWorldCenter, interactionOwner, ownWireDrag, screenToWorld])

  const beginJudgeDrag = useCallback((event: ReactPointerEvent<HTMLElement>, gate: GateNode) => {
    if (event.button !== 0) return
    if (interactionOwner.projection()?.kind === 'wire') return
    event.stopPropagation()
    event.preventDefault()
    ownWireDrag(event, { kind: 'judge', gate, moved: false, startX: event.clientX, startY: event.clientY })
    const start = endpointWorldCenter(`${gate.id}:judge`, 'out')
    const w = screenToWorld(event.clientX, event.clientY)
    setTempWire(start
      ? { ax: w.x, ay: w.y, bx: start.x, by: start.y, kind: 'judge', moving: 'a' }
      : { ax: w.x, ay: w.y, bx: w.x, by: w.y, kind: 'judge', moving: 'a' })
  }, [endpointWorldCenter, interactionOwner, ownWireDrag, screenToWorld])

  const captureConnectedInputDrag = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    const target = event.target as HTMLElement
    const port = target.closest<HTMLElement>('[data-port-in]')
    const endpoint = port?.dataset.portIn
    if (!endpoint) return
    const connection = boardRef.current?.connections.find(candidate => candidate.to === endpoint) || (
      port?.dataset.reconnectFrom
        ? { id: port.dataset.reconnectId || `${port.dataset.reconnectFrom}-${endpoint}`, from: port.dataset.reconnectFrom, to: endpoint }
        : undefined
    )
    if (connection) beginReconnect(event, connection)
  }, [beginReconnect])

  /** Grab a committed wire: near an ENDPOINT reconnects that end; the MIDDLE hand-routes its lane (reference startWireDrag). */
  const beginWireDrag = useCallback((event: ReactPointerEvent<SVGPathElement>, connection: BoardConnection) => {
    if (event.button !== 0) return
    const activeKind = interactionOwner.projection()?.kind
    if (activeKind === 'wire' || activeKind === 'lane') return
    event.stopPropagation()
    const a = endpointWorldCenter(connection.from, 'out')
    const b = endpointWorldCenter(connection.to, 'in')
    const w = screenToWorld(event.clientX, event.clientY)
    if (a && b) {
      const near = 70 // world px
      const dFrom = Math.hypot(w.x - a.x, w.y - a.y)
      const dTo = Math.hypot(w.x - b.x, w.y - b.y)
      if (dTo <= dFrom && dTo < near) return beginReconnect(event as unknown as ReactPointerEvent<HTMLElement>, connection)
      if (dFrom < dTo && dFrom < near) return beginReconnectSource(event, connection)
    }
    const previousLane = layoutRef.current?.edges?.find(edge => edge.id === connection.id)?.lane || 'auto'
    const lane: LaneDrag = { connectionId: connection.id, previousLane, moved: false }
    interactionOwner.begin({
      kind: 'lane',
      pointerId: event.pointerId,
      project: pointer => {
        lane.moved = true
        const projected = screenToWorld(pointer.clientX, pointer.clientY)
        setLaneDraft({ connectionId: lane.connectionId, y: Math.round(projected.y) })
      },
      finalize: pointer => {
        if (!lane.moved) return
        const projected = screenToWorld(pointer.clientX, pointer.clientY)
        undoStack.current.push({ kind: 'setLane', edgeId: lane.connectionId, lane: lane.previousLane })
        void patchLayoutEdge(lane.connectionId, `y:${Math.round(projected.y)}`).then(() => setLaneDraft(null))
      },
      cancel: () => setLaneDraft(null),
    })
  }, [beginReconnect, beginReconnectSource, endpointWorldCenter, interactionOwner, patchLayoutEdge, screenToWorld])

  const beginGateToken = useCallback((event: ReactPointerEvent) => {
    if (event.button !== 0) return
    if (!boardRef.current) return
    event.preventDefault()
    interactionOwner.begin({
      kind: 'gate',
      pointerId: event.pointerId,
      project: pointer => setGateGhost({ x: pointer.clientX, y: pointer.clientY }),
      finalize: pointer => {
        const rect = viewportRef.current?.getBoundingClientRect()
        if (rect && pointer.clientX >= rect.left && pointer.clientX <= rect.right && pointer.clientY >= rect.top && pointer.clientY <= rect.bottom) {
          const w = screenToWorld(pointer.clientX, pointer.clientY)
          void createGateAt(w.x, w.y)
        }
      },
      cancel: () => setGateGhost(null),
    })
    setGateGhost({ x: event.clientX, y: event.clientY })
  }, [createGateAt, interactionOwner, screenToWorld])

  const gateHasJudge = useCallback((gateId: string): boolean => {
    return (boardRef.current?.connections || []).some(connection =>
      connection.from === `${gateId}:judge` || connection.to === `${gateId}:judge`)
  }, [])

  // Undo restores the exact previous slots, including any a change to solo removed.
  const changeFormationType = useCallback((formation: FormationNode, type: FormationType, keepSlotId?: string) => {
    undoStack.current.push({ kind: 'setFormationType', id: formation.id, type: formation.type, slots: formation.slots })
    void patchBoard({ setFormationType: { id: formation.id, type, ...(keepSlotId ? { keepSlotId } : {}) } }).then(result => {
      if (!result) undoStack.current.pop()
    })
  }, [patchBoard])

  const formationTypeMenuItems = useCallback((formation: FormationNode): MenuItem[] => [
    { label: 'Change type', head: true },
    ...formationTypeChoices(formation).map(choice => ({ label: choice.label, action: () => changeFormationType(formation, choice.type, choice.keepSlotId) })),
  ], [changeFormationType])

  const formationMenu = useCallback((event: ReactMouseEvent<HTMLElement>, formation: FormationNode) => {
    openMenu(event, 'Formation actions', [
      { label: 'Run formation', action: () => void runFormation(formation) },
      { label: 'Add input port', action: () => void addPortOp(formation, 'in') },
      { label: 'Add output port', action: () => void addPortOp(formation, 'out') },
      ...(formation.verification ? [
        { label: 'Migrate legacy verification', action: () => openLegacyVerification(formation) },
      ] : []),
      ...formationTypeMenuItems(formation),
      { label: 'Delete formation', destructive: true, action: () => deleteFormationOp(formation) },
    ])
  }, [addPortOp, deleteFormationOp, formationTypeMenuItems, openLegacyVerification, openMenu, runFormation])

  const slotMenu = useCallback((event: ReactMouseEvent<HTMLElement>, formation: FormationNode, slot: FormationSlot) => {
    const items: MenuItem[] = []
    if (slot.agentId) {
      items.push({ label: `Unassign ${slot.agentId}`, action: () => unassignSlot(formation, slot) })
    }
    if (formation.type === 'orchestrated' && !slot.controller) {
      items.push({ label: 'Make controller', action: () => makeControllerOp(formation, slot) })
    }
    const assignable = agents.filter(agent => agent.id !== slot.agentId)
    if (assignable.length) {
      items.push({ label: 'Assign agent', head: true })
      for (const agent of assignable) {
        items.push({ label: agent.displayName || agent.id, action: () => assignSlot(formation, slot, agent.id, agent.harnessDefault || '') })
      }
    } else if (!items.length) {
      items.push({ label: 'No agents on the socket', disabled: true })
    }
    openMenu(event, `Slot · ${slot.label}`, items)
  }, [agents, assignSlot, makeControllerOp, openMenu, unassignSlot])

  const wireMenu = useCallback((event: ReactMouseEvent<SVGPathElement>, connection: BoardConnection) => {
    openMenu(event, 'Connection actions', [
      { label: 'Reset routing', action: () => void patchLayoutEdge(connection.id, 'auto') },
      { label: 'Remove connection', destructive: true, action: () => removeWire(connection) },
    ])
  }, [openMenu, patchLayoutEdge, removeWire])

  const openJudgePicker = useCallback((gate: GateNode, x: number, y: number) => {
    const pos = displayLayoutByNode.get(gate.id)
    const gateX = pos?.x ?? 200
    const gateY = pos?.y ?? 200
    const items: MenuItem[] = [
      { label: 'Judge with a NEW formation', head: true },
      { label: 'Solo · 1 agent', action: () => void createJudgeFor(gate, 'solo', 'Judge', gateX, gateY - 200) },
      { label: 'Peer · 2 equals', action: () => void createJudgeFor(gate, 'peer', 'Judge panel', gateX, gateY - 200) },
      { label: 'Orchestrated · controller', action: () => void createJudgeFor(gate, 'orchestrated', 'Judge desk', gateX, gateY - 200) },
    ]
    const formations = boardRef.current?.formations || []
    if (formations.length) {
      items.push({ label: '…or an existing formation', head: true })
      for (const formation of formations) {
        items.push({ label: formation.title, action: () => attachJudge(gate, [formation.id]) })
      }
    }
    if (gateHasJudge(gate.id)) {
      items.push({ label: 'Detach judge', destructive: true, action: () => detachJudge(gate) })
    }
    setMenu({ label: 'Judge', x, y, items })
  }, [attachJudge, createJudgeFor, detachJudge, displayLayoutByNode, gateHasJudge])
  openJudgePickerRef.current = openJudgePicker

  const gateMenu = useCallback((event: ReactMouseEvent<HTMLElement>, gate: GateNode) => {
    openMenu(event, 'Gate actions', [
      { label: gateHasJudge(gate.id) ? 'Change judge…' : 'Attach judge…', action: () => openJudgePicker(gate, event.clientX, event.clientY) },
      ...(gateHasJudge(gate.id) ? [{ label: 'Detach judge', action: () => detachJudge(gate) }] : []),
      { label: 'Delete gate', destructive: true, action: () => deleteGateOp(gate) },
    ])
  }, [deleteGateOp, detachJudge, gateHasJudge, openJudgePicker, openMenu])

  const missionMenu = useCallback((event: ReactMouseEvent<HTMLElement>, mission: MissionNode) => {
    openMenu(event, 'Mission actions', [
      { label: 'Start mission', action: () => setStartMission(mission) },
      { label: 'Delete mission', destructive: true, action: () => deleteMissionOp(mission) },
    ])
  }, [deleteMissionOp, openMenu])

  const inputRowMenu = useCallback((event: ReactMouseEvent<HTMLElement>, formation: FormationNode, portId: string, incoming?: BoardConnection) => {
    openMenu(event, 'Input port', [
      ...(incoming ? [{ label: 'Disconnect input', action: () => removeWire(incoming) }] : []),
      { label: 'Add input port', action: () => void addPortOp(formation, 'in') },
      { label: 'Remove this input', destructive: true, action: () => removePortOp(formation, portId) },
    ])
  }, [addPortOp, openMenu, removePortOp, removeWire])

  const outputRowMenu = useCallback((event: ReactMouseEvent<HTMLElement>, formation: FormationNode, portId: string) => {
    openMenu(event, 'Output port', [
      { label: 'Add output port', action: () => void addPortOp(formation, 'out') },
      { label: 'Remove this output', destructive: true, action: () => removePortOp(formation, portId) },
    ])
  }, [addPortOp, openMenu, removePortOp])

  const canvasMenu = useCallback((event: ReactMouseEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement
    if (target.closest('.formation,.gatecard,.missioncard,.toolcard,.ctxmenu,.pop,.run-banner,.zoomctl')) return
    if ((target as Element).closest?.('path')) return
    event.preventDefault()
    event.stopPropagation()
    const w = screenToWorld(event.clientX, event.clientY)
    setMenu({
      label: 'New',
      x: event.clientX,
      y: event.clientY,
      items: [
        { label: 'Mission', action: () => createMissionAt(w.x, w.y) },
        { label: 'Solo formation', action: () => void createFormationAt('solo', 'New formation', w.x, w.y) },
        { label: 'Peer formation', action: () => void createFormationAt('peer', 'New peers', w.x, w.y) },
        { label: 'Orchestrated formation', action: () => void createFormationAt('orchestrated', 'New desk', w.x, w.y) },
        { label: 'Gate', action: () => void createGateAt(w.x, w.y) },
      ],
    })
  }, [createFormationAt, createGateAt, createMissionAt, screenToWorld])

  // ----- render helpers -----
  const renderSlot = (formation: FormationNode, slot: FormationSlot, badge?: number) => {
    const filled = !!slot.agentId
    const key = `${formation.id}:${slot.id}`
    const runState = nodeStates.get(formation.id)
    const classes = ['slot', filled ? 'filled' : 'empty', slot.controller ? 'ctrl' : '', hoverSlot === key ? 'snaptarget' : '', runState === 'running' ? 'active' : '', runState === 'done' ? 'active done' : '']
    return (
      <div
        key={slot.id}
        className={classes.filter(Boolean).join(' ')}
        data-fid={formation.id}
        data-sid={slot.id}
        data-testid={`slot-${formation.id}-${slot.id}`}
        title={slotTooltip(slot)}
        onPointerDown={filled ? event => beginStaff(event, slot.agentId as string, slot.harness || '', { formationId: formation.id, slotId: slot.id }) : undefined}
        onContextMenu={event => slotMenu(event, formation, slot)}
      >
        <div className="slot-ring">
          {badge ? <span className="badge">{badge}</span> : null}
          {filled
            ? <span className="face">{harnessGlyph(slot.harness) ?? initials(slot.agentId as string)}</span>
            : <span className="plus">+</span>}
        </div>
        <div className="slot-label">{slot.label}</div>
        {filled ? <div className="who">{slot.agentId}</div> : null}
      </div>
    )
  }

  const renderBody = (formation: FormationNode) => (
    <FormationSeats formation={formation} renderSlot={(slot, badge) => renderSlot(formation, slot, badge)} />
  )

  const noteByNode = useMemo(
    () => new Map((notes?.elements || []).filter(note => note.entries.length).map(note => [note.nodeId, note.entries])),
    [notes?.elements],
  )
  const noteElements = useMemo(() => {
    if (!board) return []
    return [
      ...(board.missions || []).map(node => ({ id: node.id, title: node.title, kind: 'Mission' })),
      ...(board.formations || []).map(node => ({ id: node.id, title: node.title, kind: 'Formation' })),
      ...(board.gates || []).map(node => ({ id: node.id, title: node.title || 'Gate', kind: 'Gate' })),
      ...(board.tools || []).map(node => ({ id: node.id, title: node.title, kind: 'Tool' })),
    ]
  }, [board])

  const nodeNotes = useMemo(
    () => noteElements.filter(element => noteByNode.has(element.id)).map(element => ({ nodeId: element.id, title: element.title, entries: noteByNode.get(element.id) || [] })),
    [noteByNode, noteElements],
  )
  const noteTitleOf = (target: string) => target === BOARD_NOTE_TARGET
    ? board?.title || 'Board'
    : noteElements.find(element => element.id === target)?.title || target

  const changeNotesMode = useCallback((mode: NotesMode) => {
    writeNotesMode(mode)
    setNotesMode(mode)
  }, [])

  // A write that races another author reloads the notes and tries once more:
  // appends and entry-addressed edits are safe to repeat on the new revision.
  const submitNotePatch = useCallback(async (patch: NotePatch) => {
    const currentBoard = boardRef.current
    const currentNotes = notesRef.current
    if (!currentBoard || !currentNotes) return null
    try {
      return await patchBoardNote(currentBoard.slug, currentNotes.etag, patch)
    } catch (err) {
      if (!(err instanceof ApiRequestError && err.status === 409)) throw err
      const fresh = await fetchBoardNotes(currentBoard.slug)
      notesRef.current = fresh
      setNotes(fresh)
      return patchBoardNote(currentBoard.slug, fresh.etag, patch)
    }
  }, [])

  const setNoteDraft = useCallback((target: string, text: string) => {
    noteDraftsRef.current = { ...noteDraftsRef.current, [target]: text }
    setNoteDrafts(noteDraftsRef.current)
  }, [])

  const stopNoteEdit = useCallback((target: string) => {
    setNoteEditing(current => {
      const next = { ...current }
      delete next[target]
      return next
    })
  }, [])

  const runNotePatch = useCallback(async (target: string, patch: NotePatch, onSaved?: () => void) => {
    setNoteSaving(target)
    setNoteErrors(current => ({ ...current, [target]: '' }))
    try {
      const updated = await submitNotePatch(patch)
      if (!updated) return
      notesRef.current = updated
      setNotes(updated)
      setNotesConflict(false)
      onSaved?.()
    } catch (err) {
      if (err instanceof ApiRequestError && err.status === 409) setNotesConflict(true)
      setNoteErrors(current => ({ ...current, [target]: err instanceof Error ? err.message : 'Failed to save the note' }))
    } finally {
      setNoteSaving('')
    }
  }, [submitNotePatch])

  const saveNote = useCallback((target: string) => {
    const text = noteDraftsRef.current[target] || ''
    if (!text.trim()) return
    const entryId = noteEditing[target]
    const patch: NotePatch = entryId
      ? { target, action: 'edit', entryId, text }
      : { target, action: 'append', text }
    void runNotePatch(target, patch, () => {
      setNoteDraft(target, '')
      if (entryId) stopNoteEdit(target)
    })
  }, [noteEditing, runNotePatch, setNoteDraft, stopNoteEdit])

  const startNoteEdit = useCallback((target: string, entry: NoteEntry) => {
    setNoteEditing(current => ({ ...current, [target]: entry.id }))
    setNoteDraft(target, entry.text)
  }, [setNoteDraft])

  const cancelNoteEdit = useCallback((target: string) => {
    stopNoteEdit(target)
    setNoteDraft(target, '')
  }, [setNoteDraft, stopNoteEdit])

  const deleteNote = useCallback((target: string, entry: NoteEntry) => {
    void runNotePatch(target, { target, action: 'delete', entryId: entry.id }, () => {
      if (noteEditing[target] === entry.id) cancelNoteEdit(target)
    })
  }, [cancelNoteEdit, noteEditing, runNotePatch])

  const reloadSharedNotes = useCallback(async () => {
    const currentBoard = boardRef.current
    if (!currentBoard) return
    setNoteSaving(BOARD_NOTE_TARGET)
    try {
      const fresh = await fetchBoardNotes(currentBoard.slug)
      notesRef.current = fresh
      setNotes(fresh)
      noteDraftsRef.current = {}
      setNoteDrafts({})
      setNoteEditing({})
      setNotesConflict(false)
      setNoteErrors({})
    } catch (err) {
      setNoteErrors(current => ({ ...current, [BOARD_NOTE_TARGET]: err instanceof Error ? err.message : 'Failed to reload shared notes' }))
    } finally {
      setNoteSaving('')
    }
  }, [])

  // Stickies sit in their own layer, so they follow each noted card's box.
  useLayoutEffect(() => {
    const world = worldRef.current
    const next = new Map<string, NoteAnchor>()
    if (world && notesMode !== 'hidden') {
      for (const card of world.querySelectorAll<HTMLElement>(NOTE_CARDS)) {
        const nodeId = card.dataset.node || ''
        if (!noteByNode.has(nodeId)) continue
        next.set(nodeId, { x: parseFloat(card.style.left) || 0, y: parseFloat(card.style.top) || 0, width: card.offsetWidth, height: card.offsetHeight })
      }
    }
    // Compared with a ref, not a state updater: this effect runs after every
    // render, and an updater would schedule a render even when nothing moved.
    if (sameNoteAnchors(noteAnchorsRef.current, next)) return
    noteAnchorsRef.current = next
    setNoteAnchors(next)
  })

  const renderNotePin = (nodeID: string, title: string) => {
    const hasNote = noteByNode.has(nodeID)
    return (
      <button
        type="button"
        className={`note-pin${hasNote ? ' filled' : ''}`}
        aria-label={`${hasNote ? 'Open notes' : 'Add note'} for ${title}`}
        title={hasNote ? 'Open the note thread' : 'Add a note'}
        onPointerDown={event => event.stopPropagation()}
        onClick={event => { event.stopPropagation(); openNoteWindow(nodeID) }}
      >
        <span aria-hidden="true">{hasNote ? '▰' : '+'}</span>
      </button>
    )
  }

  const rosterAgents = useMemo(() => agents.filter(agent => agent.assignable && !agent.unbound), [agents])
  const rosterSections = useMemo(() => groupRosterByHarness(rosterAgents), [rosterAgents])
  const deployedAgentCount = useMemo(
    () => new Set((board?.formations || []).flatMap(f => f.slots.map(s => s.agentId).filter(Boolean))).size,
    [board?.formations],
  )
  const runBadgeClass = activeRun ? activeRun.status : ''
  const runChoices = useMemo(() => {
    const open = openRunsByAttention(boardRuns)
    return activeRun && !open.some(run => run.runId === activeRun.runId) ? [activeRun, ...open] : open
  }, [activeRun, boardRuns])
  const pendingHumanGateId = useMemo(() => openHumanGateId(runEvents), [runEvents])
  const pendingHumanGate = useMemo(() => {
    if (!pendingHumanGateId) return null
    const requestedSeq = activeRun?.waitingGates?.find(gate => gate.gateId === pendingHumanGateId)?.requestedSeq
      || [...runEvents].reverse().find(event => event.type === 'human_input_requested' && event.gateId === pendingHumanGateId)?.seq
      || 0
    if (!requestedSeq) return null
    const gate = board?.gates?.find(node => node.id === pendingHumanGateId)
    return { gateId: pendingHumanGateId, requestedSeq, title: gate?.title || pendingHumanGateId, criterion: gate?.criterion || '' }
  }, [activeRun?.waitingGates, board?.gates, pendingHumanGateId, runEvents])
  const pendingGateInput = useHumanGateUpstream(activeRun?.runId || '', pendingHumanGate)
  const pendingGateUpstream = useMemo(() => {
    if (pendingGateInput.state !== 'ready') return pendingGateInput
    const node = [...(board?.formations || []), ...(board?.gates || []), ...(board?.missions || [])].find(candidate => candidate.id === pendingGateInput.from)
    return { ...pendingGateInput, from: node?.title || pendingGateInput.from }
  }, [board?.formations, board?.gates, board?.missions, pendingGateInput])
  const openEscalations = useMemo(
    () => (activeRun && !activeRun.final ? escalations : []),
    [activeRun, escalations],
  )
  const needsYouNodeIds = useMemo(() => {
    const ids = new Set<string>()
    for (const escalation of openEscalations) {
      if (escalation.gateId) ids.add(escalation.gateId)
      if (escalation.nodeId) ids.add(escalation.nodeId)
    }
    return ids
  }, [openEscalations])
  const draftFindings = useMemo(
    () => findingsByNode(board, validation ? [...validation.errors, ...validation.warnings] : []),
    [board, validation],
  )
  const blockedFindings = useMemo(() => findingsByNode(board, admissionFindings), [board, admissionFindings])
  const draftClass = (nodeId: string) => `${draftFindings.has(nodeId) ? ' is-draft' : ''}${blockedFindings.has(nodeId) ? ' admission-blocked' : ''}`
  const renderDraftMarker = (nodeId: string) => (
    <DraftMarker nodeId={nodeId} findings={blockedFindings.get(nodeId) ?? draftFindings.get(nodeId)} blocked={blockedFindings.has(nodeId)} />
  )
  const renderRunChip = (nodeId: string) => {
    const state = nodeStates.get(nodeId)
    return state === 'blocked' || state === 'failed' ? <span className={`run-chip ${state}`} data-testid={`run-chip-${nodeId}`}>{state}</span> : null
  }
  const runPointTitle = (() => {
    const nodeId = runPoint?.nodeId
    if (!nodeId || !board) return ''
    const gate = board.gates?.find(node => node.id === nodeId)
    return board.formations?.find(node => node.id === nodeId)?.title
      || (gate ? gate.title || gate.kinds.map(gateKindLabel).join(' · ') || 'Gate' : '')
      || board.missions?.find(node => node.id === nodeId)?.title
      || ''
  })()
  const inspectedNode = useMemo(() => {
    if (!inspectedNodeId || !board) return null
    const formation = board.formations?.find(node => node.id === inspectedNodeId)
    if (formation) return { kind: 'formation' as const, id: formation.id, title: formation.title }
    const gate = board.gates?.find(node => node.id === inspectedNodeId)
    if (gate) return { kind: 'gate' as const, id: gate.id, title: gate.title || gate.kinds.map(gateKindLabel).join(' · ') || 'Gate' }
    const mission = board.missions?.find(node => node.id === inspectedNodeId)
    if (mission) return { kind: 'mission' as const, id: mission.id, title: mission.title }
    return null
  }, [inspectedNodeId, board])
  const inspectableNodeId = useCallback((escalation: OpenEscalation): string => {
    const candidate = escalation.gateId || escalation.nodeId || ''
    if (!candidate || !board) return ''
    const known = board.formations?.some(node => node.id === candidate)
      || board.gates?.some(node => node.id === candidate)
      || board.missions?.some(node => node.id === candidate)
    return known ? candidate : ''
  }, [board])

  const nodeWindowOps = useMemo<NodeWindowOps>(() => ({
    rename: renameNode,
    updateMission: updateMissionFields,
    setBrief: saveBrief,
    changeType: changeFormationType,
    assignSlot,
    updateGate: (gate, draft) => updateGateFields(gate.id, draft),
    setGateFiles: (gate, files) => setGateFiles(gate.id, files),
    attachJudge,
    detachJudge,
    openNode: openNodeWindow,
    openNotes: openNoteWindow,
    inspectEvidence: nodeId => {
      // The evidence dialog sits above the window; keyboard focus leaves the window so Escape closes the dialog first.
      if (document.activeElement instanceof HTMLElement) document.activeElement.blur()
      setInspectedNodeId(nodeId)
    },
  }), [assignSlot, attachJudge, changeFormationType, detachJudge, openNodeWindow, openNoteWindow, renameNode, saveBrief, setGateFiles, updateGateFields, updateMissionFields])

  const cockpit = (
    <div className="fmx" data-testid="formations-view" data-cockpit="d7">
      <div className="topbar">
        <div className="boardpick">
          board
          <select value={selectedSlug} onChange={event => selectBoard(event.target.value)} data-testid="board-picker" disabled={boards.length === 0 || Boolean(boardDialog)}>
            {boards.length === 0 ? <option value="">No boards</option> : null}
            {boards.map(summary => <option key={summary.slug} value={summary.slug}>{summary.title || summary.slug}</option>)}
          </select>
          {board ? <span className="rev">rev {board.rev}</span> : null}
        </div>
        <button className="newbtn board-new" type="button" onClick={event => openCreateBoard(event.currentTarget)} data-testid="new-board" disabled={Boolean(boardDialog)}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M12 5v14M5 12h14" /></svg>
          New board
        </button>
        <button className="board-action" type="button" aria-label="Rename board" disabled={!board || Boolean(boardDialog)} onClick={event => openRenameBoard(event.currentTarget)}>Rename</button>
        <button className="board-action danger" type="button" aria-label="Delete board" disabled={!board || Boolean(boardDialog)} onClick={event => openDeleteBoard(event.currentTarget)}>Delete</button>
        <div className="sep" />
        <button className="newbtn" onClick={createSolo} data-testid="new-formation" disabled={!board}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M12 5v14M5 12h14" /></svg>
          New formation
        </button>
        <div className="gatetoken" title="Drag onto the canvas to drop a gate" data-testid="gate-token" onPointerDown={beginGateToken}>
          {GATE_SVG}
          Gate
        </div>
        <div className="spacer" />
        <CanvasLegend />
        <div className="notes-switch" role="radiogroup" aria-label="Notes on the canvas">
          {NOTES_MODES.map(mode => (
            <button key={mode} type="button" role="radio" aria-checked={notesMode === mode} className={notesMode === mode ? 'on' : ''}
              onClick={() => changeNotesMode(mode)}>{mode === 'hidden' ? 'Hide notes' : mode === 'preview' ? 'Preview' : 'Full notes'}</button>
          ))}
        </div>
        <button type="button" className="newbtn board-notes-button" disabled={!board} onClick={() => openNoteWindow(BOARD_NOTE_TARGET)}>Board notes</button>
        <button type="button" className="newbtn" disabled={!activeRun}
          title={activeRun ? 'Observe run seats' : 'Start a run to observe its seats'}
          onClick={() => openPeek('')}>Open terminal</button>
      </div>

      <div className="main">
        <aside
          className={`roster${roster.collapsed ? ' collapsed' : ''}`}
          data-testid="agent-roster"
          aria-label="Agent roster"
          style={{ '--roster-width': `${roster.width}px` } as CSSProperties}
        >
          <div className="roster-hd">
            <div className="t">Agents</div>
            <span className="s" data-testid="roster-count" title={`${rosterAgents.length} catalog agents · ${deployedAgentCount} placed on this board`}>
              {rosterCountLabel(rosterAgents.length, { placed: deployedAgentCount, scope: 'board' })}
            </span>
            <button type="button" className="roster-toggle" aria-expanded={!roster.collapsed}
              aria-label={roster.collapsed ? 'Expand agent roster' : 'Collapse agent roster'} onClick={roster.toggle}>
              {roster.collapsed ? '›' : '‹'}
            </button>
          </div>
          <div
            className="roster-resize"
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize agent roster"
            aria-valuemin={ROSTER_MIN_WIDTH}
            aria-valuemax={ROSTER_MAX_WIDTH}
            aria-valuenow={roster.width}
            tabIndex={0}
            onPointerDown={roster.beginResize}
            onKeyDown={roster.resizeByKey}
          />
          <div className="roster-list">
            {rosterAgents.length === 0
              ? <div className="roster-empty">No assignable catalog agents. Create a persona in the Agents view to staff formations.</div>
              : rosterSections.map(section => (
                <section className="roster-group" key={section.id} data-provider={section.id}>
                  <div className="roster-group-label">{section.label}</div>
                  {section.agents.map(agent => {
                    const deployed = (board?.formations || []).some(f => f.slots.some(s => s.agentId === agent.id))
                    return (
                      <div
                        key={agent.id}
                        className={`ragent${deployed ? ' deployed' : ''}${agent.unbound ? ' unbound' : ''}`}
                        data-agent={agent.id}
                        data-testid={`roster-agent-${agent.id}`}
                        onPointerDown={event => beginStaff(event, agent.id, agent.harnessDefault || '')}
                      >
                        <span className="av">{harnessGlyph(agent.harnessDefault) ?? initials(agent.id)}</span>
                        <div className="ri">
                          <div className="n">{agent.displayName || agent.id}</div>
                          <div className="r">{agentRole(agent)}{agent.preset ? ` · ${agent.customized ? 'custom' : 'preset'}` : ''}{agentState(agent) === 'idle' ? ' · idle' : ''}</div>
                        </div>
                        <button
                          type="button"
                          className="agent-edit"
                          aria-label={`Edit ${agent.displayName || agent.id}`}
                          title="Edit persona override"
                          onPointerDown={event => event.stopPropagation()}
                          onClick={event => { event.stopPropagation(); setAgentEditor({ agent, trigger: event.currentTarget }) }}
                        >•••</button>
                      </div>
                    )
                  })}
                </section>
              ))}
          </div>
        </aside>

        {/* The run banner docks above the canvas so it never covers cards. */}
        <div className="canvas-column">
            {activeRun ? (
              <div className="run-banner" data-testid="run-banner">
                <span>run</span>
                <span className={`badge ${runBadgeClass}`}>{activeRun.status}</span>
                <RunPoint runId={activeRun.runId} point={runPoint} title={runPointTitle} onLocate={locateAndOpenNode} />
                <RunProduced />
                {runChoices.length > 1 ? (
                  <select
                    className="run-picker"
                    aria-label="Choose run"
                    value={activeRun.runId}
                    onChange={event => {
                      setLinkError('')
                      setPinnedRun({ slug: selectedSlug, runId: event.target.value })
                    }}
                  >
                    {runChoices.map(run => <option key={run.runId} value={run.runId}>{runChoiceLabel(run)}</option>)}
                  </select>
                ) : null}
                {activeRun.cwd && <span className="run-cwd" title={activeRun.cwd}>{activeRun.cwd}</span>}
                {activeRun.beadId && <span>{activeRun.beadId}</span>}
                {!activeRun.final && activeRun.resumeAllowed ? <button type="button" onClick={() => void resumeActiveRun()}>Resume run</button> : null}
                {!activeRun.final ? <button type="button" onClick={() => void abortActiveRun()}>stop</button> : null}
              </div>
            ) : null}
        <div className="viewport" data-testid="formations-canvas" ref={viewportRef} onPointerDownCapture={captureConnectedInputDrag} onPointerDown={onViewportPointerDown} onContextMenu={canvasMenu}>
          <div className="world" data-testid="formations-world" ref={worldRef} style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }}>
            <svg className="wires" width={3400} height={2300}>
              {wires.map(path => {
                const connection = board?.connections.find(candidate => candidate.id === path.id)
                if (!connection) return null
                return (
                  <g key={path.id}>
                    <path
                      className="wirehit"
                      d={path.d}
                      onPointerDown={event => beginWireDrag(event, connection)}
                      onContextMenu={event => wireMenu(event, connection)}
                    />
                    <path
                      className={`wire ${path.kind}${path.loop ? ' loop' : ''}${path.flowing ? ' flowing' : ''}`}
                      data-testid={`formation-wire-${path.id}`}
                      d={path.d}
                      onPointerDown={event => beginWireDrag(event, connection)}
                      onContextMenu={event => wireMenu(event, connection)}
                    />
                  </g>
                )
              })}
              {wires.map(path => path.label ? (
                <text
                  key={`label-${path.id}`}
                  className={`wire-label ${path.kind}${path.loop ? ' loop' : ''}`}
                  data-testid={`wire-label-${path.id}`}
                  x={path.label.x}
                  y={path.label.y}
                  textAnchor={path.label.anchor}
                >{path.label.text}</text>
              ) : null)}
              {tempWire ? (
                <path
                  className={`wire temp${tempWire.kind !== 'wire' ? ` ${tempWire.kind}` : ''}`}
                  d={`M ${tempWire.ax} ${tempWire.ay} C ${tempWire.ax + 50} ${tempWire.ay}, ${tempWire.bx - 50} ${tempWire.by}, ${tempWire.bx} ${tempWire.by}`}
                />
              ) : null}
            </svg>

            {!board ? (
              <div className="empty-board" data-testid="formations-empty-board">
                <div className="empty-title">No persisted formation boards</div>
                <div className="empty-copy">Create a board from the top bar to start sketching a mission.</div>
              </div>
            ) : (board.missions || []).length + board.formations.length + (board.gates || []).length + (board.tools || []).length === 0 ? (
              <div className="empty-board" data-testid="formations-empty-board">
                <div className="empty-title">This board is empty</div>
                <div className="empty-copy">Add a formation, Gate, Mission, or Tool to sketch the workflow.</div>
              </div>
            ) : null}

            {(board?.missions || []).map((mission, index) => {
              const pos = positionOf(mission.id, index)
              const state = nodeStates.get(mission.id)
              return (
                <div
                  key={mission.id}
                  className={`missioncard${state === 'blocked' || state === 'failed' ? ` ${state}` : ''}${locatedNodeId === mission.id ? ' located' : ''}${noteByNode.has(mission.id) ? ' has-note' : ''}${draftClass(mission.id)}`}
                  data-node={mission.id}
                  data-testid={`mission-node-${mission.id}`}
                  style={{ left: pos.x, top: pos.y }}
                  onPointerDown={event => beginNodeDrag(event, mission.id, index)}
                  onContextMenu={event => missionMenu(event, mission)}
                >
                  {renderNotePin(mission.id, mission.title)}
                  {renderDraftMarker(mission.id)}
                  {renderRunChip(mission.id)}
                  <div className="mhd">
                    <span className="meyebrow">◆ Mission</span>
                    <button className="mrun" title="Start mission" onClick={() => setStartMission(mission)} data-testid={`run-mission-${mission.id}`}>{PLAY_SVG}</button>
                  </div>
                  {renderNodeTitle(mission.title, 'mtitle', 'Untitled mission', 'div')}
                  <div className={`mgoal${mission.goal ? '' : ' placeholder'}`}>{mission.goal || 'set the mission objective…'}</div>
                  <ReferencedFiles nodeId={mission.id} files={nodeFileRefs(board, mission.id)} max={2} onMore={openReferencedFilesMenu} className="card-refs" />
                  <div className="mstatus">{state ? state : ''}</div>
                  <span className={`port pout ready${hoverPort === `${mission.id}:out` ? ' snaptarget' : ''}`} data-port-out={`${mission.id}:out`} title="Starts the chain — drag to a step" onPointerDown={event => beginWire(event, `${mission.id}:out`, 'wire')} />
                </div>
              )
            })}

            {(board?.formations || []).map((formation, index) => {
              const pos = positionOf(formation.id, index + (board?.missions?.length || 0))
              const state = nodeStates.get(formation.id)
              return (
                <div
                  key={formation.id}
                  className={`formation type-${formation.type}${state === 'running' || state === 'blocked' || state === 'failed' ? ` ${state}` : ''}${locatedNodeId === formation.id ? ' located' : ''}${judgeHover === formation.id ? ' judgehover' : ''}${needsYouNodeIds.has(formation.id) ? ' needs-you' : ''}${noteByNode.has(formation.id) ? ' has-note' : ''}${draftClass(formation.id)}`}
                  data-node={formation.id}
                  data-testid={`formation-node-${formation.id}`}
                  style={{ left: pos.x, top: pos.y }}
                  onContextMenu={event => formationMenu(event, formation)}
                >
                  {renderNotePin(formation.id, formation.title)}
                  {renderDraftMarker(formation.id)}
                  {renderRunChip(formation.id)}
                  {formation.inputs.map((port, portIndex) => {
                    const endpoint = `${formation.id}:${port.id}`
                    const feeds = (board?.connections || []).filter(connection => connection.to === endpoint)
                    const incoming = feeds[0]
                    const feed = inputFeedLabel(feeds, nodeId => noteElements.find(element => element.id === nodeId)?.title || nodeId)
                    return (
                      <div
                        className={`fio in${portIndex === 0 ? ' brief' : ''}`}
                        key={port.id}
                        onPointerDown={event => beginNodeDrag(event, formation.id, index)}
                        onContextMenu={event => inputRowMenu(event, formation, port.id, incoming)}
                      >
                        <span
                          className={`port pin${hoverPort === endpoint ? ' snaptarget' : ''}${incoming ? ' has' : ''}`}
                          data-port-in={endpoint}
                          data-reconnect-id={incoming?.id}
                          data-reconnect-from={incoming?.from}
                          onPointerDown={incoming ? event => beginReconnect(event, incoming) : undefined}
                          onMouseDown={incoming ? event => beginReconnect(event, incoming) : undefined}
                        />
                        <span className="glyph">in</span>
                        {/* The brief reads once, under the title; this row names what feeds the input. */}
                        <span className={`io-text${feed ? '' : ' placeholder'}`} title={feed || undefined}>{feed || (portIndex === 0 ? 'wire an input…' : `${port.label} — wire an input…`)}</span>
                      </div>
                    )
                  })}
                  <div className="fhead" onPointerDown={event => beginNodeDrag(event, formation.id, index)}>
                    <div className="ft">
                      {renderNodeTitle(formation.title, 'tt', 'Untitled formation', 'div')}
                      <div className={`tg${formation.brief?.goal?.trim() ? '' : ' placeholder'}`} title={formationSummary(formation)}>{formationSummary(formation)}</div>
                      {/* Run tools get their own row so the title keeps the header's width. */}
                      {activeRun || nodeStates.has(formation.id) ? (
                        <div className="fruntools" data-testid={`run-tools-${formation.id}`}>
                          {activeRun ? <button type="button" className="fpeek" aria-label={`Peek at ${formation.title}`}
                            onPointerDown={event => event.stopPropagation()}
                            onClick={() => openPeek(formation.id)}>Peek</button> : null}
                          {nodeStates.has(formation.id) ? (
                            <button
                              type="button"
                              className="finspect"
                              title="Inspect run evidence"
                              aria-label={`Inspect run evidence for ${formation.title}`}
                              data-testid={`inspect-node-${formation.id}`}
                              onPointerDown={event => event.stopPropagation()}
                              onClick={event => { event.stopPropagation(); setInspectedNodeId(formation.id) }}
                            >evidence</button>
                          ) : null}
                        </div>
                      ) : null}
                    </div>
                    <FormationTypeChip formation={formation} onOpen={event => {
                      // Anchor to the chip so keyboard activation opens the menu beside it.
                      const rect = event.currentTarget.getBoundingClientRect()
                      setMenu({ label: 'Formation type', x: rect.left, y: rect.bottom + 4, items: formationTypeMenuItems(formation).slice(1) })
                    }} />
                    <button className="frun" title="Run formation" onClick={() => void runFormation(formation)} data-testid={`run-formation-${formation.id}`}>{PLAY_SVG}</button>
                  </div>
                  <ReferencedFiles nodeId={formation.id} files={nodeFileRefs(board, formation.id)} max={2} onMore={openReferencedFilesMenu} className="card-refs" />
                  <div className="fstatus">{state === 'running' || state === 'waiting' ? state : ''}</div>
                  <div className="fbody" onPointerDown={event => beginNodeDrag(event, formation.id, index)}>{renderBody(formation)}</div>
                  {formation.verification ? (
                    <button
                      type="button"
                      className="verify-band legacy"
                      data-gate={formation.verification.id}
                      data-testid={`verify-band-${formation.id}`}
                      aria-label={`Inspect legacy verification for ${formation.title}`}
                      onClick={event => openLegacyVerification(formation, event.currentTarget)}
                      onContextMenu={event => {
                        const trigger = event.currentTarget
                        openMenu(event, 'Legacy verification', [
                          { label: 'Migrate legacy verification', action: () => openLegacyVerification(formation, trigger) },
                        ])
                      }}
                    >
                      <span className="vico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9"><path d="M12 3l7 3v5c0 4.4-3 7.6-7 9-4-1.4-7-4.6-7-9V6z" /><path d="M12 8v5M12 16h.01" /></svg></span>
                      <span className="vlabel">legacy verify</span>
                      <span className="vkinds">{[
                        formation.verification.kinds?.length ? formation.verification.kinds.join(' · ') : 'checks not recorded',
                        formation.verification.criterion || 'criterion not recorded',
                        formation.verification.onFail || 'failure policy not recorded',
                      ].join(' · ')}</span>
                    </button>
                  ) : null}
                  <ProducedFiles nodeId={formation.id} />
                  {formation.outputs.map((port, portIndex) => {
                    const endpoint = `${formation.id}:${port.id}`
                    return (
                      <div className="fio out" key={port.id} onContextMenu={event => outputRowMenu(event, formation, port.id)}>
                        <span className="glyph">out</span>
                        {portIndex === 0 ? (() => {
                          const output = outputRowStatus(Boolean(activeRun), nodeStates.get(formation.id), outputNodeIds.has(formation.id))
                          return <span className={`io-status ${output.tone}`} data-testid={`output-status-${formation.id}`}>{output.label}</span>
                        })() : <span className="io-status idle">{port.label.toLowerCase()}</span>}
                        <span className={`port pout ready${hoverPort === endpoint ? ' snaptarget' : ''}`} data-port-out={endpoint} title="Drag to a downstream input" onPointerDown={event => beginWire(event, endpoint, 'wire')} />
                      </div>
                    )
                  })}
                </div>
              )
            })}

            {(board?.gates || []).map((gate, index) => {
              const nodeIndex = index + (board?.missions?.length || 0) + (board?.formations?.length || 0)
              const pos = positionOf(gate.id, nodeIndex)
              const state = nodeStates.get(gate.id)
              const inputEndpoint = `${gate.id}:in`
              const incoming = (board?.connections || []).find(connection => connection.to === inputEndpoint)
              return (
                <div
                  key={gate.id}
                  className={`gatecard${state ? ` ${state}` : ''}${locatedNodeId === gate.id ? ' located' : ''}${gateHasJudge(gate.id) ? ' hasjudge' : ''}${needsYouNodeIds.has(gate.id) ? ' needs-you' : ''}${noteByNode.has(gate.id) ? ' has-note' : ''}${draftClass(gate.id)}`}
                  data-node={gate.id}
                  data-gate={gate.id}
                  data-testid={`gate-node-${gate.id}`}
                  style={{ left: pos.x, top: pos.y }}
                  onPointerDown={event => beginNodeDrag(event, gate.id, nodeIndex)}
                  onContextMenu={event => gateMenu(event, gate)}
                >
                  {renderNotePin(gate.id, gate.title || 'Gate')}
                  {renderDraftMarker(gate.id)}
                  {renderRunChip(gate.id)}
                  <span
                    className={`port pin${hoverPort === inputEndpoint ? ' snaptarget' : ''}${incoming ? ' has' : ''}`}
                    data-port-in={inputEndpoint}
                    data-reconnect-id={incoming?.id}
                    data-reconnect-from={incoming?.from}
                    title="Work to check"
                    onPointerDown={incoming ? event => beginReconnect(event, incoming) : undefined}
                    onMouseDown={incoming ? event => beginReconnect(event, incoming) : undefined}
                  />
                  <button
                    type="button"
                    className={`pjudge${hoverPort === `${gate.id}:judge` ? ' snaptarget' : ''}`}
                    data-testid={`gate-judge-socket-${gate.id}`}
                    data-gate-judge-socket={gate.id}
                    title="Judge with a formation — drag to attach, click to pick"
                    onPointerDown={event => beginJudgeDrag(event, gate)}
                  />
                  <span className="gico" onPointerDown={event => beginNodeDrag(event, gate.id, nodeIndex)}>{GATE_SVG}</span>
                  <span className="gmeta" onPointerDown={event => beginNodeDrag(event, gate.id, nodeIndex)}>{renderNodeTitle(gate.title, 'gt', 'Gate', 'span')}<GateKindChips gateId={gate.id} kinds={gate.kinds} /><span className={`gs${gate.criterion ? '' : ' placeholder'}`}>{gate.criterion || 'work is accepted before it proceeds'}</span><ReferencedFiles nodeId={gate.id} files={nodeFileRefs(board, gate.id)} max={1} onMore={openReferencedFilesMenu} className="card-refs" /></span>
                  {nodeStates.has(gate.id) ? (
                    <button
                      type="button"
                      className="ginspect"
                      title="Inspect gate evidence"
                      aria-label={`Inspect gate evidence for ${gate.title || 'gate'}`}
                      data-testid={`inspect-node-${gate.id}`}
                      onPointerDown={event => event.stopPropagation()}
                      onClick={event => { event.stopPropagation(); setInspectedNodeId(gate.id) }}
                    >evidence</button>
                  ) : null}
                  <span className="glabel pass"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4"><path d="M4 12l5 5L20 6" /></svg>pass</span>
                  <span className="glabel fail"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4"><path d="M6 6l12 12M18 6L6 18" /></svg>fail</span>
                  <span className={`port pass${hoverPort === `${gate.id}:pass` ? ' snaptarget' : ''}`} data-port-out={`${gate.id}:pass`} title="On PASS → drag to the next step" onPointerDown={event => beginWire(event, `${gate.id}:pass`, 'pass')} />
                  <span className={`port fail${hoverPort === `${gate.id}:fail` ? ' snaptarget' : ''}`} data-port-out={`${gate.id}:fail`} title="On FAIL → drag to a fallback" onPointerDown={event => beginWire(event, `${gate.id}:fail`, 'fail')} />
                </div>
              )
            })}

            {(board?.tools || []).map((tool, index) => {
              const nodeIndex = index
                + (board?.missions?.length || 0)
                + (board?.formations?.length || 0)
                + (board?.gates?.length || 0)
              const pos = positionOf(tool.id, nodeIndex)
              return (
                <div
                  key={tool.id}
                  className={`toolcard${noteByNode.has(tool.id) ? ' has-note' : ''}${draftClass(tool.id)}`}
                  data-kind="tool"
                  data-node={tool.id}
                  data-execution-state="unavailable"
                  data-testid={`tool-node-${tool.id}`}
                  style={{ left: pos.x, top: pos.y }}
                  onPointerDown={event => beginNodeDrag(event, tool.id, nodeIndex)}
                >
                  {renderNotePin(tool.id, tool.title)}
                  {renderDraftMarker(tool.id)}
                  <div className="tool-head">
                    <div className="tool-heading">
                      <span className="tool-kind">Tool</span>
                      <span className="tool-title">{tool.title}</span>
                    </div>
                    <button
                      type="button"
                      className="tool-inspect"
                      aria-label={`Inspect Tool ${tool.title}`}
                      onPointerDown={event => event.stopPropagation()}
                      onClick={event => {
                        event.stopPropagation()
                        setInspectedToolId(tool.id)
                      }}
                    >
                      details
                    </button>
                  </div>
                  <div className="tool-profile">{tool.profileId}@{tool.profileVersion}</div>
                  <div className="tool-state">execution unavailable</div>
                  <div className="tool-ports inputs">
                    {(tool.inputs || []).map(port => {
                      const endpoint = `${tool.id}:${port.id}`
                      const incoming = (board?.connections || []).find(connection => connection.to === endpoint)
                      return (
                        <div className="tool-port-row input" key={port.id}>
                          <span
                            className={`port pin${hoverPort === endpoint ? ' snaptarget' : ''}${incoming ? ' has' : ''}`}
                            data-port-in={endpoint}
                            data-reconnect-id={incoming?.id}
                            data-reconnect-from={incoming?.from}
                            onPointerDown={incoming ? event => beginReconnect(event, incoming) : undefined}
                            onMouseDown={incoming ? event => beginReconnect(event, incoming) : undefined}
                          />
                          <span className="tool-port-direction">in</span>
                          <span className="tool-port-label">{port.label}</span>
                        </div>
                      )
                    })}
                  </div>
                  <div className="tool-ports outputs">
                    {(tool.outputs || []).map(port => {
                      const endpoint = `${tool.id}:${port.id}`
                      return (
                        <div className="tool-port-row output" key={port.id}>
                          <span className="tool-port-direction">out</span>
                          <span className="tool-port-label">{port.label}</span>
                          <span
                            className={`port pout ready${hoverPort === endpoint ? ' snaptarget' : ''}`}
                            data-port-out={endpoint}
                            title="Drag to a downstream input"
                            onPointerDown={event => beginWire(event, endpoint, 'wire')}
                          />
                        </div>
                      )
                    })}
                  </div>
                </div>
              )
            })}
            <NoteLayer mode={notesMode} anchors={noteAnchors} notes={nodeNotes} onOpen={openNoteWindow} />
          </div>


          {activeRun && !activeRun.final && pendingHumanGate ? (
            <HumanGateAnswerPanel
              key={`${activeRun.runId}:${pendingHumanGate.requestedSeq}`}
              runId={activeRun.runId}
              gateId={pendingHumanGate.gateId}
              requestedSeq={pendingHumanGate.requestedSeq}
              gateTitle={pendingHumanGate.title}
              criterion={pendingHumanGate.criterion}
              upstream={pendingGateUpstream}
              onDecide={(verdict, response) => recordHumanGateVerdict(pendingHumanGate.gateId, pendingHumanGate.requestedSeq, verdict, response)}
            />
          ) : null}

          {openEscalations.length ? (
            <div className="needs-you" data-testid="escalations-banner" role="alert">
              <div className="needs-you-hd">Needs you</div>
              <div className="needs-you-list">
                {openEscalations.map(escalation => (
                  <button
                    type="button"
                    className={`needs-you-item${escalation.blocks ? ' stop' : ''}`}
                    data-testid={`escalation-${escalation.seq}`}
                    key={escalation.seq}
                    title={inspectableNodeId(escalation) ? 'Open the escalated node evidence' : undefined}
                    disabled={!inspectableNodeId(escalation)}
                    onClick={() => { const id = inspectableNodeId(escalation); if (id) setInspectedNodeId(id) }}
                  >
                    <span className="needs-you-sev">{escalation.blocks ? 'stop' : (escalation.severity || 'needs-attention')}</span>
                    <span className="needs-you-ask">{escalation.reason || 'The run is asking for you.'}</span>
                    <span className="needs-you-where">{escalation.gateId || escalation.nodeId || 'run'}</span>
                  </button>
                ))}
              </div>
            </div>
          ) : null}

          <div className="zoomlevel">{Math.round(view.scale * 100)}%</div>
          <div className="zoomctl">
            <button onClick={() => zoomBy(1.2)} title="Zoom in">+</button>
            <button onClick={() => zoomBy(1 / 1.2)} title="Zoom out">−</button>
            <button onClick={() => void arrangeBoard()} title="Arrange cards by graph flow (persists layout, Ctrl+Z to undo)" data-testid="arrange-layout">ARRANGE</button>
            <button onClick={() => fitView({ smooth: true })} title="Fit">FIT</button>
          </div>
          {error || linkError ? <div className="errbar" data-testid="formations-error">{error || linkError}</div> : null}
          <AdmissionFindingsPanel
            findings={admissionFindings}
            titleOf={nodeId => noteElements.find(element => element.id === nodeId)?.title || nodeId}
            onDismiss={() => setAdmissionFindings([])}
          />
        </div>
        </div>

      </div>

      <WindowManagerProvider stack={windows}>
        {board ? noteWindows.map(target => (
          <NoteWindow
            key={target}
            target={target}
            title={noteTitleOf(target)}
            anchor={target === BOARD_NOTE_TARGET ? undefined : () => noteWindowAnchor(worldRef.current, target)}
            entries={(target === BOARD_NOTE_TARGET ? notes?.board : noteByNode.get(target)) || []}
            draft={noteDrafts[target] || ''}
            editingEntryId={noteEditing[target]}
            saving={noteSaving !== ''}
            error={noteErrors[target] || ''}
            conflict={notesConflict}
            onDraft={text => setNoteDraft(target, text)}
            onSave={() => saveNote(target)}
            onCancelEdit={() => cancelNoteEdit(target)}
            onEdit={entry => startNoteEdit(target, entry)}
            onDelete={entry => deleteNote(target, entry)}
            onReload={() => void reloadSharedNotes()}
            onClose={() => setNoteWindows(current => current.filter(open => open !== target))}
          />
        )) : null}
        {board ? nodeWindows.map(nodeId => (
          <Suspense key={`node-${nodeId}`} fallback={null}>
            <NodeWindow nodeId={nodeId} board={board} agents={agents} profiles={gateProfiles} ops={nodeWindowOps} noteCount={noteByNode.get(nodeId)?.length || 0}
              runState={nodeStates.has(nodeId) ? nodeStates.get(nodeId) || '' : undefined}
              onClose={() => setNodeWindows(current => current.filter(open => open !== nodeId))} />
          </Suspense>
        )) : null}
        {activeRun ? peeks.map(nodeId => (
          <Suspense key={`${activeRun.runId}-${nodeId}`} fallback={<div role="status">Loading terminal…</div>}>
            <FloatingPeek windowId={`peek:${nodeId}`} runId={activeRun.runId} initialNodeId={nodeId || undefined}
              onClose={() => setPeeks(current => current.filter(open => open !== nodeId))} />
          </Suspense>
        )) : null}
        <FileWindowsLayer />
      </WindowManagerProvider>

      {agentEditor ? (
        <PersonaEditorDialog
          key={agentEditor.agent.id}
          agent={agentEditor.agent}
          returnFocus={agentEditor.trigger}
          onClose={() => setAgentEditor(null)}
          onSaved={async () => setAgents(await fetchAgents())}
        />
      ) : null}

      {startMission && <StartMissionDialog title={startMission.title} beadId={startMission.beadId} inputHint={startMission.inputHint} onStart={inputs => runMission(startMission, inputs)} onClose={() => setStartMission(null)} />}

      {boardDialog ? (
        <div
          className="pop board-dialog"
          role="dialog"
          aria-modal="true"
          aria-label={boardDialog.mode === 'create' ? 'Create board' : boardDialog.mode === 'rename' ? 'Rename board' : 'Delete board'}
          onPointerDown={event => event.stopPropagation()}
        >
          <div className="pop-head">
            <span className="pt">{boardDialog.mode === 'create' ? 'Create board' : boardDialog.mode === 'rename' ? 'Rename board' : 'Delete board'}</span>
            <button className="x" type="button" aria-label="Close board dialog" disabled={boardDialog.saving} onClick={closeBoardDialog}>x</button>
          </div>
          {boardDialog.mode === 'delete' ? (
            <div className="pop-body">
              <p className="board-delete-warning">
                <strong>{boardDialog.title}</strong> will be removed from the live board list. Its definition and layout are archived, not destroyed; run history is untouched.
              </p>
              {boardDialog.error ? <p className="field-note error">{boardDialog.error}</p> : null}
              <div className="pop-actions">
                <button autoFocus className="cancel" type="button" disabled={boardDialog.saving} onClick={closeBoardDialog}>Cancel</button>
                <button className="retire" type="button" disabled={boardDialog.saving} onClick={() => void archiveSelectedBoard()}>
                  {boardDialog.saving ? 'Archiving…' : 'Archive board'}
                </button>
              </div>
            </div>
          ) : (
            <form
              className="pop-body"
              onSubmit={event => {
                event.preventDefault()
                void saveBoardName()
              }}
            >
              <label htmlFor="formations-board-name">Board name</label>
              <input
                id="formations-board-name"
                className="f"
                autoFocus
                value={boardDialog.title}
                aria-invalid={boardDialog.error ? 'true' : undefined}
                disabled={boardDialog.saving}
                onChange={event => setBoardDialog(current => current ? { ...current, title: event.target.value, error: '' } : current)}
              />
              <p className="field-note">The board slug is derived from this name and remains stable after renaming.</p>
              {boardDialog.error ? <p className="field-note error">{boardDialog.error}</p> : null}
              <div className="pop-actions">
                <button className="cancel" type="button" disabled={boardDialog.saving} onClick={closeBoardDialog}>Cancel</button>
                <button className="save" type="submit" disabled={boardDialog.saving}>
                  {boardDialog.saving ? 'Saving…' : boardDialog.mode === 'create' ? 'Create board' : 'Save board name'}
                </button>
              </div>
            </form>
          )}
        </div>
      ) : null}

      {inspectedTool ? (
        <div
          className="pop tool-inspector"
          role="dialog"
          aria-label={`Tool details: ${inspectedTool.title}`}
          onPointerDown={event => event.stopPropagation()}
        >
          <div className="pop-head">
            <span className="pt">{inspectedTool.title}</span>
            <button
              className="x"
              type="button"
              aria-label="Close Tool details"
              onClick={() => setInspectedToolId(null)}
            >
              x
            </button>
          </div>
          <div className="pop-body tool-details">
            <div className="tool-detail-state">execution unavailable</div>
            <dl className="tool-detail-identity">
              <div><dt>Node</dt><dd>{inspectedTool.id}</dd></div>
              <div><dt>Profile</dt><dd>{inspectedTool.profileId}@{inspectedTool.profileVersion}</dd></div>
            </dl>

            <section className="tool-detail-section">
              <h3>Parameters</h3>
              <div className="tool-detail-list">
                {Object.entries(inspectedTool.params || {})
                  .sort(([left], [right]) => left.localeCompare(right))
                  .map(([name, value]) => (
                    <div className="tool-detail-row" data-testid={`tool-parameter-${name}`} key={name}>
                      <span>{name}</span>
                      <span>{typeof value === 'number' ? 'integer' : typeof value}</span>
                      <strong>{String(value)}</strong>
                    </div>
                  ))}
              </div>
            </section>

            <section className="tool-detail-section">
              <h3>Inputs</h3>
              <div className="tool-detail-list">
                {(inspectedTool.inputs || []).map(port => (
                  <div
                    className="tool-detail-port"
                    data-direction="input"
                    data-testid={`tool-port-${port.id}`}
                    key={port.id}
                  >
                    <code>{port.id}</code>
                    <strong>{port.name}</strong>
                    <span>{port.label}</span>
                    <span>{port.kind}</span>
                    <span>{(port.acceptedMediaTypes || []).length ? port.acceptedMediaTypes.join(' · ') : 'media not declared'}</span>
                    <span>required={port.required === undefined ? 'unset' : String(port.required)}</span>
                    <span>role={port.role ?? 'unset'}</span>
                  </div>
                ))}
              </div>
            </section>

            <section className="tool-detail-section">
              <h3>Outputs</h3>
              <div className="tool-detail-list">
                {(inspectedTool.outputs || []).map(port => (
                  <div
                    className="tool-detail-port"
                    data-direction="output"
                    data-testid={`tool-port-${port.id}`}
                    key={port.id}
                  >
                    <code>{port.id}</code>
                    <strong>{port.name}</strong>
                    <span>{port.label}</span>
                    <span>{port.kind}</span>
                    <span>{(port.acceptedMediaTypes || []).length ? port.acceptedMediaTypes.join(' · ') : 'media not declared'}</span>
                  </div>
                ))}
              </div>
            </section>
          </div>
        </div>
      ) : null}

      {inspectedNode && activeRun ? (
        <Suspense fallback={<div role="status">Loading run evidence…</div>}>
          <RunEvidence runId={activeRun.runId} nodeId={inspectedNode.id} title={inspectedNode.title}
            state={nodeStates.get(inspectedNode.id) || ''} board={board} onClose={() => setInspectedNodeId(null)} />
        </Suspense>
      ) : null}

      {gateEditor ? (
        <GateEditorDialog
          initial={gateEditor.initial}
          profiles={gateProfiles}
          saving={gateEditor.saving}
          onSave={draft => void saveGateEditor(draft)}
          onClose={closeGateEditor}
        />
      ) : null}

      {missionEditor ? (
        <MissionEditorDialog
          initial={missionEditor.initial}
          saving={missionEditorSaving}
          onSave={draft => void saveMissionEditor(draft)}
          onClose={closeMissionEditor}
        />
      ) : null}

      {legacyVerification ? (
        <div
          className="pop"
          role="dialog"
          aria-modal="true"
          aria-label={`Legacy verification · ${legacyVerification.title}`}
          aria-describedby="legacy-verification-warning"
          aria-busy={legacyVerification.pending}
          onPointerDown={event => event.stopPropagation()}
        >
          <div className="pop-head">
            <span className="pt">Legacy verification · {legacyVerification.title}</span>
            <button
              ref={legacyVerificationInitialFocusRef}
              className="x"
              type="button"
              aria-label="Close legacy verification"
              disabled={legacyVerification.pending}
              onClick={closeLegacyVerification}
            >x</button>
          </div>
          <div className="pop-body">
            <label>Checks</label>
            <div className="legacy-value">{legacyVerification.kinds.length ? legacyVerification.kinds.join(' · ') : 'No checks recorded'}</div>
            <label>Criterion</label>
            <div className="legacy-value criterion">{legacyVerification.criterion || 'No criterion recorded'}</div>
            <label>Legacy failure policy</label>
            <div className="legacy-value">{legacyVerification.onFail || 'No failure policy recorded'}</div>
            <p id="legacy-verification-warning" className="legacy-note">Inline verification is retired because its verdict cannot be tied safely to an exact Formation attempt and output. Create and wire an explicit Gate to make the check, result, and route visible, then remove this legacy block.</p>
            {legacyVerification.replacementGateIds.length ? (
              <>
                <label htmlFor="legacy-replacement-gate">Replacement Gate</label>
                <select
                  id="legacy-replacement-gate"
                  className="legacy-select"
                  value={legacyVerification.replacementGateId}
                  disabled={legacyVerification.pending}
                  onChange={event => setLegacyVerification(current => current ? { ...current, replacementGateId: event.target.value, error: '' } : null)}
                >
                  <option value="" disabled>Choose a wired Gate…</option>
                  {legacyVerification.replacementGateIds.map(gateId => (
                    <option key={gateId} value={gateId}>{board?.gates?.find(gate => gate.id === gateId)?.title || gateId}</option>
                  ))}
                </select>
              </>
            ) : (
              <p className="legacy-missing-gate">Wire an explicit Gate from a Formation output before removal.</p>
            )}
            {legacyVerification.pending ? <div className="legacy-note" role="status">Removing legacy verification…</div> : null}
            {legacyVerification.error ? <div className="legacy-missing-gate" role="alert">{legacyVerification.error}</div> : null}
            {runEvents.some(event => event.type === 'verification_verdict' && event.nodeId === legacyVerification.formationId) ? (
              <div className="legacy-evidence">
                <div className="legacy-evidence-title">Legacy verification evidence · non-authorizing</div>
                {runEvents
                  .filter(event => event.type === 'verification_verdict' && event.nodeId === legacyVerification.formationId)
                  .map(event => <div key={event.seq}>seq {event.seq} · {typeof event.data?.verdict === 'string' ? event.data.verdict : 'verdict not recorded'}</div>)}
              </div>
            ) : null}
            <div className="pop-actions">
              <button className="cancel" type="button" disabled={legacyVerification.pending} onClick={closeLegacyVerification}>Keep for inspection</button>
              <button
                className="retire"
                type="button"
                disabled={legacyVerification.pending || !legacyVerification.replacementGateId}
                onClick={() => void removeLegacyVerification()}
              >
                Remove legacy verification
              </button>
            </div>
          </div>
        </div>
      ) : null}

      {menu ? (
        <DismissiblePanel onDismiss={closeMenu} panelPosition="fixed">
          <div
            className="formations-context-menu ctxmenu"
            role="menu"
            aria-label={menu.label}
            style={{ left: Math.min(menu.x, window.innerWidth - 220), top: Math.min(menu.y, window.innerHeight - 80) }}
            onPointerDown={event => event.stopPropagation()}
          >
            <div className="mhead">{menu.label}</div>
            {menu.items.map((item, itemIndex) => item.head ? (
              <div key={`${item.label}-${itemIndex}`} className="msection">{item.label}</div>
            ) : (
              <button
                key={`${item.label}-${itemIndex}`}
                type="button"
                role="menuitem"
                disabled={item.disabled}
                className={item.destructive ? 'danger' : undefined}
                onClick={() => {
                  closeMenu()
                  item.action?.()
                }}
              >
                {item.label}
              </button>
            ))}
          </div>
        </DismissiblePanel>
      ) : null}

      {ghost ? (
        <div className="fmx-ghost" style={{ left: ghost.x, top: ghost.y }}>{harnessGlyph(ghost.harness) ?? initials(ghost.agentId)}</div>
      ) : null}
      {gateGhost ? (
        <div className="gateghost" style={{ left: gateGhost.x, top: gateGhost.y }}>{GATE_SVG}</div>
      ) : null}
    </div>
  )
  return (
    <FileWindowsProvider stack={windows}>
      <RunProducedProvider value={producedValue}>{cockpit}</RunProducedProvider>
    </FileWindowsProvider>
  )
}
