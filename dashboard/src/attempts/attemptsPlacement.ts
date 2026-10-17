import { fileAnchor, type FilePlace } from '../files/FileWindows'
import { measureElement, nodeBoxes } from '../windows/cockpitScene'

/** Attempts belong to the whole card or Flow status that opened them. */
export function attemptsAnchor(control: Element, gateIds: readonly string[] = []): FilePlace {
  const place = fileAnchor(control)
  const gates = gateIds.flatMap(id => {
    const row = document.querySelector(`.flow-step[data-flow-node="${CSS.escape(id)}"]`)
    if (!row) return nodeBoxes([id])
    return [...row.querySelectorAll('.flow-status, .flow-verdict')]
      .map(element => measureElement(element)).filter(box => box !== null)
  })
  place.keepClear = [...place.keepClear, ...gates]
  const status = control.closest('.flow-status')
  if (!status) return place
  const box = measureElement(status, true)!
  return { anchor: box, keepClear: [...place.keepClear, box], anchorKind: 'node' }
}
