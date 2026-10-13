import { FileWindowsLayer, FileWindowsProvider } from '../files/FileWindows'
import LinkedText from '../files/LinkedText'
import { useWindowManager, WindowManagerProvider } from '../windows/WindowManager'
import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import {
  ApiRequestError,
  deleteRole,
  fetchAgentRoster,
  fetchApi,
  fetchRoleUsage,
  setRoleRetired,
  type RoleUse,
} from './formationsApi'
import { agentRole, byRoleName, inSlotsWords, initials } from './formationsCockpitVisuals'
import { COCKPIT_NOTE_AUTHOR, noteAuthor, noteTime } from './NoteThread'
import { CharacterEditorDialog, CharactersGroup } from '../characters/Characters'
import { useCharacters, type Character } from '../characters/charactersApi'
import { retiredRolesOf, roleNamer, rolesOf } from '../staffing/staffingModel'
import PersonaEditorDialog from './PersonaEditorDialog'
import type { AgentProjection as FormationAgentProjection, EffortPolicyEntry, LaunchableHarness } from './formationsTypes'

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
  notes?: PersonaNote[]
  etag?: string
  toml?: string
  preset?: boolean
  customized?: boolean
}

/** Which slots, in every mission, use a role; read when the role is inspected. */
type RoleUsage = { uses?: RoleUse[]; error?: string }

type CachedPersona = {
  card?: PersonaCard
  etag: string
  error?: string
}

type Selection =
  | { kind: 'agent'; agentId: string }
  | { kind: 'unbound'; agentId: string }

type CreateDraft = {
  id: string
  displayName: string
  kind: string
  summary: string
  capabilities: string
}

type AgentStatus = {
  liveness: 'live' | 'ambiguous' | 'offline'
  chips: string[]
}

const EMPTY_CREATE: CreateDraft = {
  id: '',
  displayName: '',
  kind: 'specialist',
  summary: '',
  capabilities: '',
}

export function agentStatus(agent: RosterAgent, details?: CachedPersona): AgentStatus {
  const rawLiveness = agent.liveness === 'live' || agent.liveness === 'ambiguous' ? agent.liveness : 'offline'
  const chips: string[] = []
  if (agent.unbound) chips.push('no role')
  if (!agent.unbound && details?.card?.status === 'retired') chips.push('retired')
  if (!agent.unbound && !agent.assignable && details?.card?.status !== 'retired') chips.push('not assignable')
  if (agent.attached) chips.push('attached')
  return { liveness: rawLiveness, chips }
}

export default function AgentsView({ onOpenMission }: {
  /** Opens a mission on the Missions tab; a role's use is read there. */
  onOpenMission?: (slug: string) => void
}) {
  const fileWindows = useWindowManager()
  const [agents, setAgents] = useState<RosterAgent[]>([])
  const [harnesses, setHarnesses] = useState<LaunchableHarness[]>([])
  const [effortPolicy, setEffortPolicy] = useState<EffortPolicyEntry[]>([])
  const [details, setDetails] = useState<Record<string, CachedPersona>>({})
  const [usage, setUsage] = useState<Record<string, RoleUsage>>({})
  const [selection, setSelection] = useState<Selection | null>(null)
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [createError, setCreateError] = useState('')
  const [createDraft, setCreateDraft] = useState<CreateDraft>(EMPTY_CREATE)
  const [noteDraft, setNoteDraft] = useState('')
  const [editingPersona, setEditingPersona] = useState<{ agent: RosterAgent; trigger: HTMLElement | null } | null>(null)
  const { characters, reload: reloadCharacters } = useCharacters()
  const [characterEditor, setCharacterEditor] = useState<{ character: Character | null; trigger: HTMLElement | null } | null>(null)

  const rosterCounts = useMemo(() => ({
    live: agents.filter(agent => agent.liveness === 'live').length,
    assignable: agents.filter(agent => !agent.unbound && agent.assignable).length,
  }), [agents])

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

  const loadAgents = useCallback(async () => {
    const roster = await fetchAgentRoster()
    setAgents(roster.agents as RosterAgent[])
    setHarnesses(roster.harnesses)
    setEffortPolicy(roster.effortPolicy)
  }, [])

  useEffect(() => {
    let cancelled = false
    void loadAgents()
      .then(() => { if (!cancelled) setError('') })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : 'Agents request failed') })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [loadAgents])

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

  // The view opens on a role, so its right side is never an empty page.
  useEffect(() => {
    if (loading || selection) return
    const first = agents.filter(agent => !agent.unbound).sort(byRoleName)[0]
    if (first) void inspectAgent(first)
  }, [agents, inspectAgent, loading, selection])

  const loadUsage = useCallback(async (agentId: string) => {
    try {
      const uses = await fetchRoleUsage(agentId)
      setUsage(current => ({ ...current, [agentId]: { uses } }))
    } catch (err) {
      setUsage(current => ({ ...current, [agentId]: { error: err instanceof Error ? err.message : 'Could not read which slots use this role' } }))
    }
  }, [])

  // Roles follow what changes elsewhere: new, edited, retired or deleted roles, sessions coming and going,
  // and the slots that use the role shown.
  const inspectedRole = selection?.kind === 'agent' ? selection.agentId : ''
  useEffect(() => {
    if (inspectedRole) void loadUsage(inspectedRole)
    let cancelled = false
    const timer = window.setInterval(() => {
      if (cancelled) return
      void loadAgents().catch(() => undefined)
      if (inspectedRole) void loadUsage(inspectedRole)
    }, 5000)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [inspectedRole, loadAgents, loadUsage])

  const retireRole = useCallback(async (agentId: string, retired: boolean) => {
    try {
      const cached = await loadAgentDetail(agentId, true)
      const card = await setRoleRetired(agentId, cached.etag, retired)
      setDetails(current => ({ ...current, [agentId]: { card, etag: card.etag || '' } }))
      setError('')
      await loadAgents()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Role update request failed')
    }
  }, [loadAgentDetail, loadAgents])

  const removeRole = useCallback(async (agentId: string) => {
    try {
      const cached = await loadAgentDetail(agentId, true)
      const result = await deleteRole(agentId, cached.etag)
      setDetails(current => {
        const next = { ...current }
        delete next[agentId]
        return next
      })
      setError('')
      await loadAgents()
      // Deleting a card that overrode a built-in role brings the built-in back.
      if (result.builtinRemains) await loadAgentDetail(agentId, true)
      else setSelection(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Role delete request failed')
    }
  }, [loadAgentDetail, loadAgents])

  const catalog = useMemo(() => ({
    harnesses, policy: effortPolicy, roles: rolesOf(agents as FormationAgentProjection[]), retired: retiredRolesOf(agents as FormationAgentProjection[]),
  }), [agents, effortPolicy, harnesses])

  const createPersona = useCallback(async (event: FormEvent) => {
    event.preventDefault()
    if (createDraft.id.trim() && !/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(createDraft.id.trim())) {
      setCreateError('Use lowercase letters, digits and single hyphens for the role id, for example brief-critic.')
      return
    }
    const capabilities = splitCommaList(createDraft.capabilities)
    try {
      const result = await fetchApi<PersonaCard>('/api/agents', {
        method: 'POST',
        body: JSON.stringify({
          id: createDraft.id.trim(),
          displayName: createDraft.displayName.trim(),
          kind: createDraft.kind.trim(),
          summary: createDraft.summary.trim(),
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
      setCreateError(err instanceof Error ? err.message : 'Could not create the role.')
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
        body: JSON.stringify({ note, updatedBy: COCKPIT_NOTE_AUTHOR }),
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

  // A role's own session is the one named after it, so the session's name
  // becomes the role's id.
  const createFromUnbound = useCallback((agent: RosterAgent) => {
    setCreateDraft({
      ...EMPTY_CREATE,
      id: agent.id,
      displayName: agent.displayName || agent.id,
      capabilities: (agent.tags || []).join(', '),
    })
    setCreateOpen(true)
  }, [])

  const selectedAgentId = selection?.kind === 'agent' || selection?.kind === 'unbound' ? selection.agentId : ''
  const rosterSummary = [`${loading ? '…' : rosterCounts.assignable} role${rosterCounts.assignable === 1 ? '' : 's'}`, rosterCounts.live ? `${rosterCounts.live} live` : ''].filter(Boolean).join(' · ')

  return (
    <WindowManagerProvider stack={fileWindows}>
    <FileWindowsProvider stack={fileWindows}>
    <div className="fmx agx" data-testid="agents-view">
      {error && <div className="agx-alert" role="alert">{error}</div>}

      <div className="main">
        <aside className="roster" aria-label="Agent roster">
          <div className="roster-hd">
            <div className="t">Agents</div>
            <span className="s" title={`${rosterCounts.assignable} roles you can staff; ${rosterCounts.live} live sessions`}>{rosterSummary}</span>
          </div>
          <input
            className="agx-filter"
            aria-label="Filter agents"
            value={search}
            onChange={event => setSearch(event.target.value)}
            placeholder="filter agents"
          />
          <div className="roster-list">
            <CharactersGroup
              characters={characters.filter(character => !search.trim() || character.name.toLowerCase().includes(search.trim().toLowerCase()))}
              roleName={roleNamer(agents as FormationAgentProjection[])}
              selectedId={characterEditor?.character?.id}
              title={character => `${character.name}${character.description ? `: ${character.description}` : ''}`}
              onNew={trigger => setCharacterEditor({ character: null, trigger })}
              onPick={(character, trigger) => setCharacterEditor({ character, trigger })}
            />
            <RosterGroup id="roles" label="Roles" agents={personas} details={details} selectedAgentId={selectedAgentId} onInspect={inspectAgent} onNew={() => setCreateOpen(true)} />
            {!loading && personas.length === 0 && (
              <div className="roster-empty">{search.trim() ? 'No roles match this filter.' : 'No roles yet.'}</div>
            )}
            {unbound.length > 0 && (
              <RosterGroup id="unbound" label="Unbound" agents={unbound} details={details} selectedAgentId={selectedAgentId} onInspect={inspectAgent} />
            )}
          </div>
        </aside>

        <main className="agx-detail" aria-label="Role">
          {selection && (
            <Inspector
              selection={selection}
              agents={agents}
              details={details}
              usage={usage}
              noteDraft={noteDraft}
              onNoteDraft={setNoteDraft}
              onSaveNote={saveNote}
              onCreateFromUnbound={createFromUnbound}
              onEditPersona={(agent, trigger) => setEditingPersona({ agent, trigger })}
              onRetire={retireRole}
              onDelete={removeRole}
              onShowUse={onOpenMission ? use => onOpenMission(use.missionSlug) : undefined}
            />
          )}
        </main>
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

      {characterEditor && (
        <CharacterEditorDialog
          key={characterEditor.character?.id || 'new'}
          character={characterEditor.character}
          catalog={catalog}
          returnFocus={characterEditor.trigger}
          onClose={() => setCharacterEditor(null)}
          onSaved={reloadCharacters}
        />
      )}

      {createOpen && (
        <CreatePersonaPopover
          draft={createDraft}
          error={createError}
          onDismiss={() => setCreateError('')}
          onDraft={draft => { setCreateDraft(draft); setCreateError('') }}
          onSubmit={createPersona}
          onClose={() => { setCreateOpen(false); setCreateError('') }}
        />
      )}
      <FileWindowsLayer />
    </div>
    </FileWindowsProvider>
    </WindowManagerProvider>
  )
}

function RosterGroup({ id, label, agents, details, selectedAgentId, onInspect, onNew }: {
  id: string
  label: string
  agents: RosterAgent[]
  details: Record<string, CachedPersona>
  selectedAgentId: string
  onInspect: (agent: RosterAgent) => void
  onNew?: () => void
}) {
  return (
    <section className="roster-group" data-provider={id}>
      <div className="roster-group-label">
        {label}
        {onNew ? (
          <button type="button" className="characters-new" aria-label="New role" title="New role" onClick={onNew}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
          </button>
        ) : null}
      </div>
      {agents.map(agent => {
        const status = agentStatus(agent, details[agent.id])
        const name = agent.displayName || agent.id
        const selected = selectedAgentId === agent.id
        return (
          <button
            key={agent.id}
            type="button"
            className={`ragent${agent.unbound ? ' unbound' : ''}${selected ? ' selected' : ''}`}
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

function InspectorPanel({ title, meta, children }: { title: string; meta: string; children: ReactNode }) {
  return (
    <>
      <div className="board-notes-head">
        <div className="agx-inspector-heading">
          <div className="board-notes-title">{title}</div>
          <div className="board-notes-meta">{meta}</div>
        </div>
      </div>
      <div className="board-notes-body">{children}</div>
    </>
  )
}

function Inspector({ selection, agents, details, usage, noteDraft, onNoteDraft, onSaveNote, onCreateFromUnbound, onEditPersona, onRetire, onDelete, onShowUse }: {
  selection: Selection
  agents: RosterAgent[]
  details: Record<string, CachedPersona>
  usage: Record<string, RoleUsage>
  noteDraft: string
  onNoteDraft: (value: string) => void
  onSaveNote: (agentId: string) => void
  onCreateFromUnbound: (agent: RosterAgent) => void
  onEditPersona: (agent: RosterAgent, trigger: HTMLElement) => void
  onRetire: (agentId: string, retired: boolean) => void
  onDelete: (agentId: string) => void
  onShowUse?: (use: RoleUse) => void
}) {
  if (selection.kind === 'unbound') {
    const agent = agents.find(next => next.id === selection.agentId)
    return (
      <InspectorPanel title={agent?.displayName || selection.agentId} meta="unbound session">
        <section className="note-section">
          <p className="note-empty">This live session has no role. A slot can use it once a role exists for it.</p>
          <KeyValues rows={[
            ['Liveness', agent?.liveness || 'live'],
            ['Session', agent?.sessionId || agent?.id || selection.agentId],
          ]} />
          {agent && (
            <div className="board-dialog-actions">
              <button className="primary" type="button" onClick={() => onCreateFromUnbound(agent)}>
                Make a role from this session
              </button>
            </div>
          )}
        </section>
      </InspectorPanel>
    )
  }

  {
    const agent = agents.find(next => next.id === selection.agentId)
    const detail = details[selection.agentId]
    const card = detail?.card
    const uses = usage[selection.agentId]?.uses
    const states = [
      agent?.liveness || 'offline',
      card?.status || '',
      agent?.attached ? 'attached' : '',
      !agent?.assignable && !card?.status ? 'not assignable' : '',
    ].filter(Boolean)
    return (
      <InspectorPanel title={card?.displayName || agent?.displayName || selection.agentId} meta={card?.kind || agent?.kind || 'agent'}>
        <section className="note-section">
          <div className="agx-identity">
            <span className="av">{initials(card?.displayName || agent?.displayName || selection.agentId)}</span>
            <span className="ri">
              <span className="n">{uses ? inSlotsWords(uses.length) || 'in no slot' : ''}</span>
              <span className="r">{states.map(state => <span key={state} className={`is-${state}`}>{state}</span>)}</span>
            </span>
            {agent ? (
              <button className="board-action" type="button" onClick={event => onEditPersona(agent, event.currentTarget)}>Edit role</button>
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
        <RoleUsageSection
          key={selection.agentId}
          name={card?.displayName || agent?.displayName || selection.agentId}
          card={card}
          usage={usage[selection.agentId]}
          onRetire={retired => onRetire(selection.agentId, retired)}
          onDelete={() => onDelete(selection.agentId)}
          onShowUse={onShowUse}
        />
        <form
          className="note-section"
          aria-label="Notes"
          onSubmit={event => {
            event.preventDefault()
            onSaveNote(selection.agentId)
          }}
        >
          <h3>Notes</h3>
          {card?.notes?.length ? (
            <ol className="note-thread" aria-label={`Notes on ${card.displayName || selection.agentId}`}>
              {card.notes.map((note, index) => {
                const author = noteAuthor(note.actor)
                return (
                  <li key={`${note.ts}:${index}`} className={`note-entry note-entry-${author.kind}`}>
                    <div className="note-entry-head">
                      <span className={`note-author note-author-${author.kind}`} title={note.actor}>{author.name}</span>
                      <span className="note-time">{noteTime(note.ts)}</span>
                    </div>
                    <div className="note-entry-text"><LinkedText text={note.text} /></div>
                  </li>
                )
              })}
            </ol>
          ) : <p className="note-empty">{card ? 'No notes on this role yet.' : 'Reading the role…'}</p>}
          <label htmlFor="agx-note">Add note</label>
          <textarea id="agx-note" value={noteDraft} onChange={event => onNoteDraft(event.target.value)} />
          <div className="board-dialog-actions">
            <button className="primary" type="submit">Save note</button>
          </div>
        </form>
      </InspectorPanel>
    )
  }
}

/**
 * Every slot, in every mission, that uses the role, then Retire and Delete,
 * each of which says what it does to those slots before it acts (archon-o7p.12.3).
 */
function RoleUsageSection({ name, card, usage, onRetire, onDelete, onShowUse }: {
  name: string
  card?: PersonaCard
  usage?: RoleUsage
  onRetire: (retired: boolean) => void
  onDelete: () => void
  onShowUse?: (use: RoleUse) => void
}) {
  const [confirm, setConfirm] = useState<'retire' | 'delete' | null>(null)
  const uses = usage?.uses
  const retired = card?.status === 'retired'
  // A built-in role has no card to delete; a card that changes one can go.
  const deletable = Boolean(card) && (!card?.preset || card?.customized)
  const slots = (count: number) => `${count} ${count === 1 ? 'slot' : 'slots'}`
  return (
    <section className="note-section" aria-label="Used by">
      <h3>Used by</h3>
      {usage?.error ? <p className="note-empty" role="alert">{usage.error}</p> : null}
      {!usage ? <p className="note-empty">Reading which slots use this role…</p> : null}
      {uses && uses.length === 0 ? <p className="note-empty">No slot uses this role.</p> : null}
      {uses && uses.length > 0 ? (
        <div className="tool-detail-list">
          {uses.map(use => (
            <button type="button" className="tool-detail-row agx-role-slot agx-role-use" key={`${use.missionId}:${use.formationId}:${use.slotId}`} title={`Open ${use.missionTitle} on Missions`} onClick={() => onShowUse?.(use)}>
              <span>{use.missionTitle} › {use.formationTitle} › <strong>{use.slotLabel || use.slotId}</strong></span>
            </button>
          ))}
        </div>
      ) : null}
      {confirm === 'retire' ? (
        <div className="agx-role-confirm" role="group" aria-label={`Retire ${name}`}>
          <p>{uses?.length
            ? `Retire ${name}? ${uses.length === 1 ? 'The slot above stops running until it has' : `The ${slots(uses.length)} above stop running until each has`} another role or none.`
            : `Retire ${name}? It is no longer offered for slots; you can bring it back.`}</p>
          <div className="pop-actions">
            <button className="retire" type="button" onClick={() => { setConfirm(null); onRetire(true) }}>Retire {name}</button>
            <button className="cancel" type="button" onClick={() => setConfirm(null)}>Cancel</button>
          </div>
        </div>
      ) : confirm === 'delete' ? (
        <div className="agx-role-confirm" role="group" aria-label={`Delete ${name}`}>
          <p>{uses?.length
            ? `${name} staffs the ${slots(uses.length)} above. Give ${uses.length === 1 ? 'it' : 'each'} another role or none before deleting ${name}, or retire ${name} instead.`
            : card?.customized
              ? `Delete your changes to ${name}? The built-in ${name} comes back.`
              : `Delete ${name}? Its card is removed for good.`}</p>
          <div className="pop-actions">
            {uses?.length ? null : <button className="retire" type="button" onClick={() => { setConfirm(null); onDelete() }}>Delete {name}</button>}
            <button className="cancel" type="button" onClick={() => setConfirm(null)}>{uses?.length ? 'Close' : 'Cancel'}</button>
          </div>
        </div>
      ) : (
        <div className="pop-actions">
          {retired
            ? <button className="cancel" type="button" disabled={!card} onClick={() => onRetire(false)}>Bring back</button>
            : <button className="cancel" type="button" disabled={!card || !uses} onClick={() => setConfirm('retire')}>Retire</button>}
          {deletable ? <button className="cancel" type="button" disabled={!uses} onClick={() => setConfirm('delete')}>{card?.customized ? 'Delete changes' : 'Delete'}</button> : null}
        </div>
      )}
    </section>
  )
}

function CreatePersonaPopover({
  error, onDismiss,
  draft,
  onDraft,
  onSubmit,
  onClose,
}: {
  error: string
  onDismiss: () => void
  draft: CreateDraft
  onDraft: (draft: CreateDraft) => void
  onSubmit: (event: FormEvent) => void
  onClose: () => void
}) {
  const set = (key: keyof CreateDraft, value: string) => onDraft({ ...draft, [key]: value })
  return (
    <div className="pop agx-pop" role="dialog" aria-label="New role">
      <div className="pop-head">
        <span className="pt">New role</span>
        <button className="x" type="button" onClick={onClose} aria-label="Close">×</button>
      </div>
      <form className="pop-body" onSubmit={onSubmit}>
        <label htmlFor="agx-create-id">Role id</label>
        <input autoFocus id="agx-create-id" className="f" value={draft.id} onChange={event => set('id', event.target.value)} />
        <label htmlFor="agx-create-display">Display name</label>
        <input id="agx-create-display" className="f" value={draft.displayName} onChange={event => set('displayName', event.target.value)} />
        <label htmlFor="agx-create-kind">Kind</label>
        <input id="agx-create-kind" className="f" value={draft.kind} onChange={event => set('kind', event.target.value)} />
        <label htmlFor="agx-create-summary">Summary</label>
        <input id="agx-create-summary" className="f" value={draft.summary} onChange={event => set('summary', event.target.value)} />
        <label htmlFor="agx-create-capabilities">Capabilities</label>
        <input id="agx-create-capabilities" className="f" value={draft.capabilities} onChange={event => set('capabilities', event.target.value)} />
        <p className="field-note">Role id: lowercase letters, digits and hyphens, for example brief-critic.</p>
        {error && <div className="field-note error" role="alert">{error} <button type="button" onClick={onDismiss}>Dismiss</button></div>}
        <button className="save" type="submit">Create role</button>
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

function splitCommaList(value: string): string[] {
  return value.split(',').map(part => part.trim()).filter(Boolean)
}
