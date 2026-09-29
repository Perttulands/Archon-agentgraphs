import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
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
  formationSummary,
  groupRosterByHarness,
  harnessGlyph,
  initials,
  rosterCountLabel,
} from './formationsCockpitVisuals'
import { GateKindChips } from './GateEditorDialog'
import PersonaEditorDialog from './PersonaEditorDialog'
import { boardsRunHref, useMissionRun, type MissionRunState } from './useMissionRun'
import type {
  AgentProjection as FormationAgentProjection,
  BoardDocument,
  BoardSummary,
  FormationNode,
  FormationSlot,
  GateNode,
  LaunchableHarness,
  LayoutDocument,
  MissionNode,
  PersonaHarnessVariant,
  VariantSettingsPatch,
} from './formationsTypes'
import {
  EffortSelect,
  HARNESS_DEFAULT_MODEL,
  isLaunchable,
  SeatLaunch,
  VariantSettingsFields,
  variantChanges,
  variantDraft,
  variantSettingsSummary,
  type VariantDraft,
} from './PersonaHarnessSettings'

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
  model: string
  effort: string
  /** Only for a harness Archon cannot start: what `archon agent spawn` runs. */
  launch: string
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
  model: '',
  effort: '',
  launch: '',
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
  if (deployedSlots > 0) chips.push(`in ${deployedSlots} slot${deployedSlots === 1 ? '' : 's'}`)
  if (agent.attached) chips.push('attached')
  return { liveness: rawLiveness, chips, deployedSlots }
}

export default function AgentsView() {
  const [agents, setAgents] = useState<RosterAgent[]>([])
  const [harnesses, setHarnesses] = useState<LaunchableHarness[]>([])
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

  const selectedMission = board?.missions?.find(mission => mission.id === selectedMissionId) || null
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

  const assignmentsByAgent = useMemo(() => {
    const map = new Map<string, Array<{ formation: FormationNode; slot: FormationSlot }>>()
    for (const formation of reachableFormations) {
      for (const slot of formation.slots || []) {
        if (!slot.agentId) continue
        const list = map.get(slot.agentId) || []
        list.push({ formation, slot })
        map.set(slot.agentId, list)
      }
    }
    return map
  }, [reachableFormations])

  const slotCounts = useMemo(() => {
    let total = 0
    let staffed = 0
    for (const formation of reachableFormations) {
      for (const slot of formation.slots || []) {
        total += 1
        if (slot.agentId) staffed += 1
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
      agent.harnessDefault || '',
      ...(agent.tags || []),
    ].some(value => value.toLowerCase().includes(needle)))
  }, [agents, search])

  const personas = filteredAgents.filter(agent => !agent.unbound)
  const personaSections = groupRosterByHarness(personas)
  const unbound = filteredAgents.filter(agent => agent.unbound)

  const selectedSlot = useMemo(() => {
    if (selection?.kind !== 'slot') return null
    return findSlot(board, selection.formationId, selection.slotId)
  }, [board, selection])

  const loadAgents = useCallback(async () => {
    const roster = await fetchAgentRoster()
    setAgents(roster.agents as RosterAgent[])
    setHarnesses(roster.harnesses)
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
        if (current && nextBoard.missions?.some(mission => mission.id === current)) return current
        return nextBoard.missions?.[0]?.id || ''
      })
      setBoardError('')
      setError('')
    } catch (err) {
      setBoardError(err instanceof Error ? err.message : 'Failed to load board')
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
        setBoards(nextBoards)
        // Boards and Agents share one current board: the link's, else the last used here.
        const { slug, missingLinked } = chooseCurrentBoard(nextBoards.map(next => next.slug), window.location.search)
        setSelectedSlug(current => current || slug)
        setError(missingLinked && nextBoards.length ? `Board "${missingLinked}" from the link was not found` : '')
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

  const inspectSlot = useCallback(async (formation: FormationNode, slot: FormationSlot) => {
    setSelection({ kind: 'slot', formationId: formation.id, slotId: slot.id })
    if (!slot.harness) {
      setError('')
      return
    }
    const detailAgents = agents.filter(agent => !agent.unbound && agent.assignable)
    const settled = await Promise.allSettled(detailAgents.map(agent => loadAgentDetail(agent.id, Boolean(details[agent.id]?.error))))
    setDetails(current => {
      const next = { ...current }
      settled.forEach((result, index) => {
        if (result.status === 'rejected') {
          next[detailAgents[index].id] = { etag: '', error: 'failed detail load' }
        }
      })
      return next
    })
    setError('')
  }, [agents, details, loadAgentDetail])

  const updateBoardWithPatch = useCallback(async (patch: Record<string, unknown>) => {
    if (!board) return
    try {
      const result = await patchBoardDocument(board.slug, board.etag, board.rev, patch)
      setBoard(result.board)
      if (result.layout) setLayout(result.layout)
      setError('')
      await loadAgents()
    } catch (err) {
      if (err instanceof ApiRequestError && (err.status === 409 || err.status === 428)) {
        setError('Board changed; reload and retry')
        return
      }
      setError(err instanceof Error ? err.message : 'Board update failed')
    }
  }, [board, loadAgents])

  const assignSlot = useCallback(async (formation: FormationNode, slot: FormationSlot, agent: RosterAgent, harness: string) => {
    await updateBoardWithPatch({
      assignSlot: {
        formationId: formation.id,
        slotId: slot.id,
        agentId: agent.id,
        harness,
      },
    })
  }, [updateBoardWithPatch])

  const unassignSlot = useCallback(async (formation: FormationNode, slot: FormationSlot) => {
    await updateBoardWithPatch({
      assignSlot: {
        formationId: formation.id,
        slotId: slot.id,
        agentId: '',
        harness: '',
      },
    })
  }, [updateBoardWithPatch])

  const createPersona = useCallback(async (event: FormEvent) => {
    event.preventDefault()
    const capabilities = splitCommaList(createDraft.capabilities)
    // Only a harness Archon starts takes a model and effort.
    const launchable = harnesses.some(harness => harness.id === createDraft.harness.trim())
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
          ...(launchable ? { model: createDraft.model.trim(), effort: createDraft.effort } : { launch: createDraft.launch.trim() }),
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
  }, [createDraft, harnesses, loadAgents])

  /** Saves one variant's model and effort; resolves to an error message, or '' on success. */
  const saveVariantSettings = useCallback(async (agentId: string, patch: VariantSettingsPatch): Promise<string> => {
    const cached = details[agentId]
    if (!cached?.card || !cached.etag) return 'Persona detail unavailable; reload and retry'
    try {
      const result = await fetchApi<PersonaCard>(`/api/agents/${encodeURIComponent(agentId)}`, {
        method: 'PATCH',
        headers: { 'If-Match': cached.etag },
        body: JSON.stringify({ variants: [patch] }),
      })
      setDetails(current => ({
        ...current,
        [agentId]: { card: { ...result.data, etag: result.etag || result.data.etag }, etag: result.etag || result.data.etag || '' },
      }))
      return ''
    } catch (err) {
      if (err instanceof ApiRequestError && err.status === 409) {
        await loadAgentDetail(agentId, true).catch(() => undefined)
        return 'This persona changed elsewhere and was reloaded. Check the settings, then save again.'
      }
      return err instanceof Error ? err.message : 'Persona update failed'
    }
  }, [details, loadAgentDetail])

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
  const rosterSummary = rosterCountLabel(loading ? '…' : rosterCounts.total, { live: rosterCounts.live, placed: rosterCounts.deployed, scope: 'mission' })

  return (
    <div className="fmx agx" data-testid="agents-view">
      <div className="topbar">
        <div className="boardpick">
          board
          <select aria-label="Board" value={selectedSlug} onChange={event => setSelectedSlug(event.target.value)} disabled={loading || boards.length === 0}>
            {boards.length === 0 && <option value="">No boards</option>}
            {boards.map(next => (
              <option key={next.slug} value={next.slug}>{next.title || next.slug}</option>
            ))}
          </select>
          {board ? <span className="rev">rev {board.rev}</span> : null}
        </div>
        <div className="sep" />
        <div className="boardpick">
          mission
          <select
            aria-label="Mission"
            value={selectedMissionId}
            onChange={event => setSelectedMissionId(event.target.value)}
            disabled={boardLoading || !board?.missions?.length}
          >
            {!board?.missions?.length && <option value="">No missions</option>}
            {(board?.missions || []).map(mission => (
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
              title={`${rosterCounts.total} agents · ${rosterCounts.live} live · ${rosterCounts.assignable} assignable · ${rosterCounts.deployed} staffed on this mission`}
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
            {personaSections.map(section => (
              <RosterGroup
                key={section.id}
                id={section.id}
                label={section.label}
                agents={section.agents}
                details={details}
                assignmentsByAgent={assignmentsByAgent}
                selectedAgentId={selectedAgentId}
                onInspect={inspectAgent}
              />
            ))}
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
              <span>Board load failed: {boardError}</span>
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
                title={boards.length === 0 ? 'No boards' : 'No mission on this board'}
                copy={boards.length === 0
                  ? 'Create a board on the Boards tab to staff a mission.'
                  : 'Add a Mission card on the Boards tab and wire it to a formation.'}
              />
            )}
            {selectedMission && (
              <MissionCard mission={selectedMission} slotCounts={slotCounts} />
            )}
            {selectedMission && reachableItems.length === 0 && (
              <StaffingEmpty
                title="Nothing wired to this mission"
                copy="Wire the mission's output to a formation on the Boards tab to staff it here."
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
                    agents={agents}
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
              onSaveVariant={saveVariantSettings}
              onAssign={assignSlot}
              onUnassign={unassignSlot}
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

      {createOpen && (
        <CreatePersonaPopover
          draft={createDraft}
          harnesses={harnesses}
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
            <span className="av">{harnessGlyph(agent.harnessDefault) ?? initials(name)}</span>
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

/* Offline is the resting state, so it is not repeated on every row. */
function StatusWords({ agent, status }: { agent: RosterAgent; status: AgentStatus }) {
  const words = [
    ...(agent.unbound ? [] : [agentRole(agent as FormationAgentProjection)]),
    ...(agent.preset ? [agent.customized ? 'custom' : 'preset'] : []),
    ...(status.liveness === 'offline' ? [] : [status.liveness]),
    ...status.chips,
  ]
  return (
    <span className="r">
      {words.map(word => <span key={word} className={word === status.liveness ? `is-${word}` : undefined}>{word}</span>)}
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
        <span className="meyebrow">◆ Mission</span>
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
  agents,
  nodeState,
  selectedSlot,
  onSlotClick,
}: {
  formation: FormationNode | null
  via?: BranchProvenance
  viaGateTitle: string
  agents: RosterAgent[]
  nodeState: string
  selectedSlot: { formation: FormationNode; slot: FormationSlot } | null
  onSlotClick: (formation: FormationNode, slot: FormationSlot) => void
}) {
  if (!formation) return null
  const open = formation.slots.filter(slot => !slot.agentId).length
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
              agents={agents}
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
  agents,
  nodeState,
  selected,
  onClick,
}: {
  formation: FormationNode
  slot: FormationSlot
  badge?: number
  agents: RosterAgent[]
  nodeState: string
  selected: boolean
  onClick: (formation: FormationNode, slot: FormationSlot) => void
}) {
  const assigned = slot.agentId ? agents.find(agent => agent.id === slot.agentId) : null
  const assignedName = assigned?.displayName || slot.agentId || ''
  const classes = [
    'slot',
    slot.agentId ? 'filled' : 'empty',
    slot.controller ? 'ctrl' : '',
    nodeState === 'running' ? 'active' : '',
    nodeState === 'done' ? 'active done' : '',
    selected ? 'selected' : '',
  ]
  return (
    <button
      type="button"
      className={classes.filter(Boolean).join(' ')}
      aria-label={slot.agentId ? `Inspect ${slot.label} slot assigned to ${assignedName}` : `Assign ${slot.label} slot`}
      aria-pressed={selected}
      onClick={() => onClick(formation, slot)}
    >
      <span className="slot-ring">
        {badge ? <span className="badge">{badge}</span> : null}
        {slot.agentId
          ? <span className="face">{harnessGlyph(slot.harness || assigned?.harnessDefault) ?? initials(slot.agentId)}</span>
          : <span className="plus">+</span>}
      </span>
      <span className="slot-label">{slot.label}</span>
      {slot.agentId ? <span className="who">{slot.agentId}</span> : null}
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
  onSaveVariant,
  onAssign,
  onUnassign,
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
  onSaveVariant: (agentId: string, patch: VariantSettingsPatch) => Promise<string>
  onAssign: (formation: FormationNode, slot: FormationSlot, agent: RosterAgent, harness: string) => void
  onUnassign: (formation: FormationNode, slot: FormationSlot) => void
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
    const harness = card?.harnessDefault || agent?.harnessDefault || ''
    const defaultVariant = card?.harnessVariants.find(variant => variant.id === card.harnessDefault)
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
            <span className="av">{harnessGlyph(harness) ?? initials(selection.agentId)}</span>
            <span className="ri">
              <span className="n">{harness || 'no default harness'}</span>
              <span className="r">{states.map(state => <span key={state} className={`is-${state}`}>{state}</span>)}</span>
            </span>
            {agent ? (
              <button className="board-action" type="button" onClick={event => onEditPersona(agent, event.currentTarget)}>Edit persona</button>
            ) : null}
          </div>
          {card?.summary && <p className="agx-summary">{card.summary}</p>}
          <KeyValues rows={[
            ['Runs', defaultVariant ? variantSettingsSummary(defaultVariant) : ''],
            ['Session', agent?.sessionId || ''],
            ['Context', typeof agent?.contextPct === 'number' ? `${agent.contextPct}%` : ''],
            ['Bead', agent?.beadId || ''],
          ]} />
          <TagList tags={card?.tags || agent?.tags || []} />
        </section>
        <section className="note-section">
          <h3>Harness variants</h3>
          {card && card.harnessVariants.length === 0 && <p className="note-empty">No harness variants recorded.</p>}
          {!card && !detail?.error && <p className="note-empty">Loading persona card…</p>}
          {card?.harnessVariants.map(variant => (
            <VariantEditor
              key={`${card.id}:${variant.id}`}
              agentId={card.id}
              variant={variant}
              isDefault={variant.id === card.harnessDefault}
              fallbackStem={card.id}
              onSave={onSaveVariant}
            />
          ))}
        </section>
        <section className="note-section">
          <h3>Slots on this mission</h3>
          {assignments.length === 0 && <p className="note-empty">No slots on this mission.</p>}
          <div className="tool-detail-list">
            {assignments.map(({ formation, slot }) => (
              <div className="tool-detail-row" key={`${formation.id}:${slot.id}`}>
                <span>{formation.title}</span>
                <strong>{slot.label}{slot.controller && slot.label.toLowerCase() !== 'controller' ? ' · controller' : ''}</strong>
              </div>
            ))}
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
        details={details}
        formation={selectedSlot.formation}
        slot={selectedSlot.slot}
        onAssign={onAssign}
        onUnassign={onUnassign}
        onClose={onClose}
      />
    )
  }

  return (
    <InspectorPanel title="Slot" meta="no longer on this board" onClose={onClose}>
      <p className="note-empty">This slot was removed from the board.</p>
    </InspectorPanel>
  )
}

/** One harness variant in the persona inspector: its settings, editable in place, and what its seats run. */
function VariantEditor({ agentId, variant, isDefault, fallbackStem, onSave }: {
  agentId: string
  variant: PersonaHarnessVariant
  isDefault: boolean
  fallbackStem: string
  onSave: (agentId: string, patch: VariantSettingsPatch) => Promise<string>
}) {
  const [draft, setDraft] = useState<VariantDraft>(() => variantDraft(variant))
  const [saving, setSaving] = useState(false)
  const [status, setStatus] = useState<{ error: string; saved: boolean }>({ error: '', saved: false })
  // A save here or an edit elsewhere changes the card; show what it now holds.
  useEffect(() => setDraft(variantDraft(variant)), [variant.model, variant.effort, variant.launch]) // eslint-disable-line react-hooks/exhaustive-deps
  const changes = variantChanges(variant, draft)
  const launchable = isLaunchable(variant)
  const idPrefix = `agx-variant-${variant.id}`

  const save = async (event: FormEvent) => {
    event.preventDefault()
    if (!changes || saving) return
    setSaving(true)
    const error = await onSave(agentId, changes)
    setSaving(false)
    setStatus({ error, saved: !error })
  }

  return (
    <form className="ph-variant" aria-label={`${variant.id} harness variant`} onSubmit={save}>
      <div className="ph-head">
        <strong>{variant.id}</strong>
        {isDefault ? <span className="ph-default">default</span> : null}
        <span>session {variant.sessionStem || fallbackStem}</span>
      </div>
      <VariantSettingsFields
        idPrefix={idPrefix}
        harness={variant.id}
        efforts={variant.efforts}
        draft={draft}
        onDraft={next => { setDraft(next); setStatus({ error: '', saved: false }) }}
        disabled={saving}
      />
      <div className="ph-actions">
        {status.saved && !changes ? <span className="ph-saved" role="status">Saved</span> : null}
        {changes ? <button type="button" className="board-action" disabled={saving} onClick={() => { setDraft(variantDraft(variant)); setStatus({ error: '', saved: false }) }}>Revert</button> : null}
        <button className="primary" type="submit" disabled={!changes || saving} aria-label={`Save ${variant.id} ${launchable ? 'model and effort' : 'launch command'}`}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>
      {status.error ? <p className="ph-error" role="alert">{status.error}</p> : null}
      <SeatLaunch variant={variant} />
      {variant.source ? <div className="ph-head"><span>source</span><code>{variant.source}</code></div> : null}
    </form>
  )
}

function SlotInspector({
  agents,
  details,
  formation,
  slot,
  onAssign,
  onUnassign,
  onClose,
}: {
  agents: RosterAgent[]
  details: Record<string, CachedPersona>
  formation: FormationNode
  slot: FormationSlot
  onAssign: (formation: FormationNode, slot: FormationSlot, agent: RosterAgent, harness: string) => void
  onUnassign: (formation: FormationNode, slot: FormationSlot) => void
  onClose: () => void
}) {
  const assigned = slot.agentId ? agents.find(agent => agent.id === slot.agentId) : null
  return (
    <InspectorPanel title={slot.label} meta={`slot · ${formation.title}`} onClose={onClose}>
      <section className="note-section">
        <KeyValues rows={[
          ['Controller', slot.controller ? 'yes' : 'no'],
          ['Harness', slot.harness || 'default'],
          ['Current', assigned?.displayName || slot.agentId || 'open'],
        ]} />
        {slot.agentId && (
          <div className="pop-actions">
            <button className="retire" type="button" onClick={() => onUnassign(formation, slot)}>
              Unassign {assigned?.displayName || slot.agentId}
            </button>
          </div>
        )}
      </section>
      <section className="note-section">
        <h3>Eligible agents</h3>
        <div className="agx-candidates">
          {agents.map(agent => {
            const eligibility = slotEligibility(agent, slot, details[agent.id])
            const name = agent.displayName || agent.id
            return (
              <button
                key={agent.id}
                type="button"
                className="agx-candidate"
                disabled={!eligibility.eligible}
                aria-label={`Assign ${name}`}
                onClick={() => eligibility.eligible && onAssign(formation, slot, agent, eligibility.harness)}
              >
                <span className="av">{harnessGlyph(eligibility.eligible ? eligibility.harness : agent.harnessDefault) ?? initials(name)}</span>
                <span className="ri">
                  <span className="n">{name}</span>
                  <span className="r">{eligibility.eligible ? eligibility.harness : eligibility.reason}</span>
                </span>
              </button>
            )
          })}
        </div>
      </section>
    </InspectorPanel>
  )
}

function slotEligibility(agent: RosterAgent, slot: FormationSlot, detail?: CachedPersona): { eligible: true; harness: string } | { eligible: false; reason: string } {
  if (agent.unbound) return { eligible: false, reason: 'unbound session (no persona)' }
  if (detail?.error) return { eligible: false, reason: 'failed detail load' }
  if (detail?.card?.status === 'retired') return { eligible: false, reason: 'retired persona' }
  if (!agent.assignable) return { eligible: false, reason: 'not assignable' }
  const requiredHarness = slot.harness || ''
  if (requiredHarness) {
    if (!detail) return { eligible: false, reason: 'loading detail' }
    if (!detail.card) return { eligible: false, reason: 'failed detail load' }
    const hasHarness = detail.card.harnessVariants.some(variant => variant.id === requiredHarness)
    if (!hasHarness) return { eligible: false, reason: `missing harness variant ${requiredHarness}` }
    return { eligible: true, harness: requiredHarness }
  }
  return { eligible: true, harness: detail?.card?.harnessDefault || agent.harnessDefault || 'claude-code' }
}

/* The mission's run, read-only. Start, answer, resume and stop happen on Boards. */
function MissionRunState({ board, missionRun }: { board: BoardDocument; missionRun: MissionRunState }) {
  const { run, openCount, blockReason } = missionRun
  if (!run) {
    return (
      <section className="run-banner agx-run-banner" data-testid="mission-run" aria-label="Mission run">
        <span className="agx-run-label">run</span>
        <span className="agx-run-none">No run for this mission yet.</span>
        <a className="agx-run-link" href={boardsRunHref(board.slug, '')}>Start it on Boards</a>
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
      <a className="agx-run-link" href={boardsRunHref(board.slug, run.runId)}>Open on Boards</a>
    </section>
  )
}

function CreatePersonaPopover({
  draft,
  harnesses,
  onDraft,
  onSubmit,
  onClose,
}: {
  draft: CreateDraft
  harnesses: LaunchableHarness[]
  onDraft: (draft: CreateDraft) => void
  onSubmit: (event: FormEvent) => void
  onClose: () => void
}) {
  const set = (key: keyof CreateDraft, value: string) => onDraft({ ...draft, [key]: value })
  const effortsFor = (harness: string) => harnesses.find(next => next.id === harness)?.efforts || []
  const efforts = effortsFor(draft.harness)
  // An effort one harness takes may not suit the next, so a change of harness keeps only a valid one.
  const changeHarness = (harness: string) =>
    onDraft({ ...draft, harness, effort: effortsFor(harness).includes(draft.effort) ? draft.effort : '' })
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
        <label htmlFor="agx-create-harness">Harness</label>
        <select id="agx-create-harness" className="f" value={draft.harness} onChange={event => changeHarness(event.target.value)}>
          <option value="claude-code">claude-code</option>
          <option value="openai-codex">openai-codex</option>
          <option value="hermes">hermes</option>
        </select>
        {efforts.length ? (
          <div className="agx-create-pair">
            <div>
              <label htmlFor="agx-create-model">Model</label>
              <input id="agx-create-model" className="f" value={draft.model} placeholder={HARNESS_DEFAULT_MODEL} spellCheck={false} onChange={event => set('model', event.target.value)} />
            </div>
            <div>
              <label htmlFor="agx-create-effort">Effort</label>
              <EffortSelect id="agx-create-effort" className="f" efforts={efforts} value={draft.effort} onChange={effort => set('effort', effort)} />
            </div>
          </div>
        ) : (
          <>
            <p className="ph-none agx-create-none">
              Archon cannot start {draft.harness} seats, so it takes no model or effort. <code>archon agent spawn</code> runs its launch command; leave it blank to derive it from Source.
            </p>
            <label htmlFor="agx-create-launch">Launch command (archon agent spawn)</label>
            <input id="agx-create-launch" className="f" value={draft.launch} spellCheck={false} onChange={event => set('launch', event.target.value)} />
          </>
        )}
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
