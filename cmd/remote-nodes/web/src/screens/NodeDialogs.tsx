import { useState, type ReactNode } from 'react'
import { useConfig } from '../hooks/useConfig'
import {
  HostKeyCheck,
  LoginFields,
  emptyLogin,
  keyNeedsTrust,
  keyTrusted,
  loginError,
  toLogin,
  withoutSecrets,
  type LoginForm,
} from '../components/login'
import { Button, Confirm, Dialog, ErrorBox, Field, Input, Notice, cx, useToast } from '../components/ui'
import { useErrorText, useLang, type T } from '../i18n'
import { ApiError, api, type Node, type ProbeResult, type RemoveRequest, type Started } from '../lib/api'
import { openConnections } from '../lib/node'

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
      <Impact n={n}>{t(n ? 'rotate.some' : 'rotate.none', { n })}</Impact>
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
          {(p) => <Input {...p} dir="auto" value={name} maxLength={100} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && name.trim() && !same && void save()} />}
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
 * Takes a node out. A node the router can reach is removed with its agent; one it can't asks for an
 * SSH login (with its host key checked first), or can be forgotten without touching the server.
 */
export function RemoveDialog({ node, onClose, onStarted }: { node: Node; onClose: () => void; onStarted: (jobId: string) => void }) {
  const { t } = useLang()
  const errorText = useErrorText()
  const [stage, setStage] = useState<'confirm' | 'login'>('confirm')
  const [login, setLogin] = useState<LoginForm>(() => emptyLogin(node.sshPort, node.sshUser))
  const [busy, setBusy] = useState<'remove' | 'force' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const key = useHostKey(node, login)
  const probe = stage === 'login' ? key.probe : null

  const start = async (how: 'remove' | 'force', hostKey?: string) => {
    const withLogin = stage === 'login' && how === 'remove'
    if (withLogin) {
      const problem = loginError(login)
      if (problem) return setError(t(problem))
    }
    setBusy(how)
    setError(null)
    try {
      let req: RemoveRequest = {}
      if (how === 'force') req = { force: true }
      else if (withLogin) {
        const fingerprint = hostKey ?? (await key.check())
        if (fingerprint === null) return
        req = { login: toLogin(login, fingerprint) }
      }
      const res = await api.remove(node.id, req)
      setLogin(withoutSecrets)
      onStarted(res.jobId)
    } catch (e) {
      if (e instanceof ApiError && e.needLogin) setStage('login')
      else setError(errorText(e))
    } finally {
      setBusy(null)
    }
  }

  const footer = probe ? (
    <>
      <Button onClick={key.back}>{t('add.back')}</Button>
      <Button variant="danger" disabled={!key.trusted || !!busy} busy={busy === 'remove'} onClick={() => void start('remove', probe.fingerprint)}>
        {t('remove.trust')}
      </Button>
    </>
  ) : stage === 'confirm' ? (
    <>
      <Button onClick={onClose}>{t('common.cancel')}</Button>
      <Button variant="danger" busy={busy === 'remove'} onClick={() => start('remove')}>
        {t('remove.action')}
      </Button>
    </>
  ) : (
    <>
      <Button onClick={onClose}>{t('common.cancel')}</Button>
      <Button variant="danger" type="submit" form="remove-login" busy={busy === 'remove'} disabled={!!busy}>
        {t('remove.login')}
      </Button>
    </>
  )

  return (
    <Dialog onClose={onClose} title={t('remove.title', { name: node.name })} footer={footer}>
      {stage === 'confirm' && <p className="text-[13px] text-muted">{t('remove.note')}</p>}
      {probe && <HostKeyCheck host={node.host} probe={probe} acked={key.acked} onAck={key.setAcked} />}
      {stage === 'login' && !probe && (
        <form
          id="remove-login"
          className="grid grid-cols-6 gap-3"
          autoComplete="off"
          onSubmit={(e) => {
            e.preventDefault()
            void start('remove')
          }}
        >
          <div className="col-span-6">
            <Notice tone="warn">{t('remove.needsLogin')}</Notice>
          </div>
          <LoginFields form={login} onChange={setLogin} />
          <div className="col-span-6 flex flex-wrap items-center justify-between gap-2 border-t border-border pt-3 text-[13px] text-muted">
            <span>{t('remove.noLogin')}</span>
            <Button size="sm" busy={busy === 'force'} disabled={!!busy} onClick={() => start('force')}>
              {t('remove.forget')}
            </Button>
          </div>
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
