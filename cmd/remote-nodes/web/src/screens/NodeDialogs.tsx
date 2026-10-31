import { useState, type ReactNode } from 'react'
import { useConfig } from '../hooks/useConfig'
import {
  HostKeyCheck,
  LoginFields,
  emptyLogin,
  forgetOnly,
  keyNeedsTrust,
  keyTrusted,
  loginError,
  removeWithLogin,
  withoutSecrets,
  type LoginForm,
} from '../components/login'
import { Badge, Button, Confirm, Dialog, ErrorBox, Field, Input, Notice, cx, useToast } from '../components/ui'
import { useErrorText, useLang, type T } from '../i18n'
import { ApiError, MAX_NAME_LENGTH, api, type HandshakeCheck, type Node, type ProbeResult, type Started } from '../lib/api'
import { normalizeTarget, openConnections } from '../lib/node'

/** Starts a job and shows it; throws when the hub refuses. The node page provides it. */
export type StartJob = (call: () => Promise<Started>) => Promise<void>

/** A confirmation's call, with the hub's refusal in the page's words. */
function useConfirmed() {
  const errorText = useErrorText()
  return (start: StartJob, call: () => Promise<Started>) => async () => {
    try {
      await start(call)
    } catch (e) {
      // Confirm shows what is thrown
      throw new Error(errorText(e))
    }
  }
}

/** The number a confirmation turns on, big, with what it means; the number is in the language's digits. */
function Impact({ n, children }: { n: number; children: ReactNode }) {
  const { fmt } = useLang()
  return (
    <div className={cx('flex items-center gap-3 rounded-md p-3 text-[13px] text-foreground', n ? 'bg-warn-tint' : 'bg-card2')}>
      <span className={cx('tabular text-2xl font-semibold', n ? 'text-warn-text' : 'text-muted')}>{fmt.num(n)}</span>
      <span>{children}</span>
    </div>
  )
}

/** What a restart costs, after the count: who is cut off. */
const dropText = (t: T, n: number) => (n ? t('drop.some', { n }) : t('drop.none'))

// --- updates, keys and the certificate ---------------------------------------------

export function UpdateDialog({ node, what, start, onClose }: { node: Node; what: 'agent' | 'xray'; start: StartJob; onClose: () => void }) {
  const { t } = useLang()
  const confirmed = useConfirmed()
  const n = openConnections(node)
  const router = useConfig()?.xray
  const version = what === 'agent' ? node.updates.agentVersion : node.updates.xrayVersion || router || ''
  return (
    <Confirm
      title={t(what === 'agent' ? 'update.agent.title' : 'update.xray.title', { name: node.name, version })}
      action={t(what === 'agent' ? 'menu.updateAgent' : 'menu.updateXray')}
      onClose={onClose}
      onConfirm={confirmed(start, () => api.update(node.id, what))}
    >
      <Impact n={n}>{dropText(t, n)}</Impact>
      <p>{t('update.note')}</p>
    </Confirm>
  )
}

export function RotateDialog({ node, start, onClose }: { node: Node; start: StartJob; onClose: () => void }) {
  const { t } = useLang()
  const confirmed = useConfirmed()
  const n = node.subscriptionCount
  return (
    <Confirm
      danger
      title={t('rotate.title', { name: node.name })}
      action={t('rotate.action')}
      onClose={onClose}
      onConfirm={confirmed(start, () => api.rotateKeys(node.id))}
    >
      <Impact n={n}>{t(n ? 'links.some' : 'links.none', { n })}</Impact>
      <p>{t('rotate.note')}</p>
    </Confirm>
  )
}

export function RenewDialog({ node, start, onClose }: { node: Node; start: StartJob; onClose: () => void }) {
  const { t } = useLang()
  const confirmed = useConfirmed()
  return (
    <Confirm title={t('renew.title', { name: node.name })} action={t('renew.action')} onClose={onClose} onConfirm={confirmed(start, () => api.renewCert(node.id))}>
      <p>{t('renew.note')}</p>
    </Confirm>
  )
}

// --- changes that make every link new ------------------------------------------------

/** What changing a port, a handshake target or a server name does to the links. */
function LinksNotice() {
  const { t } = useLang()
  return (
    <Notice tone="warn">
      <b>{t('links.warnTitle')}</b> {t('links.warnBody')}
    </Notice>
  )
}

export function PortDialog({ node, start, onClose }: { node: Node; start: StartJob; onClose: () => void }) {
  const { t } = useLang()
  const errorText = useErrorText()
  const [port, setPort] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const text = port.trim()
  const n = /^\d{1,5}$/.test(text) ? Number(text) : NaN
  // the port subscriptions connect to: 1 to 65535, and not the one SSH uses
  const problem = !text ? null : !(n >= 1 && n <= 65535) || n === node.sshPort ? t('add.errEntryPort') : n === node.entryPort ? t('port.same') : null
  const ready = !!text && !problem
  const save = async () => {
    setBusy(true)
    setError(null)
    try {
      await start(() => api.settings(node.id, { entryPort: n }))
    } catch (e) {
      setError(errorText(e))
      setBusy(false)
    }
  }
  return (
    <Dialog
      onClose={onClose}
      title={t('port.title')}
      description={t('port.desc', { name: node.name, port: String(node.entryPort) })}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="primary" type="submit" form="entry-port" busy={busy} disabled={!ready}>
            {t('settings.apply')}
          </Button>
        </>
      }
    >
      <form
        id="entry-port"
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault()
          if (ready) void save()
        }}
      >
        <Field label={t('add.entryPort')} error={problem} hint={t('add.entryPortHint')}>
          {(p) => <Input {...p} dir="ltr" className="font-mono" inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value)} placeholder="8443" />}
        </Field>
        <LinksNotice />
        {error && <ErrorBox>{error}</ErrorBox>}
      </form>
    </Dialog>
  )
}

/** Change the handshake target in steps: type it, have the node check it, then apply it. */
export function HandshakeDialog({ node, start, onClose }: { node: Node; start: StartJob; onClose: () => void }) {
  const { t, fmt } = useLang()
  const errorText = useErrorText()
  const [input, setInput] = useState(node.handshakeTarget)
  const [check, setCheck] = useState<{ target: string; result: HandshakeCheck } | null>(null)
  const [busy, setBusy] = useState<'check' | 'save' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const wanted = normalizeTarget(input)
  // a result is for what was checked, not for what is typed now
  const result = check && check.target === wanted ? check.result : null

  const runCheck = async () => {
    setBusy('check')
    setError(null)
    setCheck(null)
    try {
      setCheck({ target: wanted, result: await api.handshakeCheck(node.id, wanted) })
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(null)
    }
  }
  const save = async () => {
    setBusy('save')
    setError(null)
    try {
      await start(() => api.settings(node.id, { handshakeTarget: wanted }))
    } catch (e) {
      setError(errorText(e))
      setBusy(null)
    }
  }
  const invalid = !!input.trim() && !wanted
  return (
    <Dialog
      onClose={onClose}
      title={t('hs.title')}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="primary" busy={busy === 'save'} disabled={!result?.ok || wanted === node.handshakeTarget || !!busy} onClick={save}>
            {t('settings.apply')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('hs.label')} error={invalid ? t('hs.invalid', { example: 'www.example.com:443' }) : null} hint={t('hs.hint', { tls: 'TLS 1.3' })}>
          {(p) => (
            <div className="flex gap-2">
              <Input
                {...p}
                dir="ltr"
                className="font-mono"
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && wanted && !busy && void runCheck()}
                spellCheck={false}
                autoCapitalize="off"
                placeholder="www.example.com:443"
              />
              <Button busy={busy === 'check'} disabled={!wanted || !!busy} onClick={runCheck}>
                {result ? t('hs.checkAgain') : t('hs.check')}
              </Button>
            </div>
          )}
        </Field>
        {result && (
          <div role="status" className="flex flex-col gap-2 text-[13px]">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone={result.ok ? 'online' : 'danger'}>{result.ok ? t('hs.working') : t('hs.failing')}</Badge>
              {result.ok && (
                <span className="text-xs text-faint">
                  {t('hs.facts', { tls: ['TLS 1.3', 'X25519'].join(' · '), size: fmt.bytes(result.handshakeBytes) })}
                </span>
              )}
            </div>
            {!result.ok && result.error && <ErrorBox>{result.error}</ErrorBox>}
            {wanted === node.handshakeTarget && <p className="text-xs text-faint">{t('hs.same')}</p>}
          </div>
        )}
        <LinksNotice />
        {error && <ErrorBox>{error}</ErrorBox>}
      </div>
    </Dialog>
  )
}

/** Confirms a new server name (the empty one is the handshake target's own). */
export function ServerNameDialog({ node, sni, start, onClose }: { node: Node; sni: string; start: StartJob; onClose: () => void }) {
  const { t } = useLang()
  const confirmed = useConfirmed()
  const n = node.subscriptionCount
  return (
    <Confirm title={t('sni.title', { name: node.name })} action={t('sni.action')} onClose={onClose} onConfirm={confirmed(start, () => api.settings(node.id, { sni }))}>
      <p>{sni ? t('sni.now', { sni }) : t('sni.same', { target: node.handshakeTarget.split(':')[0] })}</p>
      <Impact n={n}>{t(n ? 'links.some' : 'links.none', { n })}</Impact>
    </Confirm>
  )
}

// --- rename --------------------------------------------------------------------------

export function RenameDialog({ node, onClose, onRenamed }: { node: Node; onClose: () => void; onRenamed: (saved: Node) => void }) {
  const { t } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const [name, setName] = useState(node.name)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const save = async () => {
    setBusy(true)
    setError(null)
    try {
      const saved = await api.rename(node.id, name.trim())
      toast(t('rename.done', { name: saved.name }))
      onRenamed(saved)
      onClose()
    } catch (e) {
      setError(errorText(e))
      setBusy(false)
    }
  }
  const same = name.trim() === node.name
  return (
    <Dialog
      onClose={onClose}
      title={t('rename.title')}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="primary" busy={busy} disabled={!name.trim() || same} onClick={save}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('add.name')}>
          {(p) => <Input {...p} dir="auto" value={name} maxLength={MAX_NAME_LENGTH} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && name.trim() && !same && void save()} />}
        </Field>
        {error && <ErrorBox>{error}</ErrorBox>}
      </div>
    </Dialog>
  )
}

// --- remove --------------------------------------------------------------------------

/**
 * Looks at the node's SSH host key before a login goes out: a known or first key goes
 * straight on, a changed one is shown first (a rebuilt server has a new one).
 */
function useHostKey(node: Node, login: LoginForm) {
  const [probe, setProbe] = useState<ProbeResult | null>(null)
  const [acked, setAcked] = useState(false)
  return {
    probe,
    acked,
    setAcked,
    /** The key to send, or null while the user has to look at it first. */
    check: async (): Promise<string | null> => {
      const p = await api.probe({ host: node.host, sshPort: Number(login.sshPort), sshUser: login.sshUser.trim() })
      if (!keyNeedsTrust(p)) return p.fingerprint
      setProbe(p)
      setAcked(false)
      return null
    },
    back: () => setProbe(null),
    trusted: probe !== null && keyTrusted(probe, acked),
  }
}

/**
 * Takes a node out. The agent and Xray are on the server, and the hub can't take them off by itself, so
 * this asks for an SSH login first (its host key looked at before it goes out), or forgets the node and
 * leaves the server as it is.
 *
 * As `cleanup` it is an unfinished install's Clean up: the same login and key check, then the hub takes
 * off what the install put there and deletes the record. Forgetting one has a dialog of its own.
 */
export function RemoveDialog({
  node,
  cleanup = false,
  onClose,
  onStarted,
}: {
  node: Node
  cleanup?: boolean
  onClose: () => void
  onStarted: (jobId: string) => void
}) {
  const { t } = useLang()
  const errorText = useErrorText()
  const [login, setLogin] = useState<LoginForm>(() => emptyLogin(node.sshPort, node.sshUser))
  const [busy, setBusy] = useState<'remove' | 'force' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const key = useHostKey(node, login)
  const probe = key.probe

  const start = async (how: 'remove' | 'force', hostKey?: string) => {
    if (how === 'remove') {
      const problem = loginError(login)
      if (problem) return setError(t(problem))
    }
    setBusy(how)
    setError(null)
    try {
      let req = forgetOnly()
      if (how === 'remove') {
        const fingerprint = hostKey ?? (await key.check())
        // a key that changed is shown first, and Trust and remove comes back here with it
        if (fingerprint === null) return
        req = removeWithLogin(login, fingerprint)
      }
      const res = await api.remove(node.id, req)
      setLogin(withoutSecrets)
      onStarted(res.jobId)
    } catch (e) {
      // every request carries a login or force, so the hub has nothing to ask for; if it asks anyway, the login form is where it goes on
      if (e instanceof ApiError && e.needLogin) key.back()
      setError(errorText(e))
    } finally {
      setBusy(null)
    }
  }

  const footer = probe ? (
    <>
      <Button onClick={key.back}>{t('add.back')}</Button>
      <Button variant="danger" disabled={!key.trusted || !!busy} busy={busy === 'remove'} onClick={() => void start('remove', probe.fingerprint)}>
        {t(cleanup ? 'cleanup.trust' : 'remove.trust')}
      </Button>
    </>
  ) : (
    <>
      <Button onClick={onClose}>{t('common.cancel')}</Button>
      <Button variant="danger" type="submit" form="remove-login" busy={busy === 'remove'} disabled={!!busy}>
        {t(cleanup ? 'cleanup.login' : 'remove.login')}
      </Button>
    </>
  )

  return (
    <Dialog onClose={onClose} title={cleanup ? t('cleanup.title', { host: node.host }) : t('remove.title', { name: node.name })} footer={footer}>
      {probe ? (
        <HostKeyCheck host={node.host} probe={probe} acked={key.acked} onAck={key.setAcked} />
      ) : (
        <form
          id="remove-login"
          className="grid grid-cols-6 gap-3"
          autoComplete="off"
          onSubmit={(e) => {
            e.preventDefault()
            void start('remove')
          }}
        >
          <p className="col-span-6 text-[13px] text-muted">{t(cleanup ? 'cleanup.note' : 'remove.note')}</p>
          {!cleanup && (
            <div className="col-span-6">
              <Notice>{t('remove.needsLogin')}</Notice>
            </div>
          )}
          <LoginFields form={login} onChange={setLogin} />
          {!cleanup && (
            <div className="col-span-6 flex flex-wrap items-center justify-between gap-2 border-t border-border pt-3 text-[13px] text-muted">
              <span>{t('remove.noLogin')}</span>
              <Button size="sm" busy={busy === 'force'} disabled={!!busy} onClick={() => start('force')}>
                {t('remove.forget')}
              </Button>
            </div>
          )}
        </form>
      )}
      {error && (
        <div className="mt-3">
          <ErrorBox>{error}</ErrorBox>
        </div>
      )}
    </Dialog>
  )
}

/** Forgets an unfinished install: the record goes, and the server is left as it is, which it says. */
export function ForgetDialog({ node, onClose, onStarted }: { node: Node; onClose: () => void; onStarted: (jobId: string) => void }) {
  const { t } = useLang()
  const errorText = useErrorText()
  return (
    <Confirm
      danger
      title={t('forget.title', { name: node.name })}
      action={t('forget.action')}
      onClose={onClose}
      onConfirm={async () => {
        try {
          const { jobId } = await api.remove(node.id, forgetOnly())
          onStarted(jobId)
        } catch (e) {
          throw new Error(errorText(e))
        }
      }}
    >
      <p>{t('forget.note')}</p>
    </Confirm>
  )
}
