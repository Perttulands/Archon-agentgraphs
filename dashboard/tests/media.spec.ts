import { readFileSync } from 'node:fs'
import { expect, test } from '@playwright/test'
import { cockpitFixture, mediaAttemptFixture, runArtifacts } from './cockpit-fixture'
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
    if (name === 'triangle.fbx') return route.fulfill({ contentType: 'application/octet-stream', body: readFileSync(new URL('./fixtures/triangle.fbx', import.meta.url)) })
    if (name === 'triangle.bin') return route.fulfill({ contentType: 'application/octet-stream', body: Buffer.from(new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]).buffer) })
    return route.fulfill({ contentType: 'video/mp4', body: '' })
  })
  await page.route('**/api/files/raw?**', route => route.fulfill({ contentType: 'video/webm', body: '' }))
  await page.goto('/?mission=browser&run=run_browser')
  await page.getByTestId('attempts-open-execution').click()
  const attempts = page.getByTestId('attempts-window')
  const movie = attempts.getByLabel('turntable.mp4', { exact: true })
  await expect(movie).toHaveAttribute('controls', '')
  await expect(movie).toHaveAttribute('preload', 'metadata')
  await expect(movie).not.toHaveAttribute('autoplay')
  const folder = attempts.getByTestId('attempt-1-output-folder')
  await expect(folder.getByLabel('videos/folder-turntable.mp4', { exact: true })).toHaveAttribute('controls', '')
  await expect(folder.locator('.attempt-listing li')).toHaveCount(8)
  await expect(folder.getByText('and 172 more listed files')).toBeVisible()
  const cited = attempts.getByLabel('review.webm', { exact: true })
  await expect(cited).toHaveAttribute('src', `/api/files/raw?path=${encodeURIComponent(`${runArtifacts}/execution/1/judge videos/review.webm`)}`)
  await expect(cited).toHaveAttribute('controls', '')
  await expect(attempts.locator('.model-viewer')).toHaveCount(0)
  expect(loaded).not.toContain('triangle.gltf')
  expect(loaded).not.toContain('triangle.fbx')
  await attempts.getByRole('button', { name: 'Preview 3D model triangle.gltf', exact: true }).click()
  await expect(attempts.getByLabel('3D model triangle.gltf', { exact: true })).toBeVisible()
  await expect(attempts.getByRole('button', { name: 'Reset view' })).toHaveCount(1)
  expect(loaded).toContain('triangle.bin')
  await attempts.getByLabel('3D model triangle.gltf', { exact: true }).evaluate((canvas: HTMLCanvasElement) => {
    (window as unknown as { previousModelContext: WebGL2RenderingContext | null }).previousModelContext = canvas.getContext('webgl2')
  })
  await attempts.getByRole('button', { name: 'Preview 3D model triangle.fbx', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as unknown as { previousModelContext: WebGL2RenderingContext }).previousModelContext.isContextLost())).toBe(true)
  await expect(attempts.getByLabel('3D model triangle.gltf', { exact: true })).toHaveCount(0)
  await expect(attempts.getByLabel('3D model triangle.fbx', { exact: true })).toBeVisible()
  await expect(attempts.getByRole('button', { name: 'Reset view' })).toHaveCount(1)

  // A single press on the output opens the same native player in its file window.
  await attempts.getByTitle('Open turntable.mp4', { exact: true }).click()
  const file = page.getByRole('dialog', { name: 'file turntable.mp4' })
  await expect(file.getByLabel('turntable.mp4', { exact: true })).toHaveAttribute('controls', '')
  await expect(file.getByRole('link', { name: 'Download', exact: true })).toHaveAttribute('download', 'turntable.mp4')
  const player = await file.getByLabel('turntable.mp4', { exact: true }).boundingBox()
  const frame = await file.boundingBox()
  expect(player!.y + player!.height).toBeLessThanOrEqual(frame!.y + frame!.height)
  await page.keyboard.press('Escape')
  await attempts.getByTitle('Open triangle.fbx', { exact: true }).click()
  const modelFile = page.getByRole('dialog', { name: 'file triangle.fbx' })
  await expect(modelFile.getByLabel('3D model triangle.fbx', { exact: true })).toBeVisible()

})


test('a TOML mission output plays video in one window, and evidence floats and closes with Escape', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  const fixture = await cockpitFixture(page, { succeeded: true, tomlStyle: true })
  fixture.board().inputCards[0].outputs = [{ name: 'motion', kind: 'folder', from: 'execution:out' }]
  await page.route('**/api/runs/run_browser/evidence/attempts?**', route => route.fulfill({ json: { success: true, data: { attempts: {
    runId: 'run_browser', steps: [{ nodeId: 'execution', title: 'Execution', outputs: [], attempts: [{ ...mediaAttemptFixture, files: mediaAttemptFixture.files.filter(file => file.name.endsWith('.mp4')) }] }],
  } } } }))
  await page.route('**/api/runs/run_browser/artifacts/**/*.mp4', route => route.fulfill({ contentType: 'video/mp4', body: readFileSync(new URL('./fixtures/preview.mp4', import.meta.url)) }))
  await page.goto('/?mission=browser&run=run_browser')
  await page.getByTestId('run-output-motion').click()
  const attempts = page.getByRole('dialog', { name: 'Attempts of Execution' })
  await expect(page.locator('[data-window-kind]')).toHaveCount(1)
  const video = attempts.getByLabel('turntable.mp4', { exact: true })
  await video.evaluate((element: HTMLVideoElement) => element.play())
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.currentTime)).toBeGreaterThan(0.1)
  await page.keyboard.press('Escape')
  await expect(attempts).toHaveCount(0)
  const node = page.getByTestId('formation-node-execution')
  await node.getByTestId('attempts-open-execution').click()
  await expect(attempts.getByLabel('turntable.mp4', { exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await node.getByRole('button', { name: /evidence/i }).click()
  const evidence = page.getByRole('dialog', { name: 'Run evidence · Execution' })
  await expect(evidence).toHaveAttribute('data-window-kind', 'evidence')
  const before = await evidence.boundingBox()
  expect(before!.width).toBeGreaterThanOrEqual(640)
  expect(before!.height).toBeGreaterThanOrEqual(600)
  const origin = await node.boundingBox()
  expect(before!.x + before!.width <= origin!.x || before!.x >= origin!.x + origin!.width ||
    before!.y + before!.height <= origin!.y || before!.y >= origin!.y + origin!.height).toBe(true)
  const zoom = await page.locator('.zoomctl').boundingBox()
  expect(before!.x + before!.width <= zoom!.x || before!.x >= zoom!.x + zoom!.width ||
    before!.y + before!.height <= zoom!.y || before!.y >= zoom!.y + zoom!.height).toBe(true)
  await evidence.getByLabel('Move Run evidence · Execution with arrow keys').focus()
  await page.keyboard.press(before!.x > 960 ? 'ArrowLeft' : 'ArrowRight')
  await expect.poll(async () => (await evidence.boundingBox())!.x).not.toBe(before!.x)
  await page.keyboard.press('Escape')
  await expect(evidence).toHaveCount(0)
})

for (const scenario of [
  { name: 'animation-only.fbx', body: '; FBX 7.4.0 project file\nObjects: { AnimationCurve: 1 {} }', message: 'No mesh in this file' },
  { name: 'oversized.fbx', body: '', message: 'This file is too large to preview', size: 100 * 1024 * 1024 },
]) {
  test(`a ${scenario.name} preview reports a plain explanation`, async ({ page }) => {
    await iterationsFixture(page, 'single')
    await page.route('**/api/runs/run_browser/evidence/attempts?**', route => route.fulfill({ json: { success: true, data: { attempts: {
      runId: 'run_browser', steps: [{ nodeId: 'execution', title: 'Draw Ahti', outputs: [], attempts: [{
        ...mediaAttemptFixture, files: [], verdicts: [],
        outputs: [{ portId: 'model', kind: 'file', fileKind: 'model', artifact: `execution/1/${scenario.name}`, size: scenario.size || scenario.body.length, text: { text: '', bytes: 0 } }],
      }] }],
    } } } }))
    await page.route(`**/api/runs/run_browser/artifacts/execution/1/${scenario.name}`, route => route.fulfill({
      body: scenario.body, headers: scenario.size ? { 'content-length': String(scenario.size) } : {},
    }))
    await page.goto('/?mission=browser&run=run_browser')
    await page.getByTestId('attempts-open-execution').click()
    await page.getByRole('button', { name: `Preview 3D model ${scenario.name}` }).click()
    await expect(page.getByRole('alert')).toContainText(scenario.message)
    await expect(page.getByRole('alert').getByRole('link', { name: 'Download' })).toBeVisible()
  })
}

for (const missing of ['unexported', 'delayed-request'] as const) {
  test(`an FBX with ${missing} textures uses a neutral material and explains it`, async ({ page }) => {
    const material = `\tMaterial: 3, "Material::MI_Body_Baked", "" {\n\t\tShadingModel: "phong"\n\t\tProperties70:  {\n\t\t\tP: "DiffuseColor", "Color", "", "A",0,0,0\n\t\t}\n\t}\n`
    const texture = `\tTexture: 4, "Texture::Body", "" {\n\t\tFileName: "missing.png"\n\t}\n\tVideo: 5, "Video::Body", "Clip" {\n\t\tFilename: "missing.png"\n\t\tRelativeFilename: "missing.png"\n\t}\n`
    const body = ';'.repeat(256) + '\n' + readFileSync(new URL('./fixtures/triangle.fbx', import.meta.url), 'utf8')
      .replace('Objects:  {', `Objects:  {\n${material}${missing === 'delayed-request' ? texture : ''}`)
      .replace('Connections:  {', `Connections:  {\n\tC: "OO",3,2\n${missing === 'delayed-request' ? '\tC: "OP",4,3,"DiffuseColor"\n\tC: "OO",5,4\n' : ''}`)
    await iterationsFixture(page, 'single')
    await page.route('**/api/runs/run_browser/evidence/attempts?**', route => route.fulfill({ json: { success: true, data: { attempts: {
      runId: 'run_browser', steps: [{ nodeId: 'execution', title: 'Draw Ahti', outputs: [], attempts: [{
        ...mediaAttemptFixture, files: [], verdicts: [],
        outputs: [{ portId: 'model', kind: 'file', fileKind: 'model', artifact: 'execution/1/body.fbx', size: body.length, text: { text: '', bytes: 0 } }],
      }] }],
    } } } }))
    await page.route('**/api/runs/run_browser/artifacts/execution/1/body.fbx', route => route.fulfill({ body }))
    let releaseTexture = () => {}
    const textureReady = new Promise<void>(resolve => { releaseTexture = resolve })
    let textureRequested = false
    await page.route('**/api/runs/run_browser/artifacts/execution/1/missing.png', async route => {
      textureRequested = true
      await textureReady
      await route.fulfill({ status: 404, body: '' })
    })
    await page.goto('/?mission=browser&run=run_browser')
    await page.getByTestId('attempts-open-execution').click()
    await page.getByRole('button', { name: 'Preview 3D model body.fbx' }).click()
    await expect(page.getByRole('button', { name: 'Reset view' })).toBeVisible()
    const stage = page.getByLabel('3D model body.fbx', { exact: true })
    const before = await stage.boundingBox()
    const note = page.getByText(missing === 'unexported'
      ? 'This export has no textures. Showing affected surfaces with a neutral material.'
      : 'Some textures could not be loaded. Showing affected surfaces with a neutral material.', { exact: true })
    if (missing === 'delayed-request') {
      await expect.poll(() => textureRequested).toBe(true)
      await expect(note).toHaveCount(0)
      releaseTexture()
    }
    await expect(note).toBeVisible()
    expect(await stage.boundingBox()).toEqual(before)
    await expect(page.getByRole('alert')).toHaveCount(0)
  })
}

for (const width of [1920, 2560]) {
  test(`folder media stays within its attempt at ${width}px, with one model label`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 1920 ? 1080 : 1440 })
    await page.addInitScript(() => localStorage.clear())
    await iterationsFixture(page, 'single')
    const model = `report-after/capability/moss-lods/${'DryadBody_AllLODs_'.repeat(10)}.fbx`
    await page.route('**/api/runs/run_browser/evidence/attempts?**', route => route.fulfill({ json: { success: true, data: { attempts: {
      runId: 'run_browser', steps: [{ nodeId: 'execution', title: 'Draw Ahti', outputs: [], attempts: [{
        ...mediaAttemptFixture, verdicts: [],
        outputs: mediaAttemptFixture.outputs.filter(output => output.kind === 'folder'),
        files: [
          { name: 'execution/1/videos/turntable.mp4', path: `${runArtifacts}/execution/1/videos/turntable.mp4`, kind: 'video', size: 300, modifiedAt: '' },
          { name: `execution/1/${model}`, path: `${runArtifacts}/execution/1/${model}`, kind: 'model', size: 300, modifiedAt: '' },
        ],
      }] }],
    } } } }))
    await page.route('**/api/runs/run_browser/artifacts/**/*.mp4', route => route.fulfill({ contentType: 'video/mp4', body: readFileSync(new URL('./fixtures/preview.mp4', import.meta.url)) }))
    await page.goto('/?mission=browser&run=run_browser')
    await page.getByTestId('attempts-open-execution').click()
    const folder = page.getByTestId('attempt-1-output-folder')
    await expect(folder.getByRole('button', { name: `Preview 3D model ${model}`, exact: true })).toBeVisible()
    await expect(folder.getByText(model, { exact: true })).toHaveCount(1)
    const bounds = await folder.locator('..').boundingBox()
    for (const child of await folder.locator('video, .model-tile-open, .model-tile-open span').all()) {
      const box = await child.boundingBox()
      expect(box!.x).toBeGreaterThanOrEqual(bounds!.x)
      expect(box!.x + box!.width).toBeLessThanOrEqual(bounds!.x + bounds!.width)
    }
    const label = folder.locator('.model-tile-open span').first()
    const lines = await label.evaluate(element => {
      const range = document.createRange()
      range.selectNodeContents(element)
      return range.getClientRects().length
    })
    expect(lines).toBeGreaterThan(1)
  })
}
