import { Suspense, createContext, lazy, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import type { WindowStack } from '../windows/WindowManager'
import type { WindowRect } from '../windows/windowGeometry'
import { measureElement, nodeBoxes } from '../windows/cockpitScene'
import type { FileRequest } from './fileWindowModel'

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
  return { anchor, keepClear: nodeId ? nodeBoxes([nodeId]) : [] }
}

const FileWindowsContext = createContext<FileWindowsState | null>(null)

export function FileWindowsProvider({ stack, children }: { stack: WindowStack; children: ReactNode }) {
  const [files, setFiles] = useState<readonly OpenFile[]>([])
  // The stack changes whenever a window is raised; opening stays the same function.
  const stackRef = useRef(stack)
  stackRef.current = stack
  const open = useCallback((request: FileRequest, place?: FilePlace | null) => {
    // Opening is also a read request, even when the caller reuses the same
    // object or a later attempt overwrites the same artifact path. Keep the
    // window identity and its original place so its geometry stays put.
    const fresh = { ...request }
    setFiles(current => current.some(file => file.request.id === request.id)
      ? current.map(file => file.request.id === request.id ? { ...file, request: fresh } : file)
      : [...current, { request: fresh, place: place || null }])
    stackRef.current.focus(request.id)
  }, [])
  const close = useCallback((id: string) => setFiles(current => current.filter(file => file.request.id !== id)), [])
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
