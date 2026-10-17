import { fileAnchor, type FilePlace } from '../files/FileWindows'
import { measureElement } from '../windows/cockpitScene'

/** Attempts belong to the whole card or Flow status that opened them. */
export function attemptsAnchor(control: Element): FilePlace {
  const place = fileAnchor(control)
  const status = control.closest('.flow-status')
  if (!status) return place
  const box = measureElement(status, true)!
  return { anchor: box, keepClear: [...place.keepClear, box], anchorKind: 'node' }
}
