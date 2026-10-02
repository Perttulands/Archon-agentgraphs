// Adapted from CHROTE FilesView/fileService.ts and FilesViewContent.tsx:
// path navigation, parent directory and directory-first file choices. Archon
// needs a picker rather than CHROTE's editing, uploads, pins and workbench.
import { useEffect, useState } from 'react'
import { fetchApi } from '../components/formationsApi'
type Item = { name: string; path: string; isDir: boolean }
const parent = (path: string) => path.replace(/\/+$/, '').split('/').slice(0,-1).join('/') || '/'
export default function PathPicker({ kind, value, onPick, onClose }: { kind: 'file' | 'folder'; value: string; onPick: (path: string) => void; onClose: () => void }) {
 const [path, setPath] = useState(value.startsWith('/') ? kind === 'folder' ? value : parent(value) : '/')
 const [draft, setDraft] = useState(path)
 const [items, setItems] = useState<Item[]>([])
 const [error, setError] = useState('')
 const [loading, setLoading] = useState(false)
 useEffect(() => {
  let current = true;setLoading(true);setError('');setDraft(path)
  fetchApi<{items: Item[]}>(`/api/files/directory?path=${encodeURIComponent(path)}`).then(result => { if (current) setItems(result.data.items) }).catch(reason => { if (current) {setItems([]);setError(reason instanceof Error ? reason.message : String(reason))} }).finally(() => {if(current)setLoading(false)})
  return () => {current=false}
 },[path])
 return <div className="path-picker" role="group" aria-label={`Browse for a ${kind}`}>
  <div className="path-picker-nav"><button type="button" onClick={() => setPath(parent(path))}>Up</button><input aria-label="Directory to browse" value={draft} onChange={e=>setDraft(e.target.value)} onKeyDown={e=>{if(e.key==='Enter'){e.preventDefault();setPath(draft)} if(e.key==='Escape'){e.preventDefault();e.stopPropagation();onClose()}}}/><button type="button" onClick={()=>setPath(draft)}>Go</button><button type="button" onClick={onClose}>Close browser</button></div>
  <p>{path}</p>
  {loading ? <p role="status">Reading directory…</p> : error ? <p role="alert">{error}</p> : <ul aria-label="Directory entries">{items.filter(item=>kind==='file'||item.isDir).map(item=><li key={item.path}><button type="button" onClick={()=>item.isDir?setPath(item.path):onPick(item.path)}>{item.isDir?'DIR':'FILE'} · {item.name}</button></li>)}</ul>}
  {kind==='folder' && !loading && !error ? <button type="button" onClick={()=>onPick(path)}>Use this folder</button> : null}
 </div>
}
