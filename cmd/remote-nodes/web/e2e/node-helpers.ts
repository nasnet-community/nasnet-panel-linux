import type { APIRequestContext, Locator, Page } from '@playwright/test'
import { PAGE, dialog, expect } from './fixtures'

/** What mock-node.ts's POST /api/__node can put a node in; see its header. */
export interface NodeState {
  id: number
  xrayVersion?: string
  agentVersion?: string
  down?: boolean
  registered?: boolean
  crash?: boolean
  stopped?: boolean
  notApplied?: string
  certDays?: number
  hostKeyChanged?: boolean
  failNext?: string
  dialFailures?: number
  noSubscriptions?: boolean
  bridgeDown?: boolean
}

/** Puts one of the mock's nodes in a state, after the test's reset. */
export async function setNode(request: APIRequestContext, state: NodeState) {
  const res = await request.post('/api/__node', { headers: PAGE, data: state })
  expect(res.ok(), await res.text()).toBe(true)
}

type Tab = 'subscriptions' | 'status' | 'settings'

/** Opens a node's page on a tab and waits for its header. */
export async function openNode(page: Page, id: number, tab: Tab = 'subscriptions', name: string | RegExp = `Node ${id}`) {
  await page.goto(`/#/nodes/${id}${tab === 'subscriptions' ? '' : `/${tab}`}`)
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible()
}

const MORE = /^(More actions|اقدام‌های بیشتر)$/

/** Opens the header's more-actions menu. */
export async function openMenu(page: Page) {
  await page.getByRole('button', { name: MORE }).click()
  await expect(page.getByRole('menu')).toBeVisible()
}

/** An item of the open menu, by the start of its words (a hint under it is part of its name). */
export const menuItem = (page: Page, name: string | RegExp) =>
  page.getByRole('menuitem', { name: typeof name === 'string' ? new RegExp(`^${escape(name)}`) : name })

/** A card of the Status or Settings tab, found by its heading (all of it: a dialog's title may hold the same words). */
export const panel = (page: Page, title: string) =>
  page.getByRole('heading', { level: 2, name: title, exact: true }).locator('xpath=ancestor::div[contains(@class, "rounded-lg")][1]')

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

/** One row of a Status card: its label (all of it, so "Connections" is not "Connections now") and what it says. */
export const row = (scope: Locator, label: string) => scope.locator('dt').filter({ hasText: new RegExp(`^${escape(label)}$`) }).locator('xpath=../..')

/** The four chips at the top of the Status tab. */
export const verdictChip = (page: Page, label: string, value: string | RegExp) =>
  page.locator('main ul').first().getByRole('listitem').filter({ hasText: label }).filter({ hasText: value })

/** A confirmation, which is an alert dialog to screen readers. */
export const confirmDialog = (page: Page, name: string | RegExp) => page.getByRole('alertdialog', { name })

/** Closes a finished job's dialog. */
export async function finishJob(page: Page, done: string | RegExp) {
  const d = dialog(page, done)
  await expect(d).toBeVisible({ timeout: 20000 })
  await d.getByRole('button', { name: /^(Done|انجام شد)$/ }).click()
  await expect(d).toBeHidden()
}

/** The width the page overflows its window by, which is none when nothing scrolls sideways. */
export const overflow = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)

/** The contrast ratio of an element's text on what is behind it (its own and its ancestors' backgrounds, blended). */
export const contrast = (el: Locator) =>
  el.first().evaluate((node) => {
    const parse = (c: string) => {
      const m = c.match(/rgba?\(([^)]+)\)/)![1].split(/[ ,/]+/).filter(Boolean).map(Number)
      return { r: m[0], g: m[1], b: m[2], a: m[3] ?? 1 }
    }
    const chain: Element[] = []
    for (let e: Element | null = node; e; e = e.parentElement) chain.unshift(e)
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
