import { Suspense, useEffect, createContext, lazy, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import type { WindowStack } from '../windows/WindowManager'
import type { WindowRect } from '../windows/windowGeometry'
import { measureElement, nodeBoxes } from '../windows/cockpitScene'
import { folderFileRequest, referencedFileRequest, type FileRequest } from './fileWindowModel'

// The cockpit's open file windows. Anything under the provider opens a file
// with useFileWindows()?.open(request, fileAnchor(event.currentTarget)); the
// layer draws the windows inside the view's WindowManagerProvider, so they
// share its stack and workspace, and each opens near where it was opened.

const FileWindow = lazy(() => import('./FileWindow'))

export interface FileWindows {
  /**
   * Open a file in its own window, or refresh and raise its open window. A new
   * window opens near `place.anchor`, in viewport pixels, leaving
   * `place.keepClear` visible, or near the centre without a place.
   */
  open: (request: FileRequest, place?: FilePlace | null) => void
}

/** Where a file window opens: near its anchor, leaving what the anchor belongs to in view. */
export interface FilePlace {
  anchor: WindowRect
  keepClear: readonly WindowRect[]
  /** A chip in the run bar is a control: its file opens right below it. */
  anchorKind: 'node' | 'control'
  /**
   * The step the window was opened from: its card, Flow row or window, or a
   * window opened from those. It closes when that step's window is replaced.
   */
  owner?: string
}

/** A request to close windows: all of them, or those owned by the given steps. */
export interface WindowClosing {
  seq: number
  owners?: readonly string[]
}

/** Whether a closing request covers a window owned by `owner`. */
export function closes(closing: WindowClosing, owner: string | undefined): boolean {
  return !closing.owners || (owner !== undefined && closing.owners.includes(owner))
}

interface OpenFile {
  request: FileRequest
  place: FilePlace | null
}

interface FileWindowsState extends FileWindows {
  files: readonly OpenFile[]
  close: (id: string) => void
}

/**
 * Where a file window opened from `control` opens. Beside the card or the
 * floating window holding the control, or an element marked
 * data-file-anchor. A chip in a Flow row anchors on itself, since the row is
 * as wide as the column. Either way the window keeps the node the control
 * belongs to, its card or its Flow row's title and links, in view.
 */
export function fileAnchor(control: Element): FilePlace {
  const holder = control.closest('[data-node], [data-flow-node], .fwin, [data-file-anchor]')
  const nodeId = holder?.getAttribute('data-node') || holder?.getAttribute('data-flow-node') || ''
  const anchorElement = !holder || holder.hasAttribute('data-flow-node') ? control : holder
  const anchor = measureElement(anchorElement, true)!
  const anchorKind = holder?.matches('.fwin, [data-file-anchor]') ? 'control' : 'node'
  const windowId = holder?.closest('.fwin')?.getAttribute('data-window-id') || ''
  const owner = nodeId || windowOwner(windowId)
  return { anchor, keepClear: nodeId ? nodeBoxes([nodeId]) : [], anchorKind, ...(owner ? { owner } : {}) }
}

// A step's own windows name it; a file window's owner is looked up when it opens.
function windowOwner(windowId: string): string {
  const step = /^(node|attempts|evidence):(.+)$/.exec(windowId)
  if (step) return step[2]
  return windowId ? `window:${windowId}` : ''
}

const FileWindowsContext = createContext<FileWindowsState | null>(null)

export function FileWindowsProvider({ stack, closing = null, children }: { stack: WindowStack; closing?: WindowClosing | null; children: ReactNode }) {
  const [files, setFiles] = useState<readonly OpenFile[]>([])
  // The stack changes whenever a window is raised; opening stays the same function.
  const stackRef = useRef(stack)
  stackRef.current = stack
  const open = useCallback((request: FileRequest, place?: FilePlace | null) => {
    // Opening is also a read request, even when the caller reuses the same
    // object or a later attempt overwrites the same artifact path. Keep the
    // window identity and its original place so its geometry stays put.
    const fresh = { ...request }
    setFiles(current => {
      if (current.some(file => file.request.id === request.id)) return current.map(file => file.request.id === request.id ? { ...file, request: fresh } : file)
      // A file opened from another file window belongs to that window's step.
      const from = place?.owner?.startsWith('window:') ? current.find(file => file.request.id === place.owner!.slice('window:'.length)) : undefined
      const owned = place && from ? { ...place, owner: from.place?.owner } : place
      return [...current, { request: fresh, place: owned || null }]
    })
    stackRef.current.focus(request.id)
  }, [])
  useEffect(() => {
    const receive = (event: Event) => {
      const { path, anchor } = (event as CustomEvent<{ path: string; anchor?: Element }>).detail
      open(path.endsWith('/') ? folderFileRequest(path.replace(/\/+$/, '') || '/') : referencedFileRequest(path), anchor ? fileAnchor(anchor) : null)
    }
    document.addEventListener('archon-open-path', receive)
    return () => document.removeEventListener('archon-open-path', receive)
  }, [open])
  const close = useCallback((id: string) => setFiles(current => current.filter(file => file.request.id !== id)), [])
  useEffect(() => {
    if (closing) setFiles(current => current.filter(file => !closes(closing, file.place?.owner)))
  }, [closing])
  const value = useMemo(() => ({ files, open, close }), [close, files, open])
  return <FileWindowsContext.Provider value={value}>{children}</FileWindowsContext.Provider>
}

/** Null outside a view with file windows, where a file can be shown in place instead. */
export function useFileWindows(): FileWindows | null {
  return useContext(FileWindowsContext)
}

export function FileWindowsLayer() {
  const state = useContext(FileWindowsContext)
  if (!state) return null
  return (
    <>
      {state.files.map(({ request, place }) => (
        <Suspense key={request.id} fallback={null}>
          <FileWindow request={request} place={place} onOpen={state.open} onClose={() => state.close(request.id)} />
        </Suspense>
      ))}
    </>
  )
}
