import type { MissionNode } from '../components/formationsTypes'

/**
 * How a mission's human gates reach the operator (ADR-0019): a notification,
 * or the agents that did the work, in their own terminals. A run keeps the
 * channel its mission had when it started.
 */
export type HumanChannel = 'notify' | 'session'

export const HUMAN_CHANNEL_CHOICES: ReadonlyArray<{ channel: HumanChannel; label: string; detail: string }> = [
  { channel: 'notify', label: 'Notify me', detail: 'Each human gate sends you a notification, and you answer it in the cockpit.' },
  { channel: 'session', label: 'Talk with the agents', detail: 'Each human gate goes to the agents that did the work. Talk it through in their terminals; they record the decision you confirm.' },
]

export function humanChannelOf(mission: Pick<MissionNode, 'humanChannel'>): HumanChannel {
  return mission.humanChannel === 'session' ? 'session' : 'notify'
}

export function humanChannelLabel(channel: HumanChannel): string {
  return channel === 'session' ? 'Talk with the agents' : 'Notify me'
}

/** The mission field a board patch sends: notify is stored as absent, so it clears the field. */
export function humanChannelField(channel: HumanChannel): MissionNode['humanChannel'] {
  return channel === 'session' ? 'session' : ''
}

export const HUMAN_CHANNEL_TIMING = 'A change applies to runs started afterwards; runs already going keep their channel.'
