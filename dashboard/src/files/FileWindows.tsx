import { Suspense, createContext, lazy, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import type { WindowStack } from '../windows/WindowManager'
import type { FileRequest } from './fileWindowModel'

// The cockpit's open file windows. Anything under the provider opens a file
// with useFileWindows()?.open(request); the layer draws the windows inside the
// view's WindowManagerProvider, so they share its stack and workspace.

const FileWindow = lazy(() => import('./FileWindow'))

export interface FileWindows {
  /** Open a file in its own window, or bring its open window forward. */
  open: (request: FileRequest) => void
}

interface FileWindowsState extends FileWindows {
  files: readonly FileRequest[]
  close: (id: string) => void
}

const FileWindowsContext = createContext<FileWindowsState | null>(null)

export function FileWindowsProvider({ stack, children }: { stack: WindowStack; children: ReactNode }) {
  const [files, setFiles] = useState<readonly FileRequest[]>([])
  // The stack changes whenever a window is raised; opening stays the same function.
  const stackRef = useRef(stack)
  stackRef.current = stack
  const open = useCallback((request: FileRequest) => {
    setFiles(current => current.some(file => file.id === request.id) ? current : [...current, request])
    stackRef.current.focus(request.id)
  }, [])
  const close = useCallback((id: string) => setFiles(current => current.filter(file => file.id !== id)), [])
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
      {state.files.map(file => (
        <Suspense key={file.id} fallback={null}>
          <FileWindow request={file} onOpen={state.open} onClose={() => state.close(file.id)} />
        </Suspense>
      ))}
    </>
  )
}
