import MediaPreview from '../files/MediaPreview'
import { mediaKind } from '../files/mediaUrls'
import { useEffect, useState, type ReactNode } from 'react'
import Markdown from '../evidence/Markdown'
import { prettyJson } from '../evidence/TextLines'
import {
  artifactRawUrl,
  fetchArtifactPreview,
  formatBytes,
  type AttemptFile,
  type AttemptOutput,
  type AttemptVerdict,
  type EvidenceItem,
  type EvidenceText,
  type StepAttempt,
} from '../evidence/runEvidenceApi'
import LinkedText from '../files/LinkedText'
import { fileAnchor, useFileWindows } from '../files/FileWindows'
import { artifactFileRequest, folderFileRequest, referencedFileRequest, type FileRequest } from '../files/fileWindowModel'
import AttemptImage, { type ImageZoom } from './AttemptImage'
import '../files/produced.css'
import {
  OUTCOME_WORDS,
  attemptFileGroups,
  attemptOutcome,
  attemptTiming,
  baseName,
  folderSummary,
  isImagePath,
  judgeWords,
  nameWithin,
  pickerWords,
  textBytes,
  verdictHeadline,
  type AttemptOutcome,
} from './attemptsModel'

// One attempt of a step, as a column of the attempts window: its outcome and
// when it ran, what it produced drawn in place (images to judge, text to
// read, folders listed with what can be previewed), then what its gates said
// and why, and the rest of the files it left.

/** What every column in a window shares. */
export interface AttemptView {
  runId: string
  stepTitle: string
  waitingGates: ReadonlySet<string>
  zoom: ImageZoom | null
  onZoom: (zoom: ImageZoom | null) => void
  /** Two attempts side by side: images in frames of one size, so they compare at the same framing. */
  compare: boolean
  now: Date
  /** A step's title on the board, for naming the judge that picked. */
  titleOf?: (nodeId: string) => string
}

const FOLDER_THUMBS = 12
const FOLDER_ROWS = 8
const OTHER_FILES = 10

/** Text previews of output snapshots, which never change once the attempt recorded them. */
const previews = new Map<string, Promise<EvidenceText | undefined>>()

function snapshotText(runId: string, name: string): Promise<EvidenceText | undefined> {
  const key = `${runId}\n${name}`
  let preview = previews.get(key)
  if (!preview) {
    preview = fetchArtifactPreview(runId, name).then(artifact => artifact.text)
    preview.catch(() => previews.delete(key))
    previews.set(key, preview)
  }
  return preview
}

function useOpen() {
  const files = useFileWindows()
  return (request: FileRequest, control: Element) => files?.open(request, fileAnchor(control))
}

function FileChip({ request, label, detail, glyph = '▤', testId }: { request: FileRequest; label: string; detail?: string; glyph?: string; testId?: string }) {
  const open = useOpen()
  return (
    <button type="button" className="produced-chip attempt-chip" data-testid={testId} title={`Open ${request.name}`}
      onClick={event => open(request, event.currentTarget)}>
      <span className="produced-glyph" aria-hidden="true">{glyph}</span>
      <span className="attempt-chip-name">{label}</span>
      {detail ? <span className="attempt-chip-detail">{detail}</span> : null}
    </button>
  )
}

const upperFirst = (words: string) => words.charAt(0).toUpperCase() + words.slice(1)

/** What a cut text shows of the whole, as run evidence says it. */
const cutWords = (text: EvidenceText) => `… first ${formatBytes(textBytes(text.text))} of ${formatBytes(text.bytes)}`

/** A capped text: Markdown as Markdown, the rest as its lines. */
function TextBlock({ text, kind, basePath, runId }: { text: EvidenceText; kind: string; basePath?: string; runId: string }) {
  const open = useOpen()
  return (
    <div className="attempt-text">
      {kind === 'markdown' ? (
        <Markdown content={text.text} basePath={basePath}
          onOpenPath={basePath ? path => { const link = document.activeElement || document.body; open(artifactFileRequest(runId, path), link) } : undefined}
          imageUrl={basePath ? path => artifactRawUrl(runId, path) : undefined} />
      ) : <pre className="attempt-text-pre">{kind === 'json' ? prettyJson(text.text) : text.text}</pre>}
      {text.truncated ? <p className="attempt-cut">{cutWords(text)}</p> : null}
    </div>
  )
}

function SnapshotText({ runId, name, kind }: { runId: string; name: string; kind: string }) {
  const [state, setState] = useState<{ name: string; text?: EvidenceText; error?: string } | null>(null)
  useEffect(() => {
    let cancelled = false
    snapshotText(runId, name)
      .then(text => { if (!cancelled) setState({ name, text }) })
      .catch(err => { if (!cancelled) setState({ name, error: err instanceof Error ? err.message : 'cannot be read' }) })
    return () => { cancelled = true }
  }, [name, runId])
  if (!state || state.name !== name) return <p className="attempt-quiet" role="status">Reading…</p>
  if (state.error || !state.text) return <p className="attempt-quiet">{state.error || 'No text recorded.'}</p>
  return <TextBlock text={state.text} kind={kind} basePath={name} runId={runId} />
}

function ImageOutput({ src, name, group, view, request, detail, testId }: {
  src: string
  name: string
  group: string
  view: AttemptView
  request: FileRequest
  detail: string
  testId: string
}) {
  const [broken, setBroken] = useState(false)
  return (
    <figure className="attempt-figure" data-testid={testId}>
      {broken ? <p className="attempt-quiet">This image cannot be shown.</p> : (
        <AttemptImage src={src} label={name} group={group} zoom={view.zoom} onZoom={view.onZoom}
          frame={view.compare ? 'fixed' : 'natural'} onError={() => setBroken(true)} />
      )}
      <figcaption><FileChip request={request} label={name} detail={detail} /></figcaption>
    </figure>
  )
}

function FolderOutput({ runId, output, files, truncated, context, evidence }: {
  runId: string
  output: AttemptOutput
  files: AttemptFile[]
  truncated: boolean
  evidence: string[]
  context: string
}) {
  const open = useOpen()
  const folder = output.artifact || output.path || ''
  const summary = folderSummary(files, evidence)
  const thumbs = summary.images.slice(0, FOLDER_THUMBS)
  const rows = [...summary.rest, ...summary.images.slice(FOLDER_THUMBS)].slice(0, FOLDER_ROWS)
  const more = summary.count - thumbs.length - summary.videos.length - summary.models.length - rows.length
  const counted = `${summary.count} file${summary.count === 1 ? '' : 's'}${truncated ? ' listed' : ''}${summary.bytes ? ` · ${formatBytes(summary.bytes)}` : ''}`
  return (
    <div className="attempt-folder">
      {output.path ? (
        <FileChip request={folderFileRequest(output.path, context)} label={`${baseName(folder)}/`} detail={files.length || output.artifact ? counted : undefined} glyph="▥" />
      ) : null}
      {summary.videos.map(file => (
        <div key={file.name}>
          <MediaPreview kind={mediaKind(file.name)!} src={artifactRawUrl(runId, file.name)} name={nameWithin(folder, file.name)} size={file.size} />
          <FileChip request={artifactFileRequest(runId, file.name, context)} label={nameWithin(folder, file.name)} detail={formatBytes(file.size)} />
        </div>
      ))}
      {thumbs.length ? (
        <div className="attempt-thumbs">
          {thumbs.map(file => (
            <button key={file.name} type="button" className="attempt-thumb" title={nameWithin(folder, file.name)}
              onClick={event => open(artifactFileRequest(runId, file.name, context), event.currentTarget)}>
              <img src={artifactRawUrl(runId, file.name)} alt={nameWithin(folder, file.name)} loading="lazy" decoding="async" draggable={false} />
            </button>
          ))}
        </div>
      ) : null}
      {summary.models.map(file => (
        <div key={file.name}>
          <MediaPreview kind={mediaKind(file.name)!} src={artifactRawUrl(runId, file.name)} name={nameWithin(folder, file.name)} size={file.size} />
          <FileChip request={artifactFileRequest(runId, file.name, context)} label={nameWithin(folder, file.name)} detail={formatBytes(file.size)} />
        </div>
      ))}
      {rows.length ? (
        <ul className="attempt-listing">
          {rows.map(file => (
            <li key={file.name}>
              <button type="button" className="attempt-listing-file" onClick={event => open(artifactFileRequest(runId, file.name, context), event.currentTarget)}>
                <span className="attempt-listing-name">{nameWithin(folder, file.name)}</span>
                <span className="attempt-listing-size">{formatBytes(file.size)}</span>
              </button>
            </li>
          ))}
        </ul>
      ) : null}
      {more > 0 ? <p className="attempt-quiet">and {more} more listed files</p> : null}
    </div>
  )
}

/** The seat's own words beside a file or folder, when they say more than its path. */
function outputNote(output: AttemptOutput): string {
  const text = output.text.text.trim()
  return text && text !== output.path && text !== output.artifact ? text : ''
}

function OutputView({ attempt, output, files, view }: { attempt: StepAttempt; output: AttemptOutput; files: AttemptFile[]; view: AttemptView }) {
  const label = output.label || output.portId
  const context = `${view.stepTitle} · attempt ${attempt.attempt}`
  const testId = `attempt-${attempt.attempt}-output-${output.portId}`
  const note = outputNote(output)
  let body: ReactNode
  if (output.kind === 'folder' || output.fileKind === 'folder') {
    body = <FolderOutput runId={view.runId} output={output} files={files} truncated={Boolean(attempt.filesTruncated)} context={context} evidence={attempt.verdicts.flatMap(verdict => verdict.evidence.map(item => item.text.text))} />
  } else if (output.kind === 'file' && (output.artifact || output.path)) {
    const path = output.artifact || output.path || ''
    const name = baseName(path)
    const request = output.artifact ? artifactFileRequest(view.runId, output.artifact, context) : referencedFileRequest(output.path || '', context)
    const detail = output.size ? formatBytes(output.size) : ''
    const media = mediaKind(path)
    if (media && request.rawUrl) {
      body = <><MediaPreview kind={media} src={request.rawUrl} name={name} size={output.size} /><FileChip request={request} label={name} detail={detail} /></>
    } else if (output.fileKind === 'image' || (!output.fileKind && isImagePath(path))) {
      const src = output.artifact ? artifactRawUrl(view.runId, output.artifact) : `/api/files/raw?path=${encodeURIComponent(output.path || '')}`
      body = <ImageOutput src={src} name={name} group={output.portId} view={view} request={request} detail={detail} testId={`${testId}-image`} />
    } else if (output.artifact && (output.fileKind === 'markdown' || output.fileKind === 'text' || output.fileKind === 'json')) {
      body = (
        <>
          <SnapshotText runId={view.runId} name={output.artifact} kind={output.fileKind} />
          <FileChip request={request} label={name} detail={detail} />
        </>
      )
    } else {
      body = <FileChip request={request} label={name} detail={[output.fileKind, detail].filter(Boolean).join(' · ')} />
    }
  } else {
    body = output.text.text.trim() ? <TextBlock text={output.text} kind="markdown" runId={view.runId} /> : <p className="attempt-quiet">Empty.</p>
  }
  return (
    <section className={`attempt-output kind-${output.kind}`} data-testid={testId} aria-label={`${label} of attempt ${attempt.attempt}`}>
      <h4 className="attempt-label">{label}</h4>
      {body}
      {note && output.kind !== 'text' ? <p className="attempt-note"><LinkedText text={note} /></p> : null}
    </section>
  )
}

function EvidenceItems({ items, omitted, context }: { items: EvidenceItem[]; omitted?: number; context: string }) {
  const open = useOpen()
  const paths = items.map(item => item.text.text.trim())
  const isPath = (text: string) => text.startsWith('/') && !/[\r\n]/.test(text)
  const images = paths.filter(text => isPath(text) && isImagePath(text))
  const media = paths.filter(text => isPath(text) && mediaKind(text))
  const files = paths.filter(text => isPath(text) && !isImagePath(text) && !mediaKind(text))
  const notes = items.filter((_, index) => !isPath(paths[index]))
  if (!items.length) return null
  return (
    <div className="attempt-evidence">
      {images.length ? (
        <div className="attempt-thumbs evidence">
          {images.map(path => (
            <button key={path} type="button" className="attempt-thumb" title={path}
              onClick={event => open(referencedFileRequest(path, context), event.currentTarget)}>
              <img src={`/api/files/raw?path=${encodeURIComponent(path)}`} alt={baseName(path)} loading="lazy" decoding="async" draggable={false} />
            </button>
          ))}
        </div>
      ) : null}
      {media.map(path => <MediaPreview key={path} kind={mediaKind(path)!} src={referencedFileRequest(path).rawUrl!} name={baseName(path)} />)}
      {files.length ? (
        <div className="attempt-chips">
          {files.map(path => <FileChip key={path} request={referencedFileRequest(path, context)} label={baseName(path)} />)}
        </div>
      ) : null}
      {notes.length ? (
        <ul className="attempt-evidence-notes">
          {notes.map((item, index) => <li key={index}><LinkedText text={item.text.text} /></li>)}
        </ul>
      ) : null}
      {omitted ? <p className="attempt-quiet">and {omitted} more</p> : null}
    </div>
  )
}

function VerdictView({ verdict, view, attempt }: { verdict: AttemptVerdict; view: AttemptView; attempt: number }) {
  const tone = verdict.pending ? (view.waitingGates.has(verdict.gateId) ? 'waiting' : 'judging') : verdict.verdict === 'pass' ? 'pass' : verdict.verdict === 'fail' ? 'fail' : 'done'
  return (
    <div className={`attempt-verdict tone-${tone}`} data-testid={`attempt-${attempt}-verdict-${verdict.gateId}${verdict.superseded ? '-superseded' : ''}`}>
      <p className="attempt-verdict-head">{verdictHeadline(verdict, view.waitingGates)}</p>
      {verdict.reason.text.trim() ? <p className="attempt-reason"><LinkedText text={verdict.reason.text} /></p> : null}
      {verdict.reason.truncated ? <p className="attempt-cut">{cutWords(verdict.reason)}</p> : null}
      <EvidenceItems items={verdict.evidence} omitted={verdict.evidenceOmitted} context={`${verdict.gateTitle || verdict.gateId} · attempt ${attempt}`} />
    </div>
  )
}

export interface AttemptMarks {
  /** The keyboard's attempt, or the active side when two are side by side. */
  focused: boolean
  /** The attempt the loop settled on: picked, or the latest that passed. */
  settled: boolean
  /** In a pending pick, whether it may be chosen. */
  candidate?: boolean
  /** The judge's choice in a pending pick: why, and the judge's node ID. */
  recommended?: { reason: string; decidedBy?: string }
}

// A shift-click on these does their own thing, not choose the attempt beside the focused one.
const OWN_CLICKS = 'button, a, input, textarea, select, video, canvas'

export default function AttemptColumn({ attempt, view, marks, onFocus }: {
  attempt: StepAttempt
  view: AttemptView
  marks: AttemptMarks
  /** The column was pressed; `beside` when shift-pressed to set it beside the focused attempt. */
  onFocus: (beside: boolean) => void
}) {
  const outcome: AttemptOutcome = attemptOutcome(attempt, view.waitingGates)
  const { outputs, others } = attemptFileGroups(attempt)
  const open = useOpen()
  const excluded = marks.candidate === false
  const context = `${view.stepTitle} · attempt ${attempt.attempt}`
  const pickedBy = attempt.picked ? pickerWords(attempt.picked.decidedBy, view.titleOf) : ''
  const beside = (event: { shiftKey: boolean; target: EventTarget }) => event.shiftKey && !view.compare && !(event.target as Element).closest?.(OWN_CLICKS)
  return (
    <article
      className={`attempt-col${marks.focused ? ' focused' : ''}${marks.settled ? ' settled' : ''}${excluded ? ' excluded' : ''}`}
      data-attempt={attempt.attempt}
      data-testid={`attempt-${attempt.attempt}`}
      aria-label={`Attempt ${attempt.attempt}, ${OUTCOME_WORDS[outcome].replace(/^[✓✕] /, '')}`}
      aria-current={marks.focused ? 'true' : undefined}
      onPointerDown={event => onFocus(beside(event))}
      // A shift-click chooses an attempt, so it selects no text.
      onMouseDown={event => { if (beside(event)) event.preventDefault() }}
    >
      <header className="attempt-head">
        <div className="attempt-head-line">
          <span className="attempt-number">Attempt {attempt.attempt}</span>
          <span className={`attempt-outcome outcome-${outcome}${marks.settled ? ' settled' : ''}`} data-testid={`attempt-${attempt.attempt}-outcome`}>{OUTCOME_WORDS[outcome]}</span>
          {marks.recommended ? <span className="attempt-outcome outcome-recommended">judge&apos;s pick</span> : null}
        </div>
        <span className="attempt-time">{[attemptTiming(attempt, view.now), excluded ? 'not a candidate' : ''].filter(Boolean).join(' · ')}</span>
      </header>
      <div className="attempt-verdicts">
        {attempt.picked ? (
          <div className="attempt-verdict tone-picked" data-testid={`attempt-${attempt.attempt}-pick`}>
            <p className="attempt-verdict-head">Picked as the best{pickedBy ? ` · ${pickedBy}` : ''}</p>
            {attempt.picked.reason.text.trim() ? <p className="attempt-reason"><LinkedText text={attempt.picked.reason.text} /></p> : null}
          </div>
        ) : null}
        {marks.recommended ? (
          <div className="attempt-verdict tone-recommended" data-testid={`attempt-${attempt.attempt}-recommended`}>
            <p className="attempt-verdict-head">{upperFirst(judgeWords(marks.recommended.decidedBy, view.titleOf))} picks this one</p>
            {marks.recommended.reason ? <p className="attempt-reason"><LinkedText text={marks.recommended.reason} /></p> : null}
          </div>
        ) : null}
        {attempt.verdicts.map(verdict => <VerdictView key={`${verdict.gateId}:${verdict.evaluationSeq}`} verdict={verdict} view={view} attempt={attempt.attempt} />)}
      </div>
      <div className="attempt-outputs">
        {outputs.length ? outputs.map(({ output, files }) => <OutputView key={output.portId} attempt={attempt} output={output} files={files} view={view} />)
          : <p className="attempt-quiet">{attempt.status === 'running' ? 'No output yet.' : attempt.status === 'stopped' ? 'Stopped before its output.' : 'No output recorded.'}</p>}
      </div>
      <div className="attempt-files">
        {others.length ? (
          <>
            <h4 className="attempt-label">Other files</h4>
            <div className="attempt-chips">
              {others.slice(0, OTHER_FILES).map(file => (
                <FileChip key={file.name} request={artifactFileRequest(view.runId, file.name, context)}
                  label={attempt.directory ? nameWithin(attempt.directory.name, file.name) : baseName(file.name)} detail={formatBytes(file.size)} />
              ))}
              {others.length > OTHER_FILES && attempt.directory ? (
                <button type="button" className="produced-chip attempt-chip" onClick={event => open(folderFileRequest(attempt.directory!.path, context), event.currentTarget)}>
                  and {others.length - OTHER_FILES} more
                </button>
              ) : null}
            </div>
          </>
        ) : null}
        {attempt.filesTruncated ? (
          <p className="attempt-quiet">
            {attempt.filesOmitted ? `At least ${attempt.filesOmitted} more files not listed. ` : 'Folders beyond 16 levels are not listed. '}
            {attempt.directory ? <button type="button" className="evidence-link" onClick={event => open(folderFileRequest(attempt.directory!.path, context), event.currentTarget)}>Browse all files</button> : null}
          </p>
        ) : null}
      </div>
    </article>
  )
}
