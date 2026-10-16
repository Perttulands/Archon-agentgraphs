import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RunOutputs, { runOutputRequest, runOutputValueLabel } from './RunOutputs'
import type { RunOutput } from '../components/formationsTypes'

const open = vi.fn()
vi.mock('./FileWindows', () => ({
  useFileWindows: () => ({ open }),
  fileAnchor: () => null,
}))

const output = (name: string, kind: RunOutput['kind'], value: string): RunOutput => ({ name, kind, value, nodeId: 'build', portId: 'out', attempt: 1 })
const outputs = [
  output('report', 'file', '/state/.archon/artifacts/run_1/write/1/report.md'),
  output('site', 'folder', '/state/.archon/artifacts/run_1/build/1/site'),
  output('summary', 'text', 'The site builds and every page passes the link check, including the archive pages.'),
]

describe('RunOutputs', () => {
  afterEach(() => { cleanup(); open.mockReset() })

  it('names each output with its value: a path by its last name, text by its start', () => {
    expect(outputs.map(runOutputValueLabel)).toEqual(['report.md', 'site/', 'The site builds and every page passes t…'])
  })

  it('opens a file as itself, a folder as its listing and text as its full value', async () => {
    expect(runOutputRequest('run_1', outputs[0])).toMatchObject({ id: `file:${outputs[0].value}`, path: outputs[0].value })
    expect(runOutputRequest('run_1', outputs[1])).toMatchObject({ id: `folder:${outputs[1].value}`, name: 'site/', path: outputs[1].value })
    const text = runOutputRequest('run_1', outputs[2])
    expect(await text.load()).toMatchObject({ kind: 'markdown', text: { text: outputs[2].value } })
  })

  it('lists the outputs in the run bar and opens one on the first click', () => {
    render(<RunOutputs runId="run_1" outputs={outputs} />)
    expect(screen.getByTestId('run-output-report')).toHaveTextContent('reportreport.md')
    expect(screen.getByTestId('run-output-site').getAttribute('title')).toBe(`site · folder · ${outputs[1].value}`)
    fireEvent.click(screen.getByTestId('run-output-report'))
    expect(open).toHaveBeenCalledWith(expect.objectContaining({ id: `file:${outputs[0].value}` }), null)
  })

  it('shows nothing for a run without outputs', () => {
    const { container } = render(<RunOutputs runId="run_1" outputs={undefined} />)
    expect(container).toBeEmptyDOMElement()
  })
})
