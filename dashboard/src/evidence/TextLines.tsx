// Adapted from CHROTE's FilePanelViewer (dashboard/src/components/
// FilePanelViewer.tsx at 48947850): the numbered line view and prettyJson.
// The capped-view note points at the raw artifact instead of a terminal.

import { useRef } from 'react'

/** How much of a long text the viewer draws before it says it stopped. */
export const MAX_VIEWER_LINES = 2000

/** JSON reads as JSON when it parses, and as the recorded bytes when it does not. */
export function prettyJson(content: string): string {
  try {
    return JSON.stringify(JSON.parse(content), null, 2)
  } catch {
    return content
  }
}

export default function TextLines({ content, label }: { content: string; label: string }) {
  const gutterRef = useRef<HTMLPreElement>(null)
  const all = content.split('\n')
  const capped = all.length > MAX_VIEWER_LINES
  const shown = capped ? all.slice(0, MAX_VIEWER_LINES) : all
  const numbers = shown.map((_, index) => String(index + 1)).join('\n')

  return (
    <>
      <div className="evidence-lines">
        <pre className="evidence-lines-gutter" ref={gutterRef} aria-hidden="true">{numbers}</pre>
        <pre
          className="evidence-lines-text"
          aria-label={label}
          onScroll={event => {
            if (gutterRef.current) gutterRef.current.scrollTop = event.currentTarget.scrollTop
          }}
        >{shown.join('\n')}</pre>
      </div>
      {capped && (
        <p className="evidence-note">
          First {MAX_VIEWER_LINES} of {all.length} lines. Open the raw artifact to read the rest.
        </p>
      )}
    </>
  )
}
