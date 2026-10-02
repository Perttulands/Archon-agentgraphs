import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import Markdown from '../evidence/Markdown'
import TextLines from '../evidence/TextLines'
import { copyTextToClipboard } from '../utils/clipboard'
vi.mock('../utils/clipboard', () => ({ copyTextToClipboard: vi.fn(() => Promise.resolve(true)) }))
afterEach(cleanup)
it.each(['brief', 'output', 'note', 'file source'])('links paths and copies IDs in %s', surface => {
 const text = 'Read /tmp/proof.md for archon-n7u.52'
 render(surface === 'file source' ? <TextLines content={text} label="Source" /> : <Markdown content={text} />)
 const receive = vi.fn();document.addEventListener('archon-open-path', receive)
 fireEvent.click(screen.getByRole('button', { name: '/tmp/proof.md' }))
 expect(receive.mock.calls[0][0].detail.path).toBe('/tmp/proof.md')
 document.removeEventListener('archon-open-path', receive)
 fireEvent.click(screen.getByRole('button', { name: 'archon-n7u.52' }))
 expect(copyTextToClipboard).toHaveBeenCalledWith('archon-n7u.52')
})
