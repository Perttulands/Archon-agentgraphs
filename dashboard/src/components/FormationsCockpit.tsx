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
  fetchAgentRoster,
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
  projectNodeAttempts,
  projectNodeStates,
  runCurrentPoint,
  runStatusFromResponse,
  upsertRunEvent,
} from './formationsRunState'
import { chooseBoardRun, readRunLink, runChoiceLabel, runChoices, runLinkSearch, runStatusLabel } from './formationsRunDiscovery'
import { chooseCurrentBoard, rememberBoardOnDevice } from './currentBoard'
import { END_ROOM, clampScale, displayLayoutFor, fallbackNodePosition, freeGridPosition, snapToGrid, zoomTransform } from './formationsCanvas'
import { END_SVG, FormationSeats, GATE_SVG, PLAY_SVG, formationSummary, agentRole, agentState, byRoleName, inSlotsWords, initials, inputFeedLabel, outputRowStatus, roleUses, rolesInUseLabel } from './formationsCockpitVisuals'
import { useEscapeKey } from './useEscapeKey'
import { ROSTER_MAX_WIDTH, ROSTER_MIN_WIDTH, useRosterPanel } from './useRosterPanel'
const FloatingPeek = lazy(() => import('../terminal/FloatingPeek'))
const RunEvidence = lazy(() => import('../evidence/RunEvidence'))
const NodeWindow = lazy(() => import('../nodeWindow/NodeWindow'))
const FlowView = lazy(() => import('../flow/FlowView'))
import CanvasContextMenu, { type MenuItem, type MenuState } from './CanvasContextMenu'
import PersonaEditorDialog from './PersonaEditorDialog'
import HumanGateAnswerPanel, { type GateDecision } from './HumanGateAnswerPanel'
import RunPoint from './RunPoint'
import RunBarActions from './RunBarActions'
import GateAnswerWindow, { GATE_ANSWER_WINDOW_ID, cardRects } from './GateAnswerWindow'
import { nodeTitle } from '../nodeWindow/boardRoutes'
import { slotStaffed } from '../nodeWindow/staffing'
import { CanvasSlot } from '../staffing/CanvasSlot'
import type { Part } from '../staffing/SlotFace'
import { StaffingKeyHint, StaffingLayer } from '../staffing/StaffingLayer'
import { dropRole, moveStaffing, previewRole, staff, type StaffingHost } from '../staffing/staffingActions'
import { captionText, roleName, rolesOf, sameStaffing, slotSettings, staffingOf, type Staffing, type StaffingCatalog } from '../staffing/staffingModel'
import { StaffingStore, slotKey, type SlotRef } from '../staffing/staffingStore'
import { buildFlow } from '../flow/flowModel'
import CanvasLegend from './CanvasLegend'
import { FileWindowsLayer, FileWindowsProvider } from '../files/FileWindows'
import { ProducedFiles, RunProduced, RunProducedProvider } from '../files/ProducedFiles'
import { ReferencedFiles, type HiddenReferencedFile } from '../files/ReferencedFiles'
import { nodeFileRefs } from '../files/referencedFiles'
import { producedNames, summarizeProduced, useRunProduced } from '../files/produced'
import { useHumanGateUpstream } from './useHumanGateUpstream'
import { connectionKind, findInputPortAt, findOutputPortAt, isTextEditingTarget, laneYFrom } from './formationsCockpitDom'
import { gateWireNeedsLabel, loopConnectionIds, routeJudgeWire, routeLoopWires, routeOrthoWire, type LoopWire } from './formationsRouting'
import type { ObstacleRect } from './formationsRouting'
import { findAddedByID } from './formationsBoardModel'
import { defaultEndTitle, endOutcomeMeaning } from './endNode'
import { UndoHistory, WriteTracker, boardStep, combineUndo, nodeDeleteUndo, portRemoveUndo, quoted, restoreBlocker, undoOutcomeMessage, type UndoDraft, type UndoStep } from './formationsUndo'
import { NOTE_CARDS, NoteLayer, NOTES_MODES, noteWindowAnchor, readNotesMode, sameNoteAnchors, writeNotesMode, type NoteAnchor, type NotesMode } from './NoteLayer'
import NoteWindow, { BOARD_NOTE_TARGET, noteWindowId } from './NoteWindow'
import RoleWindow, { roleWindowId } from './RoleWindow'
import { FormationTypeChip, formationTypeChoices } from './FormationTypeChip'
import { MissionEditorDialog } from './MissionEditorDialog'
import type { MissionDraft } from './MissionEditorDialog'
import { AdmissionFindingsPanel, DraftMarker, findingsByNode, unresolvedFindings } from './formationsDrafts'
import { GateEditorDialog, GateKindChips, gateFieldsFromDraft, gateFieldsFromGate, gateKindLabel, newGateDraft } from './GateEditorDialog'
import type { GateDraft } from './GateEditorDialog'
import { createFormationsInteractionOwner } from './formationsInteraction'
import type { FormationsInteractionOwner } from './formationsInteraction'
import { WindowManagerProvider, useWindowManager } from '../windows/WindowManager'
import type { NodeWindowOps } from '../nodeWindow/NodeWindow'
import { readBoardView, writeBoardView, type BoardView } from '../flow/boardView'
import type { FlowRun } from '../flow/FlowView'
import { cockpitWorkspace } from '../windows/cockpitWorkspace'
import { cockpitScene, measureElement, nodeAnchor, nodeWindowKeepClear } from '../windows/cockpitScene'
import { humanChannelField, humanChannelLabel, humanChannelOf, type HumanChannel } from '../humanChannel/humanChannel'
import { useGateTalk } from '../talk/useGateTalk'
import type { WindowRect } from '../windows/windowGeometry'
import type {
  AgentProjection,
  BoardConnection,
  BoardDocument,
  BoardFinding,
  BoardNotesDocument,
  BoardSummary,
  BoardValidation,
  CodeGateProfileDescriptor,
  EffortPolicyEntry,
  EndNode,
  EndOutcome,
  FormationBrief,
  FormationNode,
  FormationPortDirection,
  FormationSlot,
  FormationType,
  GateNode,
  LaunchableHarness,
  LayoutDocument,
  LayoutEdge,
  LayoutNode,
  MissionNode,
  NoteEntry,
  NotePatch,
  OpenEscalation,
  RunEvent,
  RunStatusProjection,
  ViewTransform,
} from './formationsTypes'




/** A staffing drag: a role from the rail, or a slot's staffing; a press without a move on a slot opens its sentence. */
type StaffPayload = { kind: 'role'; roleId: string } | { kind: 'slot'; from: SlotRef }
type DragStaff = { payload: StaffPayload | null; slot?: { ref: SlotRef; part: Part | null; anchor: HTMLElement }; click?: () => void; startX: number; startY: number; moved: boolean }
/** The slot under the pointer, looking through notes, ghosts and anything else on top. */
function slotKeyAt(x: number, y: number): string | null {
  for (const element of document.elementsFromPoint?.(x, y) || [document.elementFromPoint(x, y)].filter(Boolean) as Element[]) {
    const slot = element.closest<HTMLElement>('.world .slot[data-slot-key]')
    if (slot) return slot.dataset.slotKey || null
  }
  return null
}
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
// Board notes open near their button in the toolbar, in free space over the canvas.
function boardNotesAnchor(): WindowRect | null {
  const button = document.querySelector('.board-notes-button')
  return button ? measureElement(button) : null
}

export default function FormationsCockpit({ active = true }: { active?: boolean } = {}) {
  const [boards, setBoards] = useState<BoardSummary[]>([])
  const [selectedSlug, setSelectedSlug] = useState('')
  // Canvas or Flow, remembered per board.
  const [boardView, setBoardView] = useState<BoardView>('canvas')
  useEffect(() => setBoardView(readBoardView(selectedSlug)), [selectedSlug])
  const changeBoardView = useCallback((view: BoardView) => {
    setBoardView(view)
    writeBoardView(selectedSlug, view)
  }, [selectedSlug])
  const [board, setBoard] = useState<BoardDocument | null>(null)
  const [layout, setLayout] = useState<LayoutDocument | null>(null)
  const [agents, setAgents] = useState<AgentProjection[]>([])
  // The harnesses with their models and the effort policy, as the roster serves them.
  const [staffingTerms, setStaffingTerms] = useState<{ harnesses: LaunchableHarness[]; policy: EffortPolicyEntry[] }>({ harnesses: [], policy: [] })
  const [rosterSearch, setRosterSearch] = useState('')
  const [view, setView] = useState<ViewTransform>({ x: 40, y: 40, scale: 1 })
  const [error, setError] = useState('')
  const [validation, setValidation] = useState<BoardValidation | null>(null)
  const [admissionFindings, setAdmissionFindings] = useState<BoardFinding[]>([])
  const [activeRun, setActiveRun] = useState<RunStatusProjection | null>(null)
  // A link's ?mission=&run= and the run picker pin a run to its mission.
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
  // The gate answer window, per pending request: closed by the operator, or reopened from the run bar with focus.
  const [answerWindow, setAnswerWindow] = useState<{ key: string; closed: boolean; focus: number }>({ key: '', closed: false, focus: 0 })
  // The card the run bar's phrase last located, marked briefly on the canvas.
  const [locatedNodeId, setLocatedNodeId] = useState('')
  // Missions, formations and gates open in node windows, oldest first.
  const [nodeWindows, setNodeWindows] = useState<string[]>([])
  const [ghost, setGhost] = useState<{ x: number; y: number; label: string } | null>(null)
  const [hoverSlot, setHoverSlot] = useState<string | null>(null)
  const [dragPos, setDragPos] = useState<{ id: string; x: number; y: number } | null>(null)
  const [wires, setWires] = useState<WirePath[]>([])
  const [geometryTick, setGeometryTick] = useState(0)
  const [tempWire, setTempWire] = useState<{ ax: number; ay: number; bx: number; by: number; kind: WirePath['kind']; moving: 'a' | 'b' } | null>(null)
  const [hoverPort, setHoverPort] = useState<string | null>(null)
  const [gateGhost, setGateGhost] = useState<{ x: number; y: number } | null>(null)
  const [endGhost, setEndGhost] = useState<{ x: number; y: number } | null>(null)
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
  const windows = useWindowManager(() => cockpitWorkspace(viewportRef.current), () => cockpitScene(viewportRef.current || document))
  const { focus: focusWindow, reflow: reflowWindows } = windows
  // The canvas changes size when the run bar appears or the roster collapses;
  // open windows move back inside it, so none is left over the run bar.
  useEffect(() => {
    const canvas = viewportRef.current
    if (!canvas || typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(() => reflowWindows())
    observer.observe(canvas)
    return () => observer.disconnect()
  }, [reflowWindows])
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
  // Roles opened from the rail, each in its own window beside the row it opened from.
  const [roleWindows, setRoleWindows] = useState<string[]>([])
  const roleWindowAnchors = useRef(new Map<string, WindowRect>())
  const openRoleWindow = useCallback((roleId: string, row: Element) => {
    const rect = measureElement(row)
    if (rect) roleWindowAnchors.current.set(roleId, rect)
    setRoleWindows(current => current.includes(roleId) ? current : [...current, roleId])
    focusWindow(roleWindowId(roleId))
  }, [focusWindow])
  // Where a node window opens beside, when the control that opened it says: a Flow title or route link.
  const nodeWindowAnchors = useRef(new Map<string, WindowRect>())
  const openNodeWindow = useCallback((nodeId: string, anchor?: WindowRect) => {
    if (anchor) nodeWindowAnchors.current.set(nodeId, anchor)
    else nodeWindowAnchors.current.delete(nodeId)
    setNodeWindows(current => current.includes(nodeId) ? current : [...current, nodeId])
    focusWindow(`node:${nodeId}`)
  }, [focusWindow])
  const worldRef = useRef<HTMLDivElement | null>(null)
  const viewRef = useRef<ViewTransform>(view)
  const interactionOwnerRef = useRef<FormationsInteractionOwner | null>(null)
  if (!interactionOwnerRef.current) interactionOwnerRef.current = createFormationsInteractionOwner()
  const interactionOwner = interactionOwnerRef.current
  // Every canvas edit that changes the board records one undo entry, after it succeeds.
  const undoHistoryRef = useRef<UndoHistory | null>(null)
  if (!undoHistoryRef.current) undoHistoryRef.current = new UndoHistory()
  const undoHistory = undoHistoryRef.current
  // Board and layout writes in flight; undo waits for them.
  const writesRef = useRef<WriteTracker | null>(null)
  if (!writesRef.current) writesRef.current = new WriteTracker()
  const writes = writesRef.current
  // The board the latest write went to. An entry is stamped with it, so an edit
  // that finishes after a board switch cannot put its undo on the new board.
  const lastWriteBoardRef = useRef('')
  const recordEntry = useCallback((draft: UndoDraft | null | undefined) => {
    if (draft) undoHistory.record({ ...draft, board: lastWriteBoardRef.current })
  }, [undoHistory])
  const recordUndo = useCallback((label: string, ...steps: UndoStep[]) => recordEntry({ label, steps }), [recordEntry])
  // Undo belongs to the board it was recorded on.
  useEffect(() => undoHistory.setBoard(selectedSlug), [selectedSlug, undoHistory])
  const fittedBoardRef = useRef<string | null>(null)
  const judgeHoverRef = useRef<string | null>(null)
  const openJudgePickerRef = useRef<((gate: GateNode, x: number, y: number) => void) | null>(null)
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
    setInspectedToolId(null)
    setInspectedNodeId(null)
    setEscalations([])
    setValidation(null)
    setAdmissionFindings([])
  }, [selectedSlug])

  // ----- data loading -----
  useEffect(() => {
    let cancelled = false
    fetchBoardSummaries()
      .then(list => {
        if (cancelled) return
        setBoards(list)
        if (list[0]) {
          const { slug, missingLinked } = chooseCurrentBoard(list.map(item => item.slug), window.location.search)
          if (missingLinked) {
            setLinkError(`Mission "${missingLinked}" from the link was not found`)
            setPinnedRun({ slug: '', runId: '' })
          }
          setSelectedSlug(current => current || slug)
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
      .catch(err => !cancelled && setError(err instanceof Error ? err.message : 'Failed to load missions'))
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
      .catch(err => !cancelled && setError(err instanceof Error ? err.message : 'Failed to load mission'))
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
        if (!cancelled) setNoteErrors(current => ({ ...current, [BOARD_NOTE_TARGET]: err instanceof Error ? err.message : 'Failed to load mission notes' }))
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
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to check mission changes')
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
    const load = () => fetchAgentRoster().then(roster => {
      if (cancelled) return
      setAgents(roster.agents)
      setStaffingTerms(current => JSON.stringify(current) === JSON.stringify({ harnesses: roster.harnesses, policy: roster.effortPolicy })
        ? current : { harnesses: roster.harnesses, policy: roster.effortPolicy })
    }).catch(() => undefined)
    load()
    const timer = window.setInterval(load, 8000)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [active])

  // ----- run discovery -----
  // The daemon lists every run of the board, so runs started outside this
  // browser appear. A run this browser started earlier is the last fallback.
  useEffect(() => {
    if (!selectedSlug) return
    if (activeRunRef.current?.missionSlug && activeRunRef.current.missionSlug !== selectedSlug) {
      setActiveRun(null)
      setRunEvents([])
    }
    setBoardRuns(runs => (runs.some(run => run.missionSlug !== selectedSlug) ? [] : runs))
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
        if (status.missionSlug && status.missionSlug !== selectedSlug) {
          if (runId === pinnedRunId) dropPin(`Run ${runId} belongs to mission "${status.missionSlug}", not "${selectedSlug}"`)
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
    rememberBoardOnDevice(selectedSlug)
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

  const placementForNewNode = useCallback((x: number, y: number, room?: { width: number; height: number }) => (
    freeGridPosition({ x, y }, [...displayLayoutByNode.values()], room)
  ), [displayLayoutByNode])

  const nodeStates = useMemo(() => projectNodeStates(runEvents, activeRun), [runEvents, activeRun])
  const runPoint = useMemo(() => runCurrentPoint(runEvents, activeRun), [runEvents, activeRun])
  const outputNodeIds = useMemo(() => new Set(runEvents.filter(event => event.type === 'node_output' && event.nodeId).map(event => event.nodeId)), [runEvents])
  // What the run's steps produced, for their cards, node windows and the run bar.
  const runProduced = useRunProduced(activeRun?.runId || '', runEvents, Boolean(activeRun?.final))
  const producedValue = useMemo(() => activeRun ? {
    runId: activeRun.runId,
    byNode: new Map(runProduced.produced.map(step => [step.nodeId, step])),
    summary: summarizeProduced(runProduced.produced, runProduced.artifacts, activeRun.final),
    names: producedNames(runProduced.produced),
  } : null, [activeRun, runProduced])
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
    const titleOf = (nodeId: string) => [...(board.inputCards || []), ...board.formations, ...(board.gates || []), ...(board.tools || []), ...(board.ends || [])]
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
  // Writes one board edit and adopts the result; a failure throws.
  const applyBoardPatch = useCallback(async (patch: Record<string, unknown>): Promise<{ board: BoardDocument; layout: LayoutDocument | null }> => {
    const current = boardRef.current
    if (!current) throw new Error('No mission is open')
    const result = await writes.track(patchBoardDocument(current.slug, current.etag, current.rev, patch))
    lastWriteBoardRef.current = current.slug
    // A write that finishes after the operator switched boards must not replace the new board.
    if (boardRef.current?.slug !== current.slug) return { board: result.board, layout: result.layout ?? null }
    boardRef.current = result.board
    setBoard(result.board)
    if (result.layout) {
      layoutRef.current = result.layout
      setLayout(result.layout)
    }
    return { board: result.board, layout: result.layout ?? null }
  }, [writes])

  const patchBoard = useCallback(async (patch: Record<string, unknown>): Promise<{ board: BoardDocument; layout: LayoutDocument | null } | null> => {
    try {
      const result = await applyBoardPatch(patch)
      setError('')
      return result
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Update failed')
      return null
    }
  }, [applyBoardPatch])

  const blockBoardExitForDirtyNotes = useCallback(() => {
    const dirty = Object.keys(noteDraftsRef.current).find(target => noteDraftsRef.current[target])
    if (!dirty) return false
    openNoteWindow(dirty)
    setNoteErrors(current => ({ ...current, [dirty]: 'Save the current notes before leaving this mission.' }))
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
    // A blank name saves too: the server names new missions "Untitled mission".
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
        if (!target) throw new Error('Mission target is missing; close and retry')
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
        error: err instanceof Error ? err.message : 'Mission update failed',
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
        error: err instanceof Error ? err.message : 'Mission deletion failed',
      } : dialog)
    }
  }, [blockBoardExitForDirtyNotes, boardDialog, closeBoardDialog])

  // Writes one layout edit and adopts the result; a failure throws.
  const applyLayoutPatch = useCallback(async (patch: { nodes?: LayoutNode[]; edges?: LayoutEdge[]; arrange?: boolean }) => {
    const currentBoard = boardRef.current
    const currentLayout = layoutRef.current
    if (!currentBoard || !currentLayout) throw new Error('No mission layout is open')
    const next = await writes.track(patchBoardLayout(currentBoard.slug, currentLayout.etag, patch))
    lastWriteBoardRef.current = currentBoard.slug
    if (boardRef.current?.slug !== currentBoard.slug) return next
    layoutRef.current = next
    setLayout(next)
    return next
  }, [writes])

  const patchLayoutEdge = useCallback(async (edgeId: string, lane: string): Promise<boolean> => {
    try {
      await applyLayoutPatch({ edges: [{ id: edgeId, lane }] })
      setError('')
      return true
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update wire routing')
      return false
    }
  }, [applyLayoutPatch])

  // A routing change is one undo entry that restores the wire's previous lane.
  const setWireLane = useCallback(async (edgeId: string, lane: string, label: string) => {
    const previous = layoutRef.current?.edges?.find(edge => edge.id === edgeId)?.lane || 'auto'
    if (previous === lane) return true
    if (!await patchLayoutEdge(edgeId, lane)) return false
    recordUndo(label, { layout: { edges: [{ id: edgeId, lane: previous }] } })
    return true
  }, [patchLayoutEdge, recordUndo])

  const persistPositions = useCallback(async (nodes: { id: string; x: number; y: number }[]): Promise<boolean> => {
    if (!nodes.length) return false
    try {
      await applyLayoutPatch({ nodes })
      setError('')
      return true
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save layout')
      return false
    }
  }, [applyLayoutPatch])

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
    recordUndo('the brief edit', boardStep(previous
      ? { setBrief: { formationId, goal: previous.goal || '', beadId: previous.beadId || '', files: previous.files || [], links: previous.links || [] } }
      : { clearBrief: { formationId } }))
    return true
  }, [patchBoard, recordUndo])

  const saveExecution = useCallback(async (formationId: string, timeoutSeconds: number): Promise<boolean> => {
    const previous = boardRef.current?.formations.find(formation => formation.id === formationId)?.execution?.timeoutSeconds || 0
    const result = await patchBoard({ setExecution: { formationId, timeoutSeconds } })
    if (!result) return false
    recordUndo('the execution duration edit', boardStep({ setExecution: { formationId, timeoutSeconds: previous } }))
    return true
  }, [patchBoard, recordUndo])

  // Undo waits for edits in flight, then runs the newest entry. A stale
  // revision (another editor, or the poll not yet caught up) reloads the board
  // and retries once; only an undo the board truly refuses is dropped.
  const undoRunner = useMemo(() => ({
    board: () => boardRef.current?.slug || '',
    idle: () => writes.idle(),
    apply: async (step: UndoStep) => {
      if ('board' in step) await applyBoardPatch(step.board)
      else await applyLayoutPatch(step.layout)
    },
    isConflict: (err: unknown) => err instanceof ApiRequestError && err.code === 'CONFLICT',
    reload: async () => {
      const slug = boardRef.current?.slug
      if (!slug) return
      const next = await fetchBoardWithLayout(slug)
      if (boardRef.current?.slug !== slug) return
      boardRef.current = next.board
      layoutRef.current = next.layout
      setBoard(next.board)
      setLayout(next.layout)
    },
  }), [applyBoardPatch, applyLayoutPatch, writes])

  const performUndo = useCallback(async () => {
    const outcome = await undoHistory.undo(undoRunner)
    if (outcome.status === 'failed' || outcome.status === 'busy') setError(undoOutcomeMessage(outcome))
    else if (outcome.status === 'undone') setError('')
  }, [undoHistory, undoRunner])

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

  const wire = useCallback(async (from: string, to: string) => {
    if (!from || !to || from.split(':')[0] === to.split(':')[0]) return
    const previous = boardRef.current
    if (!previous) return
    const result = await patchBoard({ wireConnection: { from, to, joinIfOccupied: true } })
    if (!result) return
    const added = result.board.connections.find(edge => edge.from === from && !previous.connections.some(old => old.id === edge.id))
    if (!added) return
    if (added.to !== to) {
      const [formationId, portId] = added.to.split(':')
      recordUndo('the join', boardStep({ removePort: { formationId, portId } }))
    } else {
      recordUndo('the connection', boardStep({ unwireConnection: { from, to } }))
    }
  }, [patchBoard, recordUndo])

  const rewireTarget = useCallback(async (connection: BoardConnection, to: string) => {
    if (!to || connection.to === to || connection.from.split(':')[0] === to.split(':')[0]) return
    const previous = boardRef.current
    if (!previous) return
    const result = await patchBoard({ rewireConnection: { from: connection.from, previousTo: connection.to, to, joinIfOccupied: true } })
    if (!result) return
    const added = result.board.connections.find(edge => edge.from === connection.from && !previous.connections.some(old => old.id === edge.id))
    if (!added) return
    recordUndo('the reconnection', boardStep({ rewireConnection: { from: connection.from, previousTo: added.to, to: connection.to, ...(added.to !== to ? { removePreviousInput: true } : {}) } }))
  }, [patchBoard, recordUndo])

  const removeWire = useCallback(async (connection: BoardConnection): Promise<boolean> => {
    if (!await patchBoard({ unwireConnection: { from: connection.from, to: connection.to } })) return false
    recordUndo('the connection removal', boardStep({ wireConnection: { from: connection.from, to: connection.to } }))
    return true
  }, [patchBoard, recordUndo])

  // An End node is created at once, done unless asked; its outcome changes in place.
  const createEndAt = useCallback(async (worldX: number, worldY: number, outcome: EndOutcome = 'done') => {
    const placement = placementForNewNode(worldX, worldY, END_ROOM)
    const before = boardRef.current
    const result = await patchBoard({ createEnd: { outcome, title: defaultEndTitle(outcome), x: placement.x, y: placement.y } })
    if (!before || !result) return
    const created = findAddedByID(before.ends || [], result.board.ends || [])
    if (created) recordUndo(`the new End node ${quoted(created.title, '')}`.trim(), boardStep({ deleteEnd: { id: created.id } }))
  }, [patchBoard, placementForNewNode, recordUndo])

  // Changing the outcome keeps a title the operator chose; a default title follows the outcome.
  const setEndOutcome = useCallback(async (end: EndNode, outcome: EndOutcome): Promise<boolean> => {
    const previous = boardRef.current?.ends?.find(item => item.id === end.id) || end
    if (previous.outcome === outcome) return true
    const title = previous.title === defaultEndTitle(previous.outcome) ? defaultEndTitle(outcome) : previous.title
    if (!await patchBoard({ updateEnd: { id: end.id, outcome, title } })) return false
    recordUndo(`the outcome of End node ${quoted(previous.title, 'untitled')}`, boardStep({ updateEnd: { id: end.id, outcome: previous.outcome, title: previous.title } }))
    return true
  }, [patchBoard, recordUndo])

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

  // ----- staffing (archon-o7p.17) -----
  // What a slot shows while it is staffed lives in this store; what it runs lives in the mission.
  const staffingStore = useMemo(() => new StaffingStore(), [])
  const slotRefOf = useCallback((formation: FormationNode, slot: FormationSlot): SlotRef => ({
    key: slotKey(formation.id, slot.id), formationId: formation.id, slotId: slot.id, label: slot.label || slot.id, step: formation.title,
  }), [])
  const slotOf = useCallback((ref: Pick<SlotRef, 'formationId' | 'slotId'>) => {
    const formation = boardRef.current?.formations.find(item => item.id === ref.formationId)
    const slot = formation?.slots.find(item => item.id === ref.slotId)
    return formation && slot ? { formation, slot } : null
  }, [])
  const savedOf = useCallback((ref: SlotRef): Staffing | null => {
    const found = slotOf(ref)
    return found ? staffingOf(found.slot) : null
  }, [slotOf])
  const refByKey = useCallback((key: string): SlotRef | null => {
    const [formationId, slotId] = key.split(':')
    const found = slotOf({ formationId, slotId })
    return found ? slotRefOf(found.formation, found.slot) : null
  }, [slotOf, slotRefOf])

  // A staffing is written in full, so the slot runs exactly what it says; undo restores the slot's own settings.
  const saveStaffing = useCallback(async (ref: SlotRef, next: Staffing | null, label: string): Promise<string | null> => {
    const found = slotOf(ref)
    if (!found) return `${ref.label} is no longer in this mission.`
    const previous = staffingOf(found.slot)
    // An unchanged staffing writes nothing, so it cannot churn the mission revision.
    if (sameStaffing(previous, next)) return null
    try {
      await applyBoardPatch({ assignSlot: { formationId: ref.formationId, slotId: ref.slotId, ...slotSettings(next) } })
      setError('')
    } catch (err) {
      return err instanceof Error ? err.message : 'The staffing was not saved.'
    }
    recordUndo(label, boardStep({ assignSlot: { formationId: ref.formationId, slotId: ref.slotId, ...slotSettings(previous) } }))
    return null
  }, [applyBoardPatch, recordUndo, slotOf])

  // A move writes the target, then the source; one undo puts both back.
  const moveStaffingOp = useCallback(async (from: SlotRef, fromNext: Staffing | null, to: SlotRef, toNext: Staffing | null): Promise<string | null> => {
    const source = slotOf(from)
    const target = slotOf(to)
    if (!source || !target) return 'That slot is no longer in this mission.'
    const restoreTarget = boardStep({ assignSlot: { formationId: to.formationId, slotId: to.slotId, ...slotSettings(staffingOf(target.slot)) } }, `the staffing of ${quoted(to.label, 'a slot')}`)
    const restoreSource = boardStep({ assignSlot: { formationId: from.formationId, slotId: from.slotId, ...slotSettings(staffingOf(source.slot)) } }, `the staffing of ${quoted(from.label, 'a slot')}`)
    try {
      await applyBoardPatch({ assignSlot: { formationId: to.formationId, slotId: to.slotId, ...slotSettings(toNext) } })
    } catch (err) {
      return err instanceof Error ? err.message : 'The staffing was not moved.'
    }
    try {
      await applyBoardPatch({ assignSlot: { formationId: from.formationId, slotId: from.slotId, ...slotSettings(fromNext) } })
    } catch (err) {
      // The target changed, so that much is undoable alone.
      recordUndo(`the staffing of ${quoted(to.label, 'a slot')}`, restoreTarget)
      return err instanceof Error ? err.message : 'The staffing was not moved.'
    }
    setError('')
    recordUndo(`the move to ${quoted(to.label, 'a slot')}`, restoreTarget, restoreSource)
    return null
  }, [applyBoardPatch, recordUndo, slotOf])

  const staffingHostRef = useRef<StaffingHost>({ catalog: { harnesses: [], roles: [], policy: [] }, save: saveStaffing, move: moveStaffingOp })

  /** Opens a slot's sentence beside what it is shown in: the slot, or a node window's sentence. */
  const openStaffing = useCallback((formation: FormationNode, slot: FormationSlot, part: Part | null, anchor: Element) => {
    staffingStore.setOpen({ ref: slotRefOf(formation, slot), part, anchor })
  }, [slotRefOf, staffingStore])

  const emptySlot = useCallback((formation: FormationNode, slot: FormationSlot) => {
    if (!slotStaffed(slot)) return
    const ref = slotRefOf(formation, slot)
    void staff(staffingStore, staffingHostRef.current, ref, staffingOf(slot), null, { label: `the unassignment from ${quoted(slot.label, 'a slot')}` })
  }, [slotRefOf, staffingStore])

  // Undo restores the exact previous slots, so it also covers a formation that had no controller.
  const makeControllerOp = useCallback((formation: FormationNode, slot: FormationSlot) => {
    const previous = boardRef.current?.formations.find(item => item.id === formation.id) || formation
    void patchBoard({ makeController: { formationId: formation.id, slotId: slot.id } }).then(result => {
      if (!result) return
      recordUndo(`the controller change in ${quoted(previous.title, 'the formation')}`, boardStep({ setFormationType: { id: previous.id, type: previous.type, slots: previous.slots } }))
    })
  }, [patchBoard, recordUndo])

  // `record: false` leaves the undo entry to a caller that combines this with more edits.
  const createFormationAt = useCallback(async (type: FormationType, title: string, x: number, y: number, record = true): Promise<FormationNode | null> => {
    const placement = placementForNewNode(x, y)
    const before = boardRef.current
    const result = await patchBoard({ createFormation: { type, title, x: placement.x, y: placement.y } })
    if (!before || !result) return null
    const created = findAddedByID(before.formations || [], result.board.formations || [])
    if (created && record) recordUndo(`the new formation ${quoted(created.title, '')}`.trim(), boardStep({ deleteFormation: { id: created.id } }))
    return created ?? null
  }, [patchBoard, placementForNewNode, recordUndo])

  const createMissionAt = useCallback((x: number, y: number) => {
    setMissionEditor({ initial: { title: boardRef.current?.title || 'Input', goal: '' }, x, y })
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
      createInputCard: { ...draft, title: draft.title || 'Input', x: placement.x, y: placement.y },
    })
    setMissionEditorSaving(false)
    if (!result) return
    const created = findAddedByID(before.inputCards || [], result.board.inputCards || [])
    if (created) recordUndo(`the new Input card ${quoted(created.title, '')}`.trim(), boardStep({ deleteInputCard: { id: created.id } }))
    setMissionEditor(null)
  }, [missionEditor, missionEditorSaving, patchBoard, placementForNewNode, recordUndo])

  // A rename keeps the node's ID, ports, edges, layout and notes.
  const renameNode = useCallback(async (nodeId: string, title: string): Promise<boolean> => {
    const current = boardRef.current
    if (!current) return false
    const formation = current.formations.find(node => node.id === nodeId)
    const mission = current.inputCards?.find(node => node.id === nodeId)
    const gate = current.gates?.find(node => node.id === nodeId)
    const end = current.ends?.find(node => node.id === nodeId)
    const previous = formation?.title ?? mission?.title ?? gate?.title ?? end?.title
    if (previous === undefined) return false
    if (previous === title) return true
    const label = `the rename of ${quoted(previous, 'an untitled node')}`
    if (formation) {
      if (!await patchBoard({ updateFormation: { id: nodeId, title } })) return false
      recordUndo(label, boardStep({ updateFormation: { id: nodeId, title: previous } }))
    } else if (mission) {
      if (!await patchBoard({ updateInputCard: { id: nodeId, title } })) return false
      recordUndo(label, boardStep({ updateInputCard: { id: nodeId, title: previous } }))
    } else if (gate) {
      if (!await patchBoard({ updateGate: { id: nodeId, title } })) return false
      recordUndo(label, boardStep({ updateGate: { id: nodeId, title: previous } }))
    } else if (end) {
      if (!await patchBoard({ updateEnd: { id: nodeId, title } })) return false
      recordUndo(label, boardStep({ updateEnd: { id: nodeId, title: previous } }))
    }
    return true
  }, [patchBoard, recordUndo])

  const updateMissionFields = useCallback(async (missionId: string, fields: Partial<Pick<MissionNode, 'goal' | 'inputHint' | 'files' | 'humanChannel'>>): Promise<boolean> => {
    const previous = boardRef.current?.inputCards?.find(mission => mission.id === missionId)
    if (!previous) return false
    if (!await patchBoard({ updateInputCard: { id: missionId, ...fields } })) return false
    // An absent field is restored as empty, which clears it.
    recordUndo(`the edit of Input card ${quoted(previous.title, 'untitled')}`, boardStep({ updateInputCard: { id: missionId, ...Object.fromEntries(Object.keys(fields).map(key => [key, previous[key as keyof typeof fields] ?? (key === 'files' ? [] : '')])) } }))
    return true
  }, [patchBoard, recordUndo])

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

  // A delete captures the node, its connections and its place first, so one
  // undo restores it with its brief, staffing, ports, wires and notes.
  const displayLayoutRef = useRef(displayLayoutByNode)
  displayLayoutRef.current = displayLayoutByNode
  const [deleteConfirm, setDeleteConfirm] = useState<{ title: string; kind: string; reason: string; patch: Record<string, unknown> } | null>(null)
  const deleteNodeOp = useCallback(async (nodeId: string, patch: Record<string, unknown>, confirmed = false) => {
    const before = boardRef.current
    if (!before) return
    // A node whose restore the server would refuse is deleted only after the operator agrees it cannot be undone.
    const blocker = restoreBlocker(before, nodeId)
    if (blocker && !confirmed) {
      const kind = patch.deleteFormation ? 'formation' : patch.deleteGate ? 'gate' : patch.deleteEnd ? 'End node' : 'Input card'
      const title = [...(before.inputCards || []), ...before.formations, ...(before.gates || []), ...(before.ends || [])].find(node => node.id === nodeId)?.title || ''
      setDeleteConfirm({ title, kind, reason: blocker, patch })
      return
    }
    const position = displayLayoutRef.current.get(nodeId) || { x: 200, y: 200 }
    const undo = blocker ? null : nodeDeleteUndo(before, nodeId, position)
    if (!await patchBoard(patch)) return
    recordEntry(undo)
  }, [patchBoard, recordEntry])

  const confirmDelete = useCallback(() => {
    if (!deleteConfirm) return
    const patch = deleteConfirm.patch
    const nodeId = String((Object.values(patch)[0] as { id: string }).id)
    setDeleteConfirm(null)
    void deleteNodeOp(nodeId, patch, true)
  }, [deleteConfirm, deleteNodeOp])

  useEffect(() => {
    if (!active || !deleteConfirm) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      setDeleteConfirm(null)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [active, deleteConfirm])

  const deleteFormationOp = useCallback((formation: FormationNode) => {
    void deleteNodeOp(formation.id, { deleteFormation: { id: formation.id } })
  }, [deleteNodeOp])

  const deleteGateOp = useCallback((gate: GateNode) => {
    void deleteNodeOp(gate.id, { deleteGate: { id: gate.id } })
  }, [deleteNodeOp])

  const deleteMissionOp = useCallback((mission: MissionNode) => {
    void deleteNodeOp(mission.id, { deleteInputCard: { id: mission.id } })
  }, [deleteNodeOp])

  const deleteEndOp = useCallback((end: EndNode) => {
    void deleteNodeOp(end.id, { deleteEnd: { id: end.id } })
  }, [deleteNodeOp])

  const addPortOp = useCallback(async (formation: FormationNode, direction: FormationPortDirection) => {
    const before = boardRef.current?.formations.find(item => item.id === formation.id)
    const result = await patchBoard({ addPort: { formationId: formation.id, direction, label: direction === 'input' ? 'Input' : 'Output' } })
    if (!before || !result) return
    const after = result.board.formations.find(item => item.id === formation.id)
    if (!after) return
    const created = direction === 'input'
      ? findAddedByID(before.inputs || [], after.inputs || [])
      : findAddedByID(before.outputs || [], after.outputs || [])
    if (created) recordUndo(`the new ${direction} on ${quoted(formation.title, 'the formation')}`, boardStep({ removePort: { formationId: formation.id, portId: created.id } }))
  }, [patchBoard, recordUndo])

  const removePortOp = useCallback(async (formation: FormationNode, portId: string) => {
    const before = boardRef.current
    if (!before) return
    const undo = portRemoveUndo(before, formation.id, portId)
    if (!await patchBoard({ removePort: { formationId: formation.id, portId } })) return
    recordEntry(undo)
  }, [patchBoard, recordEntry])

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

  // Restores a gate as it was: its previous judge chain (or none), then its
  // fields, since attaching adds the formation kind and detaching drops it.
  const gateRestoreSteps = useCallback((gate: GateNode): UndoStep[] => {
    const previous = boardRef.current?.gates?.find(item => item.id === gate.id) || gate
    const chain = judgeChainOf(gate.id)
    return [
      boardStep(chain.length ? { setGateJudge: { gateId: gate.id, chain } } : { detachGateJudge: { gateId: gate.id } }, 'the judge'),
      boardStep({ updateGate: { id: gate.id, ...gateFieldsFromGate(previous) } }, 'the gate settings'),
    ]
  }, [judgeChainOf])

  const attachJudge = useCallback(async (gate: GateNode, chain: string[]): Promise<UndoDraft | null> => {
    const steps = gateRestoreSteps(gate)
    if (!await patchBoard({ setGateJudge: { gateId: gate.id, chain } })) return null
    return { label: `the judge of ${quoted(gate.title, 'the gate')}`, steps }
  }, [gateRestoreSteps, patchBoard])

  const attachJudgeOp = useCallback((gate: GateNode, chain: string[]) => {
    void attachJudge(gate, chain).then(recordEntry)
  }, [attachJudge, recordEntry])

  const detachJudge = useCallback((gate: GateNode) => {
    const previousChain = judgeChainOf(gate.id)
    const previous = boardRef.current?.gates?.find(item => item.id === gate.id) || gate
    void patchBoard({ detachGateJudge: { gateId: gate.id } }).then(result => {
      if (!result) return
      // Restore the fields first: the chain needs the formation kind back.
      recordUndo(`the judge detach from ${quoted(gate.title, 'the gate')}`,
        boardStep({ updateGate: { id: gate.id, ...gateFieldsFromGate(previous) } }, 'the gate settings'),
        ...(previousChain.length ? [boardStep({ setGateJudge: { gateId: gate.id, chain: previousChain } }, 'the judge')] : []))
    })
  }, [judgeChainOf, patchBoard, recordUndo])

  /** Drop on empty canvas / picker "new judge": create the formation, then wire it as judge; one undo removes both. */
  const createJudgeFor = useCallback(async (gate: GateNode, type: FormationType, title: string, x: number, y: number) => {
    const created = await createFormationAt(type, title, x, y, false)
    if (!created) return
    const removeJudge: UndoDraft = { label: '', steps: [boardStep({ deleteFormation: { id: created.id } }, 'the new judge formation')] }
    const attached = await attachJudge(gate, [created.id])
    recordEntry(combineUndo(`the new judge ${quoted(created.title, '')} for ${quoted(gate.title, 'the gate')}`, removeJudge, attached))
  }, [attachJudge, createFormationAt, recordEntry])

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
    if (gate) recordUndo(`the new gate ${quoted(gate.title, '')}`.trim(), boardStep({ deleteGate: { id: gate.id } }))
    setGateEditor(null)
  }, [gateEditor, patchBoard, placementForNewNode, recordUndo])

  const setGateFiles = useCallback(async (gateId: string, files: string[]): Promise<boolean> => {
    const previous = boardRef.current?.gates?.find(gate => gate.id === gateId)
    if (!previous) return false
    if (!await patchBoard({ updateGate: { id: gateId, files } })) return false
    recordUndo(`the files of gate ${quoted(previous.title, 'untitled')}`, boardStep({ updateGate: { id: gateId, files: previous.files || [] } }))
    return true
  }, [patchBoard, recordUndo])

  // Dropping the judge kind detaches the chain, so undo restores the chain after the fields.
  const updateGateFields = useCallback(async (gateId: string, draft: GateDraft): Promise<boolean> => {
    const previous = boardRef.current?.gates?.find(gate => gate.id === gateId)
    if (!previous) return false
    const { kinds, ...rest } = gateFieldsFromDraft(draft)
    const chain = previous.kinds.includes('formation') && !kinds.includes('formation') ? judgeChainOf(previous.id) : []
    if (!await patchBoard({ updateGate: { id: previous.id, ...rest, ...(kinds.length ? { kinds } : {}) } })) return false
    recordUndo(`the edit of gate ${quoted(previous.title, 'untitled')}`,
      boardStep({ updateGate: { id: previous.id, ...gateFieldsFromGate(previous) } }, 'the gate settings'),
      ...(chain.length ? [boardStep({ setGateJudge: { gateId: previous.id, chain } }, 'the judge')] : []))
    return true
  }, [judgeChainOf, patchBoard, recordUndo])

  const rewireSource = useCallback(async (connection: BoardConnection, newFrom: string) => {
    if (!newFrom || newFrom === connection.from || newFrom.split(':')[0] === connection.to.split(':')[0]) return
    const removed = await patchBoard({ unwireConnection: { from: connection.from, to: connection.to } })
    if (!removed) return
    const restoreOld = boardStep({ wireConnection: { from: connection.from, to: connection.to } }, 'the old wire')
    const added = await patchBoard({ wireConnection: { from: newFrom, to: connection.to } })
    // If the new wire fails, the removal still changed the board, so it is undoable alone.
    if (!added) recordUndo('the connection removal', restoreOld)
    else recordUndo('the reconnection', boardStep({ unwireConnection: { from: newFrom, to: connection.to } }, 'the new wire'), restoreOld)
  }, [patchBoard, recordUndo])

  const [startMission, setStartMission] = useState<MissionNode | null>(null)

  const runMission = useCallback(async (mission: MissionNode, inputs: RunInputs) => {
    const current = boardRef.current
    if (!current) return
      const result = await startRun(current.etag, { ...inputs, mission: current.slug, inputCardId: mission.id, expectedRev: current.rev, actor: 'agent:ui' })
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

  // A human channel changed in Start mission is saved first, with undo, so the run's frozen board carries it.
  const startMissionRun = useCallback(async (mission: MissionNode, inputs: RunInputs, channel: HumanChannel) => {
    const current = boardRef.current?.inputCards?.find(item => item.id === mission.id)
    if (current && humanChannelOf(current) !== channel && !await updateMissionFields(mission.id, { humanChannel: humanChannelField(channel) })) {
      throw new Error('The human channel was not saved, so the mission did not start.')
    }
    await runMission(mission, inputs)
  }, [runMission, updateMissionFields])

  const runFormation = useCallback(async (formation: FormationNode) => {
    const current = boardRef.current
    if (!current) return
    try {
      const result = await startRun(current.etag, { mission: current.slug, formationId: formation.id, expectedRev: current.rev, actor: 'agent:ui' })
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

  // Stop is confirmed in the run bar first, which passes the operator's reason (archon-n7u.8).
  const abortActiveRun = useCallback(async (reason: string) => {
    if (!activeRun?.runId || activeRun.final) return false
    try {
      const status = runStatusFromResponse(await abortRunRequest(activeRun.runId, { reason, requestedBy: 'agent:ui' }))
      setActiveRun(status)
      await refreshRunEvents(activeRun.runId)
      if (status.final && selectedSlug) window.localStorage.removeItem(activeRunStorageKey(selectedSlug))
      setError('')
      return true
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to abort run')
      return false
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
    void createFormationAt('solo', 'New formation', center.x, center.y)
  }, [createFormationAt])

  // ----- pan + zoom -----
  const onViewportPointerDown = useCallback((event: ReactPointerEvent) => {
    if (event.button !== 0) return
    const target = event.target as HTMLElement
    if (target.closest('.formation,.gatecard,.missioncard,.toolcard,.endcard,.zoomctl,.run-banner')) return
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
    const current = boardRef.current
    const openable = Boolean(current && [...(current.inputCards || []), ...current.formations, ...(current.gates || []), ...(current.ends || [])].some(node => node.id === nodeId))
    // In Flow, bring the step's row into view and open its window beside the row's title.
    const title = boardView === 'flow'
      ? document.querySelector<HTMLElement>(`[data-flow-node="${nodeId.replace(/["\\]/g, '\\$&')}"] .flow-title`)
      : null
    if (title) {
      title.scrollIntoView?.({ block: 'center' })
      if (openable) {
        const { left, top, width, height } = title.getBoundingClientRect()
        openNodeWindow(nodeId, { left, top, width, height })
      }
      return
    }
    locateNode(nodeId)
    if (openable) window.setTimeout(() => openNodeWindow(nodeId), 450)
  }, [boardView, locateNode, openNodeWindow])

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
    try {
      await applyLayoutPatch({ arrange: true })
      setError('')
      if (previous.length) recordUndo('the arrangement', { layout: { nodes: previous } })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to arrange layout')
    }
  }, [applyLayoutPatch, recordUndo])

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

  // A drag after 6 px of movement: the slot under the pointer previews the drop,
  // a drop on a slot staffs it, and a drop anywhere else changes nothing.
  const beginStaff = useCallback((event: ReactPointerEvent, start: Pick<DragStaff, 'payload' | 'slot' | 'click'>) => {
    if (event.button !== 0) return
    event.stopPropagation()
    const staffDrag: DragStaff = { ...start, startX: event.clientX, startY: event.clientY, moved: false }
    const store = staffingStore
    let hovered: string | null = null
    const leave = () => {
      if (hovered) store.setDraft(hovered, undefined)
      hovered = null
    }
    const ghostLabel = (payload: StaffPayload) => {
      const host = staffingHostRef.current
      if (payload.kind === 'role') return host.catalog.roles.find(role => role.id === payload.roleId)?.name || payload.roleId
      const moving = store.current(payload.from.key, savedOf(payload.from))
      return moving ? `${moving.role ? `${roleName(staffingHostRef.current.catalog, moving.role)} · ` : ''}${captionText(moving)}` : payload.from.label
    }
    interactionOwner.begin({
      kind: 'staff',
      pointerId: event.pointerId,
      project: pointer => {
        if (!staffDrag.moved && Math.abs(pointer.clientX - staffDrag.startX) + Math.abs(pointer.clientY - staffDrag.startY) >= 6) staffDrag.moved = true
        const payload = staffDrag.payload
        if (!staffDrag.moved || !payload) return
        setGhost({ x: pointer.clientX, y: pointer.clientY, label: ghostLabel(payload) })
        const key = slotKeyAt(pointer.clientX, pointer.clientY)
        const target = key ? refByKey(key) : null
        setHoverSlot(target ? target.key : null)
        if (hovered !== target?.key) leave()
        if (!target || (payload.kind === 'slot' && payload.from.key === target.key)) return
        hovered = target.key
        const host = staffingHostRef.current
        store.setDraft(target.key, payload.kind === 'role'
          ? previewRole(store, host, target, savedOf(target), payload.roleId)
          : store.current(payload.from.key, savedOf(payload.from)))
      },
      finalize: pointer => {
        leave()
        const payload = staffDrag.payload
        if (!staffDrag.moved) {
          // A click: a slot opens its sentence, the word clicked if it has one; a rail row opens its role.
          if (staffDrag.slot) store.setOpen({ ref: staffDrag.slot.ref, part: staffDrag.slot.part, anchor: staffDrag.slot.anchor })
          else staffDrag.click?.()
          return
        }
        if (!payload) return
        const key = slotKeyAt(pointer.clientX, pointer.clientY)
        const target = key ? refByKey(key) : null
        // A drop that reaches no slot changes nothing, and says so (archon-n2w).
        if (!target) {
          store.say(null, 'Not on a slot, so nothing changed.', 'refused', { x: pointer.clientX, y: pointer.clientY })
          return
        }
        const host = staffingHostRef.current
        if (payload.kind === 'role') dropRole(store, host, target, savedOf(target), payload.roleId)
        else if (payload.from.key !== target.key) void moveStaffing(store, host, payload.from, savedOf(payload.from), target, savedOf(target))
      },
      cancel: () => {
        leave()
        setGhost(null)
        setHoverSlot(null)
      },
    })
  }, [interactionOwner, refByKey, savedOf, staffingStore])

  // Esc during a staffing drag lets go and changes nothing.
  useEffect(() => {
    if (!ghost) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      event.stopPropagation()
      interactionOwner.cancel()
    }
    window.addEventListener('keydown', onKeyDown, true)
    return () => window.removeEventListener('keydown', onKeyDown, true)
  }, [ghost, interactionOwner])

  // N reaches the next empty slot in Flow order, brings it into view and opens its sentence;
  // pressed again, even after Esc, it moves on to the next one.
  useEffect(() => {
    if (!active || boardView !== 'canvas') return
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.key !== 'n' && event.key !== 'N') || event.ctrlKey || event.metaKey || event.altKey || event.defaultPrevented) return
      const target = event.target as Element | null
      if (isTextEditingTarget(event.target) || target?.closest?.('.staffing-window,[role="dialog"],[role="alertdialog"],.ctxmenu')) return
      const current = boardRef.current
      const world = worldRef.current
      if (!current || !world) return
      const flow = buildFlow(current)
      const rankOf = (formationId: string) => {
        const number = flow.numbers.get(formationId)
        if (number !== undefined) return number
        const gate = flow.judgeOf.get(formationId)
        // A judge decides right after its gate; a formation no Input card reaches comes last.
        return gate !== undefined ? (flow.numbers.get(gate) ?? 1e6) + 0.5 : 1e6
      }
      const empties = [...world.querySelectorAll<HTMLElement>('.slot.empty[data-slot-key]')]
        .map((element, index) => ({ element, index, rank: rankOf(element.dataset.fid || '') }))
        .sort((a, b) => (a.rank - b.rank) || (a.index - b.index))
        .map(entry => entry.element)
      event.preventDefault()
      if (!empties.length) {
        const rect = viewportRef.current?.getBoundingClientRect()
        staffingStore.say(null, 'No empty slot in this mission.', 'note', { x: (rect?.left || 0) + 24, y: (rect?.top || 0) + 24 })
        return
      }
      const from = staffingStore.open?.ref.key || staffingStore.lastNext
      const at = empties.findIndex(element => element.dataset.slotKey === from)
      const next = empties[(at + 1) % empties.length]
      const key = next.dataset.slotKey || ''
      const ref = refByKey(key)
      if (!ref) return
      staffingStore.lastNext = key
      // Pan so the slot sits clear of the canvas edges, then open it where it now is.
      const canvas = viewportRef.current?.getBoundingClientRect()
      const box = next.getBoundingClientRect()
      if (canvas) {
        const margin = 80
        const inside = box.left >= canvas.left + margin && box.right <= canvas.right - margin && box.top >= canvas.top + margin && box.bottom <= canvas.bottom - 160
        if (!inside) {
          const dx = canvas.left + canvas.width * 0.4 - (box.left + box.width / 2)
          const dy = canvas.top + canvas.height * 0.4 - (box.top + box.height / 2)
          setView(view => ({ ...view, x: view.x + dx, y: view.y + dy }))
        }
      }
      requestAnimationFrame(() => requestAnimationFrame(() => {
        const element = world.querySelector<HTMLElement>(`.slot[data-slot-key="${CSS.escape(key)}"]`) || next
        element.focus({ preventScroll: true })
        staffingStore.setOpen({ ref, part: null, anchor: element })
      }))
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [active, boardView, refByKey, staffingStore])

  const grabSlot = useCallback((event: ReactPointerEvent<HTMLElement>, ref: SlotRef, part: Part | null) => {
    const staffed = staffingStore.current(ref.key, savedOf(ref))
    beginStaff(event, { payload: staffed ? { kind: 'slot', from: ref } : null, slot: { ref, part: staffed ? part : null, anchor: event.currentTarget } })
  }, [beginStaff, savedOf, staffingStore])

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
          if (current && [...(current.inputCards || []), ...current.formations, ...(current.gates || []), ...(current.ends || [])].some(node => node.id === drag.id)) openNodeWindow(drag.id)
          return
        }
        const scale = viewRef.current.scale || 1
        // Release snaps to the visible dot grid so hand-placed cards line up.
        const x = snapToGrid(drag.originX + (pointer.clientX - drag.startX) / scale)
        const y = snapToGrid(drag.originY + (pointer.clientY - drag.startY) / scale)
        if (x === drag.originX && y === drag.originY) return
        void persistPositions([{ id: drag.id, x, y }]).then(saved => {
          if (saved) recordUndo('the move', { layout: { nodes: [{ id: drag.id, x: drag.originX, y: drag.originY }] } })
        })
      },
      cancel: () => {
        worldRef.current?.classList.remove('nodedrag')
        setDragPos(null)
      },
    })
  }, [interactionOwner, openNodeWindow, persistPositions, positionOf, recordUndo])

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
            attachJudgeOp(gate, [hoveredJudge])
          } else {
            // Dropped on empty canvas inside the viewport → spawn a judge there (reference just-works).
            const rect = viewportRef.current?.getBoundingClientRect()
            const overCard = (pointer.target as HTMLElement | null)?.closest?.('.formation,.gatecard,.missioncard,.toolcard,.endcard,.ctxmenu,.pop')
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
              // One gesture, one undo entry: the wire removal and the judge attach run in turn.
              const moved = active.kind === 'reconnect-target' ? active.connection : null
              void (async () => {
                if (moved && !await patchBoard({ unwireConnection: { from: moved.from, to: moved.to } })) return
                const attached = await attachJudge(gate, [fromNodeId])
                const unwired: UndoDraft | null = moved ? { label: '', steps: [boardStep({ wireConnection: { from: moved.from, to: moved.to } }, 'the moved wire')] } : null
                recordEntry(combineUndo(`the judge of ${quoted(gate.title, 'the gate')}`, unwired, attached))
              })()
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
  }, [attachJudge, attachJudgeOp, createJudgeFor, interactionOwner, patchBoard, recordEntry, rewireSource, rewireTarget, screenToWorld, wire])

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
        void setWireLane(lane.connectionId, `y:${Math.round(projected.y)}`, 'the wire routing').then(() => setLaneDraft(null))
      },
      cancel: () => setLaneDraft(null),
    })
  }, [beginReconnect, beginReconnectSource, endpointWorldCenter, interactionOwner, screenToWorld, setWireLane])

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

  const beginEndToken = useCallback((event: ReactPointerEvent) => {
    if (event.button !== 0) return
    if (!boardRef.current) return
    event.preventDefault()
    interactionOwner.begin({
      kind: 'gate',
      pointerId: event.pointerId,
      project: pointer => setEndGhost({ x: pointer.clientX, y: pointer.clientY }),
      finalize: pointer => {
        const rect = viewportRef.current?.getBoundingClientRect()
        if (rect && pointer.clientX >= rect.left && pointer.clientX <= rect.right && pointer.clientY >= rect.top && pointer.clientY <= rect.bottom) {
          const w = screenToWorld(pointer.clientX, pointer.clientY)
          void createEndAt(w.x, w.y)
        }
      },
      cancel: () => setEndGhost(null),
    })
    setEndGhost({ x: event.clientX, y: event.clientY })
  }, [createEndAt, interactionOwner, screenToWorld])

  const gateHasJudge = useCallback((gateId: string): boolean => {
    return (boardRef.current?.connections || []).some(connection =>
      connection.from === `${gateId}:judge` || connection.to === `${gateId}:judge`)
  }, [])

  // Undo restores the exact previous slots, including any a change to solo removed.
  const changeFormationType = useCallback((formation: FormationNode, type: FormationType, keepSlotId?: string) => {
    void patchBoard({ setFormationType: { id: formation.id, type, ...(keepSlotId ? { keepSlotId } : {}) } }).then(result => {
      if (result) recordUndo(`the type change of ${quoted(formation.title, 'the formation')}`, boardStep({ setFormationType: { id: formation.id, type: formation.type, slots: formation.slots } }))
    })
  }, [patchBoard, recordUndo])

  const formationTypeMenuItems = useCallback((formation: FormationNode): MenuItem[] => [
    { label: 'Change type', head: true },
    ...formationTypeChoices(formation).map(choice => ({ label: choice.label, action: () => changeFormationType(formation, choice.type, choice.keepSlotId) })),
  ], [changeFormationType])

  const formationMenu = useCallback((event: ReactMouseEvent<HTMLElement>, formation: FormationNode) => {
    openMenu(event, 'Formation actions', [
      { label: 'Run formation', action: () => void runFormation(formation) },
      { label: 'Add input port', action: () => void addPortOp(formation, 'input') },
      { label: 'Add output port', action: () => void addPortOp(formation, 'output') },
      ...formationTypeMenuItems(formation),
      { label: 'Delete formation', destructive: true, action: () => deleteFormationOp(formation) },
    ])
  }, [addPortOp, deleteFormationOp, formationTypeMenuItems, openMenu, runFormation])

  // A slot's menu staffs it in place, empties it on purpose with undo, or makes it the controller.
  const slotMenu = useCallback((event: ReactMouseEvent<HTMLElement>, formation: FormationNode, slot: FormationSlot) => {
    const anchor = event.currentTarget
    const items: MenuItem[] = [{ label: `Staff ${slot.label}…`, action: () => openStaffing(formation, slot, null, anchor) }]
    if (slotStaffed(slot)) items.push({ label: `Empty ${slot.label}`, action: () => emptySlot(formation, slot) })
    if (formation.type === 'orchestrated' && !slot.controller) {
      items.push({ label: 'Make controller', action: () => makeControllerOp(formation, slot) })
    }
    openMenu(event, `Slot · ${slot.label}`, items)
  }, [emptySlot, makeControllerOp, openMenu, openStaffing])

  const wireMenu = useCallback((event: ReactMouseEvent<SVGPathElement>, connection: BoardConnection) => {
    openMenu(event, 'Connection actions', [
      { label: 'Reset routing', action: () => void setWireLane(connection.id, 'auto', 'the routing reset') },
      { label: 'Remove connection', destructive: true, action: () => void removeWire(connection) },
    ])
  }, [openMenu, removeWire, setWireLane])

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
        items.push({ label: formation.title, action: () => attachJudgeOp(gate, [formation.id]) })
      }
    }
    if (gateHasJudge(gate.id)) {
      items.push({ label: 'Detach judge', destructive: true, action: () => detachJudge(gate) })
    }
    setMenu({ label: 'Judge', x, y, items })
  }, [attachJudgeOp, createJudgeFor, detachJudge, displayLayoutByNode, gateHasJudge])
  openJudgePickerRef.current = openJudgePicker

  const gateMenu = useCallback((event: ReactMouseEvent<HTMLElement>, gate: GateNode) => {
    openMenu(event, 'Gate actions', [
      { label: gateHasJudge(gate.id) ? 'Change judge…' : 'Attach judge…', action: () => openJudgePicker(gate, event.clientX, event.clientY) },
      ...(gateHasJudge(gate.id) ? [{ label: 'Detach judge', action: () => detachJudge(gate) }] : []),
      { label: 'Delete gate', destructive: true, action: () => deleteGateOp(gate) },
    ])
  }, [deleteGateOp, detachJudge, gateHasJudge, openJudgePicker, openMenu])

  const endMenu = useCallback((event: ReactMouseEvent<HTMLElement>, end: EndNode) => {
    const other: EndOutcome = end.outcome === 'rejected' ? 'done' : 'rejected'
    openMenu(event, 'End node actions', [
      { label: other === 'rejected' ? 'End rejected instead' : 'End done instead', action: () => void setEndOutcome(end, other) },
      { label: 'Delete End node', destructive: true, action: () => deleteEndOp(end) },
    ])
  }, [deleteEndOp, openMenu, setEndOutcome])

  const missionMenu = useCallback((event: ReactMouseEvent<HTMLElement>, mission: MissionNode) => {
    openMenu(event, 'Input card actions', [
      { label: 'Start mission', action: () => setStartMission(mission) },
      { label: 'Delete Input card', destructive: true, action: () => deleteMissionOp(mission) },
    ])
  }, [deleteMissionOp, openMenu])

  const inputRowMenu = useCallback((event: ReactMouseEvent<HTMLElement>, formation: FormationNode, portId: string, incoming?: BoardConnection) => {
    openMenu(event, 'Input port', [
      ...(incoming ? [{ label: 'Disconnect input', action: () => void removeWire(incoming) }] : []),
      { label: 'Add input port', action: () => void addPortOp(formation, 'input') },
      { label: 'Remove this input', destructive: true, action: () => void removePortOp(formation, portId) },
    ])
  }, [addPortOp, openMenu, removePortOp, removeWire])

  const outputRowMenu = useCallback((event: ReactMouseEvent<HTMLElement>, formation: FormationNode, portId: string) => {
    openMenu(event, 'Output port', [
      { label: 'Add output port', action: () => void addPortOp(formation, 'output') },
      { label: 'Remove this output', destructive: true, action: () => void removePortOp(formation, portId) },
    ])
  }, [addPortOp, openMenu, removePortOp])

  const canvasMenu = useCallback((event: ReactMouseEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement
    if (target.closest('.formation,.gatecard,.missioncard,.toolcard,.endcard,.ctxmenu,.pop,.run-banner,.zoomctl')) return
    if ((target as Element).closest?.('path')) return
    event.preventDefault()
    event.stopPropagation()
    const w = screenToWorld(event.clientX, event.clientY)
    setMenu({
      label: 'New',
      x: event.clientX,
      y: event.clientY,
      items: [
        // One mission per file: the Input card is offered only while the mission has none.
        ...(boardRef.current?.inputCards?.length ? [] : [{ label: 'Input card', action: () => createMissionAt(w.x, w.y) }]),
        { label: 'Solo formation', action: () => void createFormationAt('solo', 'New formation', w.x, w.y) },
        { label: 'Peer formation', action: () => void createFormationAt('peer', 'New peers', w.x, w.y) },
        { label: 'Orchestrated formation', action: () => void createFormationAt('orchestrated', 'New desk', w.x, w.y) },
        { label: 'Gate', action: () => void createGateAt(w.x, w.y) },
        { label: 'End node · done', action: () => void createEndAt(w.x, w.y, 'done') },
        { label: 'End node · rejected', action: () => void createEndAt(w.x, w.y, 'rejected') },
      ],
    })
  }, [createEndAt, createFormationAt, createGateAt, createMissionAt, screenToWorld])

  // ----- render helpers -----
  const renderSlot = (formation: FormationNode, slot: FormationSlot, badge?: number) => {
    const ref = slotRefOf(formation, slot)
    const runState = nodeStates.get(formation.id)
    return (
      <CanvasSlot key={slot.id} store={staffingStore} host={staffingHost} slotRef={ref} slot={slot} saved={staffingOf(slot)} badge={badge}
        classes={[hoverSlot === ref.key ? 'snaptarget' : '', runState === 'running' ? 'active' : '', runState === 'done' ? 'active done' : '']}
        onGrab={grabSlot} onMenu={event => slotMenu(event, formation, slot)} />
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
      ...(board.inputCards || []).map(node => ({ id: node.id, title: node.title, kind: 'Input card' })),
      ...(board.formations || []).map(node => ({ id: node.id, title: node.title, kind: 'Formation' })),
      ...(board.gates || []).map(node => ({ id: node.id, title: node.title || 'Gate', kind: 'Gate' })),
      ...(board.tools || []).map(node => ({ id: node.id, title: node.title, kind: 'Tool' })),
      ...(board.ends || []).map(node => ({ id: node.id, title: node.title, kind: 'End node' })),
    ]
  }, [board])

  const nodeNotes = useMemo(
    () => noteElements.filter(element => noteByNode.has(element.id)).map(element => ({ nodeId: element.id, title: element.title, entries: noteByNode.get(element.id) || [] })),
    [noteByNode, noteElements],
  )
  const noteTitleOf = (target: string) => target === BOARD_NOTE_TARGET
    ? board?.title || 'Mission'
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
  const staffingCatalog = useMemo<StaffingCatalog>(() => ({ ...staffingTerms, roles: rolesOf(agents) }), [agents, staffingTerms])
  const staffingHost = useMemo<StaffingHost>(() => ({ catalog: staffingCatalog, save: saveStaffing, move: moveStaffingOp }), [moveStaffingOp, saveStaffing, staffingCatalog])
  staffingHostRef.current = staffingHost
  const filteredRosterAgents = useMemo(() => {
    const needle = rosterSearch.trim().toLowerCase()
    if (!needle) return rosterAgents
    return rosterAgents.filter(agent => [
      agent.id, agent.displayName || '', agent.kind || '', ...(agent.tags || []),
    ].some(value => value.toLowerCase().includes(needle)))
  }, [rosterAgents, rosterSearch])
  const rosterRoles = useMemo(() => [...filteredRosterAgents].sort(byRoleName), [filteredRosterAgents])
  // Roles in use are said in words, counted across the whole mission as the Agents view counts them.
  const rolesInUse = useMemo(() => roleUses(board?.formations || []), [board?.formations])
  const runBadgeClass = activeRun ? activeRun.status : ''
  const choices = useMemo(() => runChoices(boardRuns, activeRun), [activeRun, boardRuns])
  // Choosing a run pins it to the board; choosing none puts a finished run away.
  const chooseRun = (runId: string) => {
    setLinkError('')
    if (runId) {
      setPinnedRun({ slug: selectedSlug, runId })
      return
    }
    setPinnedRun({ slug: '', runId: '' })
    setActiveRun(null)
    setRunEvents([])
  }
  const runPicker = (shownRunId: string) => (
    <select className="run-picker" aria-label="Choose run" value={shownRunId} onChange={event => chooseRun(event.target.value)}>
      {!shownRunId ? <option value="">Recent runs…</option> : activeRun?.final ? <option value="">No run shown</option> : null}
      {choices.open.length ? (
        <optgroup label="Open">{choices.open.map(run => <option key={run.runId} value={run.runId}>{runChoiceLabel(run)}</option>)}</optgroup>
      ) : null}
      {choices.finished.length ? (
        <optgroup label="Finished">{choices.finished.map(run => <option key={run.runId} value={run.runId}>{runChoiceLabel(run)}</option>)}</optgroup>
      ) : null}
    </select>
  )
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
  const gateTalk = useGateTalk({ board, run: activeRun, events: runEvents, agents, gate: pendingHumanGate, focusWindow })
  const pendingGateUpstream = useMemo(() => {
    if (pendingGateInput.state !== 'ready') return pendingGateInput
    const node = [...(board?.formations || []), ...(board?.gates || []), ...(board?.inputCards || [])].find(candidate => candidate.id === pendingGateInput.from)
    return { ...pendingGateInput, from: node?.title || pendingGateInput.from }
  }, [board?.formations, board?.gates, board?.inputCards, pendingGateInput])
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
  // The pending human gate's answer panel: in a floating window over the canvas, or in the gate's row in Flow.
  const answerKey = activeRun && pendingHumanGate ? `${activeRun.runId}:${pendingHumanGate.requestedSeq}` : ''
  const answerWindowOpen = Boolean(answerKey) && !(answerWindow.key === answerKey && answerWindow.closed)
  const showAnswer = useCallback((gateId: string) => {
    locateNode(gateId)
    setAnswerWindow(current => ({ key: answerKey, closed: false, focus: current.focus + 1 }))
    focusWindow(GATE_ANSWER_WINDOW_ID)
  }, [answerKey, focusWindow, locateNode])
  const answerPanelFor = (framed: boolean) => activeRun && !activeRun.final && pendingHumanGate ? (
    <HumanGateAnswerPanel
      key={`${activeRun.runId}:${pendingHumanGate.requestedSeq}`}
      framed={framed}
      titleOf={nodeId => (board ? nodeTitle(board, nodeId) : nodeId)}
      runId={activeRun.runId}
      gateId={pendingHumanGate.gateId}
      requestedSeq={pendingHumanGate.requestedSeq}
      gateTitle={pendingHumanGate.title}
      criterion={pendingHumanGate.criterion}
      upstream={pendingGateUpstream}
      onOpenEvidence={board?.gates?.some(gate => gate.id === pendingHumanGate.gateId)
        ? () => setInspectedNodeId(pendingHumanGate.gateId) : undefined}
      talk={gateTalk.panel}
      onDecide={(verdict, response) => recordHumanGateVerdict(pendingHumanGate.gateId, pendingHumanGate.requestedSeq, verdict, response)}
    />
  ) : null
  const answerPanel = answerPanelFor(false)
  const showFlow = boardView === 'flow' && Boolean(board)
  const runPointTitle = (() => {
    const nodeId = runPoint?.nodeId
    if (!nodeId || !board) return ''
    const gate = board.gates?.find(node => node.id === nodeId)
    return board.formations?.find(node => node.id === nodeId)?.title
      || (gate ? gate.title || gate.kinds.map(gateKindLabel).join(' · ') || 'Gate' : '')
      || board.inputCards?.find(node => node.id === nodeId)?.title
      || board.ends?.find(node => node.id === nodeId)?.title
      || ''
  })()
  const nodeAttempts = useMemo(() => projectNodeAttempts(runEvents), [runEvents])
  const flowRun = useMemo<FlowRun | null>(() => (activeRun
    ? { runId: activeRun.runId, states: nodeStates, attempts: nodeAttempts, point: runPoint, pointTitle: runPointTitle }
    : null), [activeRun, nodeAttempts, nodeStates, runPoint, runPointTitle])
  const inspectedNode = useMemo(() => {
    if (!inspectedNodeId || !board) return null
    const formation = board.formations?.find(node => node.id === inspectedNodeId)
    if (formation) return { kind: 'formation' as const, id: formation.id, title: formation.title }
    const gate = board.gates?.find(node => node.id === inspectedNodeId)
    if (gate) return { kind: 'gate' as const, id: gate.id, title: gate.title || gate.kinds.map(gateKindLabel).join(' · ') || 'Gate' }
    const mission = board.inputCards?.find(node => node.id === inspectedNodeId)
    if (mission) return { kind: 'inputCard' as const, id: mission.id, title: mission.title }
    return null
  }, [inspectedNodeId, board])
  const inspectableNodeId = useCallback((escalation: OpenEscalation): string => {
    const candidate = escalation.gateId || escalation.nodeId || ''
    if (!candidate || !board) return ''
    const known = board.formations?.some(node => node.id === candidate)
      || board.gates?.some(node => node.id === candidate)
      || board.inputCards?.some(node => node.id === candidate)
    return known ? candidate : ''
  }, [board])

  const nodeWindowOps = useMemo<NodeWindowOps>(() => ({
    rename: renameNode,
    updateInputCard: updateMissionFields,
    setBrief: saveBrief,
    setExecution: saveExecution,
    changeType: changeFormationType,
    staffSlot: openStaffing,
    updateGate: (gate, draft) => updateGateFields(gate.id, draft),
    setGateFiles: (gate, files) => setGateFiles(gate.id, files),
    setEndOutcome,
    attachJudge,
    detachJudge,
    openNode: openNodeWindow,
    openNotes: openNoteWindow,
    inspectEvidence: nodeId => {
      // The evidence dialog sits above the window; keyboard focus leaves the window so Escape closes the dialog first.
      if (document.activeElement instanceof HTMLElement) document.activeElement.blur()
      setInspectedNodeId(nodeId)
    },
  }), [attachJudge, changeFormationType, detachJudge, openNodeWindow, openNoteWindow, openStaffing, renameNode, saveBrief, saveExecution, setEndOutcome, setGateFiles, updateGateFields, updateMissionFields])

  const cockpit = (
    <div className="fmx" data-testid="formations-view" data-cockpit="d7">
      <div className="topbar">
        <div className="boardpick">
          mission
          <select aria-label="Mission" value={selectedSlug} onChange={event => selectBoard(event.target.value)} data-testid="board-picker" disabled={boards.length === 0 || Boolean(boardDialog)}>
            {boards.length === 0 ? <option value="">No missions</option> : null}
            {boards.map(summary => <option key={summary.slug} value={summary.slug}>{summary.title || summary.slug}</option>)}
          </select>
          {board ? <span className="rev">rev {board.rev}</span> : null}
        </div>
        <button className="newbtn board-new" type="button" onClick={event => openCreateBoard(event.currentTarget)} data-testid="new-board" disabled={Boolean(boardDialog)}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M12 5v14M5 12h14" /></svg>
          New mission
        </button>
        <button className="board-action" type="button" aria-label="Rename mission" disabled={!board || Boolean(boardDialog)} onClick={event => openRenameBoard(event.currentTarget)}>Rename</button>
        <button className="board-action danger" type="button" aria-label="Delete mission" disabled={!board || Boolean(boardDialog)} onClick={event => openDeleteBoard(event.currentTarget)}>Delete</button>
        <div className="sep" />
        <button className="newbtn" onClick={createSolo} data-testid="new-formation" disabled={!board}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M12 5v14M5 12h14" /></svg>
          New formation
        </button>
        <div className="gatetoken" title="Drag onto the canvas to drop a gate" data-testid="gate-token" onPointerDown={beginGateToken}>
          {GATE_SVG}
          Gate
        </div>
        <div className="gatetoken endtoken" title="Drag onto the canvas to end a path on purpose" data-testid="end-token" onPointerDown={beginEndToken}>
          {END_SVG}
          End
        </div>
        <div className="spacer" />
        <CanvasLegend />
        <div className="notes-switch view-switch" role="radiogroup" aria-label="Mission view">
          {(['canvas', 'flow'] as const).map(view => (
            <button key={view} type="button" role="radio" aria-checked={boardView === view} className={boardView === view ? 'on' : ''}
              disabled={!board} onClick={() => changeBoardView(view)}>{view === 'canvas' ? 'Canvas' : 'Flow'}</button>
          ))}
        </div>
        <div className="notes-switch" role="radiogroup" aria-label="Notes on the canvas">
          {NOTES_MODES.map(mode => (
            <button key={mode} type="button" role="radio" aria-checked={notesMode === mode} className={notesMode === mode ? 'on' : ''}
              onClick={() => changeNotesMode(mode)}>{mode === 'hidden' ? 'Hide notes' : mode === 'preview' ? 'Preview' : 'Full notes'}</button>
          ))}
        </div>
        <button type="button" className="newbtn board-notes-button" disabled={!board} onClick={() => openNoteWindow(BOARD_NOTE_TARGET)}>Mission notes</button>
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
            <div className="t">Roles</div>
            <span className="s" data-testid="roster-count" title={`${rosterAgents.length} roles you can staff; ${rolesInUse.size} of them staff slots in this mission`}>
              {rolesInUseLabel(rosterAgents.length, rolesInUse.size)}
            </span>
            <button type="button" className="roster-toggle" aria-expanded={!roster.collapsed}
              aria-label={roster.collapsed ? 'Expand agent roster' : 'Collapse agent roster'} onClick={roster.toggle}>
              {roster.collapsed ? '›' : '‹'}
            </button>
          </div>
          <div className="board-roster-filter">
            <div className="board-roster-filter-input">
              <input type="search" aria-label="Filter agents" placeholder="filter roles" value={rosterSearch}
                onChange={event => setRosterSearch(event.target.value)} />
              {rosterSearch ? <button type="button" aria-label="Clear agent filter" onClick={() => setRosterSearch('')}>Clear</button> : null}
            </div>
            {rosterSearch.trim() ? <p role="status">{filteredRosterAgents.length} of {rosterAgents.length} roles</p> : null}
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
              ? <div className="roster-empty">No roles to staff with. Create one in the Agents view; a slot can also run with no role.</div>
              : filteredRosterAgents.length === 0 ? <div className="roster-empty" role="status">No roles match this filter.</div>
              : (
                <section className="roster-group">
                  {rosterRoles.map(agent => {
                    const inUse = rolesInUse.get(agent.id) || 0
                    return (
                      <div
                        key={agent.id}
                        className={`ragent${inUse ? ' in-use' : ''}${agent.unbound ? ' unbound' : ''}`}
                        data-agent={agent.id}
                        data-testid={`roster-agent-${agent.id}`}
                        title={`${agent.displayName || agent.id}: drag onto a slot to give it this role, or click to read it`}
                        onPointerDown={event => {
                          const row = event.currentTarget
                          beginStaff(event, { payload: { kind: 'role', roleId: agent.id }, click: () => openRoleWindow(agent.id, row) })
                        }}
                      >
                        <span className="av">{initials(agent.displayName || agent.id)}</span>
                        <div className="ri">
                          <div className="n">{agent.displayName || agent.id}</div>
                          {/* In use comes first, so a narrow rail never cuts it off. */}
                          <div className="r">{inUse ? <span className="in-use-words">{inSlotsWords(inUse)} · </span> : null}{[agentRole(agent), agent.preset ? (agent.customized ? 'custom' : 'preset') : '', agentState(agent) === 'idle' ? 'idle' : ''].filter(Boolean).join(' · ')}</div>
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
              )}
          </div>
        </aside>

        {/* The run banner docks above the canvas so it never covers cards. */}
        <div className="canvas-column">
            {activeRun ? (
              <div className="run-banner" data-testid="run-banner">
                <span>run</span>
                <span className={`badge ${runBadgeClass}`}>{runStatusLabel(activeRun.status)}</span>
                {/* Waiting at a gate on the canvas, the phrase brings up the answer, not the gate's editor. */}
                <RunPoint runId={activeRun.runId} point={runPoint} title={runPointTitle}
                  titleOf={nodeId => (board ? nodeTitle(board, nodeId) : nodeId)}
                  onLocate={!showFlow && runPoint?.kind === 'waiting' && answerKey ? showAnswer : locateAndOpenNode}
                  action={showFlow ? 'Open the step' : runPoint?.kind === 'waiting' && answerKey ? 'Show it on the canvas with your answer' : 'Show it on the canvas and open it'} />
                <RunProduced />
                {activeRun.final || choices.open.length + choices.finished.length > 1 ? runPicker(activeRun.runId) : null}
                {activeRun.cwd && <span className="run-cwd" title={activeRun.cwd}>{activeRun.cwd}</span>}
                {activeRun.beadId && <span>{activeRun.beadId}</span>}
                <RunBarActions run={activeRun} point={runPoint} pointTitle={runPointTitle} boardTitle={board?.title || ''}
                  titleOf={nodeId => (board ? nodeTitle(board, nodeId) : nodeId)}
                  pendingGate={pendingHumanGate} onResume={() => void resumeActiveRun()} onStop={abortActiveRun} />
              </div>
            ) : choices.finished.length ? (
              // No run is shown, but finished runs can be reopened to read what they produced.
              <div className="run-banner idle" data-testid="run-banner-idle">
                <span>run</span>
                <span className="run-none">no open run</span>
                {runPicker('')}
              </div>
            ) : null}
        <div className={`viewport${showFlow ? ' flow-mode' : ''}`} data-testid="formations-canvas" ref={viewportRef} onPointerDownCapture={captureConnectedInputDrag} onPointerDown={onViewportPointerDown} onContextMenu={canvasMenu}>
          {showFlow && board ? (
            <Suspense fallback={null}>
              <FlowView board={board} agents={agents} notes={noteByNode} run={flowRun}
                answerPanel={pendingHumanGate && answerPanel ? { gateId: pendingHumanGate.gateId, panel: answerPanel } : null}
                onOpenNode={openNodeWindow} onOpenNotes={openNoteWindow} onStartMission={setStartMission} />
            </Suspense>
          ) : null}
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
                <div className="empty-title">No missions yet</div>
                <div className="empty-copy">Create a mission from the top bar to start sketching it.</div>
              </div>
            ) : (board.inputCards || []).length + board.formations.length + (board.gates || []).length + (board.tools || []).length + (board.ends || []).length === 0 ? (
              <div className="empty-board" data-testid="formations-empty-board">
                <div className="empty-title">This mission is empty</div>
                <div className="empty-copy">Add an Input card, a formation, a Gate or a Tool to sketch the workflow.</div>
              </div>
            ) : null}

            {(board?.inputCards || []).map((mission, index) => {
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
                    <span className="meyebrow">◆ Input</span>
                    <button className="mrun" title="Start mission" onClick={() => setStartMission(mission)} data-testid={`run-mission-${mission.id}`}>{PLAY_SVG}</button>
                  </div>
                  {renderNodeTitle(mission.title, 'mtitle', 'Untitled mission', 'div')}
                  <div className={`mgoal${mission.goal ? '' : ' placeholder'}`}>{mission.goal || 'set the mission objective…'}</div>
                  <div className={`mchannel ${humanChannelOf(mission)}`} title="How this mission's human gates reach you">Human gates · {humanChannelLabel(humanChannelOf(mission))}</div>
                  <ReferencedFiles nodeId={mission.id} files={nodeFileRefs(board, mission.id)} max={2} onMore={openReferencedFilesMenu} className="card-refs" />
                  <div className="mstatus">{state ? state : ''}</div>
                  <span className={`port pout ready${hoverPort === `${mission.id}:out` ? ' snaptarget' : ''}`} data-port-out={`${mission.id}:out`} title="Starts the chain — drag to a step" onPointerDown={event => beginWire(event, `${mission.id}:out`, 'wire')} />
                </div>
              )
            })}

            {(board?.formations || []).map((formation, index) => {
              const pos = positionOf(formation.id, index + (board?.inputCards?.length || 0))
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
              const nodeIndex = index + (board?.inputCards?.length || 0) + (board?.formations?.length || 0)
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
                + (board?.inputCards?.length || 0)
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

            {(board?.ends || []).map((end, index) => {
              const nodeIndex = index
                + (board?.inputCards?.length || 0)
                + (board?.formations?.length || 0)
                + (board?.gates?.length || 0)
                + (board?.tools?.length || 0)
              const pos = positionOf(end.id, nodeIndex)
              const state = nodeStates.get(end.id)
              const inputEndpoint = `${end.id}:in`
              const incoming = (board?.connections || []).filter(connection => connection.to === inputEndpoint)
              // One route in can be picked up and moved like any wire end; with several, grab the wire itself.
              const single = incoming.length === 1 ? incoming[0] : undefined
              return (
                <div
                  key={end.id}
                  className={`endcard end-${end.outcome}${state === 'done' || state === 'failed' ? ` ${state}` : ''}${locatedNodeId === end.id ? ' located' : ''}${noteByNode.has(end.id) ? ' has-note' : ''}${draftClass(end.id)}`}
                  data-node={end.id}
                  data-end={end.id}
                  data-testid={`end-node-${end.id}`}
                  title={endOutcomeMeaning(end.outcome)}
                  style={{ left: pos.x, top: pos.y }}
                  onPointerDown={event => beginNodeDrag(event, end.id, nodeIndex)}
                  onContextMenu={event => endMenu(event, end)}
                >
                  {renderNotePin(end.id, end.title || 'End')}
                  {renderDraftMarker(end.id)}
                  <span
                    className={`port pin${hoverPort === inputEndpoint ? ' snaptarget' : ''}${incoming.length ? ' has' : ''}`}
                    data-port-in={inputEndpoint}
                    data-reconnect-id={single?.id}
                    data-reconnect-from={single?.from}
                    title="Routes that end here; any number may"
                    onPointerDown={single ? event => beginReconnect(event, single) : undefined}
                    onMouseDown={single ? event => beginReconnect(event, single) : undefined}
                  />
                  <span className="eico">{END_SVG}</span>
                  <span className="emeta">
                    <span className="eeyebrow">End · {end.outcome}</span>
                    {renderNodeTitle(end.title, 'etitle', 'End', 'span')}
                  </span>
                </div>
              )
            })}
            <NoteLayer mode={notesMode} anchors={noteAnchors} notes={nodeNotes} onOpen={openNoteWindow} />
          </div>



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

          {!showFlow && board?.formations.length ? <StaffingKeyHint store={staffingStore} /> : null}
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
            anchor={target === BOARD_NOTE_TARGET ? boardNotesAnchor : () => (showFlow ? nodeAnchor(target) : noteWindowAnchor(worldRef.current, target))}
            keepClear={target === BOARD_NOTE_TARGET ? undefined : () => nodeWindowKeepClear(target, board.connections)}
            entries={(target === BOARD_NOTE_TARGET ? notes?.mission : noteByNode.get(target)) || []}
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
        {board ? roleWindows.map(roleId => {
          const role = agents.find(agent => agent.id === roleId)
          return role ? (
            <RoleWindow key={`role-${roleId}`} role={role} formations={board.formations}
              anchor={() => roleWindowAnchors.current.get(roleId) || null}
              onOpenNode={openNodeWindow}
              onEdit={trigger => setAgentEditor({ agent: role, trigger })}
              onClose={() => setRoleWindows(current => current.filter(open => open !== roleId))} />
          ) : null
        }) : null}
        {board ? nodeWindows.map(nodeId => (
          <Suspense key={`node-${nodeId}`} fallback={null}>
            <NodeWindow nodeId={nodeId} board={board} agents={agents} profiles={gateProfiles} ops={nodeWindowOps} noteCount={noteByNode.get(nodeId)?.length || 0}
              anchor={nodeWindowAnchors.current.get(nodeId)}
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
        {gateTalk.windows}
        {!showFlow && pendingHumanGate && answerWindowOpen ? (
          <GateAnswerWindow key={answerKey} gateId={pendingHumanGate.gateId} gateTitle={pendingHumanGate.title}
            focusRequest={answerWindow.key === answerKey ? answerWindow.focus : 0}
            anchor={() => cardRects([pendingHumanGate.gateId], worldRef.current || document)[0] || null}
            // Where Approve and Send back lead stays in view beside the answer.
            keepClear={() => cardRects((board?.connections || [])
              .filter(connection => connection.from === `${pendingHumanGate.gateId}:pass` || connection.from === `${pendingHumanGate.gateId}:fail`)
              .map(connection => connection.to.split(':')[0]), worldRef.current || document)}
            onClose={() => setAnswerWindow(current => ({ key: answerKey, closed: true, focus: current.focus }))}>
            {answerPanelFor(true)}
          </GateAnswerWindow>
        ) : null}
        <FileWindowsLayer />
      </WindowManagerProvider>

      {agentEditor ? (
        <PersonaEditorDialog
          key={agentEditor.agent.id}
          agent={agentEditor.agent}
          returnFocus={agentEditor.trigger}
          onClose={() => setAgentEditor(null)}
          onSaved={async () => setAgents((await fetchAgentRoster()).agents)}
        />
      ) : null}

      {startMission && <StartMissionDialog title={startMission.title} inputHint={startMission.inputHint} humanChannel={humanChannelOf(startMission)} onStart={(inputs, channel) => startMissionRun(startMission, inputs, channel)} onClose={() => setStartMission(null)} />}

      {deleteConfirm ? (
        <div
          className="pop board-dialog"
          role="alertdialog"
          aria-modal="true"
          aria-label={`Delete ${deleteConfirm.kind}`}
          aria-describedby="delete-confirm-reason"
          onPointerDown={event => event.stopPropagation()}
        >
          <div className="pop-head">
            <span className="pt">Delete {deleteConfirm.kind}</span>
            <button className="x" type="button" aria-label="Close delete dialog" onClick={() => setDeleteConfirm(null)}>x</button>
          </div>
          <div className="pop-body">
            <p id="delete-confirm-reason" className="board-delete-warning">
              <strong>{deleteConfirm.title || `This ${deleteConfirm.kind}`}</strong> cannot be put back after deleting, because {deleteConfirm.reason}. Ctrl+Z will not undo this delete.
            </p>
            <div className="pop-actions">
              <button autoFocus className="cancel" type="button" onClick={() => setDeleteConfirm(null)}>Cancel</button>
              <button className="retire" type="button" onClick={confirmDelete}>Delete for good</button>
            </div>
          </div>
        </div>
      ) : null}

      {boardDialog ? (
        <div
          className="pop board-dialog"
          role="dialog"
          aria-modal="true"
          aria-label={boardDialog.mode === 'create' ? 'Create mission' : boardDialog.mode === 'rename' ? 'Rename mission' : 'Delete mission'}
          onPointerDown={event => event.stopPropagation()}
        >
          <div className="pop-head">
            <span className="pt">{boardDialog.mode === 'create' ? 'Create mission' : boardDialog.mode === 'rename' ? 'Rename mission' : 'Delete mission'}</span>
            <button className="x" type="button" aria-label="Close mission dialog" disabled={boardDialog.saving} onClick={closeBoardDialog}>x</button>
          </div>
          {boardDialog.mode === 'delete' ? (
            <div className="pop-body">
              <p className="board-delete-warning">
                <strong>{boardDialog.title}</strong> will be removed from the mission list. Its definition and layout are archived, not destroyed; run history is untouched.
              </p>
              {boardDialog.error ? <p className="field-note error">{boardDialog.error}</p> : null}
              <div className="pop-actions">
                <button autoFocus className="cancel" type="button" disabled={boardDialog.saving} onClick={closeBoardDialog}>Cancel</button>
                <button className="retire" type="button" disabled={boardDialog.saving} onClick={() => void archiveSelectedBoard()}>
                  {boardDialog.saving ? 'Archiving…' : 'Archive mission'}
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
              <label htmlFor="formations-board-name">Mission name</label>
              <input
                id="formations-board-name"
                className="f"
                autoFocus
                value={boardDialog.title}
                aria-invalid={boardDialog.error ? 'true' : undefined}
                disabled={boardDialog.saving}
                onChange={event => setBoardDialog(current => current ? { ...current, title: event.target.value, error: '' } : current)}
              />
              <p className="field-note">The mission slug is derived from this name and remains stable after renaming.</p>
              {boardDialog.error ? <p className="field-note error">{boardDialog.error}</p> : null}
              <div className="pop-actions">
                <button className="cancel" type="button" disabled={boardDialog.saving} onClick={closeBoardDialog}>Cancel</button>
                <button className="save" type="submit" disabled={boardDialog.saving}>
                  {boardDialog.saving ? 'Saving…' : boardDialog.mode === 'create' ? 'Create mission' : 'Save mission name'}
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

      {menu ? <CanvasContextMenu menu={menu} onClose={closeMenu} /> : null}
      {active ? <StaffingLayer store={staffingStore} host={staffingHost} savedOf={savedOf} /> : null}

      {ghost ? (
        <div className="fmx-ghost staffing-ghost" style={{ left: ghost.x, top: ghost.y }}>{ghost.label}</div>
      ) : null}
      {gateGhost ? (
        <div className="gateghost" style={{ left: gateGhost.x, top: gateGhost.y }}>{GATE_SVG}</div>
      ) : null}
      {endGhost ? (
        <div className="gateghost endghost" style={{ left: endGhost.x, top: endGhost.y }}>{END_SVG}</div>
      ) : null}
    </div>
  )
  return (
    <FileWindowsProvider stack={windows}>
      <RunProducedProvider value={producedValue}>{cockpit}</RunProducedProvider>
    </FileWindowsProvider>
  )
}
