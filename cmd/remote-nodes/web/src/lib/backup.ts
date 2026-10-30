import type { BackupFile } from './api'

/** A real backup is a few KB of state; anything this big is the wrong file. */
export const MAX_BACKUP_BYTES = 2 * 1024 * 1024

// what a file name may not carry: separators, quotes and control characters
const UNSAFE = /[\\/"\u0000-\u001f\u007f]/g

/**
 * The name for a downloaded backup, from the hub's Content-Disposition header, else a dated
 * default. An RFC 5987 `filename*` beats a plain `filename`, and the result can't hold a path.
 */
export function backupFileName(header: string | null, now = new Date()): string {
  const fallback = `remote-nodes-backup-${now.toISOString().slice(0, 10)}.bak`
  if (!header) return fallback
  let name: string | undefined
  const star = /filename\*\s*=\s*[^']*'[^']*'([^;]+)/i.exec(header)
  if (star) {
    try {
      name = decodeURIComponent(star[1].trim())
    } catch {
      // a broken escape: try the plain name instead
    }
  }
  if (!name) {
    const plain = /filename\s*=\s*(?:"((?:[^"\\]|\\.)*)"|([^;]+))/i.exec(header)
    if (plain) name = (plain[1] ?? plain[2]).trim().replace(/\\(.)/g, '$1')
  }
  return name?.replace(UNSAFE, '_').replace(/^\.+/, '').trim() || fallback
}

/** Base64 of some bytes, in slices so a big file can't overflow the call stack. */
export function bytesToBase64(bytes: Uint8Array): string {
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  return btoa(binary)
}

/** Hands a file to the browser's downloads. */
export function saveFile({ name, blob }: BackupFile) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.append(a)
  a.click()
  a.remove()
  // the download has started by the time this fires
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
}
