// Adapted from CHROTE's Markdown tests (dashboard/src/components/Markdown.test.tsx
// at 48947850) for artifact-relative links and images.
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import Markdown, { isFileLink, resolveMarkdownPath } from './Markdown'

describe('resolveMarkdownPath', () => {
  it.each([
    ['reports/final.md', '../plan.md', 'plan.md'],
    ['reports/final.md', 'nested/page.md', 'reports/nested/page.md'],
    ['reports/final.md', './same.md', 'reports/same.md'],
    ['reports/final.md', '/rooted.md', 'rooted.md'],
    ['reports/final.md', '../../../../etc/passwd', 'etc/passwd'],
    ['plan.md', 'anchored.md#section', 'anchored.md'],
  ])('resolves %s + %s', (base, href, expected) => {
    expect(resolveMarkdownPath(base, href)).toBe(expected)
  })
})

describe('isFileLink', () => {
  it.each([
    ['../PRD.md', true],
    ['/srv/chrote/PRD.md', true],
    ['https://example.com', false],
    ['mailto:someone@example.com', false],
    ['#section', false],
    ['', false],
  ])('reads %s', (href, expected) => {
    expect(isFileLink(href)).toBe(expected)
  })
})

describe('Markdown', () => {
  it('renders headings, code, tables and lists in the theme', () => {
    render(
      <Markdown
        content={[
          '# Title',
          '',
          'Some `inline` text and **strong** words.',
          '',
          '| Head | Other |',
          '| --- | --- |',
          '| one | two |',
          '',
          '- first',
          '- second',
          '',
          '```go',
          'func main() {}',
          '```',
        ].join('\n')}
      />,
    )

    expect(screen.getByRole('heading', { name: 'Title' })).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: 'one' })).toBeInTheDocument()
    expect(screen.getAllByRole('listitem')).toHaveLength(2)
    expect(screen.getByText('func main() {}')).toBeInTheDocument()
    expect(screen.getByText('inline').tagName).toBe('CODE')
  })

  it('opens a link to another artifact in the inspector', () => {
    const openPath = vi.fn()
    render(<Markdown content="See [the plan](../plan.md)." basePath="reports/final.md" onOpenPath={openPath} />)

    fireEvent.click(screen.getByRole('link', { name: 'the plan' }))
    expect(openPath).toHaveBeenCalledWith('plan.md')
  })

  it('leaves an external link external and draws raw HTML and unfollowable links as text', () => {
    const { container } = render(<Markdown content={'[out](https://example.com) and [bad](javascript:alert(1))\n\n<script>alert(2)</script>'} />)

    expect(screen.getByRole('link', { name: 'out' })).toHaveAttribute('rel', expect.stringContaining('noopener'))
    expect(screen.queryByRole('link', { name: 'bad' })).not.toBeInTheDocument()
    expect(container.querySelector('script')).toBeNull()
  })

  it('loads an image beside the document through the host URL, and no other file image', () => {
    const { rerender } = render(<Markdown content="![shot](shots/panel.png)" basePath="reports/final.md" imageUrl={path => `/raw/${path}`} />)
    expect(screen.getByRole('img', { name: 'shot' })).toHaveAttribute('src', '/raw/reports/shots/panel.png')

    rerender(<Markdown content="![shot](shots/panel.png)" basePath="reports/final.md" />)
    expect(screen.queryByRole('img')).toBeNull()
  })
})
