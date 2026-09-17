import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import type { EvidenceNames } from '../evidence/evidenceNames'
import { formatBytes } from '../evidence/runEvidenceApi'
import { fileAnchor, useFileWindows } from './FileWindows'
import { artifactFileRequest, outputFileRequest, type FileRequest } from './fileWindowModel'
import type { NodeProduced, ProducedItem, RunProducedSummary } from './produced'
import './produced.css'

// Chips for what a run produced. A card, a node window or a Flow row shows a
// step's chips with <ProducedFiles nodeId={…} />, and the run bar shows the
// run's with <RunProduced />; each chip opens its file in a file window.

export interface RunProducedValue {
  runId: string
  byNode: ReadonlyMap<string, NodeProduced>
  summary: RunProducedSummary
  names: Pick<EvidenceNames, 'node' | 'port'>
}

const RunProducedContext = createContext<RunProducedValue | null>(null)

export function RunProducedProvider({ value, children }: { value: RunProducedValue | null; children: ReactNode }) {
  return <RunProducedContext.Provider value={value}>{children}</RunProducedContext.Provider>
}

export function producedLabel(item: ProducedItem, names: RunProducedValue['names']): string {
  if (item.artifact) return item.artifact.split('/').pop() || item.artifact
  if (item.portId && item.nodeId) return names.port(item.nodeId, item.portId)
  return 'report'
}

export function producedRequest(runId: string, item: ProducedItem, names: RunProducedValue['names']): FileRequest {
  const step = item.nodeId ? names.node(item.nodeId) : ''
  if (item.artifact) return artifactFileRequest(runId, item.artifact, step || undefined)
  return outputFileRequest(runId, item.nodeId || '', producedLabel(item, names), step, item.portId)
}

function ProducedChip({ item, value, onOpened, role }: {
  item: ProducedItem
  value: RunProducedValue
  onOpened?: () => void
  role?: 'menuitem'
}) {
  const files = useFileWindows()
  const label = producedLabel(item, value.names)
  const step = item.nodeId ? value.names.node(item.nodeId) : ''
  const what = item.artifact ? item.artifact : `${label} output`
  return (
    <button
      type="button"
      role={role}
      className={`produced-chip ${item.kind}`}
      data-testid={`produced-chip-${item.key}`}
      title={`Open ${what}${step ? ` from ${step}` : ''} · ${formatBytes(item.bytes)}`}
      onPointerDown={event => event.stopPropagation()}
      onClick={event => {
        event.stopPropagation()
        files?.open(producedRequest(value.runId, item, value.names), fileAnchor(event.currentTarget))
        onOpened?.()
      }}
    >
      <span className="produced-glyph" aria-hidden="true">{item.kind === 'artifact' ? '▤' : '¶'}</span>
      {label}
    </button>
  )
}

/** A step's produced chips, for its card, node window or Flow row. Nothing until it has produced something. */
export function ProducedFiles({ nodeId, className }: { nodeId: string; className?: string }) {
  const value = useContext(RunProducedContext)
  const files = useFileWindows()
  const items = value?.byNode.get(nodeId)?.items || []
  if (!value || !files || items.length === 0) return null
  return (
    <div className={`produced${className ? ` ${className}` : ''}`} data-testid={`produced-${nodeId}`} aria-label={`Produced by ${value.names.node(nodeId)}`}>
      {items.map(item => <ProducedChip key={item.key} item={item} value={value} />)}
    </div>
  )
}

const RUN_BAR_CHIPS = 3

/** The run bar's Produced list: the run's final or latest outputs, and a menu with everything else. */
export function RunProduced() {
  const value = useContext(RunProducedContext)
  const files = useFileWindows()
  const [menu, setMenu] = useState<{ left: number; top: number } | null>(null)
  const menuRef = useRef<HTMLDivElement | null>(null)
  const moreRef = useRef<HTMLButtonElement | null>(null)

  useEffect(() => {
    if (!menu) return
    const close = (event: Event) => {
      if (event instanceof KeyboardEvent) {
        if (event.key !== 'Escape') return
        moreRef.current?.focus()
      } else if (menuRef.current?.contains(event.target as Node) || moreRef.current?.contains(event.target as Node)) {
        return
      }
      setMenu(null)
    }
    document.addEventListener('pointerdown', close, true)
    document.addEventListener('keydown', close)
    return () => {
      document.removeEventListener('pointerdown', close, true)
      document.removeEventListener('keydown', close)
    }
  }, [menu])

  if (!value || !files) return null
  const { primary, others } = value.summary
  const lead = primary.length ? primary : others
  const shown = lead.slice(0, RUN_BAR_CHIPS)
  const rest = [...lead.slice(RUN_BAR_CHIPS), ...(primary.length ? others : [])]
  if (shown.length === 0) return null

  const groups: Array<{ title: string; items: ProducedItem[] }> = []
  for (const item of rest) {
    const title = item.nodeId ? value.names.node(item.nodeId) : 'Other files'
    const group = groups.find(entry => entry.title === title)
    if (group) group.items.push(item)
    else groups.push({ title, items: [item] })
  }

  return (
    <span className="run-produced" data-testid="run-produced" data-file-anchor>
      <span className="run-produced-label">produced</span>
      {shown.map(item => <ProducedChip key={item.key} item={item} value={value} />)}
      {rest.length ? (
        <button
          ref={moreRef}
          type="button"
          className="run-produced-more"
          aria-haspopup="menu"
          aria-expanded={Boolean(menu)}
          aria-label={`${rest.length} more produced files`}
          onClick={event => {
            const rect = event.currentTarget.getBoundingClientRect()
            setMenu(menu ? null : { left: rect.left, top: rect.bottom + 4 })
          }}
        >+{rest.length}</button>
      ) : null}
      {menu ? (
        <div ref={menuRef} className="run-produced-menu" role="menu" aria-label="Everything this run produced" style={{ left: menu.left, top: menu.top }}>
          {groups.map(group => (
            <div className="run-produced-group" key={group.title}>
              <div className="run-produced-group-title">{group.title}</div>
              {group.items.map(item => <ProducedChip key={item.key} item={item} value={value} role="menuitem" onOpened={() => setMenu(null)} />)}
            </div>
          ))}
        </div>
      ) : null}
    </span>
  )
}
