// Adapted from CHROTE dashboard/src/utils/clipboard.test.ts.
import { afterEach, expect, it, vi } from 'vitest'
import { copyTextToClipboard } from './clipboard'

const clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
const execDescriptor = Object.getOwnPropertyDescriptor(document, 'execCommand')
const setClipboard = (value: unknown) => Object.defineProperty(navigator, 'clipboard', { configurable: true, value })
afterEach(() => {
  vi.restoreAllMocks()
  if (clipboardDescriptor) Object.defineProperty(navigator, 'clipboard', clipboardDescriptor)
  else Reflect.deleteProperty(navigator, 'clipboard')
  if (execDescriptor) Object.defineProperty(document, 'execCommand', execDescriptor)
  else Reflect.deleteProperty(document, 'execCommand')
  document.body.innerHTML = ''
})

it('waits for the async clipboard before confirming a copy', async () => {
  let settle!: () => void
  const writeText = vi.fn(() => new Promise<void>(resolve => { settle = resolve }))
  setClipboard({ writeText })
  const pending = copyTextToClipboard('/project/report.md')
  expect(writeText).toHaveBeenCalledWith('/project/report.md')
  settle()
  await expect(pending).resolves.toBe(true)
})

it('copies through the CHROTE fallback and restores focus and the document selection', async () => {
  setClipboard(undefined)
  document.body.innerHTML = '<button>Copy</button><p>Selected evidence</p>'
  const button = document.querySelector('button')!
  button.focus()
  const range = document.createRange()
  range.selectNodeContents(document.querySelector('p')!)
  document.getSelection()!.removeAllRanges()
  document.getSelection()!.addRange(range)
  expect(document.getSelection()!.toString()).toBe('Selected evidence')
  const execCommand = vi.fn(() => {
    expect((document.activeElement as HTMLTextAreaElement).value).toBe('/project/report.md')
    return true
  })
  Object.defineProperty(document, 'execCommand', { configurable: true, value: execCommand })
  await expect(copyTextToClipboard('/project/report.md')).resolves.toBe(true)
  expect(document.activeElement).toBe(button)
  expect(document.getSelection()!.toString()).toBe('Selected evidence')
  expect(document.querySelector('textarea')).toBeNull()
  Reflect.deleteProperty(document, 'execCommand')
})

it('reports refusal when both clipboard paths are denied', async () => {
  setClipboard({ writeText: vi.fn().mockRejectedValue(new Error('Denied')) })
  Object.defineProperty(document, 'execCommand', { configurable: true, value: () => false })
  await expect(copyTextToClipboard('/project/report.md')).resolves.toBe(false)
  expect(document.querySelector('textarea')).toBeNull()
  Reflect.deleteProperty(document, 'execCommand')
})
