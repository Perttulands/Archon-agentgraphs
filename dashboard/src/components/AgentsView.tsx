import { FormEvent, useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react'
import type { ReactNode } from 'react'
import {
  ApiRequestError,
  fetchAgentRoster,
  fetchApi,
  fetchBoardDocument,
  fetchBoardLayout,
  fetchBoardSummaries,
  patchBoardDocument,
} from './formationsApi'
import { judgeChain } from '../flow/flowModel'
import { chooseCurrentBoard, rememberCurrentBoard } from './currentBoard'
import { runStatusLabel } from './formationsRunDiscovery'
import { projectNodeStates } from './formationsRunState'
import {
  FormationSeats,
  GATE_SVG,
  agentRole,
  byRoleName,
  formationSummary,
  inSlotsWords,
  initials,
  rolesInUseLabel,
} from './formationsCockpitVisuals'
import { slotStaffed, slotTitle, staffingSentence } from '../nodeWindow/staffing'
import { harnessName } from './harnessIcons'
import { SlotCaption, SlotFace, type Part } from '../staffing/SlotFace'
import { StaffingLayer } from '../staffing/StaffingLayer'
import { staff, type StaffingHost } from '../staffing/staffingActions'
import { captionText, modelWords, offCatalog, roleNamer, rolesOf, sameStaffing, slotSettings, staffingOf, type Staffing } from '../staffing/staffingModel'
import { StaffingStore, slotKey, useStaffingVersion, type SlotRef } from '../staffing/staffingStore'
import { GateKindChips } from './GateEditorDialog'
import PersonaEditorDialog from './PersonaEditorDialog'
import { boardsRunHref, useMissionRun, type MissionRunState } from './useMissionRun'
import type {
  AgentProjection as FormationAgentProjection,
  BoardDocument,
  EffortPolicyEntry,
  BoardSummary,
  FormationNode,
  FormationSlot,
  GateNode,
  LaunchableHarness,
  LayoutDocument,
  MissionNode,
  PersonaHarnessVariant,
} from './formationsTypes'

interface PersonaNote {
  ts: string
  actor: string
  text: string
}

interface RosterAgent {
  id: string
  displayName?: string
  kind?: string
  tags?: string[]
  harnessDefault?: string
  liveness?: string
  sessionId?: string
  status?: string
  contextPct?: number
  beadId?: string
  attached?: boolean
  assignable: boolean
  unbound?: boolean
  preset?: boolean
  customized?: boolean
}

interface PersonaCard {
  id: string
  displayName?: string
  kind: string
  summary?: string
  tags: string[]
  status?: string
  harnessDefault: string
  harnessVariants: PersonaHarnessVariant[]
  notes?: PersonaNote[]
  etag?: string
  toml?: string
}

type CachedPersona = {
  card?: PersonaCard
  etag: string
  error?: string
}

type Selection =
  | { kind: 'agent'; agentId: string }
  | { kind: 'unbound'; agentId: string }
  | { kind: 'slot'; formationId: string; slotId: string }

export type ReachableMissionItem = {
  kind: 'formation' | 'gate'
  id: string
  /** Steps from the mission along the wiring, counted at first discovery. */
  depth: number
  via?: BranchProvenance
}

type BranchLabels = { pass: string[]; fail: string[]; judge: string[] }

type BranchProvenance = {
  gateId: string
  /** `judge` marks a formation in the gate's judge chain. */
  branch: 'pass' | 'fail' | 'judge'
}

type CreateDraft = {
  id: string
  displayName: string
  kind: string
  harness: string
  sessionStem: string
  summary: string
  source: string
  capabilities: string
}

type AgentStatus = {
  liveness: 'live' | 'ambiguous' | 'offline'
  chips: string[]
  deployedSlots: number
}

const EMPTY_CREATE: CreateDraft = {
  id: '',
  displayName: '',
  kind: 'specialist',
  harness: 'claude-code',
  sessionStem: '',
  summary: '',
  source: '',
  capabilities: '',
}

/**
 * Everything a run of the mission can reach: formations and gates along
 * outputs and pass/fail routes, the judge chain of every reached gate, and the
 * work behind Tools (which have no slots, so they are walked, not listed).
 */
export function reachableMissionItems(board: BoardDocument, missionId: string): ReachableMissionItem[] {
  const formationIds = new Set(board.formations.map(formation => formation.id))
  const gateIds = new Set((board.gates || []).map(gate => gate.id))
  const formationById = new Map(board.formations.map(formation => [formation.id, formation]))
  const toolById = new Map((board.tools || []).map(tool => [tool.id, tool]))
  const outgoing = new Map<string, string[]>()
  for (const connection of board.connections || []) {
    const list = outgoing.get(connection.from) || []
    list.push(connection.to)
    outgoing.set(connection.from, list)
  }

  const queue: Array<{ endpoint: string; depth: number; via?: BranchProvenance }> = [{ endpoint: `${missionId}:out`, depth: 0 }]
  const seenEndpoints = new Set<string>()
  const result: ReachableMissionItem[] = []
  const seenNodes = new Map<string, ReachableMissionItem>()

  const recordNode = (kind: ReachableMissionItem['kind'], id: string, depth: number, via?: BranchProvenance) => {
    const key = `${kind}:${id}`
    const existing = seenNodes.get(key)
    if (!existing) {
      const item = via ? { kind, id, depth, via } : { kind, id, depth }
      seenNodes.set(key, item)
      result.push(item)
      return false
    }
    if (!sameProvenance(existing.via, via)) {
      delete existing.via
    }
    return true
  }

  while (queue.length > 0) {
    const nextEndpoint = queue.shift()
    if (!nextEndpoint) continue
    const endpointKey = `${nextEndpoint.endpoint}|${provenanceKey(nextEndpoint.via)}`
    if (seenEndpoints.has(endpointKey)) continue
    seenEndpoints.add(endpointKey)

    for (const next of outgoing.get(nextEndpoint.endpoint) || []) {
      const nodeId = endpointNode(next)
      const depth = nextEndpoint.depth + 1
      if (formationIds.has(nodeId)) {
        recordNode('formation', nodeId, depth, nextEndpoint.via)
        const formation = formationById.get(nodeId)
        const outputs = formation?.outputs?.length ? formation.outputs : [{ id: 'out' }]
        outputs.forEach(output => queue.push({ endpoint: `${nodeId}:${output.id}`, depth, via: nextEndpoint.via }))
        continue
      }
      if (gateIds.has(nodeId)) {
        // A judge chain returns to its gate's judge port; that is not a new arrival.
        if (next === `${nodeId}:judge`) continue
        const seenBefore = recordNode('gate', nodeId, depth, nextEndpoint.via)
        if (!seenBefore) {
          // Judges decide before either branch runs: one step past the gate, kept in chain order.
          judgeChain(board, nodeId)
            .filter(judgeId => formationIds.has(judgeId))
            .forEach(judgeId => recordNode('formation', judgeId, depth + 1, { gateId: nodeId, branch: 'judge' }))
        }
        queue.push(
          { endpoint: `${nodeId}:pass`, depth, via: { gateId: nodeId, branch: 'pass' } },
          { endpoint: `${nodeId}:fail`, depth, via: { gateId: nodeId, branch: 'fail' } },
        )
        continue
      }
      const tool = toolById.get(nodeId)
      if (tool) {
        const outputs = tool.outputs?.length ? tool.outputs : [{ id: 'out' }]
        outputs.forEach(output => queue.push({ endpoint: `${nodeId}:${output.id}`, depth, via: nextEndpoint.via }))
      }
    }
  }

  return result
}

function provenanceKey(via?: BranchProvenance): string {
  return via ? `${via.gateId}:${via.branch}` : 'main'
}

function sameProvenance(a?: BranchProvenance, b?: BranchProvenance): boolean {
  if (!a && !b) return true
  return Boolean(a && b && a.gateId === b.gateId && a.branch === b.branch)
}

function endpointNode(endpoint: string): string {
  return endpoint.split(':')[0] || endpoint
}

export function agentStatus(agent: RosterAgent, deployedSlots: number, details?: CachedPersona): AgentStatus {
  const rawLiveness = agent.liveness === 'live' || agent.liveness === 'ambiguous' ? agent.liveness : 'offline'
  const chips: string[] = []
  if (agent.unbound) chips.push('no persona')
  if (!agent.unbound && details?.card?.status === 'retired') chips.push('retired')
  if (!agent.unbound && !agent.assignable && details?.card?.status !== 'retired') chips.push('not assignable')
  if (deployedSlots > 0) chips.push(inSlotsWords(deployedSlots))
  if (agent.attached) chips.push('attached')
  return { liveness: rawLiveness, chips, deployedSlots }
}

export default function AgentsView() {
  const [agents, setAgents] = useState<RosterAgent[]>([])
  const [harnesses, setHarnesses] = useState<LaunchableHarness[]>([])
  const [effortPolicy, setEffortPolicy] = useState<EffortPolicyEntry[]>([])
  const [boards, setBoards] = useState<BoardSummary[]>([])
  const [selectedSlug, setSelectedSlug] = useState('')
  const [board, setBoard] = useState<BoardDocument | null>(null)
  const [layout, setLayout] = useState<LayoutDocument | null>(null)
  const [selectedMissionId, setSelectedMissionId] = useState('')
  const [details, setDetails] = useState<Record<string, CachedPersona>>({})
  const [selection, setSelection] = useState<Selection | null>(null)
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(true)
  const [boardLoading, setBoardLoading] = useState(false)
  const [error, setError] = useState('')
  const [boardError, setBoardError] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [createDraft, setCreateDraft] = useState<CreateDraft>(EMPTY_CREATE)
  const [noteDraft, setNoteDraft] = useState('')
  const [editingPersona, setEditingPersona] = useState<{ agent: RosterAgent; trigger: HTMLElement | null } | null>(null)

  const selectedMission = board?.inputCards?.find(mission => mission.id === selectedMissionId) || null
  // Boards is the run console; this tab only reports the mission's run.
  const missionRun = useMissionRun(board?.slug || '', selectedMission?.id || '')
  const nodeStates = useMemo(() => projectNodeStates(missionRun.events, missionRun.run), [missionRun.events, missionRun.run])

  const reachableItems = useMemo(() => {
    if (!board || !selectedMissionId) return []
    return orderReachableItems(reachableMissionItems(board, selectedMissionId), layout)
  }, [board, layout, selectedMissionId])

  const reachableViaByFormation = useMemo(() => {
    const map = new Map<string, BranchProvenance>()
    for (const item of reachableItems) {
      if (item.kind === 'formation' && item.via) map.set(item.id, item.via)
    }
    return map
  }, [reachableItems])

  const gateBranchLabels = useMemo(() => {
    const map = new Map<string, BranchLabels>()
    if (!board) return map
    const formationTitles = new Map(board.formations.map(formation => [formation.id, formation.title]))
    for (const item of reachableItems) {
      if (item.kind !== 'formation' || !item.via) continue
      const labels = map.get(item.via.gateId) || { pass: [], fail: [], judge: [] }
      labels[item.via.branch].push(formationTitles.get(item.id) || item.id)
      map.set(item.via.gateId, labels)
    }
    return map
  }, [board, reachableItems])

  const reachableFormations = useMemo(() => {
    if (!board) return []
    const byId = new Map(board.formations.map(formation => [formation.id, formation]))
    return reachableItems
      .filter(item => item.kind === 'formation')
      .map(item => byId.get(item.id))
      .filter((formation): formation is FormationNode => Boolean(formation))
  }, [board, reachableItems])

  // Roles in use are counted across the whole mission, as the Missions rail counts them.
  const assignmentsByAgent = useMemo(() => {
    const map = new Map<string, Array<{ formation: FormationNode; slot: FormationSlot }>>()
    for (const formation of board?.formations || []) {
      for (const slot of formation.slots || []) {
        if (!slot.agentId) continue
        const list = map.get(slot.agentId) || []
        list.push({ formation, slot })
        map.set(slot.agentId, list)
      }
    }
    return map
  }, [board?.formations])

  const slotCounts = useMemo(() => {
    let total = 0
    let staffed = 0
    for (const formation of reachableFormations) {
      for (const slot of formation.slots || []) {
        total += 1
        if (slotStaffed(slot)) staffed += 1
      }
    }
    return { total, staffed, open: Math.max(total - staffed, 0) }
  }, [reachableFormations])

  const rosterCounts = useMemo(() => ({
    total: agents.length,
    live: agents.filter(agent => agent.liveness === 'live').length,
    assignable: agents.filter(agent => !agent.unbound && agent.assignable).length,
    deployed: assignmentsByAgent.size,
  }), [agents, assignmentsByAgent])

  const filteredAgents = useMemo(() => {
    const needle = search.trim().toLowerCase()
    if (!needle) return agents
    return agents.filter(agent => [
      agent.id,
      agent.displayName || '',
      agent.kind || '',
      ...(agent.tags || []),
    ].some(value => value.toLowerCase().includes(needle)))
  }, [agents, search])

  const personas = filteredAgents.filter(agent => !agent.unbound).sort(byRoleName)
  const unbound = filteredAgents.filter(agent => agent.unbound)

  const selectedSlot = useMemo(() => {
    if (selection?.kind !== 'slot') return null
    return findSlot(board, selection.formationId, selection.slotId)
  }, [board, selection])

  const loadAgents = useCallback(async () => {
    const roster = await fetchAgentRoster()
    setAgents(roster.agents as RosterAgent[])
    setHarnesses(roster.harnesses)
    setEffortPolicy(roster.effortPolicy)
  }, [])

  const loadBoards = useCallback(async () => {
    const nextBoards = await fetchBoardSummaries()
    setBoards(nextBoards)
    setSelectedSlug(current => nextBoards.some(next => next.slug === current) ? current : chooseCurrentBoard(nextBoards.map(next => next.slug), window.location.search).slug)
  }, [])

  const loadBoard = useCallback(async (slug: string) => {
    setBoardLoading(true)
    try {
      const [nextBoard, nextLayout] = await Promise.all([
        fetchBoardDocument(slug),
        fetchBoardLayout(slug),
      ])
      setBoard(nextBoard)
      setLayout(nextLayout)
      setSelectedMissionId(current => {
        if (current && nextBoard.inputCards?.some(mission => mission.id === current)) return current
        return nextBoard.inputCards?.[0]?.id || ''
      })
      setBoardError('')
      setError('')
    } catch (err) {
      setBoardError(err instanceof Error ? err.message : 'Failed to load mission')
    } finally {
      setBoardLoading(false)
    }
  }, [])

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      await Promise.all([loadAgents(), loadBoards()])
      if (selectedSlug) await loadBoard(selectedSlug)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Agents request failed')
    } finally {
      setLoading(false)
    }
  }, [loadAgents, loadBoard, loadBoards, selectedSlug])

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      setLoading(true)
      try {
        const [roster, nextBoards] = await Promise.all([fetchAgentRoster(), fetchBoardSummaries()])
        if (cancelled) return
        setAgents(roster.agents as RosterAgent[])
        setHarnesses(roster.harnesses)
        setEffortPolicy(roster.effortPolicy)
        setBoards(nextBoards)
        // Boards and Agents share one current board: the link's, else the last used here.
        const { slug, missingLinked } = chooseCurrentBoard(nextBoards.map(next => next.slug), window.location.search)
        setSelectedSlug(current => current || slug)
        setError(missingLinked && nextBoards.length ? `Mission "${missingLinked}" from the link was not found` : '')
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Agents request failed')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    if (!selectedSlug) {
      setBoard(null)
      setLayout(null)
      return
    }
    rememberCurrentBoard(selectedSlug)
    void loadBoard(selectedSlug)
  }, [loadBoard, selectedSlug])

  const loadAgentDetail = useCallback(async (agentId: string, force = false): Promise<CachedPersona> => {
    if (!force && details[agentId]) return details[agentId]
    const result = await fetchApi<PersonaCard>(`/api/agents/${encodeURIComponent(agentId)}`)
    const cached: CachedPersona = {
      card: { ...result.data, etag: result.etag || result.data.etag },
      etag: result.etag || result.data.etag || '',
    }
    setDetails(current => ({ ...current, [agentId]: cached }))
    return cached
  }, [details])

  const inspectAgent = useCallback(async (agent: RosterAgent) => {
    if (agent.unbound) {
      setSelection({ kind: 'unbound', agentId: agent.id })
      setNoteDraft('')
      return
    }
    setSelection({ kind: 'agent', agentId: agent.id })
    setNoteDraft('')
    try {
      await loadAgentDetail(agent.id, Boolean(details[agent.id]?.error))
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Agent inspect request failed')
    }
  }, [details, loadAgentDetail])

  const inspectSlot = useCallback((formation: FormationNode, slot: FormationSlot) => {
    setSelection({ kind: 'slot', formationId: formation.id, slotId: slot.id })
    setError('')
  }, [])

  // Staffing here is the canvas's sentence (archon-o7p.17): each write states the slot in full.
  const staffingStore = useMemo(() => new StaffingStore(), [])
  const boardRef = useRef(board)
  boardRef.current = board
  const savedOf = useCallback((ref: Pick<SlotRef, 'formationId' | 'slotId'>): Staffing | null => {
    const found = findSlot(boardRef.current, ref.formationId, ref.slotId)
    return found ? staffingOf(found.slot) : null
  }, [])
  const saveStaffing = useCallback(async (ref: SlotRef, next: Staffing | null): Promise<string | null> => {
    const current = boardRef.current
    if (!current) return 'No mission is open.'
    if (sameStaffing(savedOf(ref), next)) return null
    try {
      const result = await patchBoardDocument(current.slug, current.etag, current.rev, { assignSlot: { formationId: ref.formationId, slotId: ref.slotId, ...slotSettings(next) } })
      setBoard(result.board)
      if (result.layout) setLayout(result.layout)
      setError('')
      await loadAgents()
      return null
    } catch (err) {
      if (err instanceof ApiRequestError && (err.status === 409 || err.status === 428)) return 'The mission changed; reload and retry.'
      return err instanceof Error ? err.message : 'Mission update failed'
    }
  }, [loadAgents, savedOf])
  const staffingHost = useMemo<StaffingHost>(() => ({
    catalog: { harnesses, policy: effortPolicy, roles: rolesOf(agents as FormationAgentProjection[]) },
    save: saveStaffing,
  }), [agents, effortPolicy, harnesses, saveStaffing])
  const slotRefOf = (formation: FormationNode, slot: FormationSlot): SlotRef => ({
    key: slotKey(formation.id, slot.id), formationId: formation.id, slotId: slot.id, label: slot.label || slot.id, step: formation.title,
  })
  const openStaffing = (formation: FormationNode, slot: FormationSlot, part: Part | null, anchor: Element) => {
    staffingStore.setOpen({ ref: slotRefOf(formation, slot), part, anchor })
  }
  const emptySlot = (formation: FormationNode, slot: FormationSlot) => {
    void staff(staffingStore, staffingHost, slotRefOf(formation, slot), staffingOf(slot), null)
  }

  const createPersona = useCallback(async (event: FormEvent) => {
    event.preventDefault()
    const capabilities = splitCommaList(createDraft.capabilities)
    try {
      const result = await fetchApi<PersonaCard>('/api/agents', {
        method: 'POST',
        body: JSON.stringify({
          id: createDraft.id.trim(),
          displayName: createDraft.displayName.trim(),
          kind: createDraft.kind.trim(),
          harness: createDraft.harness.trim(),
          sessionStem: createDraft.sessionStem.trim(),
          summary: createDraft.summary.trim(),
          source: createDraft.source.trim(),
          capabilities,
        }),
      })
      const cached = { card: { ...result.data, etag: result.etag || result.data.etag }, etag: result.etag || result.data.etag || '' }
      setDetails(current => ({ ...current, [result.data.id]: cached }))
      setSelection({ kind: 'agent', agentId: result.data.id })
      setCreateDraft(EMPTY_CREATE)
      setCreateOpen(false)
      setError('')
      await loadAgents()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Agent create request failed')
    }
  }, [createDraft, loadAgents])

  const saveNote = useCallback(async (agentId: string) => {
    const note = noteDraft.trim()
    if (!note) return
    try {
      const cached = details[agentId]?.card ? details[agentId] : await loadAgentDetail(agentId, Boolean(details[agentId]?.error))
      if (!cached.card) {
        setError('Agent detail unavailable; reload and retry')
        return
      }
      const result = await fetchApi<PersonaCard>(`/api/agents/${encodeURIComponent(agentId)}`, {
        method: 'PATCH',
        headers: { 'If-Match': cached.etag },
        body: JSON.stringify({ note }),
      })
      setDetails(current => ({
        ...current,
        [agentId]: {
          card: { ...result.data, etag: result.etag || result.data.etag },
          etag: result.etag || result.data.etag || '',
        },
      }))
      setNoteDraft('')
      setError('')
      await loadAgents()
    } catch (err) {
      if (err instanceof ApiRequestError && err.status === 428) {
        setError(`Programming error: ${err.message}`)
        return
      }
      if (err instanceof ApiRequestError && err.status === 409) {
        setError(err.message || 'Agent card changed; reload and retry')
        return
      }
      setError(err instanceof Error ? err.message : 'Agent update request failed')
    }
  }, [details, loadAgentDetail, loadAgents, noteDraft])

  const createFromUnbound = useCallback((agent: RosterAgent) => {
    const sessionStem = agent.sessionId || agent.id
    setCreateDraft({
      ...EMPTY_CREATE,
      id: agent.id,
      displayName: agent.displayName || agent.id,
      harness: inferHarnessFromSession(sessionStem),
      sessionStem,
      capabilities: (agent.tags || []).join(', '),
    })
    setCreateOpen(true)
  }, [])

  const selectedAgentId = selection?.kind === 'agent' || selection?.kind === 'unbound' ? selection.agentId : ''
  const rosterSummary = rolesInUseLabel(loading ? '…' : rosterCounts.assignable, rosterCounts.deployed, rosterCounts.live)

  return (
    <div className="fmx agx" data-testid="agents-view">
      <div className="topbar">
        <div className="boardpick">
          mission
          <select aria-label="Mission" value={selectedSlug} onChange={event => setSelectedSlug(event.target.value)} disabled={loading || boards.length === 0}>
            {boards.length === 0 && <option value="">No missions</option>}
            {boards.map(next => (
              <option key={next.slug} value={next.slug}>{next.title || next.slug}</option>
            ))}
          </select>
          {board ? <span className="rev">rev {board.rev}</span> : null}
        </div>
        <div className="sep" />
        <div className="boardpick">
          input
          <select
            aria-label="Input card"
            value={selectedMissionId}
            onChange={event => setSelectedMissionId(event.target.value)}
            disabled={boardLoading || !board?.inputCards?.length}
          >
            {!board?.inputCards?.length && <option value="">No Input card</option>}
            {(board?.inputCards || []).map(mission => (
              <option key={mission.id} value={mission.id}>{mission.title}</option>
            ))}
          </select>
        </div>
        <div className="spacer" />
        <button className="board-action" type="button" onClick={refresh} disabled={loading || boardLoading}>
          Refresh
        </button>
        <button className="newbtn" type="button" onClick={() => setCreateOpen(true)}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
          New agent
        </button>
      </div>

      {error && <div className="agx-alert" role="alert">{error}</div>}

      <div className="main">
        <aside className="roster" aria-label="Agent roster">
          <div className="roster-hd">
            <div className="t">Agents</div>
            <span
              className="s"
              title={`${rosterCounts.assignable} roles you can staff; ${rosterCounts.deployed} of them staff slots in this mission; ${rosterCounts.live} live sessions`}
            >
              {rosterSummary}
            </span>
          </div>
          <input
            className="agx-filter"
            aria-label="Filter agents"
            value={search}
            onChange={event => setSearch(event.target.value)}
            placeholder="filter agents"
          />
          <div className="roster-list">
            {!loading && personas.length === 0 && (
              <div className="roster-empty">
                {search.trim() ? 'No personas match this filter.' : 'No personas yet. Use New agent to create one.'}
              </div>
            )}
            {personas.length > 0 && (
              <RosterGroup
                id="roles"
                label="Roles"
                agents={personas}
                details={details}
                assignmentsByAgent={assignmentsByAgent}
                selectedAgentId={selectedAgentId}
                onInspect={inspectAgent}
              />
            )}
            {unbound.length > 0 && (
              <RosterGroup
                id="unbound"
                label="Unbound"
                agents={unbound}
                details={details}
                assignmentsByAgent={assignmentsByAgent}
                selectedAgentId={selectedAgentId}
                onInspect={inspectAgent}
              />
            )}
          </div>
        </aside>

        <main className="agx-staffing" aria-label="Mission staffing">
          {boardError && (
            <div className="agx-alert agx-board-alert" role="alert">
              <span>Mission load failed: {boardError}</span>
              <button className="board-action" type="button" onClick={() => selectedSlug && loadBoard(selectedSlug)}>
                Retry board
              </button>
            </div>
          )}
          {board && selectedMission && (
            <MissionRunState board={board} missionRun={missionRun} />
          )}
          <div className="agx-cards">
            {!loading && !boardLoading && !selectedMission && (
              <StaffingEmpty
                title={boards.length === 0 ? 'No missions' : 'This mission has no Input card'}
                copy={boards.length === 0
                  ? 'Create a mission on the Missions tab to staff it.'
                  : 'Add an Input card on the Missions tab and wire it to a formation.'}
              />
            )}
            {selectedMission && (
              <MissionCard mission={selectedMission} slotCounts={slotCounts} />
            )}
            {selectedMission && reachableItems.length === 0 && (
              <StaffingEmpty
                title="Nothing wired to this mission"
                copy="Wire the Input card to a formation on the Missions tab to staff it here."
              />
            )}
            {selectedMission && reachableItems.map(item => (
              item.kind === 'gate'
                ? (
                  <GateRow
                    key={item.id}
                    gate={(board?.gates || []).find(gate => gate.id === item.id) || null}
                    state={nodeStates.get(item.id) || ''}
                    branchLabels={gateBranchLabels.get(item.id)}
                  />
                )
                : (
                  <FormationStaffingCard
                    key={item.id}
                    formation={board?.formations.find(formation => formation.id === item.id) || null}
                    via={reachableViaByFormation.get(item.id)}
                    viaGateTitle={(board?.gates || []).find(gate => gate.id === item.via?.gateId)?.title || item.via?.gateId || ''}
                    store={staffingStore}
                    host={staffingHost}
                    nodeState={nodeStates.get(item.id) || ''}
                    selectedSlot={selectedSlot}
                    onSlotClick={inspectSlot}
                  />
                )
            ))}
          </div>
        </main>

        {selection && (
          <aside className="agx-inspector" aria-label="Inspector">
            <Inspector
              selection={selection}
              agents={agents}
              details={details}
              assignmentsByAgent={assignmentsByAgent}
              selectedSlot={selectedSlot}
              noteDraft={noteDraft}
              onNoteDraft={setNoteDraft}
              onSaveNote={saveNote}
              onStaff={openStaffing}
              onEmpty={emptySlot}
              onCreateFromUnbound={createFromUnbound}
              onEditPersona={(agent, trigger) => setEditingPersona({ agent, trigger })}
              onClose={() => setSelection(null)}
            />
          </aside>
        )}
      </div>

      {editingPersona && (
        <PersonaEditorDialog
          key={editingPersona.agent.id}
          agent={editingPersona.agent}
          returnFocus={editingPersona.trigger}
          onClose={() => setEditingPersona(null)}
          onSaved={async () => {
            await loadAgents()
            await loadAgentDetail(editingPersona.agent.id, true)
          }}
        />
      )}

      <StaffingLayer store={staffingStore} host={staffingHost} savedOf={savedOf} />

      {createOpen && (
        <CreatePersonaPopover
          draft={createDraft}
          onDraft={setCreateDraft}
          onSubmit={createPersona}
          onClose={() => setCreateOpen(false)}
        />
      )}
    </div>
  )
}

/** Wiring order from the mission; canvas position only orders parallel branches at the same depth. */
export function orderReachableItems(items: ReachableMissionItem[], layout: LayoutDocument | null): ReachableMissionItem[] {
  const position = new Map((layout?.nodes || []).map(node => [node.id, node]))
  return [...items].sort((a, b) => {
    if (a.depth !== b.depth) return a.depth - b.depth
    // A gate's judges decide before its branches run.
    const aJudge = a.via?.branch === 'judge'
    if (aJudge !== (b.via?.branch === 'judge')) return aJudge ? -1 : 1
    if (aJudge && a.via?.gateId === b.via?.gateId) return items.indexOf(a) - items.indexOf(b)
    const ap = position.get(a.id)
    const bp = position.get(b.id)
    if (ap && bp && (ap.y !== bp.y || ap.x !== bp.x)) return ap.y === bp.y ? ap.x - bp.x : ap.y - bp.y
    if (ap && !bp) return -1
    if (!ap && bp) return 1
    return items.indexOf(a) - items.indexOf(b)
  })
}

function RosterGroup({
  id,
  label,
  agents,
  details,
  assignmentsByAgent,
  selectedAgentId,
  onInspect,
}: {
  id: string
  label: string
  agents: RosterAgent[]
  details: Record<string, CachedPersona>
  assignmentsByAgent: Map<string, Array<{ formation: FormationNode; slot: FormationSlot }>>
  selectedAgentId: string
  onInspect: (agent: RosterAgent) => void
}) {
  return (
    <section className="roster-group" data-provider={id}>
      <div className="roster-group-label">{label}</div>
      {agents.map(agent => {
        const status = agentStatus(agent, assignmentsByAgent.get(agent.id)?.length || 0, details[agent.id])
        const name = agent.displayName || agent.id
        const selected = selectedAgentId === agent.id
        return (
          <button
            key={agent.id}
            type="button"
            className={`ragent${status.deployedSlots > 0 ? ' deployed' : ''}${agent.unbound ? ' unbound' : ''}${selected ? ' selected' : ''}`}
            aria-label={`Inspect ${name}`}
            aria-pressed={selected}
            onClick={() => onInspect(agent)}
          >
            <span className="av">{initials(name)}</span>
            <span className="ri">
              <span className="n">{name}</span>
              <StatusWords agent={agent} status={status} />
            </span>
          </button>
        )
      })}
    </section>
  )
}

/* Offline is the resting state, so it is not repeated on every row. In use
   comes first, so a narrow rail never cuts it off. */
function StatusWords({ agent, status }: { agent: RosterAgent; status: AgentStatus }) {
  const inUse = inSlotsWords(status.deployedSlots)
  const words = [
    ...(inUse ? [inUse] : []),
    ...(agent.unbound ? [] : [agentRole(agent as FormationAgentProjection)]),
    ...(agent.preset ? [agent.customized ? 'custom' : 'preset'] : []),
    ...(status.liveness === 'offline' ? [] : [status.liveness]),
    ...status.chips.filter(chip => chip !== inUse),
  ]
  return (
    <span className="r">
      {words.map(word => <span key={word} className={word === status.liveness ? `is-${word}` : word === inUse ? 'in-use-words' : undefined}>{word}</span>)}
    </span>
  )
}

function MissionCard({ mission, slotCounts }: {
  mission: MissionNode
  slotCounts: { total: number; staffed: number; open: number }
}) {
  const readiness = slotCounts.total === 0
    ? 'no slots on this mission'
    : `${slotCounts.staffed}/${slotCounts.total} slots staffed · ${slotCounts.open > 0 ? `${slotCounts.open} open` : 'ready'}`
  return (
    <section className="missioncard">
      <div className="mhd">
        <span className="meyebrow">◆ Input</span>
      </div>
      <div className="mtitle">{mission.title}</div>
      <div className={`mgoal${mission.goal ? '' : ' placeholder'}`}>{mission.goal || 'set the mission objective…'}</div>
      <div className={`mstatus${slotCounts.open > 0 || slotCounts.total === 0 ? ' is-open' : ''}`}>{readiness}</div>
    </section>
  )
}

function StaffingEmpty({ title, copy }: { title: string; copy: string }) {
  return (
    <div className="empty-board">
      <div className="empty-title">{title}</div>
      <div className="empty-copy">{copy}</div>
    </div>
  )
}

function FormationStaffingCard({
  formation,
  via,
  viaGateTitle,
  store,
  host,
  nodeState,
  selectedSlot,
  onSlotClick,
}: {
  formation: FormationNode | null
  via?: BranchProvenance
  viaGateTitle: string
  store: StaffingStore
  host: StaffingHost
  nodeState: string
  selectedSlot: { formation: FormationNode; slot: FormationSlot } | null
  onSlotClick: (formation: FormationNode, slot: FormationSlot) => void
}) {
  if (!formation) return null
  const open = formation.slots.filter(slot => !slotStaffed(slot)).length
  const fallbackLabel = via?.branch === 'fail'
    ? `fallback on ${viaGateTitle || via.gateId} fail`
    : via?.branch === 'judge' ? `judges ${viaGateTitle || via.gateId}` : ''
  const summary = formationSummary(formation)
  return (
    <section className={`formation type-${formation.type}${nodeState === 'running' ? ' running' : ''}${nodeState ? ` state-${nodeState}` : ''}`}>
      <div className="fhead">
        <div className="ft">
          <div className="tool-kind">{formation.type}</div>
          <div className="tt">{formation.title}</div>
          <div className="tg" title={summary}>{summary}</div>
        </div>
        <span className={`io-status ${open > 0 ? 'review' : 'done'}`}>{open > 0 ? `${open} open` : 'staffed'}</span>
      </div>
      {fallbackLabel && <div className="fstatus agx-fallback">{fallbackLabel}</div>}
      <div className="fbody">
        <FormationSeats
          formation={formation}
          renderSlot={(slot, badge) => (
            <StaffingSeat
              formation={formation}
              slot={slot}
              badge={badge}
              store={store}
              host={host}
              nodeState={nodeState}
              selected={selectedSlot?.formation.id === formation.id && selectedSlot.slot.id === slot.id}
              onClick={onSlotClick}
            />
          )}
        />
      </div>
    </section>
  )
}

function StaffingSeat({
  formation,
  slot,
  badge,
  store,
  host,
  nodeState,
  selected,
  onClick,
}: {
  formation: FormationNode
  slot: FormationSlot
  badge?: number
  store: StaffingStore
  host: StaffingHost
  nodeState: string
  selected: boolean
  onClick: (formation: FormationNode, slot: FormationSlot) => void
}) {
  useStaffingVersion(store)
  const key = slotKey(formation.id, slot.id)
  const saved = staffingOf(slot)
  const staffing = store.shown(key, saved)
  const roleName = (id: string) => host.catalog.roles.find(role => role.id === id)?.name || id
  const classes = [
    'slot',
    staffing ? 'filled' : 'empty',
    slot.controller ? 'ctrl' : '',
    nodeState === 'running' ? 'active' : '',
    nodeState === 'done' ? 'active done' : '',
    selected ? 'selected' : '',
  ]
  return (
    <button
      type="button"
      className={classes.filter(Boolean).join(' ')}
      aria-label={`Inspect ${slotTitle(slot)}: ${staffing ? `${staffing.role ? roleName(staffing.role) : 'vanilla'} on ${captionText(staffing)}` : 'not staffed'}`}
      aria-pressed={selected}
      data-slot-key={key}
      data-testid={`agents-slot-${formation.id}-${slot.id}`}
      onClick={() => onClick(formation, slot)}
    >
      <SlotFace label={slot.label} badge={badge} staffing={staffing} landed={store.landed(key)} note={store.stamp?.key === key ? store.stamp : null}
        marks={staffing && offCatalog(host.catalog, staffing) ? <span className="slot-warn">model not in catalog</span> : null}
        caption={<SlotCaption shown={staffing} saved={store.current(key, saved)} drafting={store.drafting(key)} landed={store.landed(key)} roleName={roleName} />} />
    </button>
  )
}

function GateRow({
  gate,
  state,
  branchLabels,
}: {
  gate: GateNode | null
  state: string
  branchLabels?: BranchLabels
}) {
  if (!gate) return null
  const title = gate.title || 'Gate'
  const criterion = gate.criterion || 'work is accepted before it proceeds'
  return (
    <section className={`gatecard${state ? ` state-${state}` : ''}`} data-gate={gate.id}>
      <span className="gico">{GATE_SVG}</span>
      <span className="gmeta">
        <span className="gt">{title}</span>
        <GateKindChips gateId={gate.id} kinds={gate.kinds} />
        <span className="gs" title={criterion}>{criterion}</span>
        {Boolean(branchLabels?.pass.length || branchLabels?.fail.length || branchLabels?.judge.length) && (
          <span className="agx-branches" aria-label={`${title} branch targets`}>
            {branchLabels?.judge.length ? <span className="judge">judged by {branchLabels.judge.join(' → ')}</span> : null}
            {branchLabels?.pass.length ? <span className="pass">pass → {branchLabels.pass.join(', ')}</span> : null}
            {branchLabels?.fail.length ? <span className="fail">fail → {branchLabels.fail.join(', ')}</span> : null}
          </span>
        )}
      </span>
    </section>
  )
}

function InspectorPanel({ title, meta, onClose, children }: { title: string; meta: string; onClose: () => void; children: ReactNode }) {
  return (
    <>
      <div className="board-notes-head">
        <div className="agx-inspector-heading">
          <div className="board-notes-title">{title}</div>
          <div className="board-notes-meta">{meta}</div>
        </div>
        <button className="board-notes-toggle" type="button" aria-label="Close inspector" onClick={onClose}>×</button>
      </div>
      <div className="board-notes-body">{children}</div>
    </>
  )
}

function Inspector({
  selection,
  agents,
  details,
  assignmentsByAgent,
  selectedSlot,
  noteDraft,
  onNoteDraft,
  onSaveNote,
  onStaff,
  onEmpty,
  onCreateFromUnbound,
  onEditPersona,
  onClose,
}: {
  selection: Selection
  agents: RosterAgent[]
  details: Record<string, CachedPersona>
  assignmentsByAgent: Map<string, Array<{ formation: FormationNode; slot: FormationSlot }>>
  selectedSlot: { formation: FormationNode; slot: FormationSlot } | null
  noteDraft: string
  onNoteDraft: (value: string) => void
  onSaveNote: (agentId: string) => void
  onStaff: (formation: FormationNode, slot: FormationSlot, part: Part | null, anchor: Element) => void
  onEmpty: (formation: FormationNode, slot: FormationSlot) => void
  onCreateFromUnbound: (agent: RosterAgent) => void
  onEditPersona: (agent: RosterAgent, trigger: HTMLElement) => void
  onClose: () => void
}) {
  if (selection.kind === 'unbound') {
    const agent = agents.find(next => next.id === selection.agentId)
    return (
      <InspectorPanel title={agent?.displayName || selection.agentId} meta="unbound session" onClose={onClose}>
        <section className="note-section">
          <p className="note-empty">This live session has no persona card. It cannot be assigned until a persona exists.</p>
          <KeyValues rows={[
            ['Liveness', agent?.liveness || 'live'],
            ['Session', agent?.sessionId || agent?.id || selection.agentId],
          ]} />
          {agent && (
            <div className="board-dialog-actions">
              <button className="primary" type="button" onClick={() => onCreateFromUnbound(agent)}>
                Create persona from this session
              </button>
            </div>
          )}
        </section>
      </InspectorPanel>
    )
  }

  if (selection.kind === 'agent') {
    const agent = agents.find(next => next.id === selection.agentId)
    const detail = details[selection.agentId]
    const card = detail?.card
    const assignments = assignmentsByAgent.get(selection.agentId) || []
    const states = [
      agent?.liveness || 'offline',
      card?.status || '',
      agent?.attached ? 'attached' : '',
      !agent?.assignable && !card?.status ? 'not assignable' : '',
    ].filter(Boolean)
    return (
      <InspectorPanel title={card?.displayName || agent?.displayName || selection.agentId} meta={card?.kind || agent?.kind || 'agent'} onClose={onClose}>
        <section className="note-section">
          <div className="agx-identity">
            <span className="av">{initials(card?.displayName || agent?.displayName || selection.agentId)}</span>
            <span className="ri">
              <span className="n">{inSlotsWords(assignments.length) || 'in no slot on this mission'}</span>
              <span className="r">{states.map(state => <span key={state} className={`is-${state}`}>{state}</span>)}</span>
            </span>
            {agent ? (
              <button className="board-action" type="button" onClick={event => onEditPersona(agent, event.currentTarget)}>Edit persona</button>
            ) : null}
          </div>
          {card?.summary && <p className="agx-summary">{card.summary}</p>}
          <KeyValues rows={[
            ['Session', agent?.sessionId || ''],
            ['Context', typeof agent?.contextPct === 'number' ? `${agent.contextPct}%` : ''],
            ['Bead', agent?.beadId || ''],
          ]} />
          <TagList tags={card?.tags || agent?.tags || []} />
        </section>
        <section className="note-section">
          <h3>Slots on this mission</h3>
          {assignments.length === 0 && <p className="note-empty">No slots on this mission.</p>}
          {/* A role is role text; each slot it staffs says what that seat runs. */}
          <div className="tool-detail-list">
            {assignments.map(({ formation, slot }) => {
              const staffing = staffingOf(slot)
              const label = `${slot.label}${slot.controller && slot.label.toLowerCase() !== 'controller' && formation.type !== 'solo' ? ' · controller' : ''}`
              return (
                <div className="tool-detail-row agx-role-slot" key={`${formation.id}:${slot.id}`}>
                  <span>{formation.title}{slot.label !== formation.title ? <> · <strong>{label}</strong></> : null}</span>
                  <span className="agx-role-slot-runs">{staffing ? captionText(staffing) : ''}</span>
                </div>
              )
            })}
          </div>
        </section>
        <form
          className="note-section"
          onSubmit={event => {
            event.preventDefault()
            onSaveNote(selection.agentId)
          }}
        >
          <label htmlFor="agx-note">Add note</label>
          <textarea id="agx-note" value={noteDraft} onChange={event => onNoteDraft(event.target.value)} />
          <div className="board-dialog-actions">
            <button className="primary" type="submit">Save note</button>
          </div>
        </form>
      </InspectorPanel>
    )
  }

  if (selectedSlot) {
    return (
      <SlotInspector
        agents={agents}
        formation={selectedSlot.formation}
        slot={selectedSlot.slot}
        onStaff={onStaff}
        onEmpty={onEmpty}
        onClose={onClose}
      />
    )
  }

  return (
    <InspectorPanel title="Slot" meta="no longer in this mission" onClose={onClose}>
      <p className="note-empty">This slot was removed from the mission.</p>
    </InspectorPanel>
  )
}

/** A slot's staffing as its sentence; each word drops the staffing window from itself (archon-o7p.17). */
function SlotInspector({
  agents,
  formation,
  slot,
  onStaff,
  onEmpty,
  onClose,
}: {
  agents: RosterAgent[]
  formation: FormationNode
  slot: FormationSlot
  onStaff: (formation: FormationNode, slot: FormationSlot, part: Part | null, anchor: Element) => void
  onEmpty: (formation: FormationNode, slot: FormationSlot) => void
  onClose: () => void
}) {
  const staffing = staffingOf(slot)
  const roleName = roleNamer(agents as FormationAgentProjection[])
  // The sentence window drops from the word clicked, as a dropdown does.
  const open = (part: Part | null) => (event: ReactMouseEvent<HTMLElement>) => onStaff(formation, slot, part, event.currentTarget)
  const word = (part: Part, text: string) => (
    <button type="button" className={`nslot-word${part === 'effort' ? ' effort' : ''}`} aria-label={`Change the ${part} of ${slot.label}: ${text}`} onClick={open(part)}>{text}</button>
  )
  return (
    <InspectorPanel title={slot.label} meta={`slot · ${formation.title}`} onClose={onClose}>
      <section className="note-section">
        {staffing ? (
          <p className="agx-staffing-words" data-testid="slot-staffing-words">
            {slotTitle(slot)} is {word('role', staffing.role ? roleName(staffing.role) : 'vanilla')} on{' '}
            {word('harness', harnessName(staffing.harness) || staffing.harness || 'no harness')} · {word('model', modelWords(staffing.model))} · {word('effort', staffing.effort || 'no effort')}.
          </p>
        ) : (
          <p className="agx-staffing-words" data-testid="slot-staffing-words">
            {staffingSentence(slot, roleName)} <button type="button" className="nslot-word" aria-label={`Staff ${slot.label}`} onClick={open(null)}>Staff it</button>
          </p>
        )}
        <KeyValues rows={[
          ['Controller', slot.controller ? 'yes' : 'no'],
          ['Role', staffing ? (staffing.role ? roleName(staffing.role) : 'vanilla') : ''],
          ['Harness', staffing ? captionText(staffing).split(' · ')[0] : ''],
          ['Model', staffing ? modelWords(staffing.model) : ''],
          ['Effort', staffing ? staffing.effort || 'no effort' : ''],
        ]} />
        {slotStaffed(slot) && (
          <div className="pop-actions">
            <button className="retire" type="button" onClick={() => onEmpty(formation, slot)}>
              Empty {slot.label}
            </button>
          </div>
        )}
      </section>
    </InspectorPanel>
  )
}

/* The mission's run, read-only. Start, answer, resume and stop happen on Boards. */
function MissionRunState({ board, missionRun }: { board: BoardDocument; missionRun: MissionRunState }) {
  const { run, openCount, blockReason } = missionRun
  if (!run) {
    return (
      <section className="run-banner agx-run-banner" data-testid="mission-run" aria-label="Mission run">
        <span className="agx-run-label">run</span>
        <span className="agx-run-none">No run for this mission yet.</span>
        <a className="agx-run-link" href={boardsRunHref(board.slug, '')}>Start it on Missions</a>
      </section>
    )
  }
  const gateTitle = (gateId: string) => (board.gates || []).find(gate => gate.id === gateId)?.title || gateId
  const waiting = (run.waitingGates || []).map(gate => gateTitle(gate.gateId))
  return (
    <section className="run-banner agx-run-banner" data-testid="mission-run" aria-label="Mission run">
      <span className="agx-run-label">run</span>
      <span className={`badge ${run.status}`}>{runStatusLabel(run.status)}</span>
      <span className="agx-run-id" title={run.runId}>…{run.runId.slice(-6)}</span>
      {waiting.length ? <span>waiting on {waiting.join(', ')}</span> : null}
      {run.status === 'blocked' ? (
        <span className="agx-run-reason" title={blockReason || undefined}>
          {blockReason ? `blocked: ${blockReason}` : 'blocked'}{run.resumeAllowed ? ' · resumable' : ''}
        </span>
      ) : null}
      {openCount > 1 ? <span>{openCount} open runs</span> : null}
      <a className="agx-run-link" href={boardsRunHref(board.slug, run.runId)}>Open on Missions</a>
    </section>
  )
}

function CreatePersonaPopover({
  draft,
  onDraft,
  onSubmit,
  onClose,
}: {
  draft: CreateDraft
  onDraft: (draft: CreateDraft) => void
  onSubmit: (event: FormEvent) => void
  onClose: () => void
}) {
  const set = (key: keyof CreateDraft, value: string) => onDraft({ ...draft, [key]: value })
  return (
    <div className="pop agx-pop" role="dialog" aria-label="Create persona">
      <div className="pop-head">
        <span className="pt">Create persona</span>
        <button className="x" type="button" onClick={onClose} aria-label="Close">×</button>
      </div>
      <form className="pop-body" onSubmit={onSubmit}>
        <label htmlFor="agx-create-id">Agent id</label>
        <input id="agx-create-id" className="f" value={draft.id} onChange={event => set('id', event.target.value)} />
        <label htmlFor="agx-create-display">Display name</label>
        <input id="agx-create-display" className="f" value={draft.displayName} onChange={event => set('displayName', event.target.value)} />
        <label htmlFor="agx-create-kind">Kind</label>
        <input id="agx-create-kind" className="f" value={draft.kind} onChange={event => set('kind', event.target.value)} />
        <label htmlFor="agx-create-stem">Session stem</label>
        <input id="agx-create-stem" className="f" value={draft.sessionStem} onChange={event => set('sessionStem', event.target.value)} />
        <label htmlFor="agx-create-summary">Summary</label>
        <input id="agx-create-summary" className="f" value={draft.summary} onChange={event => set('summary', event.target.value)} />
        <label htmlFor="agx-create-source">Source</label>
        <input id="agx-create-source" className="f" value={draft.source} onChange={event => set('source', event.target.value)} />
        <label htmlFor="agx-create-capabilities">Capabilities</label>
        <input id="agx-create-capabilities" className="f" value={draft.capabilities} onChange={event => set('capabilities', event.target.value)} />
        <button className="save" type="submit">Create persona</button>
      </form>
    </div>
  )
}

function KeyValues({ rows }: { rows: Array<[string, string]> }) {
  const shown = rows.filter(([, value]) => value)
  if (shown.length === 0) return null
  return (
    <dl className="tool-detail-identity">
      {shown.map(([label, value]) => (
        <div key={label}>
          <dt>{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  )
}

function TagList({ tags }: { tags: string[] }) {
  if (tags.length === 0) return null
  return (
    <div className="agx-tags">
      {tags.map(tag => <span key={tag}>{tag}</span>)}
    </div>
  )
}

function findSlot(board: BoardDocument | null, formationId: string, slotId: string): { formation: FormationNode; slot: FormationSlot } | null {
  const formation = board?.formations.find(next => next.id === formationId)
  const slot = formation?.slots.find(next => next.id === slotId)
  return formation && slot ? { formation, slot } : null
}

function splitCommaList(value: string): string[] {
  return value.split(',').map(part => part.trim()).filter(Boolean)
}

function inferHarnessFromSession(value: string): string {
  const lower = value.toLowerCase()
  if (lower.includes('hermes')) return 'hermes'
  if (lower.includes('codex') || lower.includes('openai')) return 'openai-codex'
  if (lower.includes('claude')) return 'claude-code'
  return EMPTY_CREATE.harness
}
