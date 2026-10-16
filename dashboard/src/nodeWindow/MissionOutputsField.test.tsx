import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MissionOutputsField, outputPortChoices, outputsProblem } from './MissionOutputsField'
import type { FormationNode, MissionOutput } from '../components/formationsTypes'

const step = (id: string, title: string, outputs: FormationNode['outputs']): FormationNode => ({ id, type: 'solo', title, inputs: [], outputs, slots: [] })
const ports = outputPortChoices({ formations: [
  step('build', 'Build', [{ id: 'out', label: 'Result' }, { id: 'port_site', label: 'Site', kind: 'folder' }]),
  step('write', 'Write', [{ id: 'out', label: 'Report', kind: 'file' }]),
] })

describe('MissionOutputsField', () => {
  afterEach(cleanup)

  it('offers every step output port by step and port name, with its kind', () => {
    expect(ports).toEqual([
      { value: 'build:out', label: 'Build · Result', kind: 'text' },
      { value: 'build:port_site', label: 'Build · Site', kind: 'folder' },
      { value: 'write:out', label: 'Write · Report', kind: 'file' },
    ])
  })

  it('reads the declared outputs with the port each is bound to', () => {
    render(<MissionOutputsField ports={ports} onSave={vi.fn()} outputs={[
      { name: 'report', kind: 'file', required: true, from: 'write:out', description: 'The written report' },
      { name: 'notes', kind: 'text', from: '' },
    ]} />)
    const items = within(screen.getByRole('list', { name: 'Outputs' })).getAllByRole('listitem').map(item => item.textContent)
    expect(items).toEqual(['report file, required · from Write · Report · The written report', 'notes text, optional · not bound'])
  })

  it('says a mission without outputs declares none', () => {
    render(<MissionOutputsField outputs={undefined} ports={ports} onSave={vi.fn()} />)
    expect(screen.getByText('No outputs declared.')).toBeInTheDocument()
  })

  it('adds an output bound to a port, which takes the port kind, and saves the whole list', async () => {
    const onSave = vi.fn(async (_outputs: MissionOutput[]) => true)
    render(<MissionOutputsField outputs={[{ name: 'old', kind: 'text', from: 'build:out' }]} ports={ports} onSave={onSave} />)
    fireEvent.click(screen.getByRole('button', { name: 'Edit outputs' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove output old' }))
    fireEvent.click(screen.getByRole('button', { name: 'Add output' }))
    fireEvent.change(screen.getByLabelText('Name of output 1'), { target: { value: ' site ' } })
    const port = screen.getByLabelText('Port of output 1')
    expect(within(port).getAllByRole('option').map(option => option.textContent)).toEqual(['Not bound to a step output', 'Build · Result', 'Build · Site', 'Write · Report'])
    fireEvent.change(port, { target: { value: 'build:port_site' } })
    expect(screen.getByLabelText('Kind of output 1')).toHaveValue('folder')
    fireEvent.change(screen.getByLabelText('Description of output 1'), { target: { value: 'The built site ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save outputs' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledWith([
      { name: 'site', kind: 'folder', required: true, from: 'build:port_site', description: 'The built site' },
    ]))
    expect(await screen.findByRole('status')).toHaveTextContent('Outputs saved.')
  })

  it('keeps a binding to a port no longer on the board selectable', () => {
    render(<MissionOutputsField outputs={[{ name: 'gone', kind: 'text', from: 'removed:out' }]} ports={ports} onSave={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Edit outputs' }))
    expect(screen.getByLabelText('Port of output 1')).toHaveValue('removed:out')
  })

  it('refuses a name the daemon would refuse, or a repeated one, and allows an unbound draft', () => {
    expect(outputsProblem([{ name: 'Final Report', kind: 'text' }])).toBe('Name "Final Report": a lowercase letter, then lowercase letters, digits or underscores.')
    expect(outputsProblem([{ name: 'a', kind: 'text' }, { name: 'a', kind: 'file' }])).toBe('Two outputs are named a.')
    expect(outputsProblem([{ name: 'report', kind: 'file', from: '' }])).toBe('')
    const onSave = vi.fn(async () => true)
    render(<MissionOutputsField outputs={[]} ports={ports} onSave={onSave} />)
    fireEvent.click(screen.getByRole('button', { name: 'Edit outputs' }))
    fireEvent.click(screen.getByRole('button', { name: 'Add output' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save outputs' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Name each output: a lowercase letter, then lowercase letters, digits or underscores.')
    expect(onSave).not.toHaveBeenCalled()
  })
})
