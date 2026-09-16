import { useCallback, useEffect, useState } from 'react'
import { gateKindLabel } from '../components/GateEditorDialog'
import { useEscapeKey } from '../components/useEscapeKey'
import Markdown from './Markdown'
import TextLines, { prettyJson } from './TextLines'
import {
  artifactRawUrl,
  fetchArtifactPreview,
  fetchNodeEvidence,
  fetchRunArtifacts,
  fetchRunBrief,
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
import './evidence.css'

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
  onClose: () => void
}

const errorText = (error: unknown) => (error instanceof Error ? error.message : String(error))
const byteLength = (text: string) => new TextEncoder().encode(text).length

export default function RunEvidence({ runId, nodeId, title, state, onClose }: RunEvidenceProps) {
  const [evidence, setEvidence] = useState<NodeEvidence | null>(null)
  const [error, setError] = useState('')
  const [artifacts, setArtifacts] = useState<{ artifacts: RunArtifactEntry[]; truncated: boolean } | null>(null)
  const [openDocument, setOpenDocument] = useState<OpenDocument | null>(null)
  // Escape closes an open document first, then the evidence dialog.
  useEscapeKey(true, () => (openDocument ? setOpenDocument(null) : onClose()))
  const [documentError, setDocumentError] = useState('')

  useEffect(() => {
    let current = true
    setError('')
    fetchNodeEvidence(runId, nodeId).then(next => { if (current) setEvidence(next) }, reason => { if (current) setError(errorText(reason)) })
    fetchRunArtifacts(runId).then(next => { if (current) setArtifacts(next) }, () => { if (current) setArtifacts(null) })
    return () => { current = false }
  }, [runId, nodeId, state])

  const openArtifact = useCallback((name: string) => {
    setDocumentError('')
    fetchArtifactPreview(runId, name).then(preview => setOpenDocument({ kind: 'artifact', preview }), reason => setDocumentError(`${name}: ${errorText(reason)}`))
  }, [runId])

  const openBrief = useCallback((seq: number, label: string) => {
    setDocumentError('')
    fetchRunBrief(runId, seq).then(brief => setOpenDocument({ kind: 'brief', title: `Brief · ${label}`, text: brief.text }), reason => setDocumentError(`Brief: ${errorText(reason)}`))
  }, [runId])

  return (
    <div
      className="pop node-inspector run-evidence"
      role="dialog"
      aria-label={`Run evidence · ${title}`}
      data-testid="node-inspector"
      onPointerDown={event => event.stopPropagation()}
    >
      <div className="pop-head">
        <span className="pt">Run evidence · {title}</span>
        <button className="x" type="button" aria-label="Close run evidence" onClick={onClose}>x</button>
      </div>
      <div className="pop-body node-evidence">
        <dl className="node-evidence-identity">
          <div><dt>Node</dt><dd>{nodeId}</dd></div>
          <div><dt>State</dt><dd data-testid="node-evidence-state">{state || 'not started'}</dd></div>
        </dl>

        {error ? <div className="note-error" role="alert">Evidence unavailable: {error}</div> : null}
        {!evidence && !error ? <div className="node-evidence-empty" role="status">Loading evidence…</div> : null}

        {openDocument ? (
          <DocumentView runId={runId} document={openDocument} onOpenArtifact={openArtifact} onClose={() => setOpenDocument(null)} />
        ) : null}
        {documentError ? <div className="note-error" role="alert">{documentError}</div> : null}

        {evidence && evidence.kind === 'gate' ? (
          <GateEvaluations runId={runId} evaluations={evidence.evaluations || []} onOpenArtifact={openArtifact} />
        ) : null}
        {evidence && evidence.kind !== 'gate' ? (
          <Attempts runId={runId} attempts={evidence.attempts || []} onOpenArtifact={openArtifact} onOpenBrief={openBrief} />
        ) : null}

        {evidence?.problems?.length ? (
          <section className="node-evidence-section">
            <h3>Blocks and errors</h3>
            {evidence.problems.map(problem => (
              <div className="node-evidence-verdict fail" key={problem.seq}>
                <div className="node-verdict-line">#{problem.seq} {problem.type}{problem.code ? ` · ${problem.code}` : ''}{problem.resumeAllowed === undefined ? '' : problem.resumeAllowed ? ' · resumable' : ' · not resumable'}</div>
                <EvidenceTextView value={problem.reason} format="text" label="Block reason" />
              </div>
            ))}
          </section>
        ) : null}

        <section className="node-evidence-section">
          <h3>Run artifacts</h3>
          {!artifacts ? <div className="node-evidence-empty">No artifact list.</div> : artifacts.artifacts.length === 0 ? (
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
          {artifacts?.truncated ? <p className="evidence-note">The list stops at 500 files or 8 folders deep.</p> : null}
        </section>
      </div>
    </div>
  )
}

function Attempts({ runId, attempts, onOpenArtifact, onOpenBrief }: {
  runId: string
  attempts: EvidenceAttempt[]
  onOpenArtifact: (name: string) => void
  onOpenBrief: (seq: number, label: string) => void
}) {
  const latestOutput = [...attempts].reverse().find(attempt => attempt.output)?.output
  return (
    <>
      <section className="node-evidence-section">
        <h3>Output</h3>
        {latestOutput ? (
          <div className="node-evidence-output" data-testid="node-output">
            <div className="node-output-status">#{latestOutput.seq} · status · {latestOutput.status || 'done'}</div>
            {latestOutput.reason ? <EvidenceTextView value={latestOutput.reason} format="text" label="Output reason" /> : null}
            {latestOutput.text.bytes ? (
              <div data-testid="node-output-value"><EvidenceTextView value={latestOutput.text} label="Output" /></div>
            ) : null}
            {latestOutput.ports.map(port => (
              <div className="evidence-port" data-testid={`node-output-port-${port.portId}`} key={port.portId}>
                <div className="evidence-port-head">
                  <span className="node-output-port-id">{port.portId}</span>
                  <RefLink runId={runId} value={port.ref} onOpenArtifact={onOpenArtifact} />
                </div>
                {port.text.bytes ? <EvidenceTextView value={port.text} label={`Port ${port.portId}`} /> : <div className="node-evidence-empty">No inline text.</div>}
              </div>
            ))}
          </div>
        ) : <div className="node-evidence-empty">No output recorded yet.</div>}
      </section>

      <section className="node-evidence-section">
        <h3>Attempts</h3>
        {attempts.length === 0 ? <div className="node-evidence-empty">No attempts recorded yet.</div> : attempts.map(attempt => (
          <div className="node-evidence-attempt" data-testid={`node-attempt-${attempt.attempt}`} key={`${attempt.attempt}-${attempt.startedSeq || 0}`}>
            <div className="node-attempt-head">Attempt {attempt.attempt}{attempt.reason ? ` · ${attempt.reason}` : ''}</div>
            {attempt.inputs.map((input, index) => (
              <InputView key={`${input.edgeId || index}`} runId={runId} input={input} onOpenArtifact={onOpenArtifact} />
            ))}
            {attempt.dispatches.length === 0 ? <div className="node-evidence-empty">No slot dispatch recorded.</div> : attempt.dispatches.map(dispatch => (
              <div className="node-dispatch" key={dispatch.seq}>
                <span className="node-dispatch-slot">{dispatch.slotId || 'slot'}</span>
                <span className="node-dispatch-agent">{[dispatch.agentId, dispatch.harness].filter(Boolean).join(' · ') || 'agent not recorded'}</span>
                {dispatch.phase ? <span className="node-dispatch-phase">{dispatch.phase}</span> : null}
                <span className="node-dispatch-phase">{dispatch.status || 'no result yet'}</span>
                {dispatch.brief ? (
                  <button type="button" className="evidence-link" onClick={() => onOpenBrief(dispatch.seq, `${dispatch.slotId || 'slot'} attempt ${attempt.attempt}`)}>brief</button>
                ) : null}
              </div>
            ))}
            {attempt.seatCleanups?.map(cleanup => (
              <div className={`node-dispatch${cleanup.outcome === 'ended' ? '' : ' evidence-cleanup-left'}`} key={cleanup.seq}>
                <span className="node-dispatch-slot">{cleanup.slotId || 'seat'}</span>
                <span className="node-dispatch-agent">seat cleanup · {cleanup.outcome || 'not recorded'}</span>
              </div>
            ))}
          </div>
        ))}
      </section>
    </>
  )
}

function GateEvaluations({ runId, evaluations, onOpenArtifact }: {
  runId: string
  evaluations: EvidenceEvaluation[]
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
        <section className="node-evidence-section" key={evaluation.seq} data-testid={`gate-evaluation-${evaluation.seq}`}>
          <h3>{index === 0 ? 'Gate verdict' : 'Earlier evaluation'}{evaluation.attempt ? ` · attempt ${evaluation.attempt}` : ''}</h3>
          {evaluation.verdict ? (
            <div className={`node-evidence-verdict ${evaluation.verdict.verdict === 'fail' ? 'fail' : 'pass'}`} data-testid={index === 0 ? 'node-gate-verdict' : undefined}>
              <div className="node-verdict-line">verdict · {evaluation.verdict.verdict || 'not recorded'}{evaluation.verdict.routePort ? ` · route ${evaluation.verdict.routePort}` : ''}</div>
              <EvidenceTextView value={evaluation.verdict.reason} label="Verdict reason" />
              {evaluation.verdict.perKind && Object.keys(evaluation.verdict.perKind).length ? (
                <div className="node-verdict-evidence">
                  {Object.entries(evaluation.verdict.perKind).map(([kind, result]) => (
                    <div className="node-verdict-kind" key={kind}><span>{gateKindLabel(kind)}</span><strong>{result}</strong></div>
                  ))}
                </div>
              ) : null}
            </div>
          ) : <div className="node-evidence-empty">No verdict yet.</div>}

          {evaluation.humanRequests?.map(request => (
            <div className="evidence-block" key={request.seq}>
              <div className="evidence-label">Operator · request #{request.seq}</div>
              {request.decision ? (
                <>
                  <div className="node-verdict-line">{request.decision.verdict}{request.decision.decidedBy ? ` · ${request.decision.decidedBy}` : ''}</div>
                  {request.decision.response.bytes ? <EvidenceTextView value={request.decision.response} label="Operator response" /> : <div className="node-evidence-empty">No response text.</div>}
                </>
              ) : <div className="node-evidence-empty">{request.pending ? 'Waiting for the operator.' : 'Not answered.'}</div>}
            </div>
          ))}

          {evaluation.kindResults.map(result => (
            <div className="evidence-block" key={result.seq}>
              <div className="evidence-label">{gateKindLabel(result.kind)} · {result.verdict || 'no verdict'}</div>
              {result.reason.text !== evaluation.verdict?.reason.text ? <EvidenceTextView value={result.reason} label={`${result.kind} reason`} /> : null}
              <EvidenceItems items={result.evidence} omitted={result.evidenceOmitted} />
            </div>
          ))}
          {evaluation.verdict?.evidence.length && evaluation.kindResults.length === 0 ? (
            <EvidenceItems items={evaluation.verdict.evidence} omitted={evaluation.verdict.evidenceOmitted} />
          ) : null}

          {evaluation.judgeFailures?.map(failure => (
            <div className="node-evidence-verdict fail" key={failure.seq}>
              <div className="node-verdict-line">judge failed{failure.code ? ` · ${failure.code}` : ''}</div>
              <EvidenceTextView value={failure.reason} format="text" label="Judge failure" />
            </div>
          ))}

          <div className="evidence-block">
            <div className="evidence-label">Criterion · {evaluation.kinds.map(gateKindLabel).join(' · ') || 'gate'}</div>
            {evaluation.criterion.bytes ? <EvidenceTextView value={evaluation.criterion} label="Criterion" /> : <div className="node-evidence-empty">No criterion recorded.</div>}
          </div>
          {evaluation.input ? <InputView runId={runId} input={evaluation.input} onOpenArtifact={onOpenArtifact} /> : null}
        </section>
      ))}
    </>
  )
}

function InputView({ runId, input, onOpenArtifact }: { runId: string; input: EvidenceInput; onOpenArtifact: (name: string) => void }) {
  const source = [input.fromNodeId, input.fromPortId].filter(Boolean).join(':') || 'mission'
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

function EvidenceItems({ items, omitted }: { items: EvidenceItem[]; omitted?: number }) {
  if (items.length === 0) return null
  return (
    <ul className="evidence-items">
      {items.map((item, index) => (
        <li key={index}>
          {item.text.text}
          {item.text.truncated ? <span className="evidence-meta"> · first {formatBytes(byteLength(item.text.text))} of {formatBytes(item.text.bytes)}</span> : null}
        </li>
      ))}
      {omitted ? <li className="evidence-meta">{omitted} more items not shown</li> : null}
    </ul>
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
  return (
    <span className="evidence-ref">
      <span className="evidence-meta" title="Outside the run's artifact directory; read it on the host">{value.external} · outside the run</span>
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
  if (!value.text && !value.truncated) return null
  return (
    <div className="evidence-text" aria-label={label}>
      {format === 'markdown' ? (
        <Markdown
          content={value.text}
          basePath={basePath}
          onOpenPath={onOpenArtifact}
          imageUrl={runId ? path => artifactRawUrl(runId, path) : undefined}
        />
      ) : format === 'lines' ? (
        <TextLines content={value.text} label={label} />
      ) : (
        <pre className="evidence-plain">{value.text}</pre>
      )}
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
  if (preview.kind === 'image') return <img className="evidence-image" src={artifactRawUrl(runId, preview.name)} alt={preview.name} />
  if (preview.kind === 'binary' || !preview.text) return <p className="evidence-note">Binary file. Open it raw to download.</p>
  if (preview.kind === 'markdown') {
    return <EvidenceTextView value={preview.text} label={preview.name} basePath={preview.name} runId={runId} onOpenArtifact={onOpenArtifact} />
  }
  const text = preview.kind === 'json' && !preview.text.truncated ? { ...preview.text, text: prettyJson(preview.text.text) } : preview.text
  return <EvidenceTextView value={text} format="lines" label={preview.name} />
}
