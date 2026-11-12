import qrcode from 'qrcode-generator'

/** The cells of a QR code for `text`, row by row: true is dark. The same code the page's QrCode draws. */
export function qrCells(text: string): boolean[][] {
  const qr = qrcode(0, 'M')
  qr.addData(text)
  qr.make()
  const n = qr.getModuleCount()
  return Array.from({ length: n }, (_, row) => Array.from({ length: n }, (_, col) => qr.isDark(row, col)))
}

/** The quiet edge a reader needs around a code, in cells. */
const MARGIN = 4

/**
 * How a code of `count` cells is drawn as an image at least `min` pixels wide: a whole number of
 * pixels to a cell, so its edges stay sharp, and the image's width with its quiet edge.
 */
export function qrLayout(count: number, min = 720): { cell: number; size: number } {
  const cells = count + 2 * MARGIN
  const cell = Math.max(1, Math.ceil(min / cells))
  return { cell, size: cells * cell }
}

/** A PNG of the code, black on white, big enough to be read off another screen. Browser only. */
export function qrPng(text: string): Promise<Blob> {
  const cells = qrCells(text)
  const { cell, size } = qrLayout(cells.length)
  const canvas = document.createElement('canvas')
  canvas.width = size
  canvas.height = size
  const ctx = canvas.getContext('2d')
  if (!ctx) return Promise.reject(new Error('no canvas'))
  ctx.fillStyle = '#fff'
  ctx.fillRect(0, 0, size, size)
  ctx.fillStyle = '#000'
  cells.forEach((line, row) => line.forEach((dark, col) => dark && ctx.fillRect((col + MARGIN) * cell, (row + MARGIN) * cell, cell, cell)))
  return new Promise((resolve, reject) => canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error('no image'))), 'image/png'))
}

/** A file name for a saved code: "Sara - This router.png", with what a file system refuses taken out. */
export function qrFileName(subscription: string, place: string): string {
  const clean = (s: string) =>
    s
      // eslint-disable-next-line no-control-regex
      .replace(/[\\/:*?"<>|\u0000-\u001f]+/g, ' ')
      .replace(/\s+/g, ' ')
      .trim()
      .slice(0, 60)
  const name = [clean(subscription), clean(place)].filter(Boolean).join(' - ')
  return `${name || 'link'}.png`
}

/** Hands a file to the browser to save. */
export function saveBlob(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.append(a)
  a.click()
  a.remove()
  // the click has taken the address by now
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
