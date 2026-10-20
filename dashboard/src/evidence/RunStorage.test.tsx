import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import RunStorage from './RunStorage'
import type { RunStorageReport } from './runStorageApi'

const initial: RunStorageReport = { runId: 'one', policy: 'bounded', final: true, trimmed: false, archived: false, state: 'retained', bytes: 12582912, allocatedBytes: 12582912, retainedBytes: 8388608, budgetBytes: 33554432, poolBudgetBytes: 268435456, activeSeats: [], protectedBy: [] }
const response = (storage: RunStorageReport) => ({ ok: true, headers: { get: () => '' }, json: async () => ({ success: true, data: { storage } }) }) as unknown as Response
const props = { runId: 'one', final: true, state: 'succeeded', onChange: vi.fn() }
afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

it('shows accounting, explicit archives, returns policy and reviews disposal in place', async () => {
 let report = initial
 const actions: string[] = []
 vi.stubGlobal('fetch', vi.fn(async (_url, options) => {
  if (options?.method === 'POST') {
   const { action } = JSON.parse(options.body)
   actions.push(action)
   report = { ...report, archived: action === 'archive', ...(action === 'dispose' ? { state: 'expired', bytes: 0, retainedBytes: 0 } : {}) }
  }
  return response(report)
 }))
 render(<RunStorage {...props} />)
 await screen.findAllByText('12.0 MiB')
 expect(screen.getByText('32.0 MiB / run · 256.0 MiB completed pool')).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button', { name: 'Archive evidence' }))
 await screen.findByText(/Archived · excluded/)
 expect(screen.getAllByText('12.0 MiB')).toHaveLength(2)
 fireEvent.click(screen.getByRole('button', { name: 'Return to ordinary policy' }))
 await screen.findByRole('button', { name: 'Archive evidence' })
 fireEvent.click(screen.getByRole('button', { name: 'Review disposal' }))
 expect(actions).toEqual(['archive', 'unarchive'])
 expect(screen.queryByRole('dialog')).toBeNull()
 expect(screen.getByText(/Compact run history, frozen mission and briefs remain/)).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button', { name: 'Dispose run files' }))
 await screen.findByText(/Evidence expired/)
 expect(screen.queryByRole('button', { name: 'Review disposal' })).toBeNull()
 expect(actions).toEqual(['archive', 'unarchive', 'dispose'])
})

it('keeps legacy and partial evidence honest and protects unfinished references', async () => {
 vi.stubGlobal('fetch', vi.fn(async () => response({ ...initial, policy: 'legacy', state: 'partial', protectedBy: ['run_working'], lastError: 'one file is busy' })))
 render(<RunStorage {...props} />)
 await screen.findByText(/Legacy · inventory only; no automatic cleanup/)
 expect(screen.getByText(/Cleanup incomplete/)).toBeInTheDocument()
 expect(screen.getByText(/run_working/)).toBeInTheDocument()
 expect(screen.getByRole('alert')).toHaveTextContent('one file is busy')
 expect(screen.queryByRole('button', { name: 'Review disposal' })).toBeNull()
})

it('ignores a previous run’s late command and resets its disposal review', async () => {
 let complete: (value: Response) => void = () => {}
 const changed = vi.fn()
 vi.stubGlobal('fetch', vi.fn(async (url, options) => {
  if (options?.method === 'POST') return new Promise<Response>(resolve => { complete = resolve })
  return response({ ...initial, runId: String(url).includes('/two/') ? 'two' : 'one', final: !String(url).includes('/two/') })
 }))
 const view = render(<RunStorage {...props} onChange={changed} />)
 await screen.findByRole('button', { name: 'Archive evidence' })
 fireEvent.click(screen.getByRole('button', { name: 'Review disposal' }))
 fireEvent.click(screen.getByRole('button', { name: 'Archive evidence' }))
 view.rerender(<RunStorage {...props} runId="two" final={false} onChange={changed} />)
 await screen.findByText('This run is unfinished. Its files cannot be disposed.')
 await act(async () => complete(response({ ...initial, archived: true })))
 expect(changed).not.toHaveBeenCalled()
 expect(screen.queryByText(/Archived · excluded/)).toBeNull()
 expect(screen.queryByRole('button', { name: 'Dispose run files' })).toBeNull()
})

it('ignores a late old-run inventory and reports action errors without claiming disposal', async () => {
 let complete: (value: Response) => void = () => {}
 vi.stubGlobal('fetch', vi.fn(async (url, options) => {
  if (options?.method === 'POST') return { ok: false, json: async () => ({ success: false, error: { code: 'CONFLICT', message: 'referenced by a running run' } }) } as Response
  if (String(url).includes('/one/')) return new Promise<Response>(resolve => { complete = resolve })
  return response({ ...initial, runId: 'two' })
 }))
 const view = render(<RunStorage {...props} />)
 view.rerender(<RunStorage {...props} runId="two" />)
 await screen.findByText('Ordinary evidence · Evidence retained')
 await act(async () => complete(response({ ...initial, archived: true })))
 fireEvent.click(screen.getByRole('button', { name: 'Review disposal' }))
 fireEvent.click(screen.getByRole('button', { name: 'Dispose run files' }))
 await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('referenced by a running run'))
 expect(screen.getByText('Ordinary evidence · Evidence retained')).toBeInTheDocument()
})

it('can archive successfully compacted evidence while explaining cleaned files stay unavailable', async () => {
 vi.stubGlobal('fetch', vi.fn(async () => response({ ...initial, final: false, trimmed: true })))
 render(<RunStorage {...props} final={false} />)
 await screen.findByText(/Evidence retained · working files cleaned/)
 expect(screen.getByRole('button', { name: 'Archive evidence' })).toBeEnabled()
 expect(screen.getByText(/Cleaned working files cannot be restored/)).toBeInTheDocument()
 expect(screen.queryByRole('button', { name: 'Review disposal' })).toBeNull()
 expect(screen.queryByRole('alert')).toBeNull()
})

it('protects final files while an agent seat is still present or uncertain', async () => {
 vi.stubGlobal('fetch', vi.fn(async () => response({ ...initial, activeSeats: [17] })))
 render(<RunStorage {...props} />)
 await screen.findByText(/Agent seats still present or their absence is unconfirmed: #17/)
 expect(screen.queryByRole('button', { name: 'Review disposal' })).toBeNull()
})
