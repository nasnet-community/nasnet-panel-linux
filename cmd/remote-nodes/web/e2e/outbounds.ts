import type { APIRequestContext, Locator, Page } from '@playwright/test'
import { PAGE, dialog, expect } from './fixtures'
import type { Outbound, OutboundList } from '../src/lib/api'

// the Outbounds list is a table from Tailwind's lg width; below that it is cards
const TABLE_FROM = 1024

export const isTable = (page: Page) => page.viewportSize()!.width >= TABLE_FROM

export const ID = 'b831381d-6324-4d53-ad4f-8cda48b30811'
export const PBK = 'Hq3vT9mXb2KdR7wLpN5sYc8ZfA1eJ4uG6oQ0iVtBnMk'
/** A VLESS link over WebSocket and TLS, with a remark the name comes from. */
export const LINK = `vless://${ID}@nl2.example.com:443?type=ws&path=%2Fws%3Fed%3D2048&security=tls#%F0%9F%87%B3%F0%9F%87%B1%20Rotterdam`

/** Opens the Outbounds tab once its list has loaded. */
export async function openOutbounds(page: Page, wait: string | RegExp = 'Frankfurt') {
  await page.goto('/#/outbounds')
  // the table and the cards both hold every name; only one of them shows at a width
  await expect(page.getByText(wait, { exact: typeof wait === 'string' }).filter({ visible: true }).first()).toBeVisible()
}

/** One outbound's row in the table (its heading cell holds the name), or its card. */
export const obRow = (page: Page, name: string): Locator =>
  isTable(page)
    ? page.getByRole('row').filter({ has: page.getByRole('rowheader').getByText(name, { exact: true }) })
    : page.getByRole('list', { name: /^(Outbounds|خروجی‌ها)$/ }).getByRole('listitem').filter({ has: page.getByText(name, { exact: true }) })

/** The Add dialog, opened and with `text` pasted into it. */
export async function openAdd(page: Page, text?: string) {
  await page.getByRole('button', { name: /^(Add outbound|افزودن خروجی)$/ }).first().click()
  const d = dialog(page, /^(Add an outbound|افزودن یک خروجی)$/)
  await expect(d).toBeVisible()
  if (text !== undefined) await d.getByLabel(/^(Link or JSON|لینک یا JSON)$/).fill(text)
  return d
}

/** The outbounds as the mock holds them now. */
export async function listed(request: APIRequestContext): Promise<OutboundList> {
  const res = await request.get('/api/outbounds', { headers: PAGE })
  expect(res.ok()).toBe(true)
  return res.json()
}

export const byName = async (request: APIRequestContext, name: string): Promise<Outbound | undefined> => (await listed(request)).outbounds.find((o) => o.name === name)

/** Puts a sample outbound in a state after the test's reset (see mock-outbounds.ts). */
export async function setOutbound(request: APIRequestContext, state: { id: number; health?: 'up' | 'down'; error?: string; delayMs?: number }) {
  const res = await request.post('/api/__outbound', { headers: PAGE, data: state })
  expect(res.ok(), await res.text()).toBe(true)
}
