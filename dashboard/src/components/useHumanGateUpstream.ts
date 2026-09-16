import { useEffect, useState } from 'react'
import { fetchHumanGateRequest } from './formationsApi'
import type { GateUpstream } from './HumanGateAnswerPanel'

const LOADING: GateUpstream = { state: 'loading' }

/** Loads what a pending human gate received, once per exact request. */
export function useHumanGateUpstream(runId: string, gate: { gateId: string; requestedSeq: number } | null): GateUpstream {
  const gateId = gate?.gateId || ''
  const requestedSeq = gate?.requestedSeq || 0
  const requestKey = runId && gateId && requestedSeq ? `${runId}:${gateId}:${requestedSeq}` : ''
  const [loaded, setLoaded] = useState<{ key: string; upstream: GateUpstream }>({ key: '', upstream: LOADING })

  useEffect(() => {
    if (!requestKey) return
    let cancelled = false
    fetchHumanGateRequest(runId, gateId)
      .then(request => {
        if (cancelled) return
        const upstream: GateUpstream = request.requestedSeq === requestedSeq
          ? { state: 'ready', from: request.input.fromNodeId || 'upstream step', text: request.input.text, truncated: request.input.truncated, criterion: request.criterion }
          : { state: 'unavailable', message: 'This gate request changed; refresh the run.' }
        setLoaded({ key: requestKey, upstream })
      })
      .catch(err => {
        if (cancelled) return
        setLoaded({ key: requestKey, upstream: { state: 'unavailable', message: err instanceof Error ? `The gate input could not be read: ${err.message}` : 'The gate input could not be read.' } })
      })
    return () => { cancelled = true }
  }, [gateId, requestKey, requestedSeq, runId])

  return loaded.key === requestKey ? loaded.upstream : LOADING
}
