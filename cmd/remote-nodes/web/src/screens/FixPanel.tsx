import { useState, type FormEvent, type ReactNode } from 'react'
import { KeyRound } from '../components/icons'
import { clearSecrets } from '../components/login'
import { Button, ErrorBox, Field, Input, Notice, Textarea, cx } from '../components/ui'
import { useConfig } from '../hooks/useConfig'
import { useLang } from '../i18n'
import type { ErrorInfo, InstallRequest, Job, Login } from '../lib/api'
import { fixFor, otherPort, withEntryPort, withHost, withHostKey, withLogin, withTakeOver, type Fix } from '../lib/fixes'
import { badPort } from '../lib/install'

// What a person can do about an install that failed, in the dialog that shows the failure. Each fix is a
// short explanation and what it needs (a port, a host, a login, a tick), and Continue sends the install again
// from the same dialog, with that one change. The dialog keeps the install it sent while it is open, and this
// panel only ever asks for the change: where the install is kept is the dialog's business.

/**
 * What the panel needs to know of the install that was sent, and nothing of its login: this goes in the page's
 * element tree, which keeps what it was given for as long as the page lives, and a password in it would stay.
 */
export interface Sent {
  sshPort: number
  sshUser: string
  hostKey: string
}

export interface FixProps {
  job: Job
  /** what was sent, while the dialog that sent it is open and can send it again; null when there is none */
  sent: Sent | null
  busy?: boolean
  /** sends the install again with this change; `probe` when it is for another server, whose key is read first */
  onFix?: (change: (r: InstallRequest) => InstallRequest, probe?: boolean) => void
  onOpenNode?: (id: number) => void
}

/** The panel for a failed install: nothing for a failure with nothing in particular to do. */
export function FixPanel(props: FixProps) {
  const { t } = useLang()
  const fix = fixFor(props.job)
  // nothing to ask for: a failure with nothing in particular to do, and the agent's port, which is the hub's setting
  if (!fix || fix.kind === 'again' || (fix.kind === 'port' && fix.role === 'agent')) return null
  return (
    <section aria-label={t('fix.title')} className="flex flex-col gap-3 border-t border-border pt-4">
      {!props.sent && <Notice>{t('fix.noLogin')}</Notice>}
      <Body fix={fix} {...props} />
    </section>
  )
}

function Body({ fix, sent, busy = false, onFix, onOpenNode }: FixProps & { fix: Fix }) {
  const { t } = useLang()
  const go = sent && onFix ? onFix : undefined
  switch (fix.kind) {
    case 'takeover':
      return (
        <div className="flex flex-col gap-4">
          <XrayFound info={fix.info} />
          <Notice tone="warn">{t('fix.takeover.warning')}</Notice>
          {go && (
            <Button variant="primary" busy={busy} onClick={() => go(withTakeOver)}>
              {t('fix.takeover.go')}
            </Button>
          )}
          {go && <AnotherServer busy={busy} onGo={(host) => go((r) => withHost(r, host), true)} />}
        </div>
      )
    case 'stop':
      return (
        <div className="flex flex-col gap-3">
          <p className="text-[13px]">{t('fix.stop.text')}</p>
          {go && (
            <Button variant="primary" busy={busy} onClick={() => go((r) => r)}>
              {t('fix.continue')}
            </Button>
          )}
        </div>
      )
    case 'port':
      return go ? <EntryPort fix={fix} sent={sent!} busy={busy} onGo={(port) => go((r) => withEntryPort(r, port))} /> : null
    case 'login':
      return go ? <LoginAgain sent={sent!} busy={busy} onGo={(login) => go((r) => withLogin(r, login))} /> : null
    case 'hostkey':
      return <HostKey fix={fix} busy={busy} onGo={go ? () => go((r) => withHostKey(r, fix.shown)) : undefined} />
    case 'server':
      return (
        <div className="flex flex-col gap-3">
          {fix.why === 'node' && fix.nodeId > 0 && onOpenNode && (
            <div>
              <Button onClick={() => onOpenNode(fix.nodeId)}>{t('fix.openNode')}</Button>
            </div>
          )}
          {go && <AnotherServer busy={busy} onGo={(host) => go((r) => withHost(r, host), true)} />}
        </div>
      )
  }
}

/** What was found of the server's own Xray, and what becomes of its program. */
function XrayFound({ info }: { info: ErrorInfo }) {
  const { t } = useLang()
  const pinned = useConfig()?.xray ?? ''
  const found = info.found ?? []
  const version = info.xrayVersion
  const hasBinary = found.length === 0 || found.includes('binary')
  const line = !hasBinary
    ? t('fix.xray.noBinary', { pinned })
    : !version
      ? t('fix.xray.unknown')
      : t(info.sameBuild ? 'fix.xray.same' : 'fix.xray.other', { version })
  return (
    <div className="flex flex-col gap-1.5 text-[13px]">
      {found.length > 0 && (
        <>
          <span className="font-medium">{t('fix.xray.found')}</span>
          <ul className="list-disc ps-5 text-muted">
            {found.map((f) => (
              <li key={f}>{t(`fix.found.${f}`)}</li>
            ))}
          </ul>
        </>
      )}
      <p>{line}</p>
    </div>
  )
}

/** A host field and Continue, for the same install on another server. */
function AnotherServer({ busy, onGo }: { busy: boolean; onGo: (host: string) => void }) {
  const { t } = useLang()
  const [host, setHost] = useState('')
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (host.trim()) onGo(host.trim())
  }
  return (
    <form onSubmit={submit} className="flex flex-col gap-2 border-t border-border pt-3" autoComplete="off">
      <h3 className="text-[13px] font-semibold">{t('fix.other.title')}</h3>
      <div className="flex items-end gap-2">
        <Field label={t('add.host')} className="min-w-0 flex-1">
          {(p) => (
            <Input
              {...p}
              dir="ltr"
              className="font-mono"
              value={host}
              onChange={(e) => setHost(e.currentTarget.value)}
              placeholder="203.0.113.10"
              spellCheck={false}
              autoCapitalize="off"
            />
          )}
        </Field>
        <Button type="submit" busy={busy} disabled={!host.trim()}>
          {t('fix.continue')}
        </Button>
      </div>
    </form>
  )
}

/** Another entry port, with one suggested. */
function EntryPort({ fix, sent, busy, onGo }: { fix: Extract<Fix, { kind: 'port' }>; sent: Sent; busy: boolean; onGo: (port: number) => void }) {
  const { t } = useLang()
  const [port, setPort] = useState(() => String(otherPort(fix.port, sent.sshPort)))
  const n = Number(port)
  const bad = badPort(n, sent.sshPort) || n === fix.port
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!bad) onGo(n)
  }
  return (
    <form onSubmit={submit} className="flex flex-col gap-3" autoComplete="off">
      <Field label={t('fix.port.entryLabel')} hint={t('fix.port.entryHint', { port: String(fix.port) })}>
        {(p) => <Input {...p} dir="ltr" className="font-mono" inputMode="numeric" value={port} onChange={(e) => setPort(e.currentTarget.value)} />}
      </Field>
      <div>
        <Button type="submit" variant="primary" busy={busy} disabled={bad}>
          {t('fix.continue')}
        </Button>
      </div>
    </form>
  )
}

/**
 * The login again, with the port and user it had. The password, the key and its passphrase are typed into fields
 * the page does not keep a copy of: nothing holds them but the fields themselves, until Continue reads them, so
 * no part of the page's state or element tree has a secret in it to leave behind.
 */
function LoginAgain({ sent, busy, onGo }: { sent: Sent; busy: boolean; onGo: (login: Login) => void }) {
  const { t } = useLang()
  const [port, setPort] = useState(String(sent.sshPort))
  const [user, setUser] = useState(sent.sshUser)
  const [method, setMethod] = useState<'password' | 'key'>('password')
  const [problem, setProblem] = useState<string | null>(null)
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    // the fields are read here, once, and nowhere else
    const field = (name: string) => (e.currentTarget.elements.namedItem(name) as HTMLInputElement | HTMLTextAreaElement | null)?.value ?? ''
    const n = Number(port)
    const secret = method === 'password' ? field('password') : field('privateKey')
    const bad = !Number.isInteger(n) || n < 1 || n > 65535 ? 'login.errPort' : !user.trim() ? 'login.errUser' : !secret.trim() ? (method === 'password' ? 'login.errPassword' : 'login.errKey') : null
    setProblem(bad ? t(bad) : null)
    if (bad) return
    const login: Login = { sshPort: n, sshUser: user.trim(), hostKey: sent.hostKey }
    if (method === 'password') login.password = secret
    else {
      login.privateKey = secret
      const phrase = field('passphrase')
      if (phrase) login.passphrase = phrase
    }
    // read: the fields have nothing more to keep
    clearSecrets(e.currentTarget)
    onGo(login)
  }
  const pick = (m: 'password' | 'key') => (
    <button
      type="button"
      role="radio"
      aria-checked={method === m}
      tabIndex={method === m ? 0 : -1}
      onClick={() => setMethod(m)}
      className={cx(
        'flex-1 whitespace-nowrap rounded-[6px] px-1 text-[12.5px] font-medium transition sm:px-2 sm:text-[13px]',
        'focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent',
        method === m ? 'bg-card2 text-foreground shadow-sm' : 'text-muted hover:text-foreground',
      )}
    >
      {t(m === 'password' ? 'login.password' : 'login.privateKey')}
    </button>
  )
  return (
    <form onSubmit={submit} className="flex flex-col gap-3" autoComplete="off">
      <h3 className="text-[13px] font-semibold">{t('fix.login.title')}</h3>
      <div className="grid grid-cols-6 gap-3">
        <Field label={t('login.sshPort')} className="col-span-2">
          {(p) => <Input {...p} dir="ltr" className="font-mono" type="number" min={1} max={65535} value={port} onChange={(e) => setPort(e.currentTarget.value)} />}
        </Field>
        <Field label={t('login.username')} className="col-span-4">
          {(p) => <Input {...p} dir="ltr" className="font-mono" value={user} onChange={(e) => setUser(e.currentTarget.value)} spellCheck={false} autoCapitalize="off" />}
        </Field>
        <div className="col-span-6 flex flex-col gap-1.5">
          <span className="text-[13px] font-medium text-muted">{t('login.with')}</span>
          <div role="radiogroup" aria-label={t('login.with')} className="flex h-11 rounded-md border border-border-strong p-0.5 sm:h-9">
            {pick('password')}
            {pick('key')}
          </div>
        </div>
        {method === 'password' ? (
          <Field label={t('login.password')} className="col-span-6">
            {(p) => <Input {...p} name="password" dir="ltr" required type="password" autoComplete="new-password" />}
          </Field>
        ) : (
          <>
            <Field label={t('login.privateKey')} hint={t('login.keyHint')} className="col-span-6">
              {(p) => <Textarea {...p} name="privateKey" dir="ltr" required spellCheck={false} autoComplete="off" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" />}
            </Field>
            <Field label={t('login.passphrase')} hint={t('login.passphraseHint')} className="col-span-6">
              {(p) => <Input {...p} name="passphrase" dir="ltr" type="password" autoComplete="new-password" />}
            </Field>
          </>
        )}
      </div>
      {problem && <ErrorBox>{problem}</ErrorBox>}
      <div>
        <Button type="submit" variant="primary" busy={busy}>
          {t('fix.continue')}
        </Button>
      </div>
    </form>
  )
}

/** The two keys, and the same tick the add dialog wants for a key that changed, before the new one is trusted. */
function HostKey({ fix, busy, onGo }: { fix: Extract<Fix, { kind: 'hostkey' }>; busy: boolean; onGo?: () => void }) {
  const { t } = useLang()
  const [acked, setAcked] = useState(false)
  const key = (label: string, value: string): ReactNode => (
    <div className="flex flex-col gap-1">
      <span className="text-xs font-medium text-muted">{label}</span>
      <div dir="ltr" className="flex items-center gap-2 break-all rounded-md bg-card2 p-3 font-mono text-xs">
        <KeyRound className="h-4 w-4 shrink-0 text-muted" />
        {value}
      </div>
    </div>
  )
  return (
    <div className="flex flex-col gap-3">
      {key(t('fix.hostkey.agreed'), fix.pinned)}
      {key(t('fix.hostkey.shows'), fix.shown)}
      {onGo && (
        <>
          <label className="flex min-h-[44px] items-center gap-2 text-[13px] sm:min-h-0">
            <input type="checkbox" className="h-4 w-4" checked={acked} onChange={(e) => setAcked(e.currentTarget.checked)} />
            {t('hostkey.ack')}
          </label>
          <div>
            <Button variant="danger" busy={busy} disabled={!acked} onClick={onGo}>
              {t('fix.hostkey.go')}
            </Button>
          </div>
        </>
      )}
    </div>
  )
}
