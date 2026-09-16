import { fetchApi } from '../components/formationsApi'
import {
  artifactRawUrl,
  fetchArtifactPreview,
  fetchNodeEvidence,
  type ArtifactKind,
  type EvidenceText,
} from '../evidence/runEvidenceApi'

// What a floating file window shows. A request says how to load the file, so
// run artifacts, node outputs and files referenced from a board all open in
// the same window.

export type FileKind = ArtifactKind

export interface FilePreview {
  kind: FileKind
  /** The text of a textual file, capped by the daemon and marked when cut. */
  text?: EvidenceText
}

export interface FileRequest {
  /** Stable identity: opening the same file again brings its window forward. */
  id: string
  /** The file name, or the output's name. */
  name: string
  /** Where the file came from, such as the step that produced it. */
  context?: string
  load: () => Promise<FilePreview>
  /** The raw bytes, for opening in a browser tab. */
  rawUrl?: string
  /** What Copy path copies. */
  path?: string
  /** What relative Markdown links resolve against. */
  basePath?: string
  /** The file a resolved Markdown link opens. */
  link?: (target: string) => FileRequest
  /** Where an image beside a Markdown file loads from. */
  imageUrl?: (target: string) => string
}

/** A run artifact, by its name relative to the run's artifact directory. */
export function artifactFileRequest(runId: string, name: string, context?: string): FileRequest {
  return {
    id: `artifact:${runId}:${name}`,
    name: name.split('/').pop() || name,
    context: context ? `${context} · ${name}` : name,
    load: async () => {
      const preview = await fetchArtifactPreview(runId, name)
      return { kind: preview.kind, text: preview.text }
    },
    rawUrl: artifactRawUrl(runId, name),
    // Artifacts live under the daemon's state directory.
    path: `.formations/artifacts/${runId}/${name}`,
    basePath: name,
    link: target => artifactFileRequest(runId, target, context),
    imageUrl: target => artifactRawUrl(runId, target),
  }
}

/**
 * A step's output text: one port's payload, or the seat's report when no port
 * is named. It is read from the node's latest recorded output.
 */
export function outputFileRequest(runId: string, nodeId: string, name: string, context: string, portId?: string): FileRequest {
  return {
    id: `output:${runId}:${nodeId}:${portId || 'report'}`,
    name,
    context,
    load: async () => {
      const evidence = await fetchNodeEvidence(runId, nodeId)
      const output = [...(evidence.attempts || [])].reverse().find(attempt => attempt.output)?.output
      if (!output) throw new Error('this step has recorded no output yet')
      const text = portId ? output.ports.find(port => port.portId === portId)?.text : output.text
      if (!text) throw new Error('this output is no longer recorded')
      return { kind: 'markdown', text }
    },
    link: target => artifactFileRequest(runId, target, context),
    imageUrl: target => artifactRawUrl(runId, target),
  }
}

const filePath = (ref: string) => `?path=${encodeURIComponent(ref)}`

/**
 * A file a mission, brief or gate references, read under the daemon's file
 * roots (ADR-0018). A reference outside them fails as not readable here.
 */
export function referencedFileRequest(ref: string, context?: string): FileRequest {
  const name = ref.split('/').filter(Boolean).pop() || ref
  const absolute = (target: string) => (ref.startsWith('/') ? `/${target}` : target)
  return {
    id: `file:${ref}`,
    name,
    context: context ? `${context} · ${ref}` : ref,
    load: async () => {
      const { data } = await fetchApi<{ file?: FilePreview }>(`/api/formations/files/preview${filePath(ref)}`)
      if (!data.file) throw new Error('the daemon returned no file')
      return { kind: data.file.kind, text: data.file.text }
    },
    rawUrl: `/api/formations/files/raw${filePath(ref)}`,
    path: ref,
    basePath: ref,
    // Links resolve without a leading slash; beside an absolute reference they are absolute too.
    link: target => referencedFileRequest(absolute(target), context),
    imageUrl: target => `/api/formations/files/raw${filePath(absolute(target))}`,
  }
}
