import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { chooseCurrentBoard, openableSlugs, rememberBoardOnDevice, rememberCurrentBoard, rememberedBoard } from './currentBoard'

describe('current board', () => {
  beforeEach(() => {
    window.localStorage.clear()
    window.history.replaceState(null, '', '/')
  })
  afterEach(() => window.history.replaceState(null, '', '/'))

  it('opens the linked board, then the last used board, then the first board', () => {
    const slugs = ['alpha', 'delivery', 'scouting']
    expect(chooseCurrentBoard(slugs, '')).toEqual({ slug: 'alpha', missingLinked: '' })
    rememberBoardOnDevice('scouting')
    expect(chooseCurrentBoard(slugs, '')).toEqual({ slug: 'scouting', missingLinked: '' })
    expect(chooseCurrentBoard(slugs, '?mission=delivery')).toEqual({ slug: 'delivery', missingLinked: '' })
    expect(chooseCurrentBoard(slugs, '?mission=gone')).toEqual({ slug: 'scouting', missingLinked: 'gone' })
    expect(chooseCurrentBoard(['alpha'], '')).toEqual({ slug: 'alpha', missingLinked: '' })
    expect(chooseCurrentBoard([], '')).toEqual({ slug: '', missingLinked: '' })
  })

  it('never opens a mission that cannot be read, even a remembered or linked one', () => {
    const summaries = [{ slug: 'alpha' }, { slug: 'moved', broken: 'its symlink target is gone' }]
    expect(openableSlugs(summaries)).toEqual(['alpha'])
    rememberBoardOnDevice('moved')
    expect(chooseCurrentBoard(openableSlugs(summaries), '?mission=moved')).toEqual({ slug: 'alpha', missingLinked: 'moved' })
  })

  it('records the board in the address bar and keeps a run pin only on its own board', () => {
    window.history.replaceState(null, '', '/?mission=delivery&run=run_1&x=1#top')
    rememberCurrentBoard('delivery')
    expect(window.location.search).toBe('?mission=delivery&run=run_1&x=1')
    expect(window.location.hash).toBe('#top')
    rememberCurrentBoard('scouting')
    expect(window.location.search).toBe('?mission=scouting&x=1')
    expect(rememberedBoard()).toBe('scouting')
  })

  it('reads only ?mission=, so a ?board= link names no mission', () => {
    window.history.replaceState(null, '', '/?board=delivery&run=run_1')
    expect(chooseCurrentBoard(['alpha', 'delivery'], window.location.search)).toEqual({ slug: 'alpha', missingLinked: '' })
  })
})
