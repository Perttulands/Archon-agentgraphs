import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_BRIEF_HINT, StartMissionDialog, TIME_LIMIT_HINT } from './StartMissionDialog'

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

  it('shows the mission human channel and starts with the chosen one', async () => {
    const onStart = vi.fn(async () => {})
    render(<StartMissionDialog title="Wayfinding" humanChannel="session" onStart={onStart} onClose={vi.fn()} />)
    const channel = screen.getByRole('radiogroup', { name: 'Human gates' })
    expect(within(channel).getByRole('radio', { name: /Talk with the agents/ })).toBeChecked()
    expect(channel).toHaveAccessibleDescription('Saved on the mission when you start. A change applies to runs started afterwards; runs already going keep their channel.')
    fireEvent.change(screen.getByLabelText('Working directory'), { target: { value: '/work' } })
    fireEvent.change(screen.getByLabelText('Brief'), { target: { value: 'Go' } })
    fireEvent.click(within(channel).getByRole('radio', { name: /Notify me/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
    await waitFor(() => expect(onStart).toHaveBeenCalledWith(expect.objectContaining({ cwd: '/work', brief: 'Go' }), 'notify'))
  })

  it('says the time limit counts agent work, not waiting for the operator at a gate', () => {
    render(<StartMissionDialog title="Wayfinding" onStart={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByLabelText('Time limit in seconds')).toHaveAccessibleDescription(TIME_LIMIT_HINT)
    expect(TIME_LIMIT_HINT).toMatch(/how long the agents work/)
    expect(TIME_LIMIT_HINT).toMatch(/waits for you at a gate doesn\u2019t count/)
    expect(screen.getByLabelText('Maximum dispatches')).not.toHaveAccessibleDescription()
  })

  it('closes on Escape', () => {
    const onClose = vi.fn()
    render(<StartMissionDialog title="Wayfinding" onStart={vi.fn()} onClose={onClose} />)
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
