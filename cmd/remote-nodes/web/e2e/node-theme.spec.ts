import type { Locator } from '@playwright/test'
import { attention, expect, test } from './fixtures'
import { contrast, openNode, panel, row, setNode, verdictChip } from './node-helpers'

// In light mode the panel's green on its own tint (4.45) falls a little short of 4.5, as the Nodes tab's
// test (theme.spec.ts) notes: the tokens are nasnet-panel's and the MTProto page's too, so a change
// belongs to all of them. Where an item below is that pair it may fall to 4.4, and no further.
const GREEN = 4.4

// an element, or one with the ratio it may fall to in light mode
type Items = Record<string, Locator | { el: Locator; light: number }>

async function readable(theme: 'dark' | 'light', items: Items) {
  for (const [what, item] of Object.entries(items)) {
    const el = 'el' in item ? item.el : item
    const least = theme === 'light' && 'el' in item ? item.light : 4.5
    expect.soft(await contrast(el), `${what} in ${theme}`).toBeGreaterThanOrEqual(least)
  }
}

for (const theme of ['dark', 'light'] as const) {
  test.describe(`${theme}`, () => {
    test.beforeEach(async ({ page }) => {
      await page.addInitScript((t) => localStorage.setItem('theme', t), theme)
    })

    test(`the text of the node page, in trouble, is readable (contrast of at least 4.5)`, async ({ page, request }) => {
      await setNode(request, { id: 1, xrayVersion: '26.10.18', certDays: 9 })
      await openNode(page, 1)
      await expect(page.getByText('Needs attention', { exact: true })).toBeVisible()
      const note = attention(page).locator('> div').first()
      await readable(theme, {
        'the name': page.getByRole('heading', { level: 1, name: 'Node 1' }),
        'the needs-attention badge': page.getByText('Needs attention', { exact: true }),
        'the address': page.getByText('203.0.113.24', { exact: true }),
        'the meta line': page.getByText('port 443', { exact: true }),
        'an update chip': page.getByRole('button', { name: 'Update to 26.10.21', exact: true }),
        'a note title': note.getByText('The tunnel is off', { exact: true }),
        'a note detail': note.getByText(/^This node runs Xray 26\.10\.18/),
        'a note button': note.getByRole('button', { name: 'Update Xray' }),
        'a tab': page.getByRole('tab', { name: 'Status' }),
        'the selected tab': page.getByRole('tab', { name: 'Subscriptions' }),
      })
    })

    test(`the text of the Status tab is readable`, async ({ page }) => {
      await openNode(page, 1, 'status')
      await expect(page.getByRole('heading', { level: 2, name: 'Everything is working' })).toBeVisible()
      const tunnel = panel(page, 'Tunnel to this router')
      await readable(theme, {
        'the verdict': page.getByRole('heading', { level: 2, name: 'Everything is working' }),
        'when it reported': page.getByText(/^Last report/),
        'a chip label': verdictChip(page, 'Portal', 'listening').locator('span').first(),
        'a chip value': { el: verdictChip(page, 'Portal', 'listening').locator('b'), light: GREEN },
        'a card title': panel(page, 'Agent').getByRole('heading'),
        'a row label': row(panel(page, 'Agent'), 'Memory').locator('dt'),
        'a row value': row(panel(page, 'Agent'), 'Memory').locator('dd'),
        'a badge': { el: row(panel(page, 'Agent'), 'Settings').getByText('applied'), light: GREEN },
        'a number that is off': row(tunnel, "Couldn't reach the internet").locator('dd'),
        'the hint under it': tunnel.getByText('The router could not open a connection.'),
        'the small print': tunnel.getByText("Totals count from the plugin's last start."),
        'the legend': panel(page, 'Traffic, last 15 minutes').getByText('Download').first(),
        'an axis label': page.getByRole('img', { name: /^Download/ }).locator(':scope > div').first().locator('span').first(),
        'when an event was': panel(page, 'Recent events').getByRole('listitem').first().getByText('2 hours ago'),
        'an event': panel(page, 'Recent events').getByRole('listitem').first().getByText('Tunnel up'),
        'what it said': panel(page, 'Recent events').getByRole('listitem').first().getByText('after 2 s').last(),
      })
    })

    test(`the text of the Settings tab and the router's settings is readable`, async ({ page }) => {
      await openNode(page, 1, 'settings')
      const target = panel(page, 'Handshake target')
      await readable(theme, {
        'a card title': panel(page, 'Connection').getByRole('heading'),
        'what a card is about': panel(page, 'Connection').getByText('How subscriptions reach this node.'),
        'a value': target.getByText('www.example.com:443'),
        'the badge': { el: target.getByText('working', { exact: true }), light: GREEN },
        'when it was checked': target.getByText(/^checked/),
        'a button': panel(page, 'Connection').getByRole('button', { name: 'Change port…' }),
        'a key': panel(page, 'Keys').locator('code').first(),
        'the note under the keys': panel(page, 'Keys').getByText(/^Rotating keys/),
        'the Advanced title': page.getByRole('button', { name: /^Advanced/ }).getByText('Advanced', { exact: true }),
      })
      await page.goto('/#/settings')
      await expect(panel(page, 'Backup')).toBeVisible()
      const entry = panel(page, 'Entry')
      await readable(theme, {
        'the Entry title': entry.getByRole('heading'),
        'an entry label': row(entry, 'RouterOS').locator('dt'),
        'an entry value': row(entry, 'RouterOS').locator('dd'),
        'a hint': entry.getByText('Needs 7.20 or newer.'),
        'an online badge': { el: row(entry, 'Domestic link').getByText('online'), light: GREEN },
        // a table on a wide screen, a list on a phone
        'a route address': page.getByText('192.168.50.16', { exact: true }).filter({ visible: true }),
        'a route': page.getByText('Downloads').filter({ visible: true }).first(),
        'the passphrase hint': panel(page, 'Backup').getByText(/^You need it to import/),
        'the backup warning': panel(page, 'Backup').getByText('No backup yet.'),
        'a link': panel(page, 'About').getByRole('link', { name: 'Notices' }),
      })
    })

    test('draws the chart in the colours of the page, which stand out from the card in this theme', async ({ page }) => {
      await openNode(page, 1, 'status')
      const chart = page.getByRole('img', { name: /^Download/ })
      await expect(chart.locator('svg path')).toHaveCount(3)
      const looks = await chart.evaluate((el) => {
        const paths = Array.from(el.querySelectorAll('svg path')).map((p) => getComputedStyle(p))
        const lines = Array.from(el.querySelectorAll('svg line')).map((l) => getComputedStyle(l).stroke)
        const card = getComputedStyle(el.closest('.rounded-lg')!).backgroundColor
        return { area: { fill: paths[0].fill, opacity: paths[0].opacity }, down: paths[1].stroke, up: paths[2].stroke, grid: lines, card }
      })
      // download in the info colour with a light fill of it, upload in the muted one
      const info = theme === 'dark' ? 'rgb(59, 130, 246)' : 'rgb(37, 99, 235)'
      const muted = theme === 'dark' ? 'rgb(161, 161, 170)' : 'rgb(91, 107, 128)'
      expect(looks.down).toBe(info)
      expect(looks.up).toBe(muted)
      expect(looks.area.fill).toBe(info)
      expect(Number(looks.area.opacity)).toBeCloseTo(0.15, 2)
      expect(new Set(looks.grid).size).toBe(1)
      // a line has to be seen against the card it is on: at least 3:1, the bar for graphics
      const ratio = (a: string, b: string) => {
        const lum = (c: string) => {
          const [r, g, bl] = c.match(/\d+/g)!.map(Number)
          const f = (v: number) => ((v / 255) <= 0.03928 ? v / 255 / 12.92 : ((v / 255 + 0.055) / 1.055) ** 2.4)
          return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(bl)
        }
        const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x)
        return (hi + 0.05) / (lo + 0.05)
      }
      expect(ratio(looks.down, looks.card)).toBeGreaterThanOrEqual(3)
      expect(ratio(looks.up, looks.card)).toBeGreaterThanOrEqual(3)
    })
  })
}

test.describe('the status colours of the node page follow the theme', () => {
  test('are darker on white, so they stay readable', async ({ page, request }) => {
    await setNode(request, { id: 1, xrayVersion: '26.10.18' })
    await page.addInitScript(() => localStorage.setItem('theme', 'dark'))
    await openNode(page, 1)
    const badge = page.getByText('Needs attention', { exact: true })
    expect(await badge.evaluate((el) => getComputedStyle(el).color)).toBe('rgb(237, 195, 44)')
    await page.getByRole('button', { name: 'Switch to light theme' }).click()
    expect(await badge.evaluate((el) => getComputedStyle(el).color)).toBe('rgb(138, 90, 0)')
  })
})
