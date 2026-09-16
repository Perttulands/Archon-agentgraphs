import { Suspense, createContext, lazy, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import type { WindowStack } from '../windows/WindowManager'
import type { WindowRect } from '../windows/windowGeometry'
import type { FileRequest } from './fileWindowModel'

// The cockpit's open file windows. Anything under the provider opens a file
// with useFileWindows()?.open(request, fileAnchor(event.currentTarget)); the
// layer draws the windows inside the view's WindowManagerProvider, so they
// share its stack and workspace, and each opens beside where it was opened.

const FileWindow = lazy(() => import('./FileWindow'))

export interface FileWindows {
  /**
   * Open a file in its own window, or bring its open window forward. A new
   * window opens beside `anchor`, in viewport pixels, or centred without one.
   */
  open: (request: FileRequest, anchor?: WindowRect | null) => void
}

interface OpenFile {
  request: FileRequest
  anchor: WindowRect | null
}

interface FileWindowsState extends FileWindows {
  files: readonly OpenFile[]
  close: (id: string) => void
}

/**
 * What a file window opens beside: the card or floating window holding the
 * control that opened it, or an element marked data-file-anchor, else the
 * control itself.
 */
export function fileAnchor(control: Element): WindowRect {
  const { left, top, width, height } = (control.closest('[data-node], .fwin, [data-file-anchor]') || control).getBoundingClientRect()
  return { left, top, width, height }
}

const FileWindowsContext = createContext<FileWindowsState | null>(null)

export function FileWindowsProvider({ stack, children }: { stack: WindowStack; children: ReactNode }) {
  const [files, setFiles] = useState<readonly OpenFile[]>([])
  // The stack changes whenever a window is raised; opening stays the same function.
  const stackRef = useRef(stack)
  stackRef.current = stack
  const open = useCallback((request: FileRequest, anchor?: WindowRect | null) => {
    setFiles(current => current.some(file => file.request.id === request.id) ? current : [...current, { request, anchor: anchor || null }])
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
      {state.files.map(({ request, anchor }) => (
        <Suspense key={request.id} fallback={null}>
          <FileWindow request={request} anchor={anchor} onOpen={state.open} onClose={() => state.close(request.id)} />
        </Suspense>
      ))}
    </>
  )
}
