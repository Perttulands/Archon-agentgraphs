import { createContext, useContext, type ReactNode } from 'react'
import { fileAnchor, useFileWindows } from '../files/FileWindows'
import { artifactFileRequest } from '../files/fileWindowModel'
import { artifactRawUrl, type StepAttempt } from './runEvidenceApi'
import { citedPaths, resolveCitation } from './evidenceCitations'
import '../attempts/attempts.css'
import './citations.css'

export interface CitationScope {
  artifacts: string[]
  paths: string[]
  directories: string[]
}
export function attemptCitationScope(attempt: StepAttempt): CitationScope {
  return {
    artifacts: [...attempt.files.map(file => file.name), ...attempt.outputs.flatMap(output => output.artifact && output.fileKind !== 'folder' && output.kind !== 'folder' ? [output.artifact] : [])],
    paths: [...attempt.files.flatMap(file => file.path ? [file.path] : []), ...attempt.outputs.flatMap(output => output.path ? [output.path] : [])],
    directories: [attempt.directory?.name, ...attempt.outputs.filter(output => output.kind === 'folder' || output.fileKind === 'folder').map(output => output.artifact)].filter((path): path is string => !!path),
  }
}
export const CitationContext = createContext<(CitationScope & { runId: string; open?: (path: string, source?: Element) => void }) | null>(null)
export function CitationScopeProvider({ scope, children }: { scope: Partial<CitationScope>; children: ReactNode }) {
  const parent = useContext(CitationContext)
  return <CitationContext.Provider value={parent ? { ...parent, ...scope, artifacts: [...parent.artifacts, ...(scope.artifacts || [])] } : null}>{children}</CitationContext.Provider>
}
export function evidencePaths(value: unknown): string[] {
  if (typeof value === 'string') return citedPaths(value).filter(path => path.startsWith('/'))
  if (!value || typeof value !== 'object') return []
  return [...new Set(Object.values(value).flatMap(evidencePaths))]
}
export function CitationFiles({ text, imagesOnly = false, excludeMarkdownLinks = false }: { text: string; imagesOnly?: boolean; excludeMarkdownLinks?: boolean }) {
  const context = useContext(CitationContext)
  const windows = useFileWindows()
  if (!context) return null
  const linked = excludeMarkdownLinks ? new Set([
    ...[...text.matchAll(/!?\[[^\]]*\]\(<?([^\s)>]+)>?(?:\s+["'][^"']*["'])?\)/g)].flatMap(match => citedPaths(match[1])),
    ...[...text.matchAll(/^ {0,3}\[[^\]]+\]:\s*<?([^\s>]+)>?/gm)].flatMap(match => citedPaths(match[1])),
  ]) : new Set<string>()
  const paths = [...new Set(citedPaths(text).filter(path => !linked.has(path)).map(path => resolveCitation(path, context.artifacts, [...citedPaths(text).filter(path => path.startsWith('/')), ...context.paths], context.directories)).filter((path): path is string => !!path))]
  const images = paths.filter(path => /\.(?:png|jpe?g|gif|webp|avif|bmp|svg)$/i.test(path))
  const open = (path: string, source: Element) => context.open ? context.open(path, source) : windows?.open(artifactFileRequest(context.runId, path), fileAnchor(source))
  return <>
    {images.length ? <div className="attempt-thumbs evidence">{images.map(path => <button key={path} type="button" className="attempt-thumb" title={path}
      aria-label={`Open ${path.split('/').pop()}`} onClick={event => open(path, event.currentTarget)}>
      <img src={artifactRawUrl(context.runId, path)} alt={path.split('/').pop()} loading="lazy" decoding="async" draggable={false} />
    </button>)}</div> : null}
    {imagesOnly ? null : paths.filter(path => !images.includes(path)).map(path => <button key={path} type="button" className="text-token citation-file" onClick={event => open(path, event.currentTarget)}>{path}</button>)}
  </>
}
export function CitedEvidence({ text }: { text: string }) {
  const context = useContext(CitationContext)
  const paths = citedPaths(text)
  const standalone = paths.length === 1 && paths[0] === text.trim()
  const resolved = standalone && context ? resolveCitation(paths[0], context.artifacts, context.paths, context.directories) : null
  return resolved
    ? <div className="citation-only"><CitationFiles text={text} /></div>
    : <>{text}<CitationFiles text={text} /></>
}
