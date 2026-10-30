// Packs dist/index.html into dist/index.html.br, the file the Go side embeds.
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { brotliCompressSync, constants } from 'node:zlib'

const page = 'dist/index.html'
if (!existsSync(page)) {
  console.error(`${page} is missing: run "npm run build" first`)
  process.exit(1)
}

const html = readFileSync(page)
const br = brotliCompressSync(html, {
  params: {
    [constants.BROTLI_PARAM_QUALITY]: constants.BROTLI_MAX_QUALITY,
    [constants.BROTLI_PARAM_LGWIN]: constants.BROTLI_MAX_WINDOW_BITS,
    [constants.BROTLI_PARAM_MODE]: constants.BROTLI_MODE_TEXT,
    [constants.BROTLI_PARAM_SIZE_HINT]: html.length,
  },
})
writeFileSync(`${page}.br`, br)
console.log(`${page}: ${html.length} bytes, ${page}.br: ${br.length} bytes`)
