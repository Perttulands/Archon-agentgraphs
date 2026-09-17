import { fetchApi } from '../components/formationsApi'

export interface RunSeat {
  runId: string
  nodeId: string
  nodeTitle: string
  slotId: string
  slotLabel: string
  harness: string
  controller: boolean
  createdSeq: number
  sessionName: string
  state: 'live' | 'ended' | 'missing' | 'unavailable'
  /** Kept after its formation finished to answer human gate asks (ADR-0019); waitingOn lists the pending asks it received. */
  onCall?: { keptSeq: number; waitingOn: Array<{ gateId: string; requestedSeq: number }> }
  reason?: string
  columns?: number
  rows?: number
  terminalUrl?: string
}

export interface RunSeats {
  runId: string
  available: boolean
  reason?: string
  seats: RunSeat[]
}

/** A kept seat holding an ask the operator has not answered yet. */
export function seatWaitsForYou(seat: RunSeat | undefined): boolean {
  return Boolean(seat?.onCall?.waitingOn?.length)
}

export async function fetchRunSeats(runId: string): Promise<RunSeats> {
  return (await fetchApi<RunSeats>(`/api/formations/runs/${encodeURIComponent(runId)}/seats`)).data
}

/** A projection may only select a native grid and the exact run-owned URL. */
export function seatSocketUrl(seat: RunSeat): string | null {
  const path = `/api/formations/runs/${encodeURIComponent(seat.runId)}/seats/${seat.createdSeq}/terminal`
  if (seat.state !== 'live' || seat.terminalUrl !== path || !Number.isSafeInteger(seat.createdSeq) || seat.createdSeq < 1
    || !Number.isInteger(seat.columns) || !Number.isInteger(seat.rows) || seat.columns! < 1 || seat.rows! < 1) return null
  return `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}${path}`
}
