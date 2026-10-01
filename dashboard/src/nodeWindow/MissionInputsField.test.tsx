import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MissionInputsField, inputsProblem } from './MissionInputsField'
import type { MissionInput } from '../components/formationsTypes'

describe('MissionInputsField', () => {
  afterEach(cleanup)

  it('reads the declared inputs as the references briefs use', () => {
    render(<MissionInputsField inputs={[{ name: 'topic', kind: 'text', required: true, description: 'What to explore' }, { name: 'repo', kind: 'folder' }]} onSave={vi.fn()} />)
    const items = within(screen.getByRole('list', { name: 'Inputs' })).getAllByRole('listitem').map(item => item.textContent)
    expect(items).toEqual(['{topic} text, required · What to explore', '{repo} folder, optional'])
  })

  it('says a mission without inputs takes a brief', () => {
    render(<MissionInputsField inputs={undefined} onSave={vi.fn()} />)
    expect(screen.getByText('No inputs declared. Each run takes a required brief.')).toBeInTheDocument()
  })

  it('adds, changes and removes inputs in one save', async () => {
    const onSave = vi.fn(async (_inputs: MissionInput[]) => true)
    render(<MissionInputsField inputs={[{ name: 'topic', kind: 'text', required: true }, { name: 'old', kind: 'text' }]} onSave={onSave} />)
    fireEvent.click(screen.getByRole('button', { name: 'Edit inputs' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove input old' }))
    fireEvent.click(screen.getByRole('button', { name: 'Add input' }))
    fireEvent.change(screen.getByLabelText('Name of input 2'), { target: { value: ' sketch ' } })
    fireEvent.change(screen.getByLabelText('Kind of input 2'), { target: { value: 'file' } })
    fireEvent.click(within(screen.getByRole('group', { name: 'Input 2' })).getByRole('checkbox'))
    fireEvent.change(screen.getByLabelText('Description of input 1'), { target: { value: 'What to explore ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save inputs' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledWith([
      { name: 'topic', kind: 'text', required: true, description: 'What to explore' },
      { name: 'sketch', kind: 'file', required: false, description: '' },
    ]))
    expect(await screen.findByRole('status')).toHaveTextContent('Inputs saved.')
  })

  it('refuses a name briefs cannot reference, or a repeated one, before saving', () => {
    expect(inputsProblem([{ name: 'Sketch Path', kind: 'text' }])).toBe('Name "Sketch Path": a lowercase letter, then lowercase letters, digits or underscores.')
    expect(inputsProblem([{ name: 'a', kind: 'text' }, { name: 'a', kind: 'file' }])).toBe('Two inputs are named a.')
    expect(inputsProblem([{ name: 'brief', kind: 'text' }])).toBe('')
    const onSave = vi.fn(async () => true)
    render(<MissionInputsField inputs={[]} onSave={onSave} />)
    fireEvent.click(screen.getByRole('button', { name: 'Edit inputs' }))
    fireEvent.click(screen.getByRole('button', { name: 'Add input' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save inputs' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Name each input: a lowercase letter, then lowercase letters, digits or underscores.')
    expect(onSave).not.toHaveBeenCalled()
  })
})
