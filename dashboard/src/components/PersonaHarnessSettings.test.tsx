import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { EffortSelect, SeatLaunch, variantChanges, variantDraft, variantSettingsSummary } from './PersonaHarnessSettings'

const claude = { id: 'claude-code', efforts: ['low', 'medium', 'high', 'xhigh', 'max'] }

describe('persona harness settings', () => {
  it('patches only what changed, and treats an unset effort and medium as the same', () => {
    expect(variantChanges({ ...claude, effort: 'medium' }, { model: '', effort: '' })).toBeNull()
    expect(variantChanges(claude, variantDraft({ ...claude, effort: 'medium' }))).toBeNull()
    expect(variantChanges({ ...claude, model: 'claude-opus-5' }, { model: ' claude-opus-5 ', effort: '' })).toBeNull()
    expect(variantChanges(claude, { model: ' claude-opus-5 ', effort: 'low' })).toEqual({ id: 'claude-code', model: 'claude-opus-5', effort: 'low' })
    expect(variantChanges({ ...claude, model: 'm', effort: 'max' }, { model: '', effort: 'medium' })).toEqual({ id: 'claude-code', model: '', effort: '' })
  })

  it('names the defaults in words', () => {
    expect(variantSettingsSummary({ ...claude, effectiveEffort: 'medium' })).toBe('harness default · effort medium (default)')
    expect(variantSettingsSummary({ ...claude, model: 'claude-opus-5', effort: 'low', effectiveEffort: 'low' })).toBe('claude-opus-5 · effort low')
    expect(variantSettingsSummary({ id: 'hermes' })).toBe('no model or effort')
  })

  it('shows an effort the harness does not take as it is', () => {
    render(<EffortSelect id="e" efforts={claude.efforts} value="ultra" onChange={() => undefined} />)
    expect(screen.getByRole('combobox', { name: 'Effort' })).toHaveDisplayValue('ultra (not accepted by this harness)')
    expect(screen.getAllByRole('option').map(option => option.textContent)).toEqual(['medium (default)', 'low', 'high', 'xhigh', 'max', 'ultra (not accepted by this harness)'])
  })

  it('says a legacy launch string is not what seats run, and why a variant cannot start', () => {
    const { rerender } = render(<SeatLaunch variant={{ ...claude, launch: 'claude --effort="max"', seatLaunch: "exec '/bin/claude' --effort 'medium'" }} />)
    expect(screen.getByTestId('seat-launch-claude-code')).toHaveTextContent("exec '/bin/claude' --effort 'medium'")
    expect(screen.getByText(/legacy launch string/)).toHaveTextContent('Seats do not use it: they start from the harness, model and effort above, which are authoritative.')
    rerender(<SeatLaunch variant={{ id: 'hermes', launch: 'hermes --profile x', seatLaunchError: 'unsupported seat harness "hermes"' }} />)
    expect(screen.getByText('Archon cannot start hermes seats.')).toBeInTheDocument()
    expect(screen.getByText(/used only by/)).toHaveTextContent('Launch string hermes --profile x, used only by archon agent spawn.')
  })
})
