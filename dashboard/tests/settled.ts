import { expect, type Locator, type Page } from '@playwright/test'

// Measurements wait for what they measure to stop moving (archon-miz): cards
// and windows move by transitions and by state that settles a few frames after
// a pointer lets go, and a loaded host stretches those frames. A box is settled
// once two reads a frame apart agree; the ceiling only bounds a failure.
export async function settledBox(page: Page, locator: Locator) {
  let box: Awaited<ReturnType<Locator['boundingBox']>> = null
  await expect.poll(async () => {
    box = await locator.boundingBox()
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    return box !== null && JSON.stringify(box) === JSON.stringify(await locator.boundingBox())
  }, { timeout: 30_000 }).toBe(true)
  return box!
}

/** Waits until no CSS transition is running, as after Fit or Arrange moves the
 *  canvas. Looping animations, such as a flowing wire, are not waited on. */
export async function transitionsSettled(page: Page) {
  // Two frames let what was just clicked start its transitions.
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
  await page.waitForFunction(() => document.getAnimations().every(animation => !(animation instanceof CSSTransition) || animation.playState !== 'running'), undefined, { timeout: 30_000 })
}
