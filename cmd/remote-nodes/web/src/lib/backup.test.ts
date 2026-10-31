import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, backup } from './api'
import { MAX_BACKUP_BYTES, backupFileName, bytesToBase64 } from './backup'

const NOW = new Date('2026-10-30T12:00:00Z')

describe('the name of a downloaded backup', () => {
  it('is the one the hub writes: Go leaves a plain name unquoted', () => {
    // mime.FormatMediaType("attachment", {"filename": "remote-nodes-backup-2026-10-31.rnbackup"})
    expect(backupFileName('attachment; filename=remote-nodes-backup-2026-10-31.rnbackup', NOW)).toBe('remote-nodes-backup-2026-10-31.rnbackup')
    // a name with a space or a non-ASCII letter is quoted or sent as filename*
    expect(backupFileName('attachment; filename="remote nodes.rnbackup"', NOW)).toBe('remote nodes.rnbackup')
    expect(backupFileName("attachment; filename*=utf-8''%D9%86%D9%88%D8%AF.rnbackup", NOW)).toBe('نود.rnbackup')
  })

  it("is the one the hub gives, quoted or not", () => {
    expect(backupFileName('attachment; filename="remote-nodes-backup-2026-10-30.bak"', NOW)).toBe('remote-nodes-backup-2026-10-30.bak')
    expect(backupFileName('attachment; filename=remote-nodes.bak', NOW)).toBe('remote-nodes.bak')
    expect(backupFileName('attachment;filename="a b.bak";size=3', NOW)).toBe('a b.bak')
    expect(backupFileName('ATTACHMENT; FILENAME="x.bak"', NOW)).toBe('x.bak')
  })

  it('reads an RFC 5987 name, and prefers it to the plain one', () => {
    expect(backupFileName("attachment; filename*=UTF-8''remote%20nodes%20%E2%80%93%201.bak", NOW)).toBe('remote nodes – 1.bak')
    expect(backupFileName(`attachment; filename="fallback.bak"; filename*=UTF-8''better.bak`, NOW)).toBe('better.bak')
    // a broken escape falls back to the plain name
    expect(backupFileName(`attachment; filename="plain.bak"; filename*=UTF-8''bad%E0%A4%A.bak`, NOW)).toBe('plain.bak')
  })

  it('unescapes a quoted pair', () => {
    expect(backupFileName('attachment; filename="we\\"ird.bak"', NOW)).toBe('we_ird.bak')
  })

  it('cannot carry a path, control characters or a leading dot', () => {
    expect(backupFileName('attachment; filename="../../etc/passwd"', NOW)).toBe('_.._etc_passwd')
    // a backslash in a quoted name is written doubled; once read it is a separator like any other
    expect(backupFileName('attachment; filename="C:\\\\temp\\\\x.bak"', NOW)).toBe('C:_temp_x.bak')
    expect(backupFileName('attachment; filename=".hidden.bak"', NOW)).toBe('hidden.bak')
    expect(backupFileName('attachment; filename="a\u0007b.bak"', NOW)).toBe('a_b.bak')
  })

  it('is a dated default when the hub names nothing usable', () => {
    const fallback = 'remote-nodes-backup-2026-10-30.bak'
    expect(backupFileName(null, NOW)).toBe(fallback)
    expect(backupFileName('', NOW)).toBe(fallback)
    expect(backupFileName('attachment', NOW)).toBe(fallback)
    expect(backupFileName('attachment; filename=""', NOW)).toBe(fallback)
    expect(backupFileName('attachment; filename="..."', NOW)).toBe(fallback)
  })
})

describe('base64', () => {
  it('matches the standard encoding, including past the slice that keeps the call stack small', () => {
    expect(bytesToBase64(new Uint8Array([]))).toBe('')
    expect(bytesToBase64(new TextEncoder().encode('RNBK1\n'))).toBe(Buffer.from('RNBK1\n').toString('base64'))
    const big = Uint8Array.from({ length: 0x8000 * 2 + 17 }, (_, i) => (i * 31) % 256)
    expect(bytesToBase64(big)).toBe(Buffer.from(big).toString('base64'))
    expect(MAX_BACKUP_BYTES).toBeGreaterThan(100 * 1024)
  })
})

describe('the backup calls', () => {
  let fetchMock: ReturnType<typeof vi.fn>
  beforeEach(() => {
    fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
  })
  afterEach(() => vi.unstubAllGlobals())

  it('exports the file under the hub\'s name, and marks the call as the page\'s own', async () => {
    fetchMock.mockResolvedValue(
      new Response('RNBK1\nabc', { status: 200, headers: { 'Content-Type': 'application/octet-stream', 'Content-Disposition': 'attachment; filename="b.bak"' } }),
    )
    const file = await backup.export('correct horse')
    expect(file.name).toBe('b.bak')
    expect(await file.blob.text()).toBe('RNBK1\nabc')
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('api/backup/export')
    expect(init.method).toBe('POST')
    expect(init.headers['X-RemoteNodes-UI']).toBe('1')
    expect(JSON.parse(init.body)).toEqual({ passphrase: 'correct horse' })
  })

  it('throws what the hub answered when the export fails', async () => {
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ code: 'bad_request', error: 'give a passphrase' }), { status: 400 }))
    const err = await backup.export('').catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.code).toBe('bad_request')
    expect(err.message).toBe('give a passphrase')
  })

  it('imports the file as base64, and tells a wrong passphrase from a wrong file by their codes', async () => {
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ ok: true, nodes: 3 }), { status: 200 }))
    await expect(backup.import('pw', 'Uk5CSzEK')).resolves.toEqual({ ok: true, nodes: 3 })
    expect(fetchMock.mock.calls[0][0]).toBe('api/backup/import')
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ passphrase: 'pw', data: 'Uk5CSzEK' })
    for (const code of ['backup_bad_passphrase', 'backup_bad_file']) {
      fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ code, error: 'x' }), { status: 400 }))
      const err = await backup.import('pw', 'x').catch((e) => e)
      expect(err.code).toBe(code)
    }
  })
})
