// Reading one file in a window. Ported from CHROTE's file viewers
// (dashboard/src/components/FilePanelViewer.tsx and FileViewer.tsx): Markdown
// is rendered in the theme with its source a press away, JSON is
// pretty-printed, other text reads in numbered lines, an image is shown and a
// PDF opens in the browser's own viewer. Editing, diffs and sending to a
// terminal are CHROTE's and stay there; run files are evidence, read only.

import { useEffect, useState } from 'react'
import Markdown from '../evidence/Markdown'
import TextLines, { prettyJson } from '../evidence/TextLines'
import { formatBytes } from '../evidence/runEvidenceApi'
import type { FilePreview, FileRequest } from './fileWindowModel'
import '../evidence/evidence.css'
import './files.css'

type MarkdownMode = 'preview' | 'source'

const byteLength = (text: string) => new TextEncoder().encode(text).length

export function FileActions({ request, preview, mode, onMode }: {
  request: FileRequest
  preview: FilePreview | null
  mode: MarkdownMode
  onMode: (mode: MarkdownMode) => void
}) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    if (!request.path) return
    try {
      await navigator.clipboard.writeText(request.path)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      setCopied(false)
    }
  }
  return (
    <span className="file-actions" onPointerDown={event => event.stopPropagation()}>
      {preview?.kind === 'markdown' ? (
        <>
          <button type="button" className="file-action" aria-pressed={mode === 'preview'} onClick={() => onMode('preview')}>Preview</button>
          <button type="button" className="file-action" aria-pressed={mode === 'source'} onClick={() => onMode('source')}>Source</button>
        </>
      ) : null}
      {request.rawUrl ? <a className="file-action" href={request.rawUrl} target="_blank" rel="noopener noreferrer">Open raw</a> : null}
      {request.path ? (
        <button type="button" className="file-action" title={request.path} onClick={() => void copy()}>{copied ? 'Copied' : 'Copy path'}</button>
      ) : null}
    </span>
  )
}

export function useFilePreview(request: FileRequest): { preview: FilePreview | null; error: string } {
  const [state, setState] = useState<{ id: string; preview: FilePreview | null; error: string }>({ id: '', preview: null, error: '' })
  useEffect(() => {
    let current = true
    request.load().then(
      preview => { if (current) setState({ id: request.id, preview, error: '' }) },
      reason => { if (current) setState({ id: request.id, preview: null, error: reason instanceof Error ? reason.message : String(reason) }) },
    )
    return () => { current = false }
  }, [request])
  return state.id === request.id ? state : { preview: null, error: '' }
}

export default function FileView({ request, preview, error, mode, onOpen }: {
  request: FileRequest
  preview: FilePreview | null
  error: string
  mode: MarkdownMode
  /** Follows a relative Markdown link to another file. */
  onOpen?: (request: FileRequest) => void
}) {
  if (error) return <p className="file-note" role="alert">Cannot read {request.name}: {error}</p>
  if (!preview) return <p className="file-note" role="status">Reading {request.name}…</p>
  const text = preview.text
  const link = request.link
  const note = text?.truncated ? (
    <p className="file-note" data-testid="file-truncated">
      Showing {formatBytes(byteLength(text.text))} of {formatBytes(text.bytes)}.{request.rawUrl ? ' Open it raw for the whole file.' : ' The rest stays in the run evidence on the host.'}
    </p>
  ) : null
  switch (preview.kind) {
    case 'image':
      return request.rawUrl ? <div className="file-image"><img src={request.rawUrl} alt={request.name} /></div> : <p className="file-note">No image to show.</p>
    case 'pdf':
      return request.rawUrl ? <iframe className="file-pdf" src={request.rawUrl} title={request.name} /> : <p className="file-note">No PDF to show.</p>
    case 'binary':
      return (
        <p className="file-note">
          No inline view for this file.{request.rawUrl ? <> <a href={request.rawUrl} target="_blank" rel="noopener noreferrer">Download it</a>.</> : null}
        </p>
      )
    case 'markdown':
      if (!text) break
      return (
        <div className="file-text">
          {mode === 'source' ? <TextLines content={text.text} label={`${request.name} source`} /> : (
            <article className="file-markdown" aria-label={`${request.name} rendered`}>
              <Markdown
                content={text.text}
                basePath={request.basePath}
                onOpenPath={link && onOpen ? target => onOpen(link(target)) : undefined}
                imageUrl={request.imageUrl}
              />
            </article>
          )}
          {note}
        </div>
      )
    case 'json':
      if (!text) break
      return <div className="file-text"><TextLines content={text.truncated ? text.text : prettyJson(text.text)} label={`${request.name} contents`} />{note}</div>
    default:
      if (!text) break
      return <div className="file-text"><TextLines content={text.text} label={`${request.name} contents`} />{note}</div>
  }
  return <p className="file-note">This file has no text to show.</p>
}
