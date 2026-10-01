import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_BRIEF_HINT, StartMissionDialog } from './StartMissionDialog'
import { ApiRequestError } from './formationsApi'
import { missionRunInputs } from './missionInputs'
import type { MissionInput } from './formationsTypes'

const declared: MissionInput[] = [
  { name: 'topic', kind: 'text', required: true, description: 'What to **explore**' },
  { name: 'sketch_path', kind: 'file', description: 'A raw sketch' },
  { name: 'repo', kind: 'folder', required: true },
]

describe('StartMissionDialog', () => {
  afterEach(cleanup)

  it.each([false, true])('submits an automatic workspace, after switching modes: %s', async switchModes => {
    const onStart = vi.fn(async () => {})
    render(<StartMissionDialog title="Scouting" onStart={onStart} onClose={vi.fn()} />)
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
    await waitFor(() => expect(onStart).toHaveBeenCalledWith(expect.objectContaining({ cwd: '', contextPaths: [], inputs: { brief: 'Sketch' } }), 'notify'))
  })

  it.each(['automatic', 'existing'])('keeps context paths through mode switches and submits them in %s mode', async mode => {
    const onStart = vi.fn(async () => {})
    render(<StartMissionDialog title="Scouting" onStart={onStart} onClose={vi.fn()} />)
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
    const { unmount } = render(<StartMissionDialog title="Scouting" onStart={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByLabelText('Brief')).toHaveAccessibleDescription(DEFAULT_BRIEF_HINT)
    fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: 'existing' } })
    expect(screen.getByLabelText('Working directory')).toHaveAccessibleDescription('The absolute path of the directory the agents work in.')
    unmount()

    render(<StartMissionDialog title="Scouting" inputs={missionRunInputs({ id: 'inp', title: 'Start', goal: '', inputHint: 'Your raw sketch of the outcome you want.' })} onStart={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByLabelText('Brief')).toHaveAccessibleDescription('Your raw sketch of the outcome you want.')
  })

  it('renders a Markdown description instead of showing its marks', async () => {
    render(<StartMissionDialog title="Scouting" inputs={missionRunInputs({ id: 'inp', title: 'Start', goal: '', inputHint: 'Your **raw sketch** of the outcome.\n\n- the goal\n- who it is for' })} onStart={vi.fn()} onClose={vi.fn()} />)
    const help = document.getElementById('start-input-brief-help') as HTMLElement
    expect((await within(help).findByText('raw sketch')).tagName).toBe('STRONG')
    expect(within(help).getAllByRole('listitem').map(item => item.textContent)).toEqual(['the goal', 'who it is for'])
    expect(help).not.toHaveTextContent('**')
    expect(screen.getByLabelText('Brief')).toHaveAccessibleDescription(/Your raw sketch of the outcome\./)
  })

  it('asks for each declared input, with its description, and sends the values by name', async () => {
    const onStart = vi.fn(async () => {})
    render(<StartMissionDialog title="Scouting" inputs={declared} onStart={onStart} onClose={vi.fn()} />)
    expect(screen.queryByLabelText('Brief')).not.toBeInTheDocument()
    const topic = screen.getByLabelText('Topic')
    expect(topic).toHaveFocus()
    expect(topic.tagName).toBe('TEXTAREA')
    expect(topic).toHaveAccessibleDescription(/What to explore/)
    const sketch = screen.getByLabelText('Sketch path (optional)')
    expect(sketch).toHaveAttribute('placeholder', '/path/to/file')
    expect(sketch).toHaveAccessibleDescription('A raw sketch The absolute path of a file on the agent host.')
    const repo = screen.getByLabelText('Repo')
    expect(repo).toHaveAccessibleDescription('The absolute path of a directory on the agent host.')
    fireEvent.change(topic, { target: { value: '  video editing\n' } })
    fireEvent.change(repo, { target: { value: ' /work/repo ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
    await waitFor(() => expect(onStart).toHaveBeenCalledWith({ cwd: '', contextPaths: [], beadId: '', inputs: { topic: '  video editing\n', repo: '/work/repo' } }, 'notify'))
  })

  it('blocks Start with an inline message on each required input left blank', async () => {
    const onStart = vi.fn(async () => {})
    render(<StartMissionDialog title="Scouting" inputs={declared} onStart={onStart} onClose={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Topic'), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
    expect(onStart).not.toHaveBeenCalled()
    expect(screen.getByLabelText('Topic')).toHaveFocus()
    expect(screen.getByLabelText('Topic')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByText('Fill in topic to start.')).toBeInTheDocument()
    expect(screen.getByText('Fill in repo to start.')).toBeInTheDocument()
    expect(screen.queryByText(/Fill in sketch path/)).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Topic'), { target: { value: 'captions' } })
    expect(screen.queryByText('Fill in topic to start.')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Repo'), { target: { value: '/work' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
    await waitFor(() => expect(onStart).toHaveBeenCalledTimes(1))
  })

  it('lists what the daemon refused and keeps the values', async () => {
    const onStart = vi.fn(async () => {
      throw new ApiRequestError('The run needs 1 fix before it can start', 422, 'RUN_ADMISSION_FAILED', [
        { code: 'invalid_input', nodeId: 'inp', message: 'input repo must be the absolute path of an existing directory; /work does not exist' },
      ])
    })
    const onClose = vi.fn()
    render(<StartMissionDialog title="Scouting" inputs={declared} onStart={onStart} onClose={onClose} />)
    fireEvent.change(screen.getByLabelText('Topic'), { target: { value: 'captions' } })
    fireEvent.change(screen.getByLabelText('Repo'), { target: { value: '/work' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('The run needs 1 fix before it can start')
    expect(within(alert).getByRole('listitem')).toHaveTextContent('input repo must be the absolute path of an existing directory; /work does not exist')
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByLabelText('Repo')).toHaveValue('/work')
  })

  it('runs one step on its own without the human gate choice', async () => {
    const onStart = vi.fn(async () => {})
    render(<StartMissionDialog title="Scouting" step="Draft" onStart={onStart} onClose={vi.fn()} />)
    expect(screen.getByRole('dialog', { name: 'Run step' })).toHaveTextContent('Run Draft on its own')
    expect(screen.queryByRole('radiogroup', { name: 'Human gates' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Run step' }))
    expect(onStart).not.toHaveBeenCalled()
    expect(screen.getByText('Fill in brief to start.')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Brief'), { target: { value: 'Draft the plan' } })
    fireEvent.change(screen.getByLabelText('Bead'), { target: { value: 'archon-o7p.4' } })
    fireEvent.click(screen.getByRole('button', { name: 'Run step' }))
    await waitFor(() => expect(onStart).toHaveBeenCalledWith({ cwd: '', contextPaths: [], beadId: 'archon-o7p.4', inputs: { brief: 'Draft the plan' } }, 'notify'))
  })

  it('shows the mission human channel and starts with the chosen one', async () => {
    const onStart = vi.fn(async () => {})
    render(<StartMissionDialog title="Scouting" humanChannel="session" onStart={onStart} onClose={vi.fn()} />)
    const channel = screen.getByRole('radiogroup', { name: 'Human gates' })
    expect(within(channel).getByRole('radio', { name: /Talk with the agents/ })).toBeChecked()
    expect(channel).toHaveAccessibleDescription('Saved on the mission when you start. A change applies to runs started afterwards; runs already going keep their channel.')
    fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: 'existing' } })
    fireEvent.change(screen.getByLabelText('Working directory'), { target: { value: '/work' } })
    fireEvent.change(screen.getByLabelText('Brief'), { target: { value: 'Go' } })
    fireEvent.click(within(channel).getByRole('radio', { name: /Notify me/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
    await waitFor(() => expect(onStart).toHaveBeenCalledWith(expect.objectContaining({ cwd: '/work', inputs: { brief: 'Go' } }), 'notify'))
  })

  it('asks for no limits and starts the run without any', async () => {
    const onStart = vi.fn(async () => {})
    render(<StartMissionDialog title="Scouting" onStart={onStart} onClose={vi.fn()} />)
    for (const label of [/dispatch/i, /attempt/i, /time limit/i]) expect(screen.queryByLabelText(label)).not.toBeInTheDocument()
    expect(screen.queryByRole('spinbutton')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Brief'), { target: { value: 'Sketch' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start mission' }))
    await waitFor(() => expect(onStart).toHaveBeenCalledWith({ cwd: '', contextPaths: [], inputs: { brief: 'Sketch' }, beadId: '' }, 'notify'))
  })

  it('closes on Escape', () => {
    const onClose = vi.fn()
    render(<StartMissionDialog title="Scouting" onStart={vi.fn()} onClose={onClose} />)
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
