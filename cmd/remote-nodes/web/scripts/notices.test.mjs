// Tests for notices.mjs: the parts that read and format on small made-up packages, and the real
// page, which must not carry a package without a notice or with a licence nobody agreed to.
import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'
import { afterAll, describe, expect, it } from 'vitest'
import {
  ALLOWED_LICENSES,
  clean,
  collectPackages,
  format,
  headerComment,
  licenseAllowed,
  licenseFiles,
  loadPackage,
  prepare,
  problems,
  readCssNotices,
  readFont,
  readPackage,
  repoUrl,
  standardLicense,
} from './notices.mjs'

const made = []
afterAll(() => {
  for (const dir of made) rmSync(dir, { recursive: true, force: true })
})

/** A scratch tree of files: a string is written as it is, null leaves the file out, anything else is written as JSON. */
function tree(files) {
  const dir = mkdtempSync(join(tmpdir(), 'notices-test-'))
  made.push(dir)
  for (const [path, body] of Object.entries(files)) {
    if (body === null) continue
    mkdirSync(dirname(join(dir, path)), { recursive: true })
    writeFileSync(join(dir, path), typeof body === 'string' ? body : JSON.stringify(body))
  }
  return dir
}

const MIT = [
  'MIT License',
  '',
  'Copyright (c) 2026 Someone',
  '',
  'Permission is hereby granted, free of charge, to any person obtaining a copy of this software and',
  'associated documentation files, to deal in the Software without restriction.',
].join('\n')

/** The same text under another copyright holder, for the made-up licences that have to be told apart. */
const licenceOf = (holder) => MIT.replace('Someone', holder)

const CSS = "included in the page's CSS"

/** A package.json for a made-up package, with a licence unless it is told not to. */
const manifest = (name, more = {}) => ({ name, version: '1.0.0', license: 'MIT', ...more })

/** A package as loadPackage returns it, for the checks and the formatting. */
const loaded = (more = {}) => ({
  dir: '/x/node_modules/lib',
  name: 'lib',
  version: '1.0.0',
  license: 'MIT',
  homepage: '',
  repository: '',
  files: [{ name: 'LICENSE', text: MIT }],
  header: null,
  ...more,
})

const FONT = {
  name: 'Vazirmatn',
  license: 'OFL-1.1',
  licenseName: 'SIL Open Font License, Version 1.1',
  copyright: 'Copyright 2015 The Vazirmatn Project Authors (https://fonts.example/v)',
  homepage: 'https://fonts.example/v',
  files: ['v-400.woff2', 'v-600.woff2'],
  text: 'Copyright 2015 The Vazirmatn Project Authors (https://fonts.example/v)\n\nSIL OPEN FONT LICENSE Version 1.1',
}

describe('the policy', () => {
  it('allows these licences and no others', () => {
    expect(ALLOWED_LICENSES).toEqual(['MIT', 'ISC', 'OFL-1.1', 'Apache-2.0', 'BSD-2-Clause', 'BSD-3-Clause'])
  })

  it('reads an SPDX expression: OR needs one allowed id, AND needs all of them', () => {
    for (const id of ALLOWED_LICENSES) expect(licenseAllowed(id), id).toBe(true)
    for (const ok of ['mit', 'apache-2.0', '(MIT OR GPL-3.0-only)', 'GPL-3.0-only OR ISC', 'MIT AND ISC', '(MIT AND (ISC OR GPL-3.0-only))', ' MIT ']) {
      expect(licenseAllowed(ok), ok).toBe(true)
    }
    for (const no of [
      'GPL-3.0-only', 'MPL-2.0', 'UNLICENSED', 'SEE LICENSE IN LICENSE', 'MIT AND GPL-3.0-only', '(GPL-3.0-only OR MPL-2.0)',
      'MIT WITH Classpath-exception-2.0', 'MIT+', '', '   ', undefined, null,
      // what can't be parsed is refused, not guessed at
      'MIT OR', 'AND MIT', '(MIT', 'MIT)', 'MIT ISC', '()', 'OR',
    ]) {
      expect(licenseAllowed(no), String(no)).toBe(false)
    }
  })

  it('takes the list it is given', () => {
    expect(licenseAllowed('MPL-2.0', ['MPL-2.0'])).toBe(true)
    expect(licenseAllowed('MIT', ['MPL-2.0'])).toBe(false)
  })
})

describe('reading a package', () => {
  it('takes what a notice needs from package.json: name, version, licence, links and what it needs to run', () => {
    const dir = tree({
      'package.json': manifest('a', {
        version: '1.2.3',
        homepage: ' https://a.example ',
        repository: { type: 'git', url: 'git+https://github.com/x/a.git', directory: 'packages/a' },
        main: 'dist/a.js',
        module: 'dist/a.mjs',
        dependencies: { c: '^2', b: '^1' },
        optionalDependencies: { d: '*' },
        peerDependencies: { e: '*' },
        devDependencies: { f: '*' },
      }),
    })
    expect(readPackage(dir)).toEqual({
      dir,
      name: 'a',
      version: '1.2.3',
      license: 'MIT',
      homepage: 'https://a.example',
      repository: 'https://github.com/x/a',
      main: ['dist/a.js', 'dist/a.mjs'],
      deps: ['b', 'c', 'd'], // not the peer or the dev one
      optional: ['d'],
    })
  })

  it('reads the older ways to name a licence', () => {
    const license = (more) => readPackage(tree({ 'package.json': { name: 'a', version: '1.0.0', ...more } })).license
    expect(license({ license: ' ISC ' })).toBe('ISC')
    expect(license({ license: { type: 'MIT', url: 'https://example.test/mit' } })).toBe('MIT')
    expect(license({ licenses: [{ type: 'MIT' }] })).toBe('MIT')
    expect(license({ licenses: [{ type: 'MIT' }, { type: 'Apache-2.0' }] })).toBe('(MIT OR Apache-2.0)')
    expect(license({})).toBe('')
    expect(license({ license: 42 })).toBe('')
  })

  it('says very little when package.json does', () => {
    const p = readPackage(tree({ 'package.json': {} }))
    expect(p).toMatchObject({ name: '', version: '', license: '', homepage: '', repository: '', main: [], deps: [], optional: [] })
  })

  it('turns repository fields into web addresses', () => {
    for (const [field, url] of [
      ['git+https://github.com/preactjs/preact.git', 'https://github.com/preactjs/preact'],
      [{ type: 'git', url: 'https://github.com/lucide-icons/lucide.git', directory: 'packages/lucide-react' }, 'https://github.com/lucide-icons/lucide'],
      ['git://github.com/x/y.git', 'https://github.com/x/y'],
      ['git@github.com:x/y.git', 'https://github.com/x/y'],
      ['ssh://git@gitlab.com/x/y.git', 'https://gitlab.com/x/y'],
      ['https://github.com/x/y#readme', 'https://github.com/x/y'],
      ['x/y', 'https://github.com/x/y'],
      ['github:x/y', 'https://github.com/x/y'],
      ['gitlab:x/y', 'https://gitlab.com/x/y'],
      ['bitbucket:x/y', 'https://bitbucket.org/x/y'],
      ['', ''],
      [undefined, ''],
      [{ type: 'git' }, ''],
    ]) {
      expect(repoUrl(field), JSON.stringify(field)).toBe(url)
    }
  })
})

describe('finding the notice of a package', () => {
  it('takes the licence, copying, notice and patents files, by name, and nothing that is code', () => {
    const dir = tree({
      'package.json': manifest('a'),
      LICENSE: `${MIT}\n`,
      'LICENCE.txt': 'British spelling\n',
      COPYING: 'Copying terms\n',
      NOTICE: 'Notice text\n',
      NOTICES: 'Notices text\n',
      PATENTS: 'Patent grant\n',
      'license.md': 'Lower case\n',
      'LICENSE-MIT': 'Hyphenated\n',
      'license-checker.js': 'module.exports = {}',
      'licenses.json': '{}',
      'LICENSE.d.ts': 'export {}',
      'README.md': 'not one',
      'LICENSE-EMPTY': '  \n',
      'LICENSES/inside.txt': 'a folder is not a file',
      'src/LICENSE': 'only the top of the package counts',
    })
    expect(licenseFiles(dir).map((f) => f.name)).toEqual(['COPYING', 'LICENCE.txt', 'LICENSE', 'LICENSE-MIT', 'NOTICE', 'NOTICES', 'PATENTS', 'license.md'])
    expect(licenseFiles(dir).find((f) => f.name === 'LICENSE').text).toBe(MIT)
  })

  it('cleans a file up the same way for every platform: LF line ends, no byte order mark, no blank lines around it', () => {
    expect(clean('\uFEFF\r\n\r\nline one\r\nline two\r\n\r\n  \r\n')).toBe('line one\nline two')
    expect(clean('lone\rcarriage return')).toBe('lone\ncarriage return')
    expect(clean('  indented first line\n')).toBe('  indented first line')
  })

  it('uses the licence files and leaves the source alone when there are some', () => {
    const dir = tree({ 'package.json': manifest('a', { main: 'index.js' }), LICENSE: MIT, 'index.js': '// Copyright (c) 2020 Someone\nmodule.exports = 1' })
    const p = loadPackage(dir)
    expect(p.files.map((f) => f.name)).toEqual(['LICENSE'])
    expect(p.header).toBeNull()
  })

  it('falls back on the comment at the top of the main file when there is no licence file', () => {
    const dir = tree({
      'package.json': manifest('a', { main: 'lib/a' }),
      'lib/a.js': '//---------\n//\n// A thing\n//\n// Copyright (c) 2009 Someone\n//\n// Licensed under the MIT license:\n//  https://example.test/mit\n//\n//---------\n\nvar a = 1\n',
    })
    expect(loadPackage(dir).header).toEqual({
      file: 'lib/a.js',
      text: 'A thing\n\nCopyright (c) 2009 Someone\n\nLicensed under the MIT license:\n https://example.test/mit',
    })
  })

  it('adds the standard MIT text to a header, and nothing for a licence it has no text of', () => {
    const header = '// Copyright (c) 2009 Someone\n// Licensed under the MIT license\n'
    const mit = loadPackage(tree({ 'package.json': manifest('a'), 'index.js': header }))
    expect(mit.standard).toEqual(standardLicense('MIT', 'Copyright (c) 2009 Someone\nLicensed under the MIT license'))
    expect(mit.standard.text).toMatch(/^MIT License\n\nCopyright \(c\) 2009 Someone\n\nPermission is hereby granted/)
    expect(loadPackage(tree({ 'package.json': manifest('a', { license: 'ISC' }), 'index.js': header })).standard).toBeNull()
    // with a licence file of its own there is nothing to stand in for
    expect(loadPackage(tree({ 'package.json': manifest('a'), LICENSE: MIT, 'index.js': header })).standard).toBeNull()
  })

  it('looks at the module file and index.js too, and prefers the comment that names a copyright holder', () => {
    const dir = tree({
      'package.json': manifest('a', { main: 'main.js', module: 'esm/a.mjs' }),
      'main.js': '// generated, do not edit\nexports.a = 1',
      'esm/a.mjs': '/* Copyright (c) 2021 Someone */\nexport const a = 1',
    })
    expect(loadPackage(dir).header).toEqual({ file: 'esm/a.mjs', text: 'Copyright (c) 2021 Someone' })
    const bare = tree({ 'package.json': manifest('b'), 'index.js': '// Copyright (c) 2022 Someone\n' })
    expect(loadPackage(bare).header).toEqual({ file: 'index.js', text: 'Copyright (c) 2022 Someone' })
  })

  it('has no header to give when the main file starts with code, or is not there', () => {
    expect(loadPackage(tree({ 'package.json': manifest('a'), 'index.js': 'module.exports = 1\n// Copyright (c) 2020 Someone' })).header).toBeNull()
    expect(loadPackage(tree({ 'package.json': manifest('a', { main: 'gone.js' }) })).header).toBeNull()
  })
})

describe('the comment at the top of a file', () => {
  const cases = {
    'a banner of line comments, rules and blank lines left out': [
      '//-----\n//\n// QR Code\n//\n// Copyright (c) 2009 Kazuhiko Arase\n//  http://example.test/\n//\n//-----\n\nvar q = 1',
      'QR Code\n\nCopyright (c) 2009 Kazuhiko Arase\n http://example.test/',
    ],
    'a /*! one-liner': ['/*! pkg v1.0 | MIT | (c) 2026 Someone */\nvar a', 'pkg v1.0 | MIT | (c) 2026 Someone'],
    'a block with a star on every line': ['/**\n * Copyright (c) 2026 Someone\n *\n * Licensed under MIT.\n */\nvar a', 'Copyright (c) 2026 Someone\n\nLicensed under MIT.'],
    'a block with ruled edges': ['/*\n ******\n Copyright 2020 Someone\n ******\n*/', 'Copyright 2020 Someone'],
    'a comment behind a shebang': ['#!/usr/bin/env node\n// Copyright (c) 2020 Someone\n', 'Copyright (c) 2020 Someone'],
    'CRLF line ends': ['// Copyright (c) 2020 Someone\r\n// All rights reserved\r\nvar a', 'Copyright (c) 2020 Someone\nAll rights reserved'],
    'only the first block of line comments': ['// first\n\n// second\nvar a', 'first'],
    'code first': ['var a = 1 // Copyright (c) 2020 Someone', ''],
    'a block that never ends': ['/* Copyright (c) 2020 Someone', ''],
    'nothing at all': ['', ''],
  }
  for (const [name, [source, want]] of Object.entries(cases)) {
    it(`reads ${name}`, () => expect(headerComment(source)).toBe(want))
  }
})

describe('the standard text for a package that only names its licence', () => {
  const header = 'QR Code\n\nCopyright (c) 2009 Someone Else\n\nURL: http://example.test/\n\nLicensed under the MIT license:\n http://example.test/mit'

  it('is the MIT permission text under the copyright line of the header', () => {
    const got = standardLicense('MIT', header)
    expect(got.id).toBe('MIT')
    expect(got.text.split('\n').slice(0, 4)).toEqual(['MIT License', '', 'Copyright (c) 2009 Someone Else', ''])
    expect(got.text).toContain('Permission is hereby granted, free of charge, to any person obtaining a copy')
    expect(got.text).toContain('OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE\nSOFTWARE.')
    expect(got.text.endsWith('SOFTWARE.')).toBe(true)
  })

  it('takes every copyright line the header starts one with, or else the lines that mention it', () => {
    const two = standardLicense('MIT', 'Copyright (c) 2009 One\nCopyright (c) 2011 Two\nThis file is copyright of nobody')
    expect(two.text.split('\n').slice(0, 5)).toEqual(['MIT License', '', 'Copyright (c) 2009 One', 'Copyright (c) 2011 Two', ''])
    expect(standardLicense('MIT', 'This file is Copyright 2020 Someone, and (c) another').text).toContain('\n\nThis file is Copyright 2020 Someone, and (c) another\n\n')
  })

  it('knows MIT in any case, and nothing else', () => {
    expect(standardLicense('mit', header)).not.toBeNull()
    expect(standardLicense(' MIT ', header)).not.toBeNull()
    for (const other of ['ISC', 'Apache-2.0', 'BSD-3-Clause', '(MIT OR Apache-2.0)', 'MIT AND ISC', 'SEE LICENSE IN LICENSE', '', undefined]) {
      expect(standardLicense(other, header), String(other)).toBeNull()
    }
  })

  it('is not made up out of nothing: a header with no copyright line gets no text', () => {
    expect(standardLicense('MIT', 'A small helper\n\nLicensed under the MIT license')).toBeNull()
    expect(standardLicense('MIT', '')).toBeNull()
  })
})

describe('collecting the runtime packages', () => {
  const names = (dir) => collectPackages(dir).map((p) => `${p.name} ${p.version}`)

  it('takes dependencies and theirs in turn, not dev or peer dependencies, sorted by name whatever the order they are declared in', () => {
    const dir = tree({
      'package.json': manifest('app', { dependencies: { zed: '*', mid: '*', abc: '*' }, devDependencies: { tool: '*' } }),
      'node_modules/zed/package.json': manifest('zed', { dependencies: { deep: '*' }, peerDependencies: { host: '*' } }),
      'node_modules/mid/package.json': manifest('mid'),
      'node_modules/abc/package.json': manifest('abc'),
      'node_modules/deep/package.json': manifest('deep'),
      'node_modules/tool/package.json': manifest('tool'),
      'node_modules/host/package.json': manifest('host'), // installed for the peer, but not bundled
    })
    expect(names(dir)).toEqual(['abc 1.0.0', 'deep 1.0.0', 'mid 1.0.0', 'zed 1.0.0'])
  })

  it('finds a dependency where Node would: next to the package, or above it, and in a scope', () => {
    const dir = tree({
      'package.json': manifest('app', { dependencies: { a: '*', '@s/p': '*' } }),
      'node_modules/a/package.json': manifest('a', { dependencies: { nested: '*', hoisted: '*' } }),
      'node_modules/a/node_modules/nested/package.json': manifest('nested', { version: '2.0.0' }),
      'node_modules/hoisted/package.json': manifest('hoisted'),
      'node_modules/@s/p/package.json': manifest('@s/p'),
    })
    expect(names(dir)).toEqual(['@s/p 1.0.0', 'a 1.0.0', 'hoisted 1.0.0', 'nested 2.0.0'])
  })

  it('lists two versions of one package, each once, and a package that two others need once', () => {
    const dir = tree({
      'package.json': manifest('app', { dependencies: { a: '*', b: '*', shared: '^1' } }),
      'node_modules/a/package.json': manifest('a', { dependencies: { shared: '^2', common: '*' } }),
      'node_modules/a/node_modules/shared/package.json': manifest('shared', { version: '2.0.0' }),
      'node_modules/b/package.json': manifest('b', { dependencies: { common: '*' } }),
      'node_modules/shared/package.json': manifest('shared', { version: '1.0.0' }),
      'node_modules/common/package.json': manifest('common'),
    })
    expect(names(dir)).toEqual(['a 1.0.0', 'b 1.0.0', 'common 1.0.0', 'shared 1.0.0', 'shared 2.0.0'])
  })

  it('does not go round in circles', () => {
    const dir = tree({
      'package.json': manifest('app', { dependencies: { a: '*' } }),
      'node_modules/a/package.json': manifest('a', { dependencies: { b: '*' } }),
      'node_modules/b/package.json': manifest('b', { dependencies: { a: '*' } }),
    })
    expect(names(dir)).toEqual(['a 1.0.0', 'b 1.0.0'])
  })

  it('skips an optional package that is not installed, and stops for a required one', () => {
    const dir = tree({
      'package.json': manifest('app', { dependencies: { a: '*' }, optionalDependencies: { maybe: '*' } }),
      'node_modules/a/package.json': manifest('a', { optionalDependencies: { 'only-on-windows': '*' } }),
    })
    expect(names(dir)).toEqual(['a 1.0.0'])
    const missing = tree({ 'package.json': manifest('app', { dependencies: { gone: '*' } }) })
    expect(() => collectPackages(missing)).toThrow(/gone is not installed.*npm ci/)
  })

  it('reads each package with its notice', () => {
    const dir = tree({
      'package.json': manifest('app', { dependencies: { a: '*', b: '*' } }),
      'node_modules/a/package.json': manifest('a', { homepage: 'https://a.example' }),
      'node_modules/a/LICENSE': MIT,
      'node_modules/b/package.json': manifest('b', { main: 'b.js' }),
      'node_modules/b/b.js': '// Copyright (c) 2020 Someone\n',
    })
    const [a, b] = collectPackages(dir)
    expect([a.name, a.homepage, a.files.map((f) => f.name), a.header]).toEqual(['a', 'https://a.example', ['LICENSE'], null])
    expect([b.name, b.files, b.header]).toEqual(['b', [], { file: 'b.js', text: 'Copyright (c) 2020 Someone' }])
  })
})

describe("what the page's CSS carries", () => {
  const tailwind = (more = {}) => ({
    'package.json': manifest('app'),
    'node_modules/tailwindcss/package.json': manifest('tailwindcss', {
      version: '3.4.19',
      homepage: 'https://tailwindcss.com',
      repository: { type: 'git', url: 'https://github.com/tailwindlabs/tailwindcss.git#v3' },
    }),
    'node_modules/tailwindcss/LICENSE': licenceOf('Tailwind Authors'),
    ...more,
  })

  it("is Tailwind with its own licence, and modern-normalize with the licence file Tailwind ships next to Preflight", () => {
    const dir = tree(tailwind({ 'node_modules/tailwindcss/src/css/LICENSE': licenceOf('Preflight Authors') }))
    const [tw, normalize] = readCssNotices(dir)
    expect(tw).toMatchObject({
      name: 'tailwindcss',
      version: '3.4.19',
      license: 'MIT',
      homepage: 'https://tailwindcss.com',
      repository: 'https://github.com/tailwindlabs/tailwindcss',
      label: CSS,
    })
    expect(tw.files).toEqual([{ name: 'LICENSE', text: licenceOf('Tailwind Authors') }])
    expect(normalize).toMatchObject({ name: 'modern-normalize', version: '', installed: false, license: 'MIT', label: CSS })
    expect(normalize.files).toEqual([{ name: 'tailwindcss/src/css/LICENSE', text: licenceOf('Preflight Authors') }])
    expect(normalize.note.join(' ')).toContain('Preflight, the base styles in the page\'s CSS, is derived from modern-normalize.')
    expect(normalize.note.join(' ')).toContain('the licence file Tailwind ships next to Preflight, tailwindcss/src/css/LICENSE.')
    expect(problems([], FONT, [tw, normalize])).toEqual([])
  })

  it('looks in lib/css when Tailwind ships no src', () => {
    const [, normalize] = readCssNotices(tree(tailwind({ 'node_modules/tailwindcss/lib/css/LICENSE': licenceOf('Preflight Authors') })))
    expect(normalize.files).toEqual([{ name: 'tailwindcss/lib/css/LICENSE', text: licenceOf('Preflight Authors') }])
    expect(normalize.note.join(' ')).toContain('tailwindcss/lib/css/LICENSE.')
  })

  it("falls back on Tailwind's own licence when it ships none for Preflight, and says so, with no copyright line of its own", () => {
    const [tw, normalize] = readCssNotices(tree(tailwind()))
    expect(normalize.files).toEqual([{ name: 'tailwindcss/LICENSE', text: licenceOf('Tailwind Authors') }])
    expect(normalize.note.join(' ')).toContain('Tailwind ships no separate licence file for Preflight, so the text below is Tailwind\'s own licence, tailwindcss/LICENSE.')
    // every copyright line in what is written is one that a file has
    const text = format([], FONT, [tw, normalize])
    const given = new Set(licenceOf('Tailwind Authors').split('\n').filter((l) => /^Copyright/.test(l)))
    const written = text.split('\n').filter((l) => /^Copyright \(c\)/.test(l))
    expect(written.length).toBeGreaterThan(0)
    for (const line of written) expect(given.has(line), line).toBe(true)
  })

  it("names Tailwind's own licence file as it is called when that stands in", () => {
    const dir = tree(tailwind({ 'node_modules/tailwindcss/LICENSE': null }))
    // a made-up Tailwind that keeps its licence in LICENSE.md
    writeFileSync(join(dir, 'node_modules/tailwindcss/LICENSE.md'), licenceOf('Tailwind Authors'))
    const [, normalize] = readCssNotices(dir)
    expect(normalize.files).toEqual([{ name: 'tailwindcss/LICENSE.md', text: licenceOf('Tailwind Authors') }])
    expect(normalize.note.join(' ')).toContain("is Tailwind's own licence, tailwindcss/LICENSE.md.")
  })

  it("uses modern-normalize's own licence when it is installed", () => {
    const dir = tree(
      tailwind({
        'node_modules/modern-normalize/package.json': manifest('modern-normalize', { version: '3.0.1', repository: 'sindresorhus/modern-normalize' }),
        'node_modules/modern-normalize/license': licenceOf('Normalize Author'),
        'node_modules/tailwindcss/src/css/LICENSE': licenceOf('Preflight Authors'),
      }),
    )
    const [, normalize] = readCssNotices(dir)
    expect(normalize).toMatchObject({ name: 'modern-normalize', version: '3.0.1', license: 'MIT', label: CSS, repository: 'https://github.com/sindresorhus/modern-normalize' })
    expect(normalize.installed).toBeUndefined()
    expect(normalize.files).toEqual([{ name: 'license', text: licenceOf('Normalize Author') }])
    expect(normalize.note).toEqual(["Tailwind's Preflight, the base styles in the page's CSS, is derived from", 'modern-normalize.'])
  })

  it('stops when Tailwind is not installed', () => {
    expect(() => readCssNotices(tree({ 'package.json': manifest('app') }))).toThrow(/tailwindcss is not installed.*npm ci/)
  })

  it('finds Tailwind where Node would, like any other package', () => {
    const dir = tree({ 'package.json': manifest('app'), 'node_modules/tailwindcss/package.json': manifest('tailwindcss', { version: '3.0.0' }), 'node_modules/tailwindcss/LICENSE': MIT })
    expect(readCssNotices(dir)[0]).toMatchObject({ name: 'tailwindcss', version: '3.0.0' })
  })

  it('is held to the same rules as the libraries', () => {
    const gpl = tree(tailwind({ 'node_modules/tailwindcss/package.json': manifest('tailwindcss', { version: '3.4.19', license: 'GPL-3.0-only' }) }))
    expect(problems([], FONT, readCssNotices(gpl))).toEqual([expect.stringMatching(/^tailwindcss 3\.4\.19: licence "GPL-3\.0-only" is not one of MIT, ISC.*decision first/)])
    const short = tree(tailwind({ 'node_modules/tailwindcss/LICENSE': 'MIT' }))
    expect(problems([], FONT, readCssNotices(short))).toEqual(
      expect.arrayContaining(['tailwindcss 3.4.19: LICENSE is too short to be a licence text', 'modern-normalize: tailwindcss/LICENSE is too short to be a licence text']),
    )
    const none = tree(tailwind({ 'node_modules/tailwindcss/LICENSE': '' }))
    expect(problems([], FONT, readCssNotices(none))).toEqual([
      'tailwindcss 3.4.19: it has no licence file and no comment at the top of its main file',
      'modern-normalize: Tailwind ships no licence text to stand for it (no src/css/LICENSE, and no LICENSE of its own)',
    ])
  })

  it('wants a version of what is installed, not of the stand-in, and the MIT licence under the name it is listed with', () => {
    const rec = (more) => loaded({ name: 'modern-normalize', version: '', installed: false, label: CSS, ...more })
    expect(problems([], FONT, [rec()])).toEqual([])
    expect(problems([], FONT, [loaded({ name: 'tailwindcss', version: '', label: CSS })])).toEqual([expect.stringContaining('has no name or no version')])
    expect(problems([], FONT, [rec({ files: [{ name: 'tailwindcss/src/css/LICENSE', text: `BSD 3-Clause. ${'x'.repeat(120)}` }] })])).toEqual([
      'modern-normalize: tailwindcss/src/css/LICENSE is not the MIT licence that modern-normalize is listed under',
    ])
  })
})

describe('the font', () => {
  it('is its notice, the copyright line in it, where it is from, and the files that are embedded', () => {
    const dir = tree({
      'src/fonts/OFL.txt': '\r\nCopyright 2015 The Vazirmatn Project Authors (https://fonts.example/v)\r\n\r\nSIL OPEN FONT LICENSE Version 1.1\r\n',
      'src/fonts/b-600.woff2': 'x',
      'src/fonts/a-400.woff2': 'x',
      'src/fonts/readme.txt': 'not a font',
    })
    expect(readFont(dir)).toEqual({
      name: 'Vazirmatn',
      license: 'OFL-1.1',
      licenseName: 'SIL Open Font License, Version 1.1',
      text: 'Copyright 2015 The Vazirmatn Project Authors (https://fonts.example/v)\n\nSIL OPEN FONT LICENSE Version 1.1',
      copyright: 'Copyright 2015 The Vazirmatn Project Authors (https://fonts.example/v)',
      homepage: 'https://fonts.example/v',
      files: ['a-400.woff2', 'b-600.woff2'],
    })
  })

  it('is empty when the page has none', () => {
    expect(readFont(tree({ 'package.json': manifest('app') }))).toMatchObject({ text: '', copyright: '', homepage: '', files: [] })
  })
})

describe('what stops the notices', () => {
  it('is nothing for packages with a licence file, or a copyright header, under an allowed licence', () => {
    const header = { file: 'index.js', text: 'Copyright (c) 2020 Someone' }
    const standard = standardLicense('MIT', header.text)
    const list = [loaded(), loaded({ name: 'bare', files: [], header, standard }), loaded({ name: 'dual', license: '(MIT OR GPL-3.0-only)' })]
    expect(problems(list, FONT)).toEqual([])
  })

  it('names a licence that is not allowed, or not given', () => {
    expect(problems([loaded({ license: 'GPL-3.0-only' })], FONT)).toEqual([expect.stringMatching(/^lib 1\.0\.0: licence "GPL-3\.0-only" is not one of MIT, ISC.*decision first/)])
    expect(problems([loaded({ license: '' })], FONT)).toEqual([expect.stringMatching(/^lib 1\.0\.0: licence \(none declared\)/)])
    expect(problems([loaded({ license: 'SEE LICENSE IN LICENSE.md' })], FONT)).toHaveLength(1)
  })

  it('names a package with no text to carry', () => {
    expect(problems([loaded({ files: [], header: null })], FONT)).toEqual(['lib 1.0.0: it has no licence file and no comment at the top of its main file'])
    expect(problems([loaded({ files: [{ name: 'LICENSE', text: 'MIT' }] })], FONT)).toEqual(['lib 1.0.0: LICENSE is too short to be a licence text'])
    const nobody = problems([loaded({ files: [], header: { file: 'index.js', text: 'A small helper' } })], FONT)
    expect(nobody).toEqual(['lib 1.0.0: it has no licence file, and the comment at the top of index.js names no copyright holder'])
  })

  it('names a package with only a header, when the standard text of its licence is not known here', () => {
    const header = { file: 'index.js', text: 'Copyright (c) 2020 Someone' }
    expect(problems([loaded({ license: 'ISC', files: [], header, standard: standardLicense('ISC', header.text) })], FONT)).toEqual([
      'lib 1.0.0: it has no licence file, and the standard text of "ISC" is only known here for MIT with a copyright line in the header',
    ])
    // MIT, but a header that mentions no copyright is refused for that, not for the text
    expect(problems([loaded({ files: [], header: { file: 'index.js', text: 'MIT' }, standard: null })], FONT)).toEqual([
      'lib 1.0.0: it has no licence file, and the comment at the top of index.js names no copyright holder',
    ])
  })

  it('wants one usable file among several', () => {
    const files = [{ name: 'NOTICE', text: 'See LICENSE' }, { name: 'LICENSE', text: MIT }]
    expect(problems([loaded({ files })], FONT)).toEqual([])
  })

  it('names a package with no name or version', () => {
    expect(problems([loaded({ name: '', version: '' })], FONT)).toEqual([expect.stringContaining('has no name or no version')])
  })

  it('names a font that is missing its notice, its copyright line or its files', () => {
    expect(problems([], { ...FONT, text: '', copyright: '' })).toEqual([expect.stringMatching(/Vazirmatn notice \(src\/fonts\/OFL\.txt\) is missing/)])
    expect(problems([], { ...FONT, copyright: '' })).toEqual(['src/fonts/OFL.txt has no copyright line'])
    expect(problems([], { ...FONT, text: 'Copyright 2015 Someone\n\nSome other terms' })).toEqual(['src/fonts/OFL.txt is not the SIL Open Font License'])
    expect(problems([], { ...FONT, files: [] })).toEqual([expect.stringMatching(/holds no \.woff2 files/)])
  })
})

describe('the text', () => {
  const bare = loaded({
    name: 'bare',
    version: '2.1.0',
    repository: 'https://github.com/x/bare',
    files: [],
    header: { file: 'index.js', text: 'Copyright (c) 2020 Someone\nLicensed under the MIT license.' },
    standard: standardLicense('MIT', 'Copyright (c) 2020 Someone\nLicensed under the MIT license.'),
  })
  const full = loaded({
    name: 'full-lib',
    homepage: 'https://full.example',
    repository: 'https://github.com/x/full-lib',
    files: [{ name: 'LICENSE', text: 'The licence' }, { name: 'NOTICE', text: 'The notice' }],
  })
  const rule = '='.repeat(64)

  it('is a preface, each package in the order given, then the font', () => {
    expect(format([bare, full], FONT)).toBe(
      [
        'The licences of the software built into the web page of the V2ray/Xray plugin: the',
        'libraries it bundles and the font it embeds. The page itself is free',
        'software under the GNU Affero General Public License, version 3 or',
        'later: see LICENSE.',
        '',
        rule,
        'bare 2.1.0',
        rule,
        '',
        'License:    MIT (as package.json declares it)',
        'Repository: https://github.com/x/bare',
        '',
        'This package ships no licence file. Below is the comment at the top of its',
        'main source file, index.js, which holds the copyright notice.',
        '',
        '--- index.js, top of the file ---',
        '',
        'Copyright (c) 2020 Someone',
        'Licensed under the MIT license.',
        '',
        'The package declares the MIT licence but ships no text of it. The standard',
        'MIT text follows, under the copyright notice from the comment above. It is',
        'the standard text, not a file of the package.',
        '',
        '--- standard MIT licence text, not a file of the package ---',
        '',
        ...bare.standard.text.split('\n'),
        '',
        rule,
        'full-lib 1.0.0',
        rule,
        '',
        'License:    MIT',
        'Homepage:   https://full.example',
        'Repository: https://github.com/x/full-lib',
        '',
        '--- LICENSE ---',
        '',
        'The licence',
        '',
        '--- NOTICE ---',
        '',
        'The notice',
        '',
        rule,
        'Vazirmatn (font)',
        rule,
        '',
        'License:    OFL-1.1 (SIL Open Font License, Version 1.1)',
        'Homepage:   https://fonts.example/v',
        'Copyright:  Copyright 2015 The Vazirmatn Project Authors (https://fonts.example/v)',
        '',
        'The page embeds a subset of Vazirmatn: the font cut down to the glyphs its',
        'text uses, in these files. The notice below goes with them.',
        '',
        '  v-400.woff2',
        '  v-600.woff2',
        '',
        '--- OFL.txt ---',
        '',
        'Copyright 2015 The Vazirmatn Project Authors (https://fonts.example/v)',
        '',
        'SIL OPEN FONT LICENSE Version 1.1',
        '',
      ].join('\n'),
    )
  })

  it("puts what the CSS carries after the libraries and before the font, labelled, and says so in the preface", () => {
    const tailwind = loaded({
      name: 'tailwindcss',
      version: '3.4.19',
      homepage: 'https://tailwindcss.com',
      repository: 'https://github.com/tailwindlabs/tailwindcss',
      files: [{ name: 'LICENSE', text: 'Tailwind licence' }],
      label: CSS,
      note: ['Tailwind note.'],
    })
    const normalize = loaded({
      name: 'modern-normalize',
      version: '',
      installed: false,
      files: [{ name: 'tailwindcss/src/css/LICENSE', text: 'Preflight licence' }],
      label: CSS,
      note: ['Normalize note, two', 'lines.'],
    })
    const out = format([full], FONT, [tailwind, normalize])
    expect(out.startsWith(
      [
        'The licences of the software built into the web page of the V2ray/Xray plugin: the',
        'libraries it bundles, the base styles its CSS gets from Tailwind CSS, and',
        'the font it embeds. The page itself is free software under the GNU Affero',
        'General Public License, version 3 or later: see LICENSE.',
        '',
        rule,
        'full-lib 1.0.0',
      ].join('\n'),
    )).toBe(true)
    expect(out).toContain(
      [
        rule,
        "tailwindcss 3.4.19 (included in the page's CSS)",
        rule,
        '',
        'License:    MIT',
        'Homepage:   https://tailwindcss.com',
        'Repository: https://github.com/tailwindlabs/tailwindcss',
        '',
        'Tailwind note.',
        '',
        '--- LICENSE ---',
        '',
        'Tailwind licence',
        '',
        rule,
        "modern-normalize (included in the page's CSS)",
        rule,
        '',
        'License:    MIT',
        '',
        'Normalize note, two',
        'lines.',
        '',
        '--- tailwindcss/src/css/LICENSE ---',
        '',
        'Preflight licence',
        '',
        rule,
        'Vazirmatn (font)',
      ].join('\n'),
    )
    const at = (s) => out.indexOf(s)
    expect(at('full-lib 1.0.0')).toBeLessThan(at('tailwindcss 3.4.19'))
    expect(at('tailwindcss 3.4.19')).toBeLessThan(at('modern-normalize (included'))
    expect(at('modern-normalize (included')).toBeLessThan(at('Vazirmatn (font)'))
    // without any, the preface doesn't mention them
    expect(format([full], FONT)).not.toContain('Tailwind')
  })

  it('is the same every time, with one newline at the end and nothing from the clock or the machine', () => {
    const once = format([bare, full], FONT)
    expect(format([bare, full], FONT)).toBe(once)
    expect(once.endsWith('\n') && !once.endsWith('\n\n')).toBe(true)
    expect(once).not.toContain('\r')
    expect(once).not.toContain(tmpdir())
    expect(once).not.toContain('node_modules')
  })
})

// what the page really ships, checked against the real node_modules
describe('the page as it is', () => {
  const web = fileURLToPath(new URL('..', import.meta.url))
  const declared = Object.keys(JSON.parse(readFileSync(join(web, 'package.json'), 'utf8')).dependencies)
  let cached
  const real = () => (cached ??= prepare(web))

  it('carries no package without a usable notice or with a licence the project has not allowed', () => {
    expect(real().problems).toEqual([])
  })

  it('lists every runtime dependency, and not what a peer dependency brought in', () => {
    const names = real().packages.map((p) => p.name)
    for (const dep of declared) expect(names, dep).toContain(dep)
    // npm installs react for lucide-react's peer dependency on it, but the page aliases react to preact
    for (const peer of ['react', 'loose-envify', 'js-tokens']) if (!declared.includes(peer)) expect(names, peer).not.toContain(peer)
  })

  it('has every notice in the text, word for word, and the font with its files', () => {
    const { packages, css, font, text } = real()
    for (const p of [...packages, ...css]) {
      expect(text).toContain([p.name, p.version].filter(Boolean).join(' '))
      for (const f of p.files) expect(text, `${p.name}: ${f.name}`).toContain(f.text)
      if (p.header) expect(text).toContain(p.header.text)
      if (p.standard) expect(text).toContain(p.standard.text)
    }
    expect(text).toContain('Vazirmatn (font)')
    expect(text).toContain(font.copyright)
    expect(text).toContain('SIL OPEN FONT LICENSE Version 1.1')
    for (const file of font.files) expect(text).toContain(file)
    // the font notice is the one in the source, whole
    expect(text).toContain(font.text)
  })

  it("lists Tailwind CSS and modern-normalize as included in the page's CSS, with the licences node_modules has", () => {
    const { css, text } = real()
    expect(css.map((p) => p.name)).toEqual(['tailwindcss', 'modern-normalize'])
    for (const p of css) expect(text).toContain(`${[p.name, p.version].filter(Boolean).join(' ')} (included in the page's CSS)`)
    const [tailwind, normalize] = css
    expect(tailwind.version).toMatch(/^\d+\.\d+\.\d+/)
    expect(tailwind.files).toEqual([{ name: 'LICENSE', text: clean(readFileSync(join(web, 'node_modules', 'tailwindcss', 'LICENSE'), 'utf8')) }])
    // modern-normalize has no package of its own here, so the licence Tailwind ships for Preflight stands in, and the notice says so
    if (normalize.installed === false) {
      expect(normalize.files).toHaveLength(1)
      expect(normalize.files[0].name).toMatch(/^tailwindcss\/(?:src\/css\/|lib\/css\/)?LICENSE$/)
      expect(clean(readFileSync(join(web, 'node_modules', normalize.files[0].name), 'utf8'))).toBe(normalize.files[0].text)
      expect(normalize.note.join(' ')).toContain(`${normalize.files[0].name}.`)
    }
    expect(normalize.note.join(' ')).toContain('is derived from modern-normalize')
  })

  it("still has Preflight in the page's CSS, which is why those two are listed", () => {
    const sheet = readFileSync(join(web, 'src', 'index.css'), 'utf8')
    const config = readFileSync(join(web, 'tailwind.config.ts'), 'utf8')
    expect(sheet, "src/index.css has no @tailwind base: if Preflight is gone from the CSS, take the CSS notices out of notices.mjs").toMatch(/^@tailwind base;?\s*$/m)
    expect(config, 'tailwind.config.ts switches Preflight off: take the CSS notices out of notices.mjs').not.toMatch(/preflight\s*:\s*false/)
  })

  it('has the MIT text word for word as the MIT licences in node_modules have it', () => {
    const body = (t) => t.slice(t.indexOf('Permission is hereby granted'))
    const ours = body(standardLicense('MIT', 'Copyright (c) 2000 Someone').text)
    const files = ['preact/LICENSE', 'tailwindcss/LICENSE', 'tailwindcss/src/css/LICENSE'].filter((f) => existsSync(join(web, 'node_modules', f)))
    expect(files.length).toBeGreaterThan(1)
    for (const f of files) expect(body(clean(readFileSync(join(web, 'node_modules', f), 'utf8'))), f).toBe(ours)
  })

  it('gives every package that has only a header the standard text, under its own copyright line', () => {
    for (const p of real().packages.filter((x) => x.header)) {
      expect(p.standard, p.name).not.toBeNull()
      const line = p.header.text.split('\n').map((l) => l.trim()).find((l) => /^copyright/i.test(l))
      expect(p.standard.text, p.name).toContain(`\n\n${line}\n\nPermission is hereby granted`)
      expect(real().text, p.name).toContain('--- standard MIT licence text, not a file of the package ---')
    }
  })

  it('imports only runtime dependencies, so nothing is bundled without a notice', () => {
    // react and react-dom are what the page's code says; vite.config.ts aliases them to preact
    const alias = { react: 'preact', 'react-dom': 'preact' }
    const files = (dir) =>
      readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
        const path = join(dir, e.name)
        if (e.isDirectory()) return files(path)
        return /\.tsx?$/.test(e.name) && !/\.(?:test|d)\.tsx?$/.test(e.name) ? [path] : []
      })
    // what a file imports, as a compiler reads it: not what a string or a comment says; a type
    // import leaves nothing in the bundle
    const imports = (file) => {
      const specs = []
      const visit = (node) => {
        const spec = node.moduleSpecifier
        if ((ts.isImportDeclaration(node) && !node.importClause?.isTypeOnly) || (ts.isExportDeclaration(node) && !node.isTypeOnly && spec)) {
          if (spec && ts.isStringLiteral(spec)) specs.push(spec.text)
        } else if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword && ts.isStringLiteral(node.arguments[0])) {
          specs.push(node.arguments[0].text)
        }
        ts.forEachChild(node, visit)
      }
      visit(ts.createSourceFile(file, readFileSync(file, 'utf8'), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX))
      return specs
    }
    const sources = files(join(web, 'src'))
    expect(sources.length).toBeGreaterThan(20)
    const wrong = []
    let seen = 0
    for (const file of sources) {
      for (const spec of imports(file)) {
        if (spec.startsWith('.') || spec.startsWith('/')) continue
        seen++
        const name = spec.startsWith('@') ? spec.split('/').slice(0, 2).join('/') : spec.split('/')[0]
        const pkg = alias[name] ?? name
        if (!declared.includes(pkg)) wrong.push(`${relative(web, file)} imports ${spec}, but ${pkg} is not in dependencies`)
      }
    }
    expect(seen, 'the scan found no package imports at all').toBeGreaterThan(10)
    expect(wrong).toEqual([])
  })

  it('is written out the same every time by npm run notices', () => {
    const script = join(web, 'scripts', 'notices.mjs')
    const out = join(tree({}), 'NOTICES.txt')
    const run = () => execFileSync(process.execPath, [script, out], { encoding: 'utf8' })
    const said = run()
    const first = readFileSync(out, 'utf8')
    run()
    expect(readFileSync(out, 'utf8')).toBe(first)
    expect(first).toBe(real().text)
    expect(said).toContain(`${real().packages.length} packages, ${real().css.length} for the page's CSS and the Vazirmatn font, ${Buffer.byteLength(first)} bytes`)
  })
})
