import { expect, test } from '@playwright/test'
import { talkRunFixture } from './human-channel-fixture'

const longAnswer = `  ${'Vastaus: ääkköset 日本語 🧭\twith space.\n'.repeat(200)}\n`

for (const [button, verdict] of [['Approve', 'pass'], ['Send back', 'fail']]) {
  for (const edit of [false, true]) {
    test(`${button} sends the complete Unicode response file${edit ? ' after editing' : ''}`, async ({ page }) => {
      const fixture = await talkRunFixture(page)
      const payloads: unknown[] = []
      await page.route('**/gates/*/verdict', async route => {
        payloads.push(route.request().postDataJSON())
        await route.fulfill({ status: 409, json: { success: false, error: { message: 'Fixture-only verdict captured' } } })
      })
      await page.goto('/?board=wayfinding')
      const panel = page.getByRole('dialog', { name: 'Answer gate Answer questions' })
      const chooser = page.waitForEvent('filechooser')
      await panel.getByRole('button', { name: 'Load response file', exact: true }).click()
      await (await chooser).setFiles({ name: 'answer.txt', mimeType: 'text/plain', buffer: Buffer.from(longAnswer, 'utf8') })
      const response = panel.getByLabel('Your response')
      await expect(response).toHaveValue(longAnswer)
      expect(payloads).toEqual([])
      const expected = edit ? `${longAnswer}Edited answer.  \n` : longAnswer
      if (edit) {
        await response.focus()
        await response.press('ControlOrMeta+End')
        await response.pressSequentially('Edited answer.  ')
        await response.press('Enter')
        await expect(response).toHaveValue(expected)
      }
      await panel.getByRole('button', { name: button, exact: true }).click()
      await expect.poll(() => payloads.length).toBe(1)
      expect(payloads[0]).toEqual({ actor: 'agent:ui', requestedSeq: 11, verdict, reason: expected })
      expect(fixture.writes).toEqual([])
    })
  }
}

test('unmodified imported text preserves BOM and CRLF in the actual verdict payload', async ({ page }) => {
  await talkRunFixture(page)
  const payloads: Array<{ reason: string }> = []
  await page.route('**/gates/*/verdict', async route => {
    payloads.push(route.request().postDataJSON())
    await route.fulfill({ status: 409, json: { success: false, error: { message: 'Fixture-only verdict captured' } } })
  })
  await page.goto('/?board=wayfinding')
  const panel = page.getByRole('dialog', { name: 'Answer gate Answer questions' })
  const text = `\uFEFF${longAnswer.replace(/\n/g, '\r\n')}`
  await panel.getByLabel('Response file', { exact: true }).setInputFiles({ name: 'answer.txt', mimeType: 'text/plain', buffer: Buffer.from(text, 'utf8') })
  await expect(panel.getByRole('status')).toContainText('Draft saved')
  await panel.getByRole('button', { name: 'Approve', exact: true }).click()
  await expect.poll(() => payloads.length).toBe(1)
  expect(payloads[0].reason).toBe(text)
})

for (const failure of ['read failure', 'invalid UTF-8']) {
  test(`a response file ${failure} leaves the editable draft intact`, async ({ page }) => {
    const fixture = await talkRunFixture(page)
    if (failure === 'read failure') {
      await page.addInitScript(() => {
        File.prototype.arrayBuffer = async () => { throw new Error('Fixture read failed') }
      })
    }
    await page.goto('/?board=wayfinding')
    const panel = page.getByRole('dialog', { name: 'Answer gate Answer questions' })
    const response = panel.getByLabel('Your response')
    await response.fill(longAnswer)
    await panel.getByLabel('Response file', { exact: true }).setInputFiles({ name: 'broken.txt', mimeType: 'text/plain', buffer: Buffer.from([0xff]) })
    await expect(panel.getByRole('alert')).toContainText('Your response has not changed.')
    await expect(response).toHaveValue(longAnswer)
    await expect(response).toBeEnabled()
    expect(fixture.writes).toEqual([])
  })
}
