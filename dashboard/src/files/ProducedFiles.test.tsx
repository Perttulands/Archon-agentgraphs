import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { BoardDocument } from '../components/formationsTypes'
import { evidenceNamesForBoard } from '../evidence/evidenceNames'
import { WindowManagerProvider, useWindowManager } from '../windows/WindowManager'
import type { Workspace } from '../windows/windowGeometry'
import { FileWindowsLayer, FileWindowsProvider, useFileWindows } from './FileWindows'
import { ProducedFiles, RunProduced, RunProducedProvider } from './ProducedFiles'
import { producedFromEvidence, summarizeProduced, type NodeProduced } from './produced'
import type { NodeEvidence } from '../evidence/runEvidenceApi'
import { artifactFileRequest } from './fileWindowModel'

const text = (value: string, extra: Partial<{ truncated: boolean; bytes: number }> = {}) => ({ text: value, bytes: value.length, ...extra })

const finalReview: NodeEvidence = {
  runId: 'run_1', nodeId: 'fmn_final', kind: 'formation',
  definition: { title: 'Final review', outputs: [{ id: 'port_final', label: 'Review' }], outgoing: [] },
  attempts: [{
    attempt: 1, inputs: [], dispatches: [],
    output: { seq: 56, text: text('Verdict: revise. See the review.'), ports: [{ portId: 'port_final', text: text('# Final review\n\nVerdict: **revise**'), ref: { artifact: 'final-review.md' } }] },
  }],
}
const plan: NodeEvidence = {
  runId: 'run_1', nodeId: 'fmn_plan', kind: 'formation',
  definition: { title: 'Plan', outputs: [], outgoing: [{ id: 'c1', from: 'fmn_plan:port_plan', to: 'fmn_final:in' }] },
  attempts: [{ attempt: 1, inputs: [], dispatches: [], output: { seq: 11, text: text('Plan: three steps\nstep one'), ports: [] } }],
}

const routes: Record<string, unknown> = {
  '/api/formations/runs/run_1/evidence/nodes/fmn_final': { evidence: finalReview },
  '/api/formations/runs/run_1/evidence/nodes/fmn_plan': { evidence: plan },
  '/api/formations/runs/run_1/evidence/artifacts/final-review.md': { artifact: { name: 'final-review.md', size: 34, modifiedAt: '', kind: 'markdown', text: text('# Final review\n\nVerdict: **revise**. See [the log](logs/run.json).') } },
  '/api/formations/runs/run_1/evidence/artifacts/logs/run.json': { artifact: { name: 'logs/run.json', size: 11, modifiedAt: '', kind: 'json', text: text('{"ok":true}') } },
  '/api/formations/runs/run_1/evidence/artifacts/worker.log': { artifact: { name: 'worker.log', size: 300000, modifiedAt: '', kind: 'text', text: text('line one\nline two', { truncated: true, bytes: 300000 }) } },
  '/api/formations/runs/run_1/evidence/artifacts/paper.pdf': { artifact: { name: 'paper.pdf', size: 900, modifiedAt: '', kind: 'pdf' } },
}

const board = {
  id: 'brd', slug: 'delivery', title: 'Delivery', rev: 1, etag: 'e',
  formations: [
    { id: 'fmn_plan', type: 'solo', title: 'Plan', inputs: [], outputs: [{ id: 'port_plan', label: 'Plan' }], slots: [] },
    { id: 'fmn_final', type: 'solo', title: 'Final review', inputs: [], outputs: [{ id: 'port_final', label: 'Review' }], slots: [] },
  ],
  connections: [{ id: 'c1', from: 'fmn_plan:port_plan', to: 'fmn_final:in' }],
} as BoardDocument

const workspace: Workspace = { bounds: { left: 0, top: 0, width: 1600, height: 1000 }, avoid: [{ left: 1540, top: 700, width: 60, height: 300 }] }

function OpenArtifact({ name }: { name: string }) {
  const files = useFileWindows()
  return <button type="button" onClick={() => files?.open(artifactFileRequest('run_1', name, 'Execution'))}>Open {name}</button>
}

const listed = [{ name: 'final-review.md', size: 34, modifiedAt: '' }, { name: 'worker.log', size: 300000, modifiedAt: '' }]

function Cockpit({ produced, final, artifacts = listed }: { produced: NodeProduced[]; final: boolean; artifacts?: typeof listed }) {
  const stack = useWindowManager(() => workspace)
  const value = {
    runId: 'run_1',
    byNode: new Map(produced.map(step => [step.nodeId, step])),
    summary: summarizeProduced(produced, artifacts, final),
    names: evidenceNamesForBoard(board),
  }
  return (
    <FileWindowsProvider stack={stack}>
      <RunProducedProvider value={value}>
        <div data-testid="run-bar"><RunProduced /></div>
        <div data-testid="card-plan" data-node="fmn_plan"><ProducedFiles nodeId="fmn_plan" /></div>
        <div data-testid="card-final"><ProducedFiles nodeId="fmn_final" /></div>
        <OpenArtifact name="paper.pdf" />
        <WindowManagerProvider stack={stack}><FileWindowsLayer /></WindowManagerProvider>
      </RunProducedProvider>
    </FileWindowsProvider>
  )
}

const produced = [producedFromEvidence(plan)!, producedFromEvidence(finalReview)!]
const rectOf = (dialog: HTMLElement) => ({ left: parseFloat(dialog.style.left), top: parseFloat(dialog.style.top), width: parseFloat(dialog.style.width), height: parseFloat(dialog.style.height) })

describe('produced files', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      const found = url in routes
      return Promise.resolve({
        ok: found, status: found ? 200 : 404, headers: { get: () => '' },
        json: () => Promise.resolve(found ? { success: true, data: routes[url] } : { success: false, error: { code: 'NOT_FOUND', message: 'run evidence not found' } }),
      } as unknown as Response)
    }))
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('shows chips from evidence on each step and a finished run leads Produced with its final review', () => {
    render(<Cockpit produced={produced} final />)
    const final = within(screen.getByTestId('card-final'))
    expect(final.getAllByRole('button').map(button => button.textContent)).toEqual(['▤final-review.md', '¶report'])
    expect(within(screen.getByTestId('card-plan')).getAllByRole('button').map(button => button.textContent)).toEqual(['¶report'])

    const bar = within(screen.getByTestId('run-produced'))
    expect(bar.getByText('produced')).toBeInTheDocument()
    expect(bar.getAllByRole('button').map(button => button.textContent)).toEqual(['▤final-review.md', '¶report', '+2'])
    fireEvent.click(bar.getByRole('button', { name: '2 more produced files' }))
    const menu = screen.getByRole('menu', { name: 'Everything this run produced' })
    expect(within(menu).getByText('Plan')).toBeInTheDocument()
    expect(within(menu).getByText('Other files')).toBeInTheDocument()
    expect(within(menu).getAllByRole('menuitem').map(item => item.textContent)).toEqual(['¶report', '▤worker.log'])
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('menu')).toBeNull()
  })

  it('opens a Markdown artifact rendered from the run bar in one click, with raw and path actions', async () => {
    render(<Cockpit produced={produced} final />)
    fireEvent.click(within(screen.getByTestId('run-produced')).getByRole('button', { name: 'final-review.md' }))

    const review = await screen.findByRole('dialog', { name: 'file final-review.md' })
    expect(await within(review).findByRole('heading', { name: 'Final review' })).toBeInTheDocument()
    expect(within(review).getByText('revise').tagName).toBe('STRONG')
    expect(review).toHaveTextContent('Final review · final-review.md')
    expect(within(review).getByRole('link', { name: 'Open raw' })).toHaveAttribute('href', '/api/formations/runs/run_1/artifacts/final-review.md')
    expect(within(review).getByRole('button', { name: 'Copy path' })).toHaveAttribute('title', '.formations/artifacts/run_1/final-review.md')

    fireEvent.click(within(review).getByRole('button', { name: 'Source' }))
    expect(within(review).getByLabelText('final-review.md source', { selector: 'pre' })).toHaveTextContent('# Final review')

    // A link in the file opens the linked artifact beside it.
    fireEvent.click(within(review).getByRole('button', { name: 'Preview' }))
    fireEvent.click(within(review).getByRole('link', { name: 'the log' }))
    const log = await screen.findByRole('dialog', { name: 'file run.json' })
    await waitFor(() => expect(within(log).getByLabelText('run.json contents', { selector: 'pre' })).toHaveTextContent('"ok": true'))
  })

  it('opens a text output and a truncated artifact side by side, and reopening a file brings its window forward', async () => {
    render(<Cockpit produced={produced} final />)
    fireEvent.click(within(screen.getByTestId('card-plan')).getByRole('button', { name: 'report' }))
    const report = await screen.findByRole('dialog', { name: 'file report' })
    expect(await within(report).findByText(/Plan: three steps/)).toBeInTheDocument()
    expect(report).toHaveTextContent('Plan')
    expect(within(report).queryByRole('link', { name: 'Open raw' })).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: '2 more produced files' }))
    fireEvent.click(within(screen.getByRole('menu')).getByRole('menuitem', { name: 'worker.log' }))
    const log = await screen.findByRole('dialog', { name: 'file worker.log' })
    await waitFor(() => expect(within(log).getByTestId('file-truncated')).toHaveTextContent('Showing 17 B of 293 KiB. Open it raw for the whole file.'))

    expect(screen.getAllByRole('dialog')).toHaveLength(2)
    expect(rectOf(log)).not.toEqual(rectOf(report))
    expect(Number(log.style.zIndex)).toBeGreaterThan(Number(report.style.zIndex))

    fireEvent.click(within(screen.getByTestId('card-plan')).getByRole('button', { name: 'report' }))
    expect(screen.getAllByRole('dialog')).toHaveLength(2)
    await waitFor(() => expect(Number(report.style.zIndex)).toBeGreaterThan(Number(log.style.zIndex)))

    fireEvent.click(within(log).getByRole('button', { name: 'Close file worker.log' }))
    expect(screen.getAllByRole('dialog')).toHaveLength(1)
  })

  it('opens a file window beside the card whose chip opened it, and centred from a control without a place', async () => {
    render(<Cockpit produced={produced} final />)
    const card = screen.getByTestId('card-plan')
    card.getBoundingClientRect = () => ({ left: 100, top: 120, width: 300, height: 200, right: 400, bottom: 320, x: 100, y: 120, toJSON: () => ({}) })
    fireEvent.click(within(card).getByRole('button', { name: 'report' }))
    expect(rectOf(await screen.findByRole('dialog', { name: 'file report' }))).toEqual({ left: 412, top: 120, width: 720, height: 560 })

    // A control that passes no anchor opens its window centred, stepped past the open one.
    fireEvent.click(screen.getByRole('button', { name: 'Open paper.pdf' }))
    expect(rectOf(await screen.findByRole('dialog', { name: 'file paper.pdf' }))).toMatchObject({ width: 720, height: 560, left: 440 + 28, top: 220 + 28 })
  })

  it('refreshes a revised output in its existing moved window when the latest chip is opened', async () => {
    const route = '/api/formations/runs/run_1/evidence/nodes/fmn_plan'
    const previous = routes[route]
    try {
      const { rerender } = render(<Cockpit produced={produced} final />)
      const chip = () => within(screen.getByTestId('card-plan')).getByRole('button', { name: 'report' })
      fireEvent.click(chip())
      const report = await screen.findByRole('dialog', { name: 'file report' })
      expect(await within(report).findByText(/Plan: three steps/)).toBeInTheDocument()

      const initial = rectOf(report)
      const title = report.querySelector('.fwin-head')!
      fireEvent.pointerDown(title, { button: 0, pointerId: 1, clientX: 500, clientY: 230 })
      fireEvent.pointerMove(title, { pointerId: 1, clientX: 580, clientY: 290 })
      fireEvent.pointerUp(title, { pointerId: 1, clientX: 580, clientY: 290 })
      const placed = rectOf(report)
      expect(placed).not.toEqual(initial)

      const revised: NodeEvidence = { ...plan, attempts: [...plan.attempts!, {
        attempt: 2, inputs: [], dispatches: [], output: { seq: 80, text: text('Revised plan: two steps'), ports: [] },
      }] }
      routes[route] = { evidence: revised }
      rerender(<Cockpit produced={[producedFromEvidence(revised)!, produced[1]]} final />)
      fireEvent.click(chip())
      expect(await within(report).findByText('Revised plan: two steps')).toBeInTheDocument()
      expect(report).not.toHaveTextContent('Plan: three steps')
      expect(screen.getAllByRole('dialog')).toEqual([report])
      expect(rectOf(report)).toEqual(placed)

      fireEvent.click(chip())
      expect(await within(report).findByText('Revised plan: two steps')).toBeInTheDocument()
      expect(rectOf(report)).toEqual(placed)
    } finally {
      routes[route] = previous
    }
  })

  it('refreshes an artifact overwritten at the same path without replacing its window', async () => {
    const route = '/api/formations/runs/run_1/evidence/artifacts/final-review.md'
    const previous = routes[route]
    try {
      render(<Cockpit produced={produced} final />)
      const chip = within(screen.getByTestId('run-produced')).getByRole('button', { name: 'final-review.md' })
      fireEvent.click(chip)
      const review = await screen.findByRole('dialog', { name: 'file final-review.md' })
      expect(await within(review).findByText('revise')).toBeInTheDocument()
      const placed = rectOf(review)
      routes[route] = { artifact: { name: 'final-review.md', size: 13, modifiedAt: 'later', kind: 'markdown', text: text('Verdict: pass') } }

      fireEvent.click(chip)
      expect(await within(review).findByText('Verdict: pass')).toBeInTheDocument()
      expect(review).not.toHaveTextContent('revise')
      expect(screen.getAllByRole('dialog')).toEqual([review])
      expect(rectOf(review)).toEqual(placed)
    } finally {
      routes[route] = previous
    }
  })

  it('shows a PDF in the browser viewer from the raw route', async () => {
    render(<Cockpit produced={produced} final />)
    fireEvent.click(screen.getByRole('button', { name: 'Open paper.pdf' }))
    const pdf = await screen.findByRole('dialog', { name: 'file paper.pdf' })
    const frame = await within(pdf).findByTitle('paper.pdf')
    expect(frame.tagName).toBe('IFRAME')
    expect(frame).toHaveAttribute('src', '/api/formations/runs/run_1/artifacts/paper.pdf')
  })

  it('shows what exists so far, and nothing before a run has produced anything', () => {
    const { rerender } = render(<Cockpit produced={[]} final={false} />)
    expect(within(screen.getByTestId('run-produced')).getAllByRole('button').map(button => button.textContent)).toEqual(['▤final-review.md', '▤worker.log'])
    expect(screen.queryByTestId('produced-fmn_plan')).toBeNull()
    rerender(<Cockpit produced={[]} final={false} artifacts={[]} />)
    expect(screen.queryByTestId('run-produced')).toBeNull()
  })
})
