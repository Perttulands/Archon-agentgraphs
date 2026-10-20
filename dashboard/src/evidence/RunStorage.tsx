import { useEffect, useRef, useState } from 'react'
import { formatBytes } from './runEvidenceApi'
import { fetchRunStorage, type RunStorageReport, type StorageAction } from './runStorageApi'

/** Storage belongs to this selected run, including late reads and commands. */
export default function RunStorage({ runId, final, state, onChange }: { runId: string; final: boolean; state: string; onChange: () => void }) {
  const owner = useRef({ runId, generation: 0 })
  if (owner.current.runId !== runId) owner.current = { runId, generation: owner.current.generation + 1 }
  const [report, setReport] = useState<RunStorageReport | null>(null)
  const [error, setError] = useState<{ runId: string; text: string } | null>(null)
  const [busy, setBusy] = useState('')
  const [review, setReview] = useState('')
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    const token = owner.current
    let current = true
    setReport(null); setError(null); setBusy(''); setReview('')
    fetchRunStorage(runId).then(next => { if (current && owner.current === token) setReport(next) }, reason => {
      if (current && owner.current === token) setError({ runId, text: reason instanceof Error ? reason.message : String(reason) })
    })
    return () => {
      current = false
      if (owner.current === token) owner.current = { runId, generation: token.generation + 1 }
    }
  }, [runId, state, retry])
  const storage = report?.runId === runId ? report : null
  const act = async (action: StorageAction) => {
    if (!storage || busy === runId) return
    const token = owner.current
    setBusy(runId); setError(null)
    try {
      const next = await fetchRunStorage(runId, action)
      if (owner.current !== token) return
      setReport(next); setReview(''); onChange()
    } catch (reason) {
      if (owner.current === token) setError({ runId, text: reason instanceof Error ? reason.message : String(reason) })
    } finally { if (owner.current === token) setBusy('') }
  }
  const protectedBy = storage?.protectedBy || []
  const pending = busy === runId
  const activeSeats = storage?.activeSeats || []
  const runFinal = final && Boolean(storage?.final)
  const canDispose = runFinal && !protectedBy.length && !activeSeats.length && storage?.state !== 'expired'
  return <section className="node-evidence-section run-storage" aria-label="Run storage">
    <h3>Run storage</h3>
    <p className="evidence-meta">{runId}</p>
    {error?.runId === runId ? <p className="note-error" role="alert">Storage unavailable: {error.text} <button className="evidence-link" onClick={() => setRetry(n => n + 1)}>Retry storage</button></p> : null}
    {!storage && error?.runId !== runId ? <p role="status">Loading storage…</p> : null}
    {storage ? <>
      <p>{storage.archived ? 'Archived · excluded from the ordinary budget' : storage.policy === 'legacy' ? 'Legacy · inventory only; no automatic cleanup' : 'Ordinary evidence'} · {storage.state === 'expired' ? 'Evidence expired' : storage.state === 'partial' ? 'Cleanup incomplete' : storage.trimmed ? 'Evidence retained · working files cleaned' : 'Evidence retained'}</p>
      <dl className="storage-accounting">
        <div><dt>Run files</dt><dd>{formatBytes(storage.bytes)}</dd></div>
        <div><dt>Retained evidence</dt><dd>{formatBytes(storage.retainedBytes)}</dd></div>
        <div><dt>Disk allocation</dt><dd>{formatBytes(storage.allocatedBytes)}</dd></div>
        <div><dt>Ordinary limit</dt><dd>{formatBytes(storage.budgetBytes)} / run · {formatBytes(storage.poolBudgetBytes)} completed pool</dd></div>
      </dl>
      <p className="evidence-note">Compact run history, frozen mission and briefs stay inspectable when evidence expires. Expired files cannot be opened.</p>
      {storage.lastError ? <p role="alert" className="note-error">Storage cleanup incomplete: {storage.lastError}</p> : null}
      {protectedBy.length ? <p>Files still in use by runs: {protectedBy.join(', ')}.</p> : null}
      {activeSeats.length ? <p>Agent seats still present or their absence is unconfirmed: {activeSeats.map(seq => `#${seq}`).join(', ')}. Files cannot be disposed.</p> : null}
      {!runFinal ? <p className="evidence-note">This run is unfinished. Its files cannot be disposed.</p> : null}
      <div className="storage-actions">
        <button type="button" className="btn" disabled={pending || (!storage.archived && storage.state !== 'retained')} onClick={() => void act(storage.archived ? 'unarchive' : 'archive')}>{pending ? 'Updating storage…' : storage.archived ? 'Return to ordinary policy' : 'Archive evidence'}</button>
        {canDispose ? <button type="button" className="btn" disabled={pending} onClick={() => setReview(runId)}>Review disposal</button> : null}
      </div>
      {review === runId && canDispose ? <div className="storage-review">
        <p>Dispose {formatBytes(storage.bytes)} of files from {runId}? Retained outputs and working files become unavailable. Compact run history, frozen mission and briefs remain. This cannot be undone.</p>
        <button type="button" className="btn" disabled={pending} onClick={() => void act('dispose')}>Dispose run files</button> <button type="button" className="btn" disabled={pending} onClick={() => setReview('')}>Keep files</button>
      </div> : null}
      {!storage.archived && storage.state !== 'retained' ? <p className="evidence-note">Archiving is unavailable while cleanup is incomplete or evidence has expired.</p> : null}
      {storage.trimmed ? <p className="evidence-note">Archiving preserves only files still retained. Cleaned working files cannot be restored.</p> : null}
      <p className="evidence-note">Archiving keeps large evidence explicitly. Returning to ordinary policy permits budget eviction for bounded runs; legacy runs stay inventory only. To keep an oversized output, archive this run, then Resume.</p>
    </> : null}
  </section>
}
