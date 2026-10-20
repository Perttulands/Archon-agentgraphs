import { useWindowStack } from '../windows/WindowManager'
import MediaPreview from '../files/MediaPreview'
import { useContext, useCallback, useEffect, useMemo, useState } from 'react'
import { gateKindLabel } from '../components/GateEditorDialog'
import FloatingWindow from '../windows/FloatingWindow'
import { mediaKind } from '../files/mediaUrls'
import { problemHeadline } from '../components/runOutcome'
import type { BoardDocument } from '../components/formationsTypes'
import { evidenceNamesForBoard, type EvidenceNames } from './evidenceNames'
import { fileAnchor, useFileWindows } from '../files/FileWindows'
import { attemptsAnchor } from '../attempts/attemptsPlacement'
import { artifactFileRequest, referencedFileRequest } from '../files/fileWindowModel'
import Markdown from './Markdown'
import TextLines, { prettyJson } from './TextLines'
import {
  artifactRawUrl,
  fetchArtifactPreview,
  fetchReferencedPreview,
  fetchNodeEvidence,
  fetchRunArtifacts,
  fetchRunBrief,
  type StepAttempts,
  formatBytes,
  type EvidenceAttempt,
  type EvidenceEvaluation,
  type EvidenceInput,
  type EvidenceItem,
  type EvidenceRef,
  type EvidenceText,
  type NodeEvidence,
  type RunArtifactEntry,
  type RunArtifactPreview,
} from './runEvidenceApi'
import { CitationContext, CitationScopeProvider, CitationFiles, CitedEvidence, evidencePaths, attemptCitationScope } from './CitationFiles'
import '../attempts/attempts.css'
import './evidence.css'
import { resolveCitation } from './evidenceCitations'
import { fetchCitationAttempts, evaluationCitationScope, withJudgingAttempts } from './citationAttempts'

// The cockpit's run evidence inspector. It reads the evidence routes
// (ADR-0017) rather than the sanitized projection, so it shows what a node
// actually produced: outputs, gate reasons and judge evidence, human
// responses, the briefs seats received and the run's artifacts.

type OpenDocument =
  | { kind: 'brief'; title: string; text: EvidenceText }
  | { kind: 'artifact'; preview: RunArtifactPreview }

interface RunEvidenceProps {
  runId: string
  nodeId: string
  title: string
  /** The node's run state from the live projection; a change refetches. */
  state: string
  /** Names the evidence's node, port and slot IDs; without it they show as IDs. */
  board?: BoardDocument | null
  onClose: () => void
}

// A seat that ended, or was kept on call for a human gate (ADR-0019), needs nothing; any other outcome left a seat behind or lost it.
const CLEANUP_SETTLED = new Set(['ended', 'kept_on_call'])

const errorText = (error: unknown) => (error instanceof Error ? error.message : String(error))
const byteLength = (text: string) => new TextEncoder().encode(text).length

/** Runtime codes remain in data; evidence reads in the operator's words. */
const evidenceWord = (value: string) => ({ pass: 'PASS', fail: 'FAIL', canceled: 'Stopped', initial: 'first attempt', waiting_human: 'waiting for you' }[value] || value.replace(/_/g, ' '))

export default function RunEvidence({ runId, nodeId, title, state, board, onClose }: RunEvidenceProps) {
  const names = useMemo(() => evidenceNamesForBoard(board), [board])
  const [evidence, setEvidence] = useState<NodeEvidence | null>(null)
  const [error, setError] = useState('')
  const [artifacts, setArtifacts] = useState<{ artifacts: RunArtifactEntry[]; truncated: boolean; omitted: number } | null>(null)
  const [attemptSteps, setAttemptSteps] = useState<StepAttempts[]>([])
  useEffect(() => {
    let current = true
    if (!evidence || evidence.nodeId !== nodeId) return
    fetchCitationAttempts(runId, evidence).then(steps => { if (current) setAttemptSteps(steps) }, () => {})
    return () => { current = false }
  }, [runId, nodeId, evidence])
  const [artifactError, setArtifactError] = useState('')
  const [artifactRetry, setArtifactRetry] = useState(0)
  const [openDocument, setOpenDocument] = useState<OpenDocument | null>(null)
  // Escape closes an open document first, then the evidence dialog.
  const stack = useWindowStack()
  const [anchor] = useState(() => document.activeElement ? attemptsAnchor(document.activeElement) : null)
  useEffect(() => {
    if (!openDocument) return
    const closeDocument = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented || stack.order[stack.order.length - 1] !== `evidence:${nodeId}`) return
      event.preventDefault()
      setOpenDocument(null)
    }
    window.addEventListener('keydown', closeDocument, true)
    return () => window.removeEventListener('keydown', closeDocument, true)
  }, [nodeId, openDocument, stack.order])
  const [documentError, setDocumentError] = useState('')

  useEffect(() => {
    let current = true
    setError('')
    fetchNodeEvidence(runId, nodeId).then(next => { if (current) setEvidence(next) }, reason => { if (current) setError(errorText(reason)) })
    return () => { current = false }
  }, [runId, nodeId, state])

  // Retrying the artifact list leaves the healthy node and open document alone.
  // Each read owns its lifetime, including a changed run/state or closed view.
  useEffect(() => {
    let current = true
    setArtifacts(null)
    setArtifactError('')
    fetchRunArtifacts(runId).then(next => { if (current) setArtifacts(next) }, reason => { if (current) setArtifactError(errorText(reason)) })
    return () => { current = false }
  }, [runId, nodeId, state, artifactRetry])

  // In the cockpit an artifact opens in its own file window; elsewhere it opens in place.
  // A name with a leading slash is a ref outside the artifact directory, by absolute path.
  const files = useFileWindows()
  const openArtifact = useCallback((name: string, source?: Element) => {
    setDocumentError('')
    const absolute = name.startsWith('/')
    if (files) {
      files.open(absolute ? referencedFileRequest(name, title) : artifactFileRequest(runId, name, title), (source || document.activeElement) ? fileAnchor((source || document.activeElement)!) : anchor)
      return
    }
    const preview = absolute ? fetchReferencedPreview(name) : fetchArtifactPreview(runId, name)
    preview.then(preview => setOpenDocument({ kind: 'artifact', preview }), reason => setDocumentError(`${name}: ${errorText(reason)}`))
  }, [anchor, files, runId, title])

  const failures = evidence?.problems || []
  const gate = evidence?.kind === 'gate' || board?.gates?.some(gate => gate.id === nodeId)
  const verdict = gate && (state === 'done' || state === 'failed') ? (state === 'done' ? 'PASS' : 'FAIL') : ''

  const openBrief = useCallback((seq: number, label: string) => {
    setDocumentError('')
    fetchRunBrief(runId, seq).then(brief => setOpenDocument({ kind: 'brief', title: `Brief · ${label}`, text: brief.text }), reason => setDocumentError(`Brief: ${errorText(reason)}`))
  }, [runId])

  return (
    <FloatingWindow id={`evidence:${nodeId}`} kind="evidence" label={`Run evidence · ${title}`}
      title={`Run evidence · ${title}`} className="run-evidence" defaultSize={{ width: 660, height: 640 }}
      anchor={() => anchor?.anchor || null}
      keepClear={() => anchor?.keepClear || []} anchorKind={anchor?.anchorKind}
      onClose={onClose}>
      <CitationContext.Provider value={{ runId, artifacts: [...(artifacts?.artifacts.map(file => file.name) || []), ...attemptSteps.flatMap(step => step.attempts.flatMap(attempt => attemptCitationScope(attempt).artifacts))], directories: [], paths: evidencePaths(evidence), open: openArtifact }}>
      <div data-testid="node-inspector" className="node-evidence">
        <dl className="node-evidence-identity">
          <div><dt>{verdict ? 'Verdict' : 'State'}</dt><dd data-testid="node-evidence-state">{evidenceWord(verdict || state || 'not started')}</dd></div>
        </dl>
        <EvidenceIds runId={runId} nodeId={nodeId} evidence={evidence} names={names} />

        {error ? <div className="note-error" role="alert">Evidence unavailable: {error}</div> : null}
        {!evidence && !error ? <div className="node-evidence-empty" role="status">Loading evidence…</div> : null}

        {openDocument ? (
          <DocumentView runId={runId} document={openDocument} onOpenArtifact={openArtifact} onClose={() => setOpenDocument(null)} />
        ) : null}
        {documentError ? <div className="note-error" role="alert">{documentError}</div> : null}

        {evidence && evidence.kind === 'gate' ? (
          <GateEvaluations runId={runId} evaluations={evidence.evaluations || []} attemptSteps={attemptSteps} names={names} onOpenArtifact={openArtifact} />
        ) : null}
        {evidence && evidence.kind !== 'gate' ? (
          <Attempts runId={runId} nodeId={nodeId} attempts={evidence.attempts || []} attemptSteps={attemptSteps} names={names} onOpenArtifact={openArtifact} onOpenBrief={openBrief} />
        ) : null}

        {failures.length ? (
          <section className="node-evidence-section" data-testid="evidence-failures">
            <h3>Blocks and errors</h3>
            {failures.map(problem => (
              <div className={`node-evidence-verdict fail${problem.resumedSeq ? ' resumed' : ''}`} key={problem.seq} data-testid={`evidence-problem-${problem.seq}`}>
                <div className="node-verdict-line">{problemHeadline(problem, names)}</div>
                <EvidenceTextView value={problem.reason} format="text" label={problem.type === 'run_failed' || problem.type === 'run_canceled' ? 'Why the run ended' : 'Block reason'} />
              </div>
            ))}
          </section>
        ) : null}

        <section className="node-evidence-section">
          <h3>Run artifacts</h3>
          {artifactError ? (
            <div className="note-error" role="alert">Artifacts unavailable: {artifactError} <button type="button" className="evidence-link" onClick={() => setArtifactRetry(retry => retry + 1)}>Retry</button></div>
          ) : !artifacts ? <div className="node-evidence-empty" role="status">Loading artifacts…</div> : artifacts.artifacts.length === 0 ? (
            <div className="node-evidence-empty">This run has no artifacts.</div>
          ) : (
            <ul className="evidence-artifacts" data-testid="run-artifacts">
              {artifacts.artifacts.map(artifact => (
                <li key={artifact.name}>
                  <button type="button" className="evidence-link" onClick={() => openArtifact(artifact.name)}>{artifact.name}</button>
                  <span className="evidence-meta">{formatBytes(artifact.size)}</span>
                  <a className="evidence-meta" href={artifactRawUrl(runId, artifact.name)} target="_blank" rel="noopener noreferrer">raw</a>
                </li>
              ))}
            </ul>
          )}
          {artifacts?.omitted ? <p className="evidence-note">At least {artifacts.omitted} more files not listed.</p> : null}
        </section>
      </div>
      </CitationContext.Provider>
    </FloatingWindow>
  )
}

const sameText = (a: EvidenceText, b: EvidenceText) => a.bytes > 0 && a.bytes === b.bytes && a.text === b.text

function Attempts({ runId, nodeId, attempts, attemptSteps, names, onOpenArtifact, onOpenBrief }: {
  runId: string
  nodeId: string
  attempts: EvidenceAttempt[]
  attemptSteps: StepAttempts[]
  names: EvidenceNames
  onOpenArtifact: (name: string) => void
  onOpenBrief: (seq: number, label: string) => void
}) {
  const latestAttempt = [...attempts].reverse().find(attempt => attempt.output)
  const latestOutput = latestAttempt?.output
  const scopeFor = (number?: number) => {
    const attempt = attemptSteps.find(step => step.nodeId === nodeId)?.attempts.find(attempt => attempt.attempt === number)
    return attempt ? withJudgingAttempts(attempt, attemptSteps) : {}
  }
  // Seats usually return the same text as the whole output and as its port: print it once.
  const outputInPort = latestOutput ? latestOutput.ports.some(port => sameText(port.text, latestOutput.text)) : false
  return (
    <>
      <CitationScopeProvider scope={scopeFor(latestAttempt?.attempt)}>
      <section className="node-evidence-section">
        <h3>Output</h3>
        {latestOutput ? (
          <div className="node-evidence-output" data-testid="node-output">
            <div className="node-output-status">#{latestOutput.seq} · status · {evidenceWord(latestOutput.status || 'done')}</div>
            {latestOutput.reason ? <EvidenceTextView value={latestOutput.reason} format="text" label="Output reason" /> : null}
            {latestOutput.text.bytes && !outputInPort ? (
              <div data-testid="node-output-value"><EvidenceTextView value={latestOutput.text} label="Output" /></div>
            ) : null}
            {latestOutput.ports.map((port, index) => {
              const label = names.port(nodeId, port.portId)
              const earlier = latestOutput.ports.slice(0, index).find(other => sameText(other.text, port.text))
              return (
                <div className="evidence-port" data-testid={`node-output-port-${port.portId}`} key={port.portId}>
                  <div className="evidence-port-head">
                    <span className="node-output-port-id">{label}</span>
                    <RefLink runId={runId} value={port.ref} onOpenArtifact={onOpenArtifact} />
                  </div>
                  {!port.text.bytes ? <div className="node-evidence-empty">No inline text.</div>
                    : earlier ? <div className="node-evidence-empty">Same text as {names.port(nodeId, earlier.portId)}.</div>
                      : <EvidenceTextView value={port.text} label={`Port ${label}`} />}
                </div>
              )
            })}
          </div>
        ) : <div className="node-evidence-empty">No output recorded yet.</div>}
      </section>
      </CitationScopeProvider>

      <section className="node-evidence-section">
        <h3>Attempts</h3>
        {attempts.length === 0 ? <div className="node-evidence-empty">No attempts recorded yet.</div> : attempts.map(attempt => (
          <CitationScopeProvider key={`${attempt.attempt}-${attempt.startedSeq || 0}`} scope={scopeFor(attempt.attempt)}>
          <div className="node-evidence-attempt" data-testid={`node-attempt-${attempt.attempt}`} key={`${attempt.attempt}-${attempt.startedSeq || 0}`}>
            <div className="node-attempt-head">Attempt {attempt.attempt}{attempt.reason ? ` · ${evidenceWord(attempt.reason)}` : ''}</div>
            {attempt.inputs.map((input, index) => (
              <InputView key={`${input.edgeId || index}`} runId={runId} input={input} names={names} onOpenArtifact={onOpenArtifact} />
            ))}
            {attempt.dispatches.length === 0 ? <div className="node-evidence-empty">No slot dispatch recorded.</div> : attempt.dispatches.map(dispatch => {
              const slot = dispatch.slotId ? names.slot(nodeId, dispatch.slotId) : 'slot'
              return (
                <div className="node-dispatch" key={dispatch.seq}>
                  <span className="node-dispatch-slot">{slot}</span>
                  <span className="node-dispatch-agent">{[dispatch.agentId, dispatch.harness].filter(Boolean).join(' · ') || 'agent not recorded'}</span>
                  {dispatch.phase ? <span className="node-dispatch-phase">{evidenceWord(dispatch.phase)}</span> : null}
                  {dispatch.rejected
                    ? <span className="node-dispatch-phase node-dispatch-rejected" title={dispatch.rejected}>verdict rejected</span>
                    : <span className="node-dispatch-phase">{evidenceWord(dispatch.status || 'no result yet')}</span>}
                  {dispatch.brief ? (
                    <button type="button" className="evidence-link" onClick={() => onOpenBrief(dispatch.seq, `${slot} attempt ${attempt.attempt}`)}>brief</button>
                  ) : null}
                </div>
              )
            })}
            {attempt.seatCleanups?.map(cleanup => (
              <div className={`node-dispatch${CLEANUP_SETTLED.has(cleanup.outcome) ? '' : ' evidence-cleanup-left'}`} key={cleanup.seq}>
                <span className="node-dispatch-slot">{cleanup.slotId ? names.slot(nodeId, cleanup.slotId) : 'seat'}</span>
                <span className="node-dispatch-agent">seat cleanup · {cleanup.outcome === 'kept_on_call' ? 'kept on call' : cleanup.outcome || 'not recorded'}</span>
              </div>
            ))}
          </div>
          </CitationScopeProvider>
        ))}
      </section>
    </>
  )
}

export function gateCitationScope(evaluation: EvidenceEvaluation, steps: StepAttempts[]) {
  return evaluationCitationScope(evaluation, steps)
}

function GateEvaluations({ runId, evaluations, attemptSteps, names, onOpenArtifact }: {
  runId: string
  evaluations: EvidenceEvaluation[]
  attemptSteps: StepAttempts[]
  names: EvidenceNames
  onOpenArtifact: (name: string) => void
}) {
  if (evaluations.length === 0) {
    return (
      <section className="node-evidence-section">
        <h3>Gate verdict</h3>
        <div className="node-evidence-empty">This gate has not been evaluated yet.</div>
      </section>
    )
  }
  return (
    <>
      {[...evaluations].reverse().map((evaluation, index) => (
        <CitationScopeProvider key={evaluation.seq} scope={gateCitationScope(evaluation, attemptSteps)}>
        <section className="node-evidence-section" data-testid={`gate-evaluation-${evaluation.seq}`}>
          <h3>{index === 0 ? 'Gate verdict' : 'Earlier evaluation'}{evaluation.attempt ? ` · attempt ${evaluation.attempt}` : ''}</h3>
          {evaluation.verdict ? (
            <div className={`node-evidence-verdict ${evaluation.verdict.verdict === 'fail' ? 'fail' : 'pass'}`} data-testid={index === 0 ? 'node-gate-verdict' : undefined}>
              <div className="node-verdict-line">verdict · {evidenceWord(evaluation.verdict.verdict || 'not recorded')}{evaluation.verdict.routePort ? ` · route ${evidenceWord(evaluation.verdict.routePort)}` : ''}</div>
              <EvidenceTextView value={evaluation.verdict.reason} label="Verdict reason" />
              {evaluation.verdict.perKind && Object.keys(evaluation.verdict.perKind).length ? (
                <div className="node-verdict-evidence">
                  {Object.entries(evaluation.verdict.perKind).map(([kind, result]) => (
                    <div className="node-verdict-kind" key={kind}><span>{gateKindLabel(kind)}</span><strong>{evidenceWord(result)}</strong></div>
                  ))}
                </div>
              ) : null}
            </div>
          ) : <div className="node-evidence-empty">{evaluation.judgeFailures?.length ? 'No verdict: the judge’s verdict was rejected.' : 'No verdict yet.'}</div>}

          {evaluation.humanRequests?.map(request => (
            <div className="evidence-block" key={request.seq}>
              <div className="evidence-label">Operator · request #{request.seq}</div>
              {request.decision ? (
                <>
                  <div className="node-verdict-line">
                    {evidenceWord(request.decision.verdict)}{request.decision.decidedBy ? ` · ${request.decision.decidedBy}` : ''}
                    {request.decision.relayedBy ? <span className="node-verdict-via" title={`Recorded by seat ${request.decision.relayedBy}`}> · via {names.relayer(request.decision.relayedBy)}</span> : null}
                  </div>
                  {request.decision.response.bytes ? <EvidenceTextView value={request.decision.response} label="Operator response" /> : <div className="node-evidence-empty">No response text.</div>}
                </>
              ) : <div className="node-evidence-empty">{request.pending ? 'Waiting for the operator.' : 'Not answered.'}</div>}
            </div>
          ))}

          {evaluation.kindResults.map(result => (
            <div className="evidence-block" key={result.seq}>
              <div className="evidence-label">{gateKindLabel(result.kind)} · {evidenceWord(result.verdict || 'no verdict')}</div>
              {result.reason.text !== evaluation.verdict?.reason.text ? <EvidenceTextView value={result.reason} label={`${result.kind} reason`} /> : null}
              <EvidenceItems items={result.evidence} omitted={result.evidenceOmitted} />
            </div>
          ))}
          {evaluation.verdict?.evidence.length && evaluation.kindResults.length === 0 ? (
            <EvidenceItems items={evaluation.verdict.evidence} omitted={evaluation.verdict.evidenceOmitted} />
          ) : null}

          {evaluation.judgeFailures?.map(failure => (
            <div className="node-evidence-verdict fail" key={failure.seq}>
              <div className="node-verdict-line">judge failed{failure.code ? ` · ${evidenceWord(failure.code)}` : ''}</div>
              <EvidenceTextView value={failure.reason} format="text" label="Judge failure" />
              {failure.returned ? (
                <div data-testid={`judge-returned-${failure.seq}`}>
                  <div className="evidence-label">The judge returned</div>
                  <EvidenceTextView value={failure.returned} format="text" label="Rejected verdict" />
                </div>
              ) : null}
            </div>
          ))}

          <div className="evidence-block">
            <div className="evidence-label">Criterion · {evaluation.kinds.map(gateKindLabel).join(' · ') || 'gate'}</div>
            {evaluation.criterion.bytes ? <EvidenceTextView value={evaluation.criterion} label="Criterion" /> : <div className="node-evidence-empty">No criterion recorded.</div>}
          </div>
          {evaluation.input ? <InputView runId={runId} input={evaluation.input} names={names} onOpenArtifact={onOpenArtifact} /> : null}
        </section>
        </CitationScopeProvider>
      ))}
    </>
  )
}

function InputView({ runId, input, names, onOpenArtifact }: { runId: string; input: EvidenceInput; names: EvidenceNames; onOpenArtifact: (name: string) => void }) {
  const source = names.source(input.fromNodeId, input.fromPortId)
  return (
    <details className="evidence-input">
      <summary>
        Input from {source} · {formatBytes(input.text.bytes)}
        <RefLink runId={runId} value={input.ref} onOpenArtifact={onOpenArtifact} />
      </summary>
      <EvidenceTextView value={input.text} label={`Input from ${source}`} />
    </details>
  )
}

/** The ledger IDs behind the names, for the CLI and the ledger. */
function EvidenceIds({ runId, nodeId, evidence, names }: { runId: string; nodeId: string; evidence: NodeEvidence | null; names: EvidenceNames }) {
  const rows = new Map<string, string>([[runId, 'Run'], [nodeId, names.node(nodeId)]])
  // A name that is its own ID, such as a gate's "pass" port, has nothing to disclose.
  const add = (id: string | undefined, name: string) => { if (id && name !== id && !rows.has(id)) rows.set(id, name) }
  const addInput = (input?: EvidenceInput) => {
    if (!input) return
    add(input.fromNodeId, names.node(input.fromNodeId || ''))
    if (input.fromNodeId) add(input.fromPortId, names.port(input.fromNodeId, input.fromPortId || ''))
    add(input.toPortId, names.port(nodeId, input.toPortId || ''))
    add(input.edgeId, `Connection from ${names.source(input.fromNodeId, input.fromPortId)}`)
  }
  for (const attempt of evidence?.attempts || []) {
    attempt.inputs.forEach(addInput)
    for (const dispatch of attempt.dispatches) add(dispatch.slotId, names.slot(nodeId, dispatch.slotId || ''))
    for (const port of attempt.output?.ports || []) add(port.portId, names.port(nodeId, port.portId))
  }
  for (const evaluation of evidence?.evaluations || []) {
    addInput(evaluation.input)
    for (const judge of evaluation.judgeChain || []) add(judge, names.node(judge))
  }
  return (
    <details className="evidence-ids" data-testid="evidence-ids">
      <summary>IDs</summary>
      <dl>
        {[...rows].map(([id, name]) => (
          <div key={id}><dt>{name === id ? 'ID' : name}</dt><dd>{id}</dd></div>
        ))}
      </dl>
    </details>
  )
}

function EvidenceItems({ items, omitted }: { items: EvidenceItem[]; omitted?: number }) {
  const context = useContext(CitationContext)
  if (items.length === 0) return null
  return (
    <CitationContext.Provider value={context ? { ...context, paths: [...evidencePaths(items), ...context.paths] } : null}>
    <ul className="evidence-items">
      {items.map((item, index) => (
        <li key={index}>
          <CitedEvidence text={item.text.text} />
          {item.text.truncated ? <span className="evidence-meta"> · first {formatBytes(byteLength(item.text.text))} of {formatBytes(item.text.bytes)}</span> : null}
        </li>
      ))}
      {omitted ? <li className="evidence-meta">{omitted} more items not shown</li> : null}
    </ul>
    </CitationContext.Provider>
  )
}

function RefLink({ runId, value, onOpenArtifact }: { runId: string; value?: EvidenceRef; onOpenArtifact: (name: string) => void }) {
  if (!value) return null
  if (value.artifact) {
    const name = value.artifact
    return (
      <span className="evidence-ref">
        <button type="button" className="evidence-link" onClick={event => { event.preventDefault(); onOpenArtifact(name) }}>{name}</button>
        <a className="evidence-meta" href={artifactRawUrl(runId, name)} target="_blank" rel="noopener noreferrer">raw</a>
      </span>
    )
  }
  const path = value.external
  if (!path) return null
  return (
    <span className="evidence-ref">
      <button type="button" className="evidence-link" title={`Open ${path}`} onClick={event => { event.preventDefault(); onOpenArtifact(path) }}>{path}</button>
    </span>
  )
}

/** One capped text, drawn as Markdown or plain lines, with its truncation said plainly. */
function EvidenceTextView({ value, label, format = 'markdown', basePath, runId, onOpenArtifact }: {
  value: EvidenceText
  label: string
  format?: 'markdown' | 'text' | 'lines'
  basePath?: string
  runId?: string
  onOpenArtifact?: (name: string) => void
}) {
  const citations = useContext(CitationContext)
  const markdownOpen = onOpenArtifact || (citations?.open ? (path: string) => {
    const resolved = resolveCitation(path, citations.artifacts, citations.paths, citations.directories)
    if (resolved) citations.open?.(resolved)
  } : undefined)
  if (!value.text && !value.truncated) return null
  return (
    <div className="evidence-text" aria-label={label}>
      {format === 'markdown' ? (
        <Markdown
          content={value.text}
          basePath={basePath}
          onOpenPath={markdownOpen}
          imageUrl={runId ? path => artifactRawUrl(runId, path) : undefined}
        />
      ) : format === 'lines' ? (
        <TextLines content={value.text} label={label} />
      ) : (
        <pre className="evidence-plain">{value.text}</pre>
      )}
      <CitationFiles text={value.text} excludeMarkdownLinks={format === 'markdown'} />
      {value.truncated ? (
        <p className="evidence-note" data-testid="evidence-truncated">
          Showing {formatBytes(byteLength(value.text))} of {formatBytes(value.bytes)}. The rest stays in the run evidence on the host.
        </p>
      ) : null}
    </div>
  )
}

function DocumentView({ runId, document, onOpenArtifact, onClose }: {
  runId: string
  document: OpenDocument
  onOpenArtifact: (name: string) => void
  onClose: () => void
}) {
  const heading = document.kind === 'brief' ? document.title : document.preview.name
  return (
    <section className="node-evidence-section evidence-document" data-testid="evidence-document" aria-label={heading}>
      <div className="evidence-document-head">
        <h3>{heading}</h3>
        {document.kind === 'artifact' ? (
          <>
            <span className="evidence-meta">{document.preview.kind} · {formatBytes(document.preview.size)}</span>
            <a className="evidence-meta" href={artifactRawUrl(runId, document.preview.name)} target="_blank" rel="noopener noreferrer">open raw</a>
          </>
        ) : null}
        <button type="button" className="evidence-link" onClick={onClose}>close</button>
      </div>
      {document.kind === 'brief' ? <EvidenceTextView value={document.text} format="lines" label={heading} /> : <ArtifactBody runId={runId} preview={document.preview} onOpenArtifact={onOpenArtifact} />}
    </section>
  )
}

function ArtifactBody({ runId, preview, onOpenArtifact }: { runId: string; preview: RunArtifactPreview; onOpenArtifact: (name: string) => void }) {
  const media = mediaKind(preview.name) || (preview.kind === 'video' || preview.kind === 'model' ? preview.kind : null)
  if (media) return <MediaPreview kind={media} src={artifactRawUrl(runId, preview.name)} name={preview.name} />
  if (preview.kind === 'image') return <img className="evidence-image" src={artifactRawUrl(runId, preview.name)} alt={preview.name} />
  if (preview.kind === 'pdf') return <p className="evidence-note">PDF. Open it raw to read it.</p>
  if (preview.kind === 'binary' || !preview.text) return <p className="evidence-note">Binary file. Open it raw to download.</p>
  if (preview.kind === 'markdown') {
    return <EvidenceTextView value={preview.text} label={preview.name} basePath={preview.name} runId={runId} onOpenArtifact={onOpenArtifact} />
  }
  const text = preview.kind === 'json' && !preview.text.truncated ? { ...preview.text, text: prettyJson(preview.text.text) } : preview.text
  return <EvidenceTextView value={text} format="lines" label={preview.name} />
}
