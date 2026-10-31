import { test as base, expect, type APIRequestContext, type Locator, type Page } from '@playwright/test'

/** Every test starts from the mock's sample data and fails on page errors. */
export const test = base.extend<{ errors: string[] }>({
  errors: [
    async ({ page, request }, use) => {
      await reset(request)
      const errors: string[] = []
      page.on('pageerror', (e) => errors.push(e.message))
      page.on('console', (m) => m.type() === 'error' && !isExpectedHttpError(m.text()) && errors.push(m.text()))
      await use(errors)
      expect(errors, 'the page logged errors').toEqual([])
    },
    { auto: true },
  ],
})

// The browser logs failed requests (a refused login's 400, a node at its limit's 409, a job
// stream that answers 503) as console errors; the page handles those, so they are not failures.
function isExpectedHttpError(text: string) {
  return (
    /Failed to load resource: the server responded with a status of (4|5)\d\d/.test(text) ||
    /EventSource's response has a status (4|5)\d\d/.test(text)
  )
}

export { expect }

export interface Reset {
  /** the first-run state: no nodes, no subscriptions, entry off */
  fresh?: boolean
  /** how many of the sample nodes stay, 0 to 3 */
  nodes?: number
  maxNodes?: number
  entry?: 'off' | 'domestic-offline' | 'vpn-offline' | 'stale' | 'old-routeros'
  /** how long install steps take, as a multiple of the mock's usual */
  pace?: number
}

/** Puts the mock back to its sample data, or to the state asked for; installs run at twice the usual speed. */
export const reset = (request: APIRequestContext, state: Reset = {}) => request.post('/api/__reset', { data: { pace: 0.5, ...state } })

/** The header the page sends on every API call except the job stream; a test calling the API directly needs it too. */
export const PAGE = { 'X-RemoteNodes-UI': '1' }

/** Lets a field take more than its maxlength, which a script or a page of another build could put in it: filling stops at the limit. */
export const liftLimit = (field: Locator) => field.evaluate((el) => el.removeAttribute('maxlength'))

export const dialog = (page: Page, name: string | RegExp) => page.getByRole('dialog', { name })

export const toast = (page: Page, text: string | RegExp) => page.locator('[data-toast]').filter({ hasText: text })

/** A node's card on the Nodes tab. */
export const card = (page: Page, name: string) => page.getByRole('link', { name: new RegExp(`^${name}(?=\\s|$)`) })

/** One of the entry card's chips: what it is about and how it is doing. */
export const chip = (page: Page, label: string, value: string) =>
  page.getByRole('listitem').filter({ hasText: label }).filter({ hasText: value })

export const attention = (page: Page) => page.getByRole('region', { name: /Needs attention|نیاز به توجه/ })

/** Opens the Add node dialog once the page has loaded the list (the button works before, but the name needs it). */
export async function openAddNode(page: Page, name: string | RegExp = 'Add node') {
  await page.goto('/')
  await expect(page.getByText(/\d+ of \d+ nodes?|[۰-۹]+ از [۰-۹]+ نود/)).toBeVisible()
  await page.getByRole('button', { name }).click()
}

/** Fills the add-node form with the address and a password; the rest keeps its defaults. */
export async function fillAddNode(page: Page, host: string, password = 'a-password') {
  const d = dialog(page, /Add a node|افزودن نود/)
  await d.getByLabel(/Server address|آدرس سرور/).fill(host)
  await d.locator('input[type="password"]').fill(password)
  return d
}

/**
 * Whether any component's hook state on the page still holds `text` (a secret
 * kept too long). Walks Preact's tree from the root: the root element keeps its
 * vnode in `__k`, vnodes keep children in `__k` and their component in `__c`,
 * and a component keeps its hooks in `__H.__`, each with its value in `__`
 * (and a pending value in `__N`). These names are fixed by Preact's mangle map.
 */
export function stateHolds(page: Page, text: string) {
  return page.evaluate((needle) => {
    const holds = (v: unknown, depth: number): boolean => {
      if (typeof v === 'string') return v.includes(needle)
      if (!v || typeof v !== 'object' || depth > 4 || v instanceof Node) return false
      return Object.values(v).some((x) => holds(x, depth + 1))
    }
    const root = document.getElementById('root') as any
    // the whole tree, so a mounted component that renders nothing (a closed dialog) is seen too
    const stack: any[] = root?.__k ? [root.__k] : []
    if (!stack.length) throw new Error('no Preact tree on #root: did the internals change?')
    const seen = new Set<unknown>()
    let hooks = 0
    while (stack.length) {
      const v = stack.pop()
      if (!v || typeof v !== 'object' || seen.has(v)) continue
      seen.add(v)
      if (Array.isArray(v.__k)) stack.push(...v.__k)
      const list = v.__c?.__H?.__
      if (!Array.isArray(list)) continue
      hooks += list.length
      for (const h of list) if (holds(h.__, 0) || holds(h.__N, 0)) return true
    }
    // a walk that finds no hooks at all is looking in the wrong place
    if (!hooks) throw new Error('no hook state found: did the internals change?')
    return false
  }, text)
}
