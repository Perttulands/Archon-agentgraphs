// Reused from CHROTE dashboard/src/utils/clipboard.ts. Keep its HTTP fallback
// and focus/selection restoration: Archon is also used on trusted HTTP hosts.
function canUseAsyncClipboard(): boolean {
  if (typeof navigator === 'undefined' || !navigator.clipboard?.writeText) return false
  if (typeof window === 'undefined' || window.isSecureContext) return true
  return ['localhost', '127.0.0.1', '::1', '[::1]'].includes(window.location.hostname)
}

function fallbackCopyText(text: string): boolean {
  if (typeof document === 'undefined' || !document.body || typeof document.execCommand !== 'function') return false
  const activeElement = document.activeElement instanceof HTMLElement ? document.activeElement : null
  const selection = document.getSelection()
  const ranges: Range[] = []
  if (selection) {
    for (let index = 0; index < selection.rangeCount; index += 1) {
      ranges.push(selection.getRangeAt(index).cloneRange())
    }
  }
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', '')
  textarea.setAttribute('aria-hidden', 'true')
  textarea.setAttribute('data-archon-clipboard-fallback', 'true')
  Object.assign(textarea.style, {
    position: 'fixed', top: '0', left: '0', width: '1px', height: '1px',
    padding: '0', border: '0', opacity: '0', pointerEvents: 'none',
  })
  document.body.appendChild(textarea)
  textarea.focus({ preventScroll: true })
  textarea.select()
  textarea.setSelectionRange(0, textarea.value.length)
  let copied = false
  try {
    copied = document.execCommand('copy')
  } catch {
    // Restore the DOM and leave a false result for visible failure feedback.
  } finally {
    textarea.remove()
    activeElement?.focus({ preventScroll: true })
    if (selection) {
      selection.removeAllRanges()
      ranges.forEach(range => selection.addRange(range))
    }
  }
  return copied
}

export async function copyTextToClipboard(text: string): Promise<boolean> {
  if (canUseAsyncClipboard()) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // HTTP origins and browser permissions may require the click-driven fallback.
    }
  }
  return fallbackCopyText(text)
}

/** Where a copy says how it went: a confirmation, or a failure with its reason. */
export type CopyAnnouncer = (message: string, severity: 'success' | 'error') => void

// Ported from CHROTE dashboard/src/utils/clipboard.ts (355ace49). The boolean
// says only that the text did not land; this says what stood in the way.
function clipboardFailureReason(): string {
  if (canUseAsyncClipboard()) return 'the browser refused'
  if (typeof document === 'undefined' || typeof document.execCommand !== 'function') return 'this browser has no clipboard API'
  return 'the clipboard API is unavailable here and the fallback was refused'
}

/**
 * Copy, wait for the write to settle, and say how it went: "Copied <what>" as a
 * confirmation, or "Could not copy <what>: <reason>" as a failure, so no copy
 * claims success before it has it.
 */
export async function copyAndAnnounce(text: string, what: string, announce: CopyAnnouncer): Promise<boolean> {
  const copied = await copyTextToClipboard(text)
  if (copied) announce(`Copied ${what}`, 'success')
  else announce(`Could not copy ${what}: ${clipboardFailureReason()}`, 'error')
  return copied
}
