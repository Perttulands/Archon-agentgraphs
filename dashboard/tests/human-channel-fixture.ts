import { type Page } from '@playwright/test'
import { wayfinding, wayfindingFixture } from './wayfinding-fixture'

/* The Wayfinding board with a writable mission: updateMission patches apply
 * and are recorded, so a spec can follow a human channel change and its undo.
 * Every other write is refused as in the Wayfinding fixture. */

export const missionId: string = wayfinding.board.missions[0].id

type Patch = { updateMission?: { id: string } & Record<string, unknown> }

export async function humanChannelFixture(page: Page) {
  const fixture = await wayfindingFixture(page)
  let board = structuredClone(wayfinding.board)
  const patches: Patch[] = []
  // Routes added later answer first, so this one serves the board and takes its mission patches.
  await page.route('**/api/formations/boards/wayfinding', async route => {
    const request = route.request()
    if (request.method() === 'GET') return route.fulfill({ json: { success: true, data: { board } }, headers: { ETag: board.etag } })
    const patch = request.postDataJSON() as Patch
    if (request.method() !== 'PATCH' || !patch.updateMission) return route.fallback()
    patches.push(patch)
    const { id, ...fields } = patch.updateMission
    board = {
      ...board, rev: board.rev + 1, etag: `wayfinding-${board.rev + 1}`,
      missions: board.missions.map((mission: { id: string }) => mission.id === id ? { ...mission, ...fields } : mission),
    }
    return route.fulfill({ json: { success: true, data: { board } }, headers: { ETag: board.etag } })
  })
  return { ...fixture, patches }
}

/** Saves a screenshot when ARCHON_SCREENSHOT_DIR names a directory, for evidence outside the test run. */
export async function evidenceShot(page: Page, name: string) {
  const dir = process.env.ARCHON_SCREENSHOT_DIR
  if (dir) await page.screenshot({ path: `${dir}/${name}.png` })
}
