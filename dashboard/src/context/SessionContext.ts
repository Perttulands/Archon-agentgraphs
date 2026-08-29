import type { FormationsTextSize } from '../types'

interface FormationsSessionSettings {
  formationsTextSize?: FormationsTextSize
}

interface FormationsSession {
  settings: FormationsSessionSettings
  sessions: Array<{ name: string }>
  openFloatingModal: (sessionName: string) => void
}

// The extracted cockpit no longer depends on CHROTE's dashboard session
// provider. Keeping this optional adapter preserves the component boundary.
export function useSessionOptional(): FormationsSession | null {
  return null
}
