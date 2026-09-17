import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { useFilePreview } from './FileView'
import type { FilePreview, FileRequest } from './fileWindowModel'

afterEach(cleanup)

it('clears old content while refreshing and ignores a superseded read of the same output', async () => {
  const preview = (text: string): FilePreview => ({ kind: 'text', text: { text, bytes: text.length } })
  const request: FileRequest = { id: 'output:run:work:report', name: 'report', load: async () => preview('first') }
  const { result, rerender } = renderHook(useFilePreview, { initialProps: request })
  await waitFor(() => expect(result.current.preview?.text?.text).toBe('first'))

  let resolveOld!: (value: FilePreview) => void
  rerender({ ...request, load: () => new Promise(resolve => { resolveOld = resolve }) })
  expect(result.current.preview).toBeNull()
  expect(result.current.error).toBe('')
  rerender({ ...request, load: async () => preview('latest') })
  await waitFor(() => expect(result.current.preview?.text?.text).toBe('latest'))
  await act(async () => { resolveOld(preview('superseded')) })
  expect(result.current.preview?.text?.text).toBe('latest')
})
