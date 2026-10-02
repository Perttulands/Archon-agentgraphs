import { describe, expect, it } from 'vitest'
import type { BoardDocument, LimitNode } from './formationsTypes'
import {
  durationInput, durationWords, grantWords, leftWords, limitCoverage, limitCoversWords, limitKnobWords, limitLine, limitMeaning, limitSummary, limitUsePhrase,
  limitWarnWords, limitsCovering, parseDuration, roundsProblem, spentAllowance, tetherLine, timeProblem, tokenWords, tokensProblem, warnProblem,
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
    expect(limitCoversWords(board, card('mis', 3))).toBe('Covers the mission')
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
    expect(limitMeaning(board, card('fmn_review'))).toBe('It sets no limit yet: give it rounds, time or tokens, or delete it.')
  })

  it('states a tokens knob, alone and beside the others, with how it counts (archon-o7p.9)', () => {
    const tokens = (target: string, extra: Partial<LimitNode> = {}): LimitNode => ({ ...card(target), tokens: 50000, ...extra })
    expect(tokenWords(1)).toBe('1 token')
    expect(tokenWords(1234567)).toBe('1,234,567 tokens')
    expect(limitKnobWords(board, tokens('fmn_review'))).toBe('at most 50,000 tokens')
    expect(limitKnobWords(board, tokens('fmn_review', { rounds: 3, seconds: 1800 }))).toBe('at most 3 rounds · 30 min · 50,000 tokens')
    expect(limitSummary(board, tokens('fmn_review'))).toBe('Cap: at most 50,000 tokens')
    expect(limitSummary(board, tokens('fmn_review', { rounds: 3, seconds: 1800, warnSeconds: 300 }))).toBe('Cap: at most 3 rounds, 30 min of work and 50,000 tokens, warns at 5 min left')
    expect(limitSummary(board, tokens('mis', { rounds: 20 }))).toBe('Cap: the whole mission may make at most 20 step runs and spend at most 50,000 tokens')
    expect(limitMeaning(board, tokens('fmn_review'))).toBe('Review may spend at most 50,000 tokens over all its attempts. When they are spent the step stops and the run blocks until you grant 50,000 tokens more. Tokens are approximate: input not read from the cache, cache writes included, plus output, subagents included.')
    expect(limitMeaning(board, tokens('mis'))).toContain('The whole mission may spend at most 50,000 tokens, counted over every step, judges included.')
    const spent = { kind: 'tokens' as const, limitId: 'lim_cap', nodeId: 'fmn_review', used: 51230, max: 100000, granted: 50000 }
    expect(limitUsePhrase(spent, 'Review')).toBe('Review used 51,230 of 100,000 tokens, 50,000 of them granted')
    expect(spentAllowance(spent)).toBe('all 100,000 tokens it may spend')
    expect(grantWords(spent)).toBe('50,000 tokens more')
    expect(tokensProblem('')).toBe('')
    expect(tokensProblem('50000')).toBe('')
    for (const value of ['0', '-1', '2.5', 'lots']) expect(tokensProblem(value)).not.toBe('')
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

  it('says durations as the server does', () => {
    expect(durationWords(45)).toBe('45 s')
    expect(durationWords(300)).toBe('5 min')
    expect(durationWords(5400)).toBe('1 h 30 min')
    expect(durationWords(90)).toBe('1 min 30 s')
    expect(durationWords(7200)).toBe('2 h')
    expect(durationWords(3661)).toBe('1 h 1 min 1 s')
  })

  it('reads a typed time in whole seconds, and writes it back', () => {
    expect(parseDuration('')).toBe(0)
    expect(parseDuration('30m')).toBe(1800)
    expect(parseDuration('1h30m')).toBe(5400)
    expect(parseDuration('45s')).toBe(45)
    expect(parseDuration('90')).toBe(90)
    expect(parseDuration('1 h 30 min')).toBe(5400)
    expect(parseDuration('1.5h')).toBe(5400)
    for (const value of ['0', '0s', '-5m', '1.5s', 'soon', '5 minutes', '30m soon', '99999999999999999999']) expect(parseDuration(value), value).toBeNull()
    expect(durationInput(5400)).toBe('1h30m')
    expect(durationInput(45)).toBe('45s')
    expect(durationInput(90)).toBe('1m30s')
    expect(durationInput(undefined)).toBe('')
    expect(timeProblem('30m')).toBe('')
    expect(timeProblem('')).toBe('')
    expect(timeProblem('soon')).not.toBe('')
    expect(warnProblem('5m', 1800)).toBe('')
    expect(warnProblem('', undefined)).toBe('')
    expect(warnProblem('5m', undefined)).toBe('A warning needs time: give the card time first.')
    expect(warnProblem('30m', 1800)).toBe('Warn with less time left than the card\'s 30 min.')
    expect(warnProblem('x', 1800)).not.toBe('')
  })

  it('states a time knob, alone or with rounds, and its warning', () => {
    const clock = (target: string, seconds: number, extra: Partial<LimitNode> = {}): LimitNode => ({ id: 'lim_clock', title: 'Clock', target, seconds, ...extra })
    expect(limitKnobWords(board, clock('fmn_review', 1800))).toBe('at most 30 min of work')
    expect(limitKnobWords(board, clock('fmn_review', 1800, { rounds: 3 }))).toBe('at most 3 rounds · 30 min')
    expect(limitKnobWords(board, clock('fmn_peers', 1800, { rounds: 40 }))).toBe('at most 40 journal messages · 30 min')
    expect(limitKnobWords(board, clock('mis', 7200))).toBe('at most 2 h of work')
    expect(limitWarnWords(clock('fmn_review', 1800, { warnSeconds: 300 }))).toBe('warns at 5 min left')
    expect(limitWarnWords({ ...card('fmn_review', 3), warnSeconds: 300 })).toBe('')
    expect(limitSummary(board, clock('fmn_review', 1800))).toBe('Clock: at most 30 min of work')
    expect(limitSummary(board, clock('fmn_review', 1800, { rounds: 3, warnSeconds: 300 }))).toBe('Clock: at most 3 rounds and 30 min of work, warns at 5 min left')
    expect(limitSummary(board, clock('mis', 7200))).toBe('Clock: the whole mission may work at most 2 h')
    expect(limitLine(board, clock('mis', 7200, { rounds: 20, warnSeconds: 900 }))).toBe('Limit Clock: the whole mission may make at most 20 step runs and work at most 2 h, warns at 15 min left')
  })

  it('says what a time card does: waiting does not count and a send-back resumes with the time left', () => {
    expect(limitMeaning(board, { id: 'lim_clock', title: 'Clock', target: 'fmn_review', seconds: 1800, warnSeconds: 300 })).toBe(
      'Review may work at most 30 min over all its attempts, counted only while one runs. Waiting on a human gate does not count, so a send-back resumes it with the time it has left. When the time runs out the step stops and the run blocks until you grant 30 min more. With 5 min left, Archon pastes a warning into its seats.')
    const both = limitMeaning(board, { id: 'lim_clock', title: 'Clock', target: 'fmn_review', rounds: 3, seconds: 1800 })
    expect(both).toContain('Review may run at most 3 times')
    expect(both).toContain('Review may work at most 30 min')
    expect(limitMeaning(board, { id: 'lim_all', title: 'All', target: 'mis', seconds: 7200 })).toBe(
      'The whole mission may work at most 2 h, counted while any step runs, judges included. Waiting on a human gate does not count while no other step runs, nor does a blocked run. When the time runs out the running step stops and the run blocks until you grant 2 h more.')
  })

  it('words a spent time card and its grant as the engine does', () => {
    const use = { kind: 'time' as const, limitId: 'lim_clock', nodeId: 'fmn_review', used: 1800, max: 1800 }
    expect(limitUsePhrase(use, 'Review')).toBe('Review used 30 min of 30 min')
    expect(limitUsePhrase({ ...use, used: 3600, max: 3600, granted: 1800 }, 'The mission')).toBe('The mission used 1 h of 1 h, 30 min of it granted')
    expect(spentAllowance(use)).toBe('all 30 min of its time')
    expect(grantWords(use)).toBe('30 min more')
    expect(grantWords({ ...use, max: 3600, granted: 1800 })).toBe('30 min more')
    expect(grantWords({ kind: 'rounds', limitId: 'lim_cap', nodeId: 'fmn_review', used: 3, max: 3 })).toBe('one more round')
    expect(leftWords({ ...use, used: 300 })).toBe('25 min of 30 min left')
  })

  it('draws the tether between the two cards\' edges', () => {
    // A card below its step: from the card's top edge to the step's bottom edge.
    expect(tetherLine({ x: 100, y: 400, width: 220, height: 72 }, { x: 100, y: 100, width: 220, height: 200 })).toEqual({ x1: 210, y1: 400, x2: 210, y2: 300 })
    // Beside it: from the side.
    expect(tetherLine({ x: 500, y: 100, width: 100, height: 100 }, { x: 100, y: 100, width: 100, height: 100 })).toEqual({ x1: 500, y1: 150, x2: 200, y2: 150 })
  })
})
