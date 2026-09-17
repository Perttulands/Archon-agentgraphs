import { EditableField } from './EditableField'

function durationProblem(value: string): string {
  if (!value || (/^\d+$/.test(value) && Number.isSafeInteger(Number(value)) && Number(value) > 0)) return ''
  return 'Enter a positive whole number of seconds, or leave blank to inherit the run default.'
}

export function FormationDurationField({ timeoutSeconds, onSave }: {
  timeoutSeconds: number | undefined
  onSave: (timeoutSeconds: number) => Promise<boolean>
}) {
  return (
    <EditableField label="Execution duration (seconds)" value={timeoutSeconds ? String(timeoutSeconds) : ''}
      placeholder="Inherit the run default"
      hint="Total time for this formation, including preparation and finalization. Leave blank to inherit the run default. Changes apply to new runs."
      validate={durationProblem} onSave={value => onSave(value ? Number(value) : 0)}>
      {timeoutSeconds ? `${timeoutSeconds} seconds` : undefined}
    </EditableField>
  )
}
