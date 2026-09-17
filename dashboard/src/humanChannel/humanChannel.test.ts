import { describe, expect, it } from 'vitest'
import { humanChannelField, humanChannelLabel, humanChannelOf } from './humanChannel'

describe('mission human channel', () => {
  it('reads absent, empty and notify as notify, and patches notify as a cleared field', () => {
    expect([{}, { humanChannel: '' as const }, { humanChannel: 'notify' as const }, { humanChannel: 'session' as const }].map(humanChannelOf))
      .toEqual(['notify', 'notify', 'notify', 'session'])
    expect(humanChannelField('notify')).toBe('')
    expect(humanChannelField('session')).toBe('session')
    expect(humanChannelLabel('session')).toBe('Talk with the agents')
    expect(humanChannelLabel('notify')).toBe('Notify me')
  })
})
