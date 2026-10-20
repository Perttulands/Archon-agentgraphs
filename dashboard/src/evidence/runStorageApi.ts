import { fetchApi } from '../components/formationsApi'

export interface RunStorageReport {
  runId: string
  policy: 'bounded' | 'legacy'
  final: boolean
  trimmed: boolean
  archived: boolean
  state: 'retained' | 'partial' | 'expired'
  bytes: number
  allocatedBytes: number
  retainedBytes: number
  budgetBytes: number
  poolBudgetBytes: number
  protectedBy: string[]
  activeSeats: number[]
  lastError?: string
}
export type StorageAction = 'archive' | 'unarchive' | 'dispose'
export async function fetchRunStorage(runId: string, action?: StorageAction): Promise<RunStorageReport> {
  const { data } = await fetchApi<{ storage: RunStorageReport }>(`/api/runs/${encodeURIComponent(runId)}/storage`, action ? { method: 'POST', body: JSON.stringify({ action }) } : undefined)
  return data.storage
}
