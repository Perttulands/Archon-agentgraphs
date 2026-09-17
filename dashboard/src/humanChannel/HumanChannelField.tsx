import { useId, useState } from 'react'
import { HUMAN_CHANNEL_TIMING, type HumanChannel } from './humanChannel'
import { HumanChannelChoice } from './HumanChannelChoice'

/** A mission window's human channel: choosing saves at once, with undo like any field. */
export function HumanChannelField({ channel, onSave }: { channel: HumanChannel; onSave: (channel: HumanChannel) => Promise<boolean> }) {
  const [saving, setSaving] = useState<HumanChannel | null>(null)
  const [error, setError] = useState('')
  const note = useId()
  const choose = async (next: HumanChannel) => {
    if (next === channel || saving) return
    setSaving(next)
    setError('')
    const saved = await onSave(next)
    setSaving(null)
    if (!saved) setError('The human channel was not saved.')
  }
  return (
    <div className="nfield channel-field">
      <div className="nfield-head"><span className="nfield-label">Human gates</span></div>
      {/* Choices stay enabled while saving, so keyboard focus stays in the window; a choice made meanwhile is ignored. */}
      <HumanChannelChoice value={saving ?? channel} describedBy={note} onChange={next => void choose(next)} />
      {error
        ? <p id={note} className="nfield-note error" role="alert">{error}</p>
        : <p id={note} className="nfield-note">{saving ? 'Saving…' : HUMAN_CHANNEL_TIMING}</p>}
    </div>
  )
}
