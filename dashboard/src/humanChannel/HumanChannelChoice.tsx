import { useId } from 'react'
import { HUMAN_CHANNEL_CHOICES, type HumanChannel } from './humanChannel'
import './humanChannel.css'

/** The two ways a mission's human gates reach the operator, as one radio group. */
export function HumanChannelChoice({ value, disabled = false, describedBy, onChange }: {
  value: HumanChannel
  disabled?: boolean
  describedBy?: string
  onChange: (channel: HumanChannel) => void
}) {
  const name = useId()
  return (
    <div className="channel-choice" role="radiogroup" aria-label="Human gates" aria-describedby={describedBy}>
      {HUMAN_CHANNEL_CHOICES.map(choice => (
        <label key={choice.channel} className={`channel-option${value === choice.channel ? ' chosen' : ''}`}>
          <input type="radio" name={name} value={choice.channel} checked={value === choice.channel} disabled={disabled}
            onChange={() => onChange(choice.channel)} />
          <span className="channel-text">
            <span className="channel-label">{choice.label}</span>
            <span className="channel-detail">{choice.detail}</span>
          </span>
        </label>
      ))}
    </div>
  )
}
