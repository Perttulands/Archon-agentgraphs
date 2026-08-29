export type FormationsTextSize = 'default' | 'large' | 'xlarge'

export function resolveFormationsTextSize(value: unknown): FormationsTextSize {
  return value === 'large' || value === 'xlarge' ? value : 'default'
}
