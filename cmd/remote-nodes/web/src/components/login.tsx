import { useId, type KeyboardEvent, type ReactNode } from 'react'
import { useLang, type MsgKey } from '../i18n'
import type { Login, ProbeResult } from '../lib/api'
import { KeyRound, TriangleAlert } from './icons'
import { Field, Input, Textarea, cx } from './ui'

// Lives only in component state for one submission; never persisted.
export interface LoginForm {
  sshPort: string
  sshUser: string
  method: 'password' | 'key'
  password: string
  privateKey: string
  passphrase: string
}

export const emptyLogin = (sshPort = 22, sshUser = 'root'): LoginForm => ({
  sshPort: String(sshPort || 22),
  sshUser: sshUser || 'root',
  method: 'password',
  password: '',
  privateKey: '',
  passphrase: '',
})

export const withoutSecrets = (f: LoginForm): LoginForm => ({
  ...f,
  password: '',
  privateKey: '',
  passphrase: '',
})

/** What is wrong with the login, as the key of the message to show, or null. */
export function loginError(f: LoginForm): MsgKey | null {
  const port = Number(f.sshPort)
  if (!Number.isInteger(port) || port < 1 || port > 65535) return 'login.errPort'
  if (!f.sshUser.trim()) return 'login.errUser'
  if (f.method === 'password' && !f.password) return 'login.errPassword'
  if (f.method === 'key' && !f.privateKey.trim()) return 'login.errKey'
  return null
}

/** The login to send: only the secret that was chosen. */
export function toLogin(f: LoginForm, hostKey: string): Login {
  const login: Login = { sshPort: Number(f.sshPort), sshUser: f.sshUser.trim(), hostKey }
  if (f.method === 'password') login.password = f.password
  else {
    login.privateKey = f.privateKey
    if (f.passphrase) login.passphrase = f.passphrase
  }
  return login
}

const METHODS = [
  ['password', 'login.password'],
  ['key', 'login.privateKey'],
] as const

/** SSH port, user and a password or private key, laid out on a 6-column grid. */
export function LoginFields({
  form,
  onChange,
  host,
  afterHost,
}: {
  form: LoginForm
  onChange: (f: LoginForm) => void
  /** A host field to put in front of the port, on the same row. */
  host?: ReactNode
  /** A line under the host and port row. */
  afterHost?: ReactNode
}) {
  const { t } = useLang()
  const methodId = useId()
  const set = (k: keyof LoginForm) => (e: { target: { value: string } }) => onChange({ ...form, [k]: e.target.value })
  // arrow keys move between the two choices, as in any radio group
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)) return
    e.preventDefault()
    const next = form.method === 'password' ? 1 : 0
    onChange({ ...form, method: METHODS[next][0] })
    e.currentTarget.querySelectorAll<HTMLElement>('[role="radio"]')[next]?.focus()
  }
  // what is typed here is Latin (names, numbers, keys), so it reads left to right in a right-to-left page too
  return (
    <>
      {host}
      <Field label={t('login.sshPort')} className="col-span-2">
        {(p) => <Input {...p} dir="ltr" className="font-mono" required type="number" min={1} max={65535} value={form.sshPort} onChange={set('sshPort')} />}
      </Field>
      {afterHost}
      <Field label={t('login.username')} className={host ? 'col-span-3' : 'col-span-4'}>
        {(p) => <Input {...p} dir="ltr" className="font-mono" required value={form.sshUser} onChange={set('sshUser')} spellCheck={false} autoCapitalize="off" />}
      </Field>
      <div className={cx('flex flex-col gap-1.5', host ? 'col-span-3' : 'col-span-6')}>
        <span id={methodId} className="text-[13px] font-medium text-muted">
          {t('login.with')}
        </span>
        <div
          role="radiogroup"
          aria-labelledby={methodId}
          onKeyDown={onKey}
          className="flex h-11 rounded-md border border-border-strong p-0.5 sm:h-9"
        >
          {METHODS.map(([m, label]) => (
            <button
              key={m}
              type="button"
              role="radio"
              aria-checked={form.method === m}
              tabIndex={form.method === m ? 0 : -1}
              onClick={() => onChange({ ...form, method: m })}
              className={cx(
                'flex-1 whitespace-nowrap rounded-[6px] px-1 text-[12.5px] font-medium transition sm:px-2 sm:text-[13px]',
                'focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent',
                form.method === m ? 'bg-card2 text-foreground shadow-sm' : 'text-muted hover:text-foreground',
              )}
            >
              {t(label)}
            </button>
          ))}
        </div>
      </div>
      {form.method === 'password' ? (
        <Field label={t('login.password')} className="col-span-6">
          {(p) => <Input {...p} dir="ltr" required type="password" value={form.password} onChange={set('password')} autoComplete="new-password" />}
        </Field>
      ) : (
        <>
          <Field label={t('login.privateKey')} hint={t('login.keyHint')} className="col-span-6">
            {(p) => (
              <Textarea
                {...p}
                dir="ltr"
                required
                value={form.privateKey}
                onChange={set('privateKey')}
                spellCheck={false}
                autoComplete="off"
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
              />
            )}
          </Field>
          <Field label={t('login.passphrase')} hint={t('login.passphraseHint')} className="col-span-6">
            {(p) => <Input {...p} dir="ltr" type="password" value={form.passphrase} onChange={set('passphrase')} autoComplete="new-password" />}
          </Field>
        </>
      )}
    </>
  )
}

/** A first key is trusted and pinned, like ssh does; only a key that changed stops to ask. */
export const keyNeedsTrust = (p: ProbeResult) => p.status === 'changed'

/** A changed key needs the box ticked. */
export const keyTrusted = (p: ProbeResult, acked: boolean) => p.status !== 'changed' || acked

/** The warning for a host key that differs from the pinned one. */
export function HostKeyCheck({
  host,
  probe,
  acked,
  onAck,
}: {
  host: string
  probe: ProbeResult
  acked: boolean
  onAck: (acked: boolean) => void
}) {
  const { t } = useLang()
  return (
    <div className="flex flex-col gap-3">
      <div className="flex gap-3 rounded-md border border-warn bg-warn-tint p-3">
        <TriangleAlert className="h-5 w-5 shrink-0 text-warn-text" />
        <p className="text-[13px]">
          <b dir="ltr">{host}</b> {t('hostkey.changed')}
        </p>
      </div>
      <div dir="ltr" className="flex items-center gap-2 break-all rounded-md bg-card2 p-3 font-mono text-xs">
        <KeyRound className="h-4 w-4 shrink-0 text-muted" />
        {probe.fingerprint}
      </div>
      <label className="flex min-h-[44px] items-center gap-2 text-[13px] sm:min-h-0">
        <input type="checkbox" className="h-4 w-4" checked={acked} onChange={(e) => onAck(e.target.checked)} />
        {t('hostkey.ack')}
      </label>
    </div>
  )
}
