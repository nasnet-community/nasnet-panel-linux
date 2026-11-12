import qrcode from 'qrcode-generator'
import { describe, expect, it } from 'vitest'
import { qrCells, qrFileName, qrLayout } from './qr'

const LINK = 'vless://3f0c9a52-7d1e-4b8a-9c44-0e5b2a1d6f73@a1b2c3d4e5f6.sn.mynetname.net:41873?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=Hq3vT9mXb2KdR7wLpN5sYc8ZfA1eJ4uG6oQ0iVtBnMk&sid=9f3a1c7e&type=tcp#Sara'

describe('a QR code as an image', () => {
  it('has the cells of the code the page shows', () => {
    const cells = qrCells(LINK)
    // the page's QrCode makes its code the same way, so the saved image reads as the one on screen
    const qr = qrcode(0, 'M')
    qr.addData(LINK)
    qr.make()
    expect(cells.length).toBe(qr.getModuleCount())
    for (const line of cells) expect(line.length).toBe(cells.length)
    for (let row = 0; row < cells.length; row += 1) for (let col = 0; col < cells.length; col += 1) expect(cells[row][col]).toBe(qr.isDark(row, col))
    // every code starts with a finder pattern: a dark corner, a light ring inside it
    expect(cells[0].slice(0, 7).every(Boolean)).toBe(true)
    expect(cells[1].slice(1, 6).some(Boolean)).toBe(false)
  })

  it('is another code for another link', () => {
    expect(qrCells(`${LINK}x`)).not.toEqual(qrCells(LINK))
  })

  it('is drawn in whole pixels, at least as wide as asked, with a quiet edge', () => {
    // 57 cells and 4 on each side make 65; 12 pixels each is the first size over 720
    expect(qrLayout(57)).toEqual({ cell: 12, size: 780 })
    expect(qrLayout(21, 290)).toEqual({ cell: 10, size: 290 })
    expect(qrLayout(21, 291)).toEqual({ cell: 11, size: 319 })
    // never less than a pixel to a cell
    expect(qrLayout(177, 10)).toEqual({ cell: 1, size: 185 })
  })

  it('is named after the subscription and the place, with nothing a file system refuses', () => {
    expect(qrFileName('Sara', 'This router')).toBe('Sara - This router.png')
    expect(qrFileName('a/b:c*?"<>|d', 'Node 1')).toBe('a b c d - Node 1.png')
    expect(qrFileName('  ', '')).toBe('link.png')
    expect(qrFileName('سارا', 'این روتر')).toBe('سارا - این روتر.png')
    expect(qrFileName('x'.repeat(200), 'Node 1')).toBe(`${'x'.repeat(60)} - Node 1.png`)
    expect(qrFileName('tab\there', 'new\nline')).toBe('tab here - new line.png')
  })
})
