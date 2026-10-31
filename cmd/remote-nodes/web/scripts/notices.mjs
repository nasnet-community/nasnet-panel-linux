// Writes dist/NOTICES.txt: the licences of what the built page carries, which is the libraries
// bundled into it and the font it embeds. MIT, ISC and the OFL all want their notice to travel
// with copies, so the image ships the file as /licenses/web-notices.txt and the hub serves it.
//
//   node scripts/notices.mjs [OUTFILE]     (npm run notices; npm run pack runs it as well)
//
// The libraries are the "dependencies" of package.json and whatever those depend on in turn, read
// from node_modules (run npm ci first). devDependencies are tools and never reach the page. Peer
// dependencies are not followed: npm installs react, loose-envify and js-tokens for lucide-react's
// peer on react, but vite.config.ts aliases react to preact/compat, so none of the three is in the
// bundle.
//
// The script stops with an error when a package has no usable notice or a licence the project
// hasn't agreed to, so a build can't ship one by accident. The unit tests run the same checks.
import { existsSync, mkdirSync, readdirSync, readFileSync, realpathSync, statSync, writeFileSync } from 'node:fs'
import { dirname, join, relative, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

// What the page may carry. Anything else, or an id that can't be read, needs a decision first.
export const ALLOWED_LICENSES = ['MIT', 'ISC', 'OFL-1.1', 'Apache-2.0', 'BSD-2-Clause', 'BSD-3-Clause']

// A licence file shorter than this is a pointer ("see the website"), not the text.
const MIN_NOTICE = 100

const COPYRIGHT = /copyright|\(c\)|©/i
const LICENSE_FILE = /^(?:licen[cs]es?|copying|notices?|patents)(?![a-z])/i
const CODE_FILE = /\.(?:[cm]?[jt]sx?|json|map)$/i
const RULE_LINE = /^\s*[-=*#_~]{3,}\s*$/
const REPO_HOSTS = { github: 'github.com', gitlab: 'gitlab.com', bitbucket: 'bitbucket.org' }

const text = (s) => (typeof s === 'string' ? s.trim() : '')
const isFile = (p) => {
  try {
    return statSync(p).isFile()
  } catch {
    return false
  }
}

/** A file's text as it goes into the notices: LF line ends, no byte order mark, no blank lines around it. */
export function clean(s) {
  return s.replace(/^\uFEFF/, '').replace(/\r\n?/g, '\n').replace(/^\n+/, '').replace(/\s+$/, '')
}

/** A repository field (a string or {url}) as a web address: git+https://, git://, git@host: and "user/repo" all become https. */
export function repoUrl(field) {
  const u = text(typeof field === 'string' ? field : field?.url)
  const short = /^(?:(github|gitlab|bitbucket):)?([\w.-]+\/[\w.-]+)$/.exec(u)
  const url = short
    ? `https://${REPO_HOSTS[short[1] ?? 'github']}/${short[2]}`
    : u.replace(/^git\+/, '').replace(/^git@([^:]+):/, 'https://$1/').replace(/^(?:git|ssh):\/\/(?:git@)?/, 'https://')
  return url.replace(/#.*$/, '').replace(/\.git$/, '')
}

// package.json says it in a string, or in the older forms: {type}, and a licenses list
function licenseOf(json) {
  if (typeof json.license === 'string') return json.license.trim()
  if (typeof json.license?.type === 'string') return json.license.type.trim()
  const list = Array.isArray(json.licenses) ? json.licenses.map((l) => text(l?.type ?? l)).filter(Boolean) : []
  return list.length > 1 ? `(${list.join(' OR ')})` : (list[0] ?? '')
}

/** What package.json of the package in dir says that a notice needs, and what it depends on at runtime. */
export function readPackage(dir) {
  const json = JSON.parse(readFileSync(join(dir, 'package.json'), 'utf8'))
  const optional = Object.keys(json.optionalDependencies ?? {}).sort()
  return {
    dir,
    name: text(json.name),
    version: text(json.version),
    license: licenseOf(json),
    homepage: text(json.homepage),
    repository: repoUrl(json.repository),
    main: [json.main, json.module].filter((m) => typeof m === 'string' && m),
    deps: [...new Set([...Object.keys(json.dependencies ?? {}), ...optional])].sort(),
    optional,
  }
}

/** The licence, copying, notice and patents files in the top of a package, by name, with their text. */
export function licenseFiles(dir) {
  return readdirSync(dir)
    .filter((n) => LICENSE_FILE.test(n) && !CODE_FILE.test(n) && isFile(join(dir, n)))
    .sort()
    .map((name) => ({ name, text: clean(readFileSync(join(dir, name), 'utf8')) }))
    .filter((f) => f.text)
}

/** The comment a source file starts with, without its comment marks and rule lines; "" when it starts with code. */
export function headerComment(source) {
  const src = clean(source).replace(/^#!.*\n/, '').replace(/^\s+/, '')
  let lines
  if (src.startsWith('/*')) {
    const end = src.indexOf('*/')
    if (end < 0) return ''
    lines = src
      .slice(2, end)
      .replace(/^[!*]/, '')
      .split('\n')
      .map((l) => l.replace(/^\s*\*\s?/, ''))
  } else if (src.startsWith('//')) {
    lines = []
    for (const l of src.split('\n')) {
      if (!l.trimStart().startsWith('//')) break
      lines.push(l.trimStart().slice(2).replace(/^ /, ''))
    }
  } else {
    return ''
  }
  return lines.filter((l) => !RULE_LINE.test(l)).join('\n').replace(/^\s+|\s+$/g, '')
}

// the header of the file a package's main field points at (the module field and index.js are
// the others a bundler may take), preferring one that names a copyright holder
function mainHeader(dir, mains) {
  const found = []
  for (const m of [...mains, 'index.js']) {
    for (const rel of [m, `${m}.js`, join(m, 'index.js')]) {
      const file = join(dir, rel)
      if (!isFile(file)) continue
      const comment = headerComment(readFileSync(file, 'utf8'))
      if (comment) found.push({ file: relative(dir, file).split(sep).join('/'), text: comment })
    }
  }
  return found.find((h) => COPYRIGHT.test(h.text)) ?? found[0] ?? null
}

/** A package with its notice: its licence files, or, when it has none, the header of its main file. */
export function loadPackage(dir) {
  const p = readPackage(dir)
  p.files = licenseFiles(dir)
  p.header = p.files.length ? null : mainHeader(dir, p.main)
  return p
}

// Node's own lookup: the nearest node_modules above `from` that holds the package
function locate(name, from) {
  for (let dir = from; ; dir = dirname(dir)) {
    const candidate = join(dir, 'node_modules', name)
    if (isFile(join(candidate, 'package.json'))) return realpathSync(candidate)
    if (dirname(dir) === dir) return null
  }
}

const byNameAndVersion = (a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : a.version < b.version ? -1 : a.version > b.version ? 1 : 0)

/**
 * The runtime packages of the project in dir, sorted by name and version: its dependencies and, in
 * turn, theirs. A package that is missing is an error, except an optional one, which npm may skip.
 */
export function collectPackages(dir) {
  const top = JSON.parse(readFileSync(join(dir, 'package.json'), 'utf8'))
  const found = new Map() // real directory -> package, so each is read once
  const visit = (name, from, optional) => {
    const where = locate(name, from)
    if (!where) {
      if (optional) return
      throw new Error(`${name} is not installed (looked from ${from}): run npm ci first`)
    }
    if (found.has(where)) return
    const pkg = loadPackage(where)
    found.set(where, pkg)
    for (const dep of pkg.deps) visit(dep, where, pkg.optional.includes(dep))
  }
  for (const name of Object.keys(top.dependencies ?? {})) visit(name, dir, false)
  for (const name of Object.keys(top.optionalDependencies ?? {})) visit(name, dir, true)
  return [...found.values()].sort(byNameAndVersion)
}

const FONT = { name: 'Vazirmatn', license: 'OFL-1.1', licenseName: 'SIL Open Font License, Version 1.1' }

/** The font the page embeds: its notice (src/fonts/OFL.txt), the copyright line in it, and the files. */
export function readFont(dir) {
  const fonts = join(dir, 'src', 'fonts')
  const notice = join(fonts, 'OFL.txt')
  const body = isFile(notice) ? clean(readFileSync(notice, 'utf8')) : ''
  const copyright = body.split('\n').map((l) => l.trim()).find((l) => /^copyright/i.test(l)) ?? ''
  return {
    ...FONT,
    text: body,
    copyright,
    homepage: /\((https?:\/\/[^\s)]+)\)/.exec(copyright)?.[1] ?? '',
    files: existsSync(fonts) ? readdirSync(fonts).filter((n) => n.endsWith('.woff2')).sort() : [],
  }
}

/**
 * Whether an SPDX licence expression is covered by the allowed ids: "A OR B" needs one of them, "A AND B"
 * both. WITH, a trailing + and whatever else is not an id, AND, OR or a parenthesis is refused.
 */
export function licenseAllowed(expression, allowed = ALLOWED_LICENSES) {
  const ok = new Set(allowed.map((id) => id.toLowerCase()))
  const tokens = String(expression ?? '').match(/[()]|[^\s()]+/g) ?? []
  let i = 0
  const is = (word) => tokens[i]?.toUpperCase() === word
  const atom = () => {
    const t = tokens[i++]
    if (t === '(') {
      const v = or()
      if (tokens[i++] !== ')') throw new SyntaxError('unbalanced parenthesis')
      return v
    }
    if (t === undefined || t === ')' || ['AND', 'OR', 'WITH'].includes(t.toUpperCase())) throw new SyntaxError('an id was expected')
    return ok.has(t.toLowerCase())
  }
  const and = () => {
    let v = atom()
    while (is('AND')) {
      i++
      v = atom() && v
    }
    return v
  }
  const or = () => {
    let v = and()
    while (is('OR')) {
      i++
      v = and() || v
    }
    return v
  }
  try {
    const v = or()
    return v && i === tokens.length
  } catch {
    return false
  }
}

/** What keeps the notices from being written: a package with no usable notice or a licence not allowed, and a font without its notice. */
export function problems(packages, font) {
  const out = []
  const allowed = ALLOWED_LICENSES.join(', ')
  for (const p of packages) {
    const who = `${p.name || p.dir} ${p.version}`.trim()
    if (!p.name || !p.version) out.push(`${p.dir}: its package.json has no name or no version`)
    if (!licenseAllowed(p.license)) {
      out.push(`${who}: licence ${p.license ? `"${p.license}"` : '(none declared)'} is not one of ${allowed}; a new dependency needs a decision first`)
    }
    if (p.files.length) {
      if (!p.files.some((f) => f.text.length >= MIN_NOTICE)) out.push(`${who}: ${p.files.map((f) => f.name).join(', ')} is too short to be a licence text`)
    } else if (!p.header) {
      out.push(`${who}: it has no licence file and no comment at the top of its main file`)
    } else if (!COPYRIGHT.test(p.header.text)) {
      out.push(`${who}: it has no licence file, and the comment at the top of ${p.header.file} names no copyright holder`)
    }
  }
  if (!font.text) out.push(`the ${font.name} notice (src/fonts/OFL.txt) is missing or empty`)
  else {
    if (!font.copyright) out.push('src/fonts/OFL.txt has no copyright line')
    if (!/SIL OPEN FONT LICENSE/i.test(font.text)) out.push('src/fonts/OFL.txt is not the SIL Open Font License')
  }
  if (!font.files.length) out.push(`src/fonts holds no .woff2 files, so the page embeds no ${font.name}: take it out of notices.mjs`)
  return out
}

const RULE = '='.repeat(64)
const fields = (rows) => rows.filter(([, v]) => v).map(([k, v]) => `${`${k}:`.padEnd(12)}${v}`)

function packageBlock(p) {
  const lines = [RULE, `${p.name} ${p.version}`, RULE, '']
  lines.push(...fields([['License', p.header ? `${p.license} (as package.json declares it)` : p.license], ['Homepage', p.homepage], ['Repository', p.repository]]))
  if (p.header) {
    lines.push(
      '',
      'This package ships no licence file. Below is the comment at the top of its',
      `main source file, ${p.header.file}, which holds the copyright notice.`,
      '',
      `--- ${p.header.file}, top of the file ---`,
      '',
      p.header.text,
    )
  }
  for (const f of p.files) lines.push('', `--- ${f.name} ---`, '', f.text)
  return lines.join('\n')
}

function fontBlock(f) {
  return [
    RULE,
    `${f.name} (font)`,
    RULE,
    '',
    ...fields([['License', `${f.license} (${f.licenseName})`], ['Homepage', f.homepage], ['Copyright', f.copyright]]),
    '',
    `The page embeds a subset of ${f.name}: the font cut down to the glyphs its`,
    'text uses, in these files. The notice below goes with them.',
    '',
    ...f.files.map((n) => `  ${n}`),
    '',
    '--- OFL.txt ---',
    '',
    f.text,
  ].join('\n')
}

/** The notices as one text: a short preface, each package in the order given, then the font. */
export function format(packages, font) {
  const preface = [
    'The licences of the software built into the Remote Nodes web page: the',
    'libraries it bundles and the font it embeds. The page itself is free',
    'software under the GNU Affero General Public License, version 3 or',
    'later: see LICENSE.',
  ].join('\n')
  return `${[preface, ...packages.map(packageBlock), fontBlock(font)].join('\n\n')}\n`
}

/** Everything the notices of the project in dir are made of, what is wrong with it, and the text. */
export function prepare(dir) {
  const packages = collectPackages(dir)
  const font = readFont(dir)
  return { packages, font, problems: problems(packages, font), text: format(packages, font) }
}

// an import (the tests) only gets the functions
const ranDirectly = () => {
  try {
    return realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)
  } catch {
    return false
  }
}

if (ranDirectly()) {
  const out = resolve(process.argv[2] ?? join(webRoot, 'dist', 'NOTICES.txt'))
  let result
  try {
    result = prepare(webRoot)
  } catch (err) {
    console.error(`notices: ${err.message}`)
    process.exit(1)
  }
  if (result.problems.length) {
    for (const p of result.problems) console.error(`notices: ${p}`)
    process.exit(1)
  }
  mkdirSync(dirname(out), { recursive: true })
  writeFileSync(out, result.text)
  console.log(`${relative(process.cwd(), out)}: ${result.packages.length} packages and the ${result.font.name} font, ${Buffer.byteLength(result.text)} bytes`)
}
