import { describe, expect, it } from 'vitest'
import type { BoardDocument, LimitNode } from './formationsTypes'
import {
  limitCoverage, limitCoversWords, limitKnobWords, limitLine, limitMeaning, limitSummary, limitUsePhrase, limitsCovering, roundsProblem, spentAllowance, tetherLine,
} from './limitCard'

const step = (id: string, title: string, type = 'solo') => ({ id, type, title, inputs: [], outputs: [], slots: [] })
const board = {
  inputCards: [{ id: 'mis', title: 'Delivery', goal: '' }],
  formations: [step('fmn_review', 'Review'), step('fmn_peers', 'Peer review', 'peer')],
  limits: [] as LimitNode[],
} as unknown as BoardDocument
const card = (target: string, rounds?: number, title = 'Cap'): LimitNode => ({ id: `lim_${title.toLowerCase()}`, title, target, ...(rounds ? { rounds } : {}) })

describe('Limit card words', () => {
  it('knows what a card covers', () => {
    expect(limitCoverage(board, card('fmn_review', 3))).toMatchObject({ kind: 'step', node: { id: 'fmn_review' } })
    expect(limitCoverage(board, card('mis', 20))).toMatchObject({ kind: 'mission', node: { id: 'mis' } })
    expect(limitCoverage(board, card('', 3))).toEqual({ kind: 'none' })
    expect(limitCoverage(board, card('fmn_gone', 3))).toEqual({ kind: 'missing', target: 'fmn_gone' })
    const two = { ...board, limits: [card('fmn_review', 3), card('fmn_review', 2, 'Guard'), card('mis', 20, 'All')] }
    expect(limitsCovering(two, 'fmn_review').map(limit => limit.title)).toEqual(['Cap', 'Guard'])
  })

  it('states the knob for a step, a peer step and the whole mission', () => {
    expect(limitKnobWords(board, card('fmn_review', 3))).toBe('at most 3 rounds')
    expect(limitKnobWords(board, card('fmn_review', 1))).toBe('at most 1 round')
    expect(limitKnobWords(board, card('fmn_peers', 40))).toBe('at most 40 journal messages')
    expect(limitKnobWords(board, card('mis', 20))).toBe('at most 20 step runs')
    expect(limitKnobWords(board, card('fmn_review'))).toBe('sets no limit yet')
  })

  it('says what the card covers on its face', () => {
    expect(limitCoversWords(board, card('fmn_review', 3))).toBe('Covers Review')
    expect(limitCoversWords(board, card('mis', 3))).toBe('Covers the whole mission')
    expect(limitCoversWords(board, card('', 3))).toBe('Wired to nothing yet')
    expect(limitCoversWords(board, card('fmn_gone', 3))).toBe('Covers a step that is gone')
  })

  it('states the limit on what it covers', () => {
    expect(limitLine(board, card('fmn_review', 3))).toBe('Limit Cap: at most 3 rounds')
    expect(limitLine(board, card('fmn_peers', 40))).toBe('Limit Cap: at most 40 journal messages')
    expect(limitLine(board, card('mis', 20))).toBe('Limit Cap: the whole mission may make at most 20 step runs')
    expect(limitSummary(board, card('fmn_review'))).toBe('Cap: sets no limit yet')
  })

  it('says what a card does to a run, and what a draft lacks', () => {
    expect(limitMeaning(board, card('fmn_review', 3))).toBe('Review may run at most 3 times, send-backs and resumed re-runs included. When its rounds are spent the run blocks before it starts again, until you grant one more round.')
    expect(limitMeaning(board, card('fmn_review', 1))).toContain('Review may run at most once')
    expect(limitMeaning(board, card('fmn_peers', 40))).toContain('Peer review may hold at most 40 journal messages over all its attempts')
    expect(limitMeaning(board, card('mis', 20))).toContain('The whole mission may make at most 20 step runs, judges included.')
    expect(limitMeaning(board, card('', 3))).toBe('Wired to nothing: drag its handle onto a step, or onto the Input card for the whole mission.')
    expect(limitMeaning(board, card('fmn_review'))).toBe('It sets no limit yet: give it rounds, or delete it.')
  })

  it('accepts blank or a positive whole number of rounds', () => {
    expect(roundsProblem('')).toBe('')
    expect(roundsProblem('3')).toBe('')
    for (const value of ['0', '-1', '2.5', 'three', '99999999999999999999']) expect(roundsProblem(value)).not.toBe('')
  })

  it('words a spent card as the engine and the CLI do', () => {
    const use = { kind: 'rounds' as const, limitId: 'lim_cap', nodeId: 'fmn_review', used: 3, max: 3 }
    expect(limitUsePhrase(use, 'Review')).toBe('Review used 3 of 3 rounds')
    expect(limitUsePhrase({ ...use, used: 20, max: 20, granted: 1 }, 'The mission')).toBe('The mission used 20 of 20 rounds, 1 of them granted')
    expect(spentAllowance(use)).toBe('all 3 of its rounds')
    expect(spentAllowance({ ...use, used: 1, max: 1 })).toBe('its only round')
  })

  it('draws the tether between the two cards\' edges', () => {
    // A card below its step: from the card's top edge to the step's bottom edge.
    expect(tetherLine({ x: 100, y: 400, width: 220, height: 72 }, { x: 100, y: 100, width: 220, height: 200 })).toEqual({ x1: 210, y1: 400, x2: 210, y2: 300 })
    // Beside it: from the side.
    expect(tetherLine({ x: 500, y: 100, width: 100, height: 100 }, { x: 100, y: 100, width: 100, height: 100 })).toEqual({ x1: 500, y1: 150, x2: 200, y2: 150 })
  })
})
