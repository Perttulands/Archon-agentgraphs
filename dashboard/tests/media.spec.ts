import { expect, test } from '@playwright/test'
import { mediaAttemptFixture, runArtifacts } from './cockpit-fixture'
import { iterationsFixture } from './iterations-fixture'

test('an attempt shows its movie and judge video inline, and opens a lazy 3D model with its sidecar', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  await iterationsFixture(page, 'single')
  await page.route('**/api/runs/run_browser/evidence/attempts?**', route => route.fulfill({ json: { success: true, data: { attempts: {
    runId: 'run_browser', steps: [{ nodeId: 'execution', title: 'Draw Ahti', outputs: [], attempts: [mediaAttemptFixture] }],
  } } } }))
  await page.route('**/api/runs/run_browser/evidence/artifacts/execution/1/*', route => {
    const name = new URL(route.request().url()).pathname.split('/').pop()!
    return route.fulfill({ json: { success: true, data: { artifact: { name: `execution/1/${name}`, kind: name.endsWith('.mp4') ? 'video' : 'model', size: 300 } } } })
  })
  const loaded: string[] = []
  await page.route('**/api/runs/run_browser/artifacts/execution/1/*', route => {
    const name = new URL(route.request().url()).pathname.split('/').pop()!
    loaded.push(name)
    if (name === 'triangle.gltf') return route.fulfill({ contentType: 'model/gltf+json', json: {
      asset: { version: '2.0' }, scene: 0, scenes: [{ nodes: [0] }], nodes: [{ mesh: 0 }],
      meshes: [{ primitives: [{ attributes: { POSITION: 0 } }] }],
      buffers: [{ uri: 'triangle.bin', byteLength: 36 }], bufferViews: [{ buffer: 0, byteLength: 36 }],
      accessors: [{ bufferView: 0, componentType: 5126, count: 3, type: 'VEC3', min: [0, 0, 0], max: [1, 1, 0] }],
    } })
    if (name === 'triangle.bin') return route.fulfill({ contentType: 'application/octet-stream', body: Buffer.from(new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]).buffer) })
    return route.fulfill({ contentType: 'video/mp4', body: '' })
  })
  await page.route('**/api/files/raw?**', route => route.fulfill({ contentType: 'video/webm', body: '' }))
  await page.goto('/?mission=browser&run=run_browser')
  await page.getByTestId('formation-node-execution').locator('.tt').click()
  await page.getByTestId('attempts-open-execution').click()
  const attempts = page.getByTestId('attempts-window')
  const movie = attempts.getByLabel('turntable.mp4', { exact: true })
  await expect(movie).toHaveAttribute('controls', '')
  await expect(movie).toHaveAttribute('preload', 'metadata')
  await expect(movie).not.toHaveAttribute('autoplay')
  const cited = attempts.getByLabel('review.webm', { exact: true })
  await expect(cited).toHaveAttribute('src', `/api/files/raw?path=${encodeURIComponent(`${runArtifacts}/execution/1/judge videos/review.webm`)}`)
  await expect(cited).toHaveAttribute('controls', '')
  await expect(attempts.getByLabel('3D model triangle.gltf')).toBeVisible()
  await expect(attempts.getByRole('button', { name: 'Reset view' })).toBeVisible()
  expect(loaded).toContain('triangle.bin')

  // A single press on the output opens the same native player in its file window.
  await attempts.getByTitle('Open turntable.mp4', { exact: true }).click()
  const file = page.getByRole('dialog', { name: 'file turntable.mp4' })
  await expect(file.getByLabel('turntable.mp4', { exact: true })).toHaveAttribute('controls', '')
  await expect(file.getByRole('link', { name: 'Download', exact: true })).toHaveAttribute('download', 'turntable.mp4')
})
