import { describe, expect, it } from 'vitest'
import { END_OUTCOMES, defaultEndTitle, endOutcomeMeaning, endPathWords, endTitleSuffix } from './endNode'

describe('End node words', () => {
  it('says how a path ends, with done when no outcome is known', () => {
    expect(endPathWords('done')).toBe('this path ends (done)')
    expect(endPathWords('rejected')).toBe('this path ends (rejected)')
    expect(endPathWords(undefined)).toBe('this path ends (done)')
  })

  it('titles new End nodes after their outcome and names only a renamed one', () => {
    expect(END_OUTCOMES).toEqual(['done', 'rejected'])
    expect(defaultEndTitle('done')).toBe('Done')
    expect(defaultEndTitle('rejected')).toBe('Rejected')
    expect(endTitleSuffix('Done', 'done')).toBe('')
    expect(endTitleSuffix('Rejected', 'rejected')).toBe('')
    expect(endTitleSuffix('', 'done')).toBe('')
    expect(endTitleSuffix('Shipped', 'done')).toBe(' · Shipped')
    expect(endTitleSuffix('Done', 'rejected')).toBe(' · Done')
  })

  it('says what each outcome means for the run', () => {
    expect(endOutcomeMeaning('rejected')).toContain('fails the run with the reason of the gate')
    expect(endOutcomeMeaning('done')).toContain('The run succeeds once every path has ended')
  })
})
