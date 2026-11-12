import type { APIRequestContext, Locator, Page } from '@playwright/test'
import qrcode from 'qrcode-generator'
import { PAGE, expect } from './fixtures'

// the page switches from cards to a table at Tailwind's md width
const TABLE_FROM = 768

/** Whether the lists show as a table: wide screens do, phones get cards. */
export const isTable = (page: Page) => page.viewportSize()!.width >= TABLE_FROM

/** Opens the root Subscriptions tab once its list has loaded. */
export async function openSubscriptions(page: Page) {
  await page.goto('/#/subscriptions')
  await expect(page.getByRole('switch', { name: /^Sara on Node 1$|^Sara روی Node 1$/ })).toBeVisible()
}

/** Opens a node's Subscriptions tab once its list has loaded. */
export async function openNodeSubscriptions(page: Page, id: number) {
  await page.goto(`/#/nodes/${id}`)
  await expect(page.getByRole('switch', { name: /^Sara on Node \d$|^Sara روی Node \d$/ })).toBeVisible()
}

/** One subscription's row in a table (its heading cell holds the name and, under it, how it is doing), or its card on a phone. */
export const row = (page: Page, name: string): Locator =>
  isTable(page)
    ? page.getByRole('row').filter({ has: page.getByRole('rowheader').getByText(name, { exact: true }) })
    : page.locator('main ul > li').filter({ has: page.getByText(name, { exact: true }) })

/** The switch of a subscription on a node. */
export const flipper = (page: Page, name: string, node: string, on = 'on') => page.getByRole('switch', { name: `${name} ${on} ${node}`, exact: true })

/** The names of the subscriptions in the order they are listed, read from their first switch. */
export const listed = (page: Page, node: string, on = 'on') =>
  page.getByRole('switch', { name: new RegExp(` ${on} ${node}$`) }).evaluateAll((els) => els.map((e) => e.getAttribute('aria-label')!.replace(/ \S+ Node \d$/, '')))

/**
 * Starts one of a subscription's actions. Share and Edit are buttons of its row; resetting its usage and
 * deleting it are in the Edit dialog. `lang`: the page is in Persian.
 */
export async function pick(page: Page, name: string, item: 'Edit' | 'Share link' | 'Reset usage' | 'Delete subscription', lang: 'en' | 'fa' = 'en') {
  if (item === 'Share link') return row(page, name).getByRole('button', { name: lang === 'fa' ? 'اشتراک‌گذاری' : 'Share', exact: true }).click()
  const edit = lang === 'fa' ? `ویرایش ${name}` : `Edit ${name}`
  await page.getByRole('button', { name: edit, exact: true }).click()
  if (item === 'Edit') return
  const inside = { en: { 'Reset usage': 'Reset usage…', 'Delete subscription': 'Delete subscription…' }, fa: { 'Reset usage': 'صفر کردن مصرف…', 'Delete subscription': 'حذف اشتراک…' } }
  await page.getByRole('dialog', { name: edit }).getByRole('button', { name: inside[lang][item] }).click()
}

/** The button that adds, in the Add dialog: "Add and share" while a place is ticked, since its link is ready at once. */
export const addButton = (d: Locator) => d.getByRole('button', { name: /^(Add and share|Add subscription|افزودن و اشتراک‌گذاری|افزودن اشتراک)$/ })

/** The tabs of the Share dialog, one per place the subscription has a link on. */
export const placeTabs = (d: Locator) => d.getByRole('tablist', { name: /^(Place|جا)$/ }).getByRole('tab')

/** The link in the share dialog, as text. */
export const linkText = (page: Page) => page.locator('[data-link]').innerText()

/** Adds a subscription without the page, as another browser tab (or a script) would. */
export const addViaApi = (request: APIRequestContext, name: string, nodeIds: number[] = [1]) =>
  request.post('/api/subscriptions', { headers: PAGE, data: { name, nodeIds } })

/** Puts the router at its limit of subscriptions, 10 in all, with nothing on the page yet. */
export async function fillToLimit(request: APIRequestContext, to = 10) {
  const have = (await (await request.get('/api/subscriptions', { headers: PAGE })).json()).length
  for (let n = have; n < to; n += 1) expect((await addViaApi(request, `Extra ${n + 1}`)).ok()).toBe(true)
}

/**
 * The path a QR code of `text` draws, as the page's QrCode component makes it (same library and
 * settings). Comparing it with the page's own tells what text the page's code holds.
 */
export function qrPath(text: string): string {
  const qr = qrcode(0, 'M')
  qr.addData(text)
  qr.make()
  return /<path d="([^"]*)"/.exec(qr.createSvgTag({ cellSize: 4, margin: 4, scalable: true }))![1]
}

/** The path of the QR code the share dialog shows. */
export const shownQrPath = (page: Page) => page.locator('[role="img"] svg path').first().getAttribute('d')

/** The contrast ratio of an element's text on what is behind it (its own and its ancestors' backgrounds, blended). */
export const contrast = (el: Locator) =>
  el.first().evaluate((node) => {
    const parse = (c: string) => {
      const m = c.match(/rgba?\(([^)]+)\)/)![1].split(/[ ,/]+/).filter(Boolean).map(Number)
      return { r: m[0], g: m[1], b: m[2], a: m[3] ?? 1 }
    }
    const chain: Element[] = []
    for (let e: Element | null = node; e; e = e.parentElement) chain.unshift(e)
    // from the page down: each background lies over the last
    let bg = { r: 255, g: 255, b: 255 }
    for (const e of chain) {
      const c = parse(getComputedStyle(e).backgroundColor)
      bg = { r: c.r * c.a + bg.r * (1 - c.a), g: c.g * c.a + bg.g * (1 - c.a), b: c.b * c.a + bg.b * (1 - c.a) }
    }
    const fg = parse(getComputedStyle(node).color)
    const lum = (c: { r: number; g: number; b: number }) => {
      const f = (v: number) => {
        const s = v / 255
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
      }
      return 0.2126 * f(c.r) + 0.7152 * f(c.g) + 0.0722 * f(c.b)
    }
    const [hi, lo] = [lum(fg), lum(bg)].sort((a, b) => b - a)
    return (hi + 0.05) / (lo + 0.05)
  })
