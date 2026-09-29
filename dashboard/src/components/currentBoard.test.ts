import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { chooseCurrentBoard, rememberBoardOnDevice, rememberCurrentBoard, rememberedBoard } from './currentBoard'

describe('current board', () => {
  beforeEach(() => {
    window.localStorage.clear()
    window.history.replaceState(null, '', '/')
  })
  afterEach(() => window.history.replaceState(null, '', '/'))

  it('opens the linked board, then the last used board, then the first board', () => {
    const slugs = ['alpha', 'delivery', 'wayfinding']
    expect(chooseCurrentBoard(slugs, '')).toEqual({ slug: 'alpha', missingLinked: '' })
    rememberBoardOnDevice('wayfinding')
    expect(chooseCurrentBoard(slugs, '')).toEqual({ slug: 'wayfinding', missingLinked: '' })
    expect(chooseCurrentBoard(slugs, '?board=delivery')).toEqual({ slug: 'delivery', missingLinked: '' })
    expect(chooseCurrentBoard(slugs, '?board=gone')).toEqual({ slug: 'wayfinding', missingLinked: 'gone' })
    expect(chooseCurrentBoard(['alpha'], '')).toEqual({ slug: 'alpha', missingLinked: '' })
    expect(chooseCurrentBoard([], '')).toEqual({ slug: '', missingLinked: '' })
  })

  it('records the board in the address bar and keeps a run pin only on its own board', () => {
    window.history.replaceState(null, '', '/?board=delivery&run=run_1&x=1#top')
    rememberCurrentBoard('delivery')
    expect(window.location.search).toBe('?board=delivery&run=run_1&x=1')
    expect(window.location.hash).toBe('#top')
    rememberCurrentBoard('wayfinding')
    expect(window.location.search).toBe('?board=wayfinding&x=1')
    expect(rememberedBoard()).toBe('wayfinding')
  })
})
