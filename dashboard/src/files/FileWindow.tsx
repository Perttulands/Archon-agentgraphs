import { useState } from 'react'
import FloatingWindow from '../windows/FloatingWindow'
import FileView, { FileActions, useFilePreview } from './FileView'
import type { FileRequest } from './fileWindowModel'

/** One file in a floating window of the 'file' kind. */
export default function FileWindow({ request, onOpen, onClose }: {
  request: FileRequest
  onOpen: (request: FileRequest) => void
  onClose: () => void
}) {
  const { preview, error } = useFilePreview(request)
  const [mode, setMode] = useState<'preview' | 'source'>('preview')
  return (
    <FloatingWindow
      id={request.id}
      kind="file"
      label={`file ${request.name}`}
      title={(
        <span className="file-window-title">
          <span className="file-window-name">{request.name}</span>
          {request.context ? <span className="file-window-context">{request.context}</span> : null}
        </span>
      )}
      defaultSize={{ width: 720, height: 560 }}
      className="file-window"
      actions={<FileActions request={request} preview={preview} mode={mode} onMode={setMode} />}
      onClose={onClose}
    >
      <FileView request={request} preview={preview} error={error} mode={mode} onOpen={onOpen} />
    </FloatingWindow>
  )
}
