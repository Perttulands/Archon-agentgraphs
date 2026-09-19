import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_BRIEF_HINT, StartMissionDialog, TIME_LIMIT_HINT } from './StartMissionDialog'

describe('StartMissionDialog', () => {
  afterEach(cleanup)

  it.each([false, true])('submits an automatic workspace, after switching modes: %s', async switchModes => {
    const onStart = vi.fn(async () => {})
    render(<StartMissionDialog title="Wayfinding" onStart={onStart} onClose={vi.fn()} />)
    expect(screen.getByLabelText('Workspace')).toHaveValue('automatic')
    expect(screen.queryByLabelText('Working directory')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Brief')).toHaveFocus()
    fireEvent.change(screen.getByLabelText('Brief'), { target: { value: 'Sketch' } })
    if (switchModes) {
      fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: 'existing' } })
      const cwd = screen.getByLabelText('Working directory')
      for (const value of ['', 'relative/path']) {
        fireEvent.change(cwd, { target: { value } })
        fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
        expect(onStart).not.toHaveBeenCalled()
      }
      fireEvent.change(cwd, { target: { value: '/work/project' } })
      expect(cwd).toBeValid()
      fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: 'automatic' } })
      expect(screen.queryByLabelText('Working directory')).not.toBeInTheDocument()
    }
    fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
    await waitFor(() => expect(onStart).toHaveBeenCalledWith(expect.objectContaining({ cwd: '', contextPaths: [], brief: 'Sketch' }), 'notify'))
  })

  it.each(['automatic', 'existing'])('keeps context paths through mode switches and submits them in %s mode', async mode => {
    const onStart = vi.fn(async () => {})
    render(<StartMissionDialog title="Wayfinding" onStart={onStart} onClose={vi.fn()} />)
    const paths = '/work/prior art\n\n/work/notes.md\n'
    fireEvent.change(screen.getByLabelText('Context paths'), { target: { value: paths } })
    fireEvent.change(screen.getByLabelText('Brief'), { target: { value: 'Sketch' } })
    fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: 'existing' } })
    fireEvent.change(screen.getByLabelText('Working directory'), { target: { value: '/work/project' } })
    fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: 'automatic' } })
    fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: mode } })
    expect(screen.getByLabelText('Context paths')).toHaveValue(paths)
    fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
    await waitFor(() => expect(onStart).toHaveBeenCalledWith(expect.objectContaining({
      cwd: mode === 'automatic' ? '' : '/work/project', contextPaths: ['/work/prior art', '/work/notes.md'],
    }), 'notify'))
  })

  it('explains the brief, using the mission input hint when one is set', () => {
    const { unmount } = render(<StartMissionDialog title="Wayfinding" onStart={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByLabelText('Brief')).toHaveAccessibleDescription(DEFAULT_BRIEF_HINT)
    fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: 'existing' } })
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
    fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: 'existing' } })
    fireEvent.change(screen.getByLabelText('Working directory'), { target: { value: '/work' } })
    fireEvent.change(screen.getByLabelText('Brief'), { target: { value: 'Go' } })
    fireEvent.click(within(channel).getByRole('radio', { name: /Notify me/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
    await waitFor(() => expect(onStart).toHaveBeenCalledWith(expect.objectContaining({ cwd: '/work', brief: 'Go' }), 'notify'))
  })

  it('says the time limit counts agent work, not waiting for the operator at a gate', () => {
    render(<StartMissionDialog title="Wayfinding" onStart={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByLabelText('Time limit in seconds')).toHaveAccessibleDescription(`30 minutes ${TIME_LIMIT_HINT}`)
    expect(TIME_LIMIT_HINT).toMatch(/how long the agents work/)
    expect(TIME_LIMIT_HINT).toMatch(/waits for you at a gate doesn\u2019t count/)
    expect(screen.getByLabelText('Maximum dispatches')).toHaveAccessibleDescription('Formation executions across this run, including judge steps.')
    expect(screen.getByLabelText('Maximum attempts')).toHaveAccessibleDescription('Maximum visits to each node.')
  })

  it('keeps cleared limits blank and submits replacement values as numbers', async () => {
    const onStart = vi.fn(async () => {})
    render(<StartMissionDialog title="Wayfinding" onStart={onStart} onClose={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: 'existing' } })
    fireEvent.change(screen.getByLabelText('Working directory'), { target: { value: '/work' } })
    fireEvent.change(screen.getByLabelText('Brief'), { target: { value: 'Sketch' } })
    const dispatches = screen.getByLabelText('Maximum dispatches')
    fireEvent.change(dispatches, { target: { value: '' } })
    expect(dispatches).toHaveValue(null)
    fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
    expect(onStart).not.toHaveBeenCalled()
    fireEvent.change(dispatches, { target: { value: '0' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
    expect(onStart).not.toHaveBeenCalled()
    fireEvent.change(dispatches, { target: { value: '24' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
    await waitFor(() => expect(onStart).toHaveBeenCalledWith({
      cwd: '/work', contextPaths: [], brief: 'Sketch', beadId: '',
      limits: { maxDispatch: 24, maxAttempts: 3, wallClockSeconds: 1800, redact: false },
    }, 'notify'))
  })

  it('updates the readable duration and omits it for an invalid value', () => {
    render(<StartMissionDialog title="Wayfinding" onStart={vi.fn()} onClose={vi.fn()} />)
    const time = screen.getByLabelText('Time limit in seconds')
    fireEvent.change(time, { target: { value: '3661' } })
    expect(time).toHaveAccessibleDescription(`1 hour 1 minute 1 second ${TIME_LIMIT_HINT}`)
    fireEvent.change(time, { target: { value: '' } })
    expect(time).toHaveValue(null)
    expect(time).toHaveAccessibleDescription(TIME_LIMIT_HINT)
  })

  it('closes on Escape', () => {
    const onClose = vi.fn()
    render(<StartMissionDialog title="Wayfinding" onStart={vi.fn()} onClose={onClose} />)
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
