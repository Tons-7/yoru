import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 8000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(150)) if (await fn().catch(() => false)) return true
  return false
}
const profile = async () => (await fetch(`${BASE}/api/profile`)).json()

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

for (let i = 0; i < 60; i++) {
  try {
    if ((await fetch(`${BASE}/api/setup`)).ok) break
  } catch {}
  await sleep(1000)
}

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))

await page.goto(`${BASE}/settings?tab=Accounts`, { waitUntil: 'domcontentloaded' })
const row = page.locator('[data-row]', { hasText: 'Picture' }).first()
check(await until(() => row.getByRole('button', { name: 'Upload' }).isVisible()), 'Accounts offers to upload a picture')
check((await profile()).avatar === '', 'none is set to begin with')

const png = await page.evaluate(() => {
  const c = document.createElement('canvas')
  c.width = 600
  c.height = 300
  const g = c.getContext('2d')
  g.fillStyle = '#243b8f'
  g.fillRect(0, 0, 600, 300)
  g.fillStyle = '#f2b84b'
  g.beginPath()
  g.arc(450, 150, 90, 0, Math.PI * 2)
  g.fill()
  return c.toDataURL('image/png').split(',')[1]
})
const file = { name: 'me.png', mimeType: 'image/png', buffer: Buffer.from(png, 'base64') }
const input = page.locator('input[type="file"][accept="image/*"]')

await input.setInputFiles(file)
const dialog = page.getByRole('dialog', { name: 'Edit picture' })
check(await until(() => dialog.isVisible()), 'a chosen picture opens the editor first')
const zoom = dialog.getByRole('slider', { name: 'Zoom' })
check(await until(() => zoom.isEnabled()), 'with a zoom slider')
const shown = () => dialog.locator('img').evaluate((img) => ({ w: img.getBoundingClientRect().width, x: img.getBoundingClientRect().x }))
const before = await shown()
await zoom.fill('2')
const zoomed = await shown()
check(zoomed.w > before.w * 1.9, 'zooming enlarges the picture', `${Math.round(before.w)} → ${Math.round(zoomed.w)} px`)
const stage = await dialog.locator('img').locator('..').boundingBox()
await page.mouse.move(stage.x + stage.width / 2, stage.y + stage.height / 2)
await page.mouse.down()
await page.mouse.move(stage.x + stage.width / 2 - 120, stage.y + stage.height / 2, { steps: 6 })
await page.mouse.up()
const dragged = await shown()
check(dragged.x < zoomed.x - 60, 'dragging moves it', `${Math.round(zoomed.x)} → ${Math.round(dragged.x)}`)
await page.mouse.move(stage.x + stage.width / 2, stage.y + stage.height / 2)
await page.mouse.down()
await page.mouse.move(stage.x + stage.width / 2 - 2000, stage.y + stage.height / 2, { steps: 6 })
await page.mouse.up()
const edge = await dialog.locator('img').evaluate((img) => {
  const i = img.getBoundingClientRect()
  const c = img.parentElement.querySelector('[data-crop]').getBoundingClientRect()
  return Math.round(i.right - c.right)
})
check(edge >= -1, 'never past its edge, so the circle stays filled', `${edge} px`)
const roomy = await dialog.locator('img').evaluate((img) => {
  const s = img.parentElement.getBoundingClientRect()
  const c = img.parentElement.querySelector('[data-crop]').getBoundingClientRect()
  return { stage: Math.round(s.width), circle: Math.round(c.width) }
})
check(roomy.stage >= 480 && roomy.circle >= 300 && roomy.stage > roomy.circle + 100, 'the editor is roomy, with the picture visible around the circle', JSON.stringify(roomy))
if (process.env.AVATAR_SHOT) await page.screenshot({ path: process.env.AVATAR_SHOT }).catch(() => {})

await dialog.getByRole('button', { name: 'Save' }).click()
check(await until(async () => (await profile()).avatar === 'own'), 'saving sets the picture')
check(await until(async () => !(await dialog.isVisible())), 'and closes the editor')
const first = (await profile()).version
check(
  await until(() => page.locator('header img[src^="/api/profile/avatar"]').first().evaluate((img) => img.complete && img.naturalWidth === 128)),
  'the top bar shows it, 128 px',
)
const colour = await page.evaluate(async () => {
  const bitmap = await createImageBitmap(await (await fetch('/api/profile/avatar')).blob())
  const c = document.createElement('canvas')
  c.width = bitmap.width
  c.height = bitmap.height
  const g = c.getContext('2d')
  g.drawImage(bitmap, 0, 0)
  const [r, , b] = g.getImageData(64, 64, 1, 1).data
  return { r, b }
})
check(colour.r > colour.b, 'cropped to where it was dragged: the yellow circle fills the middle', JSON.stringify(colour))
check(await until(() => row.getByRole('button', { name: 'Remove' }).isVisible()), 'with Change and Remove offered')

await input.setInputFiles(file)
await dialog.getByRole('button', { name: 'Cancel' }).click()
await sleep(400)
check((await profile()).version === first, 'cancelling the editor keeps the picture as it was')

await row.getByRole('button', { name: 'Remove' }).click()
check(await until(async () => (await profile()).avatar === ''), 'Remove takes it off')

const nameBox = page.getByRole('textbox', { name: 'My own name', exact: true })
check(await until(() => nameBox.isVisible()), 'Accounts has a name of your own')
check((await nameBox.getAttribute('maxlength')) === '32', 'kept to 32 letters')
await nameBox.fill('  Tons  ')
await nameBox.press('Enter')
check(await until(async () => (await profile()).name === 'Tons'), 'Enter saves it, trimmed', JSON.stringify(await profile()))
check(await until(async () => (await nameBox.inputValue()) === 'Tons'), 'and the box shows what was saved')
check((await profile()).nameFrom === 'own' && (await page.getByRole('radio', { name: 'A name of my own' }).isChecked()), 'a typed name becomes the one used')
await page.getByRole('button', { name: 'Profile and settings' }).click()
check(await until(() => page.getByRole('menu').getByText('Tons', { exact: true }).isVisible(), 3000), 'the profile menu shows the name')
await page.keyboard.press('Escape')
await page.goto(`${BASE}/settings?tab=Accounts`, { waitUntil: 'domcontentloaded' })
check(await until(async () => (await nameBox.inputValue()) === 'Tons'), 'a reload keeps it')
await nameBox.fill('')
await nameBox.press('Enter')
check(await until(async () => (await profile()).name === ''), 'emptied, it goes back to the account name')
check(errors.length === 0, 'no page errors', errors.join(' | '))

await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
