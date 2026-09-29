import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_BRIEF_HINT, StartMissionDialog } from './StartMissionDialog'

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

  it('asks for no limits and starts the run without any', async () => {
    const onStart = vi.fn(async () => {})
    render(<StartMissionDialog title="Wayfinding" onStart={onStart} onClose={vi.fn()} />)
    for (const label of [/dispatch/i, /attempt/i, /time limit/i]) expect(screen.queryByLabelText(label)).not.toBeInTheDocument()
    expect(screen.queryByRole('spinbutton')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Brief'), { target: { value: 'Sketch' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
    await waitFor(() => expect(onStart).toHaveBeenCalledWith({ cwd: '', contextPaths: [], brief: 'Sketch', beadId: '' }, 'notify'))
  })

  it('closes on Escape', () => {
    const onClose = vi.fn()
    render(<StartMissionDialog title="Wayfinding" onStart={vi.fn()} onClose={onClose} />)
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
