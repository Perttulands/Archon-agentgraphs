import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_BRIEF_HINT, StartMissionDialog } from './StartMissionDialog'

describe('StartMissionDialog', () => {
  afterEach(cleanup)

  it('explains the brief, using the mission input hint when one is set', () => {
    const { unmount } = render(<StartMissionDialog title="Wayfinding" onStart={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByLabelText('Brief')).toHaveAccessibleDescription(DEFAULT_BRIEF_HINT)
    expect(screen.getByLabelText('Working directory')).toHaveAccessibleDescription('The absolute path of the directory the agents work in.')
    unmount()

    render(<StartMissionDialog title="Wayfinding" inputHint="Your raw sketch of the outcome you want." onStart={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByLabelText('Brief')).toHaveAccessibleDescription('Your raw sketch of the outcome you want.')
  })

  it('renders a Markdown input hint instead of showing its marks', async () => {
    render(<StartMissionDialog title="Wayfinding" inputHint={'Your **raw sketch** of the outcome.\n\n- the goal\n- who it is for'} onStart={vi.fn()} onClose={vi.fn()} />)
    const help = document.getElementById('start-mission-brief-help') as HTMLElement
    expect((await within(help).findByText('raw sketch')).tagName).toBe('STRONG')
    expect(within(help).getAllByRole('listitem').map(item => item.textContent)).toEqual(['the goal', 'who it is for'])
    expect(help).not.toHaveTextContent('**')
    expect(screen.getByLabelText('Brief')).toHaveAccessibleDescription(/Your raw sketch of the outcome\./)
  })

  it('closes on Escape', () => {
    const onClose = vi.fn()
    render(<StartMissionDialog title="Wayfinding" onStart={vi.fn()} onClose={onClose} />)
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
