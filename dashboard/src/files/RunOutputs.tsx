import type { RunOutput } from '../components/formationsTypes'
import { fileAnchor, useFileWindows } from './FileWindows'
import { folderFileRequest, referencedFileRequest, textValueRequest, type FileRequest } from './fileWindowModel'
import './produced.css'

// What a succeeded or picked run delivered, as its mission's outputs declare:
// each opens in a file window on click. A file opens as itself, a folder as
// its listing, and a text value as its full text. An output that came through
// a pick is failed work a spent limit carried on (ADR-0022): its chip says
// picked, and its tooltip gives the verdict that failed the picked attempt.

const EXCERPT = 40
const GLYPH: Record<RunOutput['kind'], string> = { text: '¶', file: '▤', folder: '▥' }

/** The value as the chip shows it: a path's last name, or the start of the text on one line. */
export function runOutputValueLabel(output: RunOutput): string {
  if (output.kind !== 'text') {
    const name = output.value.split('/').filter(Boolean).pop() || output.value
    return output.kind === 'folder' ? `${name}/` : name
  }
  const text = output.value.replace(/\s+/g, ' ').trim()
  return text.length > EXCERPT ? `${text.slice(0, EXCERPT - 1)}…` : text
}

/** The failed verdict of each pick the output came through, one line each, then its findings. */
export function runOutputPickLines(output: RunOutput, titleOf: (nodeId: string) => string = id => id): string[] {
  return (output.picks || []).flatMap(pick => [
    `picked: ${titleOf(pick.nodeId)} attempt ${pick.attempt}, which ${titleOf(pick.gateId)} judged ${pick.verdict.verdict || 'unknown'}${pick.verdict.reason ? `: ${pick.verdict.reason}` : ''}`,
    ...(pick.verdict.findings || []).map(finding => `  - ${finding}`),
  ])
}

export function runOutputRequest(runId: string, output: RunOutput): FileRequest {
  const context = `output ${output.name}`
  if (output.kind === 'file') return referencedFileRequest(output.value, context)
  if (output.kind === 'folder') return folderFileRequest(output.value, context)
  return textValueRequest(`run-output:${runId}:${output.name}`, output.name, output.value, `run ${runId.slice(-6)}`)
}

export default function RunOutputs({ runId, outputs, titleOf }: { runId: string; outputs: RunOutput[] | undefined; titleOf?: (nodeId: string) => string }) {
  const files = useFileWindows()
  if (!outputs?.length || !files) return null
  return (
    <span className="run-produced run-outputs" data-testid="run-outputs" data-file-anchor>
      <span className="run-produced-label">outputs</span>
      {outputs.map(output => (
        <button
          key={output.name}
          type="button"
          className={`produced-chip run-output kind-${output.kind}${output.picks?.length ? ' picked' : ''}`}
          data-testid={`run-output-${output.name}`}
          title={[`${output.name} · ${output.kind} · ${output.value.length > 400 ? `${output.value.slice(0, 399)}…` : output.value}`, ...runOutputPickLines(output, titleOf)].join('\n')}
          onPointerDown={event => event.stopPropagation()}
          onClick={event => {
            event.stopPropagation()
            files.open(runOutputRequest(runId, output), fileAnchor(event.currentTarget))
          }}
        >
          <span className="produced-glyph" aria-hidden="true">{GLYPH[output.kind]}</span>
          <span className="run-output-name">{output.name}</span>
          <span className="run-output-value">{runOutputValueLabel(output)}</span>
          {output.picks?.length ? <span className="run-output-picked">picked</span> : null}
        </button>
      ))}
    </span>
  )
}
