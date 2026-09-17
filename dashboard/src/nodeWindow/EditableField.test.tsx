import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { EditableField } from './EditableField'

afterEach(cleanup)
describe('field save feedback', () => {
  it('waits for confirmation and clears the receipt on the next edit', async () => {
    let resolveSave!: (saved: boolean) => void
    const onSave = vi.fn(() => new Promise<boolean>(resolve => { resolveSave = resolve }))
    render(<EditableField label="Brief" value="Original" multiline placeholder="Brief" onSave={onSave} />)
    fireEvent.click(screen.getByRole('button', { name: 'Edit brief' }))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Updated' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter', ctrlKey: true })
    expect(onSave).toHaveBeenCalledWith('Updated')
    expect(screen.queryByRole('status')).toBeNull()
    expect(screen.getByRole('button', { name: 'Save brief' })).toBeDisabled()
    await act(async () => resolveSave(true))
    expect(screen.getByRole('status')).toHaveTextContent('Brief saved.')
    fireEvent.click(screen.getByRole('button', { name: 'Edit brief' }))
    expect(screen.queryByRole('status')).toBeNull()
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Escape' })
    expect(screen.queryByRole('textbox')).toBeNull()
  })
  it('retains failed edits without a success receipt', async () => {
    render(<EditableField label="Brief" value="Original" multiline placeholder="Brief" onSave={async () => false} />)
    fireEvent.click(screen.getByRole('button', { name: 'Edit brief' }))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Updated' } })
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Save brief' })))
    expect(screen.getByRole('alert')).toHaveTextContent('The brief was not saved.')
    expect(screen.getByRole('textbox')).toHaveValue('Updated')
    expect(screen.queryByRole('status')).toBeNull()
  })
  it('does not report an unchanged field as a newly completed save', async () => {
    const onSave = vi.fn(async () => true)
    render(<EditableField label="Title" value="Original" placeholder="Title" onSave={onSave} />)
    fireEvent.click(screen.getByRole('button', { name: 'Edit title' }))
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Save title' })))
    expect(onSave).not.toHaveBeenCalled()
    expect(screen.queryByRole('status')).toBeNull()
  })
})
