import type { EndOutcome } from './formationsTypes'

/**
 * End nodes end a path on purpose (form-o7p.10): every route leads to a step,
 * a gate or an End node, done or rejected. These are the words the canvas, the
 * Flow view, node windows and the gate answer panel share.
 */

export const END_OUTCOMES: readonly EndOutcome[] = ['done', 'rejected']

/** The title a new End node gets: Done or Rejected. */
export function defaultEndTitle(outcome: EndOutcome): string {
  return outcome === 'rejected' ? 'Rejected' : 'Done'
}

/** How a path that ends at an End node reads: "this path ends (done)". */
export function endPathWords(outcome: string | undefined): string {
  return `this path ends (${outcome || 'done'})`
}

/** A renamed End node keeps its name beside the outcome: " · Shipped". */
export function endTitleSuffix(title: string, outcome: EndOutcome): string {
  return title && title !== defaultEndTitle(outcome) ? ` · ${title}` : ''
}

/** What a run does when a path ends there, for the End node's card and window. */
export function endOutcomeMeaning(outcome: EndOutcome): string {
  return outcome === 'rejected'
    ? 'A path that ends here fails the run with the reason of the gate that sent it, once nothing else can run.'
    : 'A path that ends here is done. The run succeeds once every path has ended, unless one ended rejected.'
}
