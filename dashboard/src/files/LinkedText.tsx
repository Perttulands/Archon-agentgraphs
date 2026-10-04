import { Fragment, useState } from 'react'
import { findPaths } from '../terminal/pathLinks'
import { copyBeadId, findBeadIds, openPath } from './textLinks'
export function BeadChip({ id }: { id: string }) {
  const [state, setState] = useState('')
  return <button type="button" className="text-token" title="Bead ID, copied on click" onClick={() => void copyBeadId(id).then(ok => setState(ok ? 'Copied' : 'Copy failed'))}>{id}{state && <span role="status"> · {state}</span>}</button>
}
export default function LinkedText({ text }: { text: string }) {
  const matches = [...findPaths(text).map(p => ({ text: p.path, index: p.index, path: true })), ...findBeadIds(text).map(p => ({ text: p.id, index: p.index, path: false }))].sort((a,b) => a.index - b.index)
  let end = 0
  return <>{matches.map(match => {
    if (match.index < end) return null
    const before = text.slice(end, match.index); end = match.index + match.text.length
    return <Fragment key={match.index}>{before}{match.path ? <button type="button" className="text-token" onClick={event => openPath(match.text, event.currentTarget)}>{match.text}</button> : <BeadChip id={match.text} />}</Fragment>
  })}{text.slice(end)}</>
}
