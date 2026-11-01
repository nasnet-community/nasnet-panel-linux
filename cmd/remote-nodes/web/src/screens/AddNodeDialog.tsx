import { useEffect, useRef, useState, type FormEvent, type MutableRefObject } from 'react'
import { KeyRound } from '../components/icons'
import { JobSteps } from '../components/job'
import { useJobs } from '../components/jobs'
import {
  HostKeyCheck,
  LoginFields,
  emptyLogin,
  clearSecrets,
  keyNeedsTrust,
  keyTrusted,
  loginError,
  toLogin,
  withoutSecrets,
  type LoginForm,
} from '../components/login'
import { Button, Dialog, Disclosure, ErrorBox, Field, Input, useToast } from '../components/ui'
import { useConfig } from '../hooks/useConfig'
import { useNow } from '../hooks/usePoll'
import { useErrorText, useLang, type T } from '../i18n'
import { MAX_NAME_LENGTH, api, type InstallForm, type InstallRequest, type Job, type Node, type ProbeResult } from '../lib/api'
import { fixFor, forAnotherTry, withHostKey } from '../lib/fixes'
import { badPort, installOutcome, realNodes } from '../lib/install'
import { jobHeader, jobLine } from '../lib/jobs'
import { FixPanel } from './FixPanel'

type Stage = 'form' | 'hostkey' | 'installing'

interface Props {
  /** the nodes there are now, to pick a name that isn't taken; null while the list loads */
  nodes: Node[] | null
  /** what a failed install was asked for, to start from again (Try again); the login's secrets are typed again */
  initial?: InstallForm | null
  onClose: () => void
  /** The install job started: the node is in the list, installing. */
  onStarted: () => void
  /** The install job ended, set up or cleaned away (or kept as an unfinished install). */
  onInstalled: () => void
  onOpenNode: (id: number) => void
}

/** The username is not a secret, so the next add starts from it. */
let lastUser = 'root'

// Mounted only while open: closing it however you like drops everything typed, the login included.
export function AddNodeDialog({ open, ...props }: Props & { open: boolean }) {
  return open ? <AddNode {...props} /> : null
}

/** "Node 3": the first number whose name is free. An unfinished install is no node, and its name is free too. */
function suggestName(all: Node[], t: T) {
  const nodes = realNodes(all)
  let n = nodes.length + 1
  while (nodes.some((x) => x.name === t('node.defaultName', { n }))) n += 1
  return t('node.defaultName', { n })
}

type Say = ReturnType<typeof useToast>

/**
 * What happens when an install ends, whether or not this dialog is still open: a toast, and the list is read
 * again. It is made out here, with nothing of the dialog's scope, because the job tray keeps it for as long as
 * the page lives, and it must not keep what the dialog holds: the install that was sent, login and all. A
 * good install no longer needs it, so it is let go of here.
 */
function installEnded(name: string, toast: Say, t: T, held: MutableRefObject<InstallRequest | null>, onInstalled: () => void) {
  return (j: Job) => {
    if (j.state === 'ok') {
      held.current = null
      toast(t('job.install.ok', { target: name }))
    } else toast(t('job.installFailed'), 'error')
    onInstalled()
  }
}

function AddNode({ nodes, initial, onClose, onStarted, onInstalled, onOpenNode }: Props) {
  const { t, fmt } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const jobs = useJobs()
  const now = useNow()
  const entryOn = useConfig()?.entry.on
  const suggested = suggestName(nodes ?? [], t)
  const [form, setForm] = useState(() =>
    initial
      ? { host: initial.host, name: initial.name, entryPort: String(initial.entryPort || 443) }
      : { host: '', name: '', entryPort: '443' },
  )
  const [login, setLogin] = useState<LoginForm>(() => (initial ? emptyLogin(initial.sshPort || 22, initial.sshUser || lastUser) : emptyLogin(22, lastUser)))
  const [stage, setStage] = useState<Stage>('form')
  const [key, setKey] = useState<ProbeResult | null>(null)
  const [ackChanged, setAckChanged] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [fixError, setFixError] = useState<string | null>(null)
  const [jobId, setJobId] = useState<string | null>(null)
  // the name the install goes with, fixed at submit: the suggestion moves on once the node is in the list
  const [named, setNamed] = useState('')
  const job = jobs.get(jobId)

  // The install as it was sent, login and all, so that a fix can send it again with one change. It is kept
  // here, in memory, for as long as this dialog is open and its install can still fail: it is let go of when
  // the install works, when nothing can be done about a failure, and when the dialog closes. It is never
  // written anywhere, and nothing but this dialog and the panel it hands it to ever reads it.
  const held = useRef<InstallRequest | null>(null)
  // an install waiting for a host key to be trusted, and the stage Back goes to
  const waiting = useRef<{ req: InstallRequest; back: Stage } | null>(null)
  useEffect(
    () => () => {
      held.current = null
      waiting.current = null
    },
    [],
  )
  useEffect(() => {
    if (job && job.state !== 'running' && (job.state === 'ok' || !fixFor(job))) held.current = null
  }, [job?.state, job?.code])

  const set = (k: 'host' | 'name' | 'entryPort') => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }))

  /** Sends the install, from the form or from a fix, and follows it in this dialog. */
  const begin = async (req: InstallRequest, from: 'form' | 'fix') => {
    setBusy(true)
    setError(null)
    setFixError(null)
    try {
      const { jobId: id } = await api.install(req)
      held.current = req
      waiting.current = null
      // the secrets are kept in `held` and nowhere else, not in the form's state or its fields
      setLogin(withoutSecrets)
      clearSecrets(document.getElementById('add-node'))
      setNamed(req.name)
      setJobId(id)
      setStage('installing')
      onStarted()
      // reported here even when the dialog is closed by then
      jobs.watch(id, installEnded(req.name, toast, t, held, onInstalled))
    } catch (e) {
      if (from === 'form') {
        setError(errorText(e))
        setStage('form')
      } else {
        setFixError(errorText(e))
        setStage('installing')
      }
    } finally {
      setBusy(false)
    }
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const ssh = Number(login.sshPort)
    const problem =
      loginError(login) ?? (!form.host.trim() ? 'add.errHost' : badPort(Number(form.entryPort), ssh) ? 'add.errEntryPort' : null)
    if (problem) return setError(t(problem))
    lastUser = login.sshUser.trim()
    setBusy(true)
    setError(null)
    try {
      // a quick submit can beat the list, and a free name needs it
      const chosen = form.name.trim() || suggestName(nodes ?? (await api.nodes().catch(() => [])), t)
      setNamed(chosen)
      const probe = await api.probe({ host: form.host.trim(), sshPort: ssh, sshUser: login.sshUser.trim() })
      setKey(probe)
      setAckChanged(false)
      const req: InstallRequest = { ...toLogin(login, probe.fingerprint), host: form.host.trim(), name: chosen, entryPort: Number(form.entryPort) }
      // a first key is pinned as it is, like ssh does; only a changed one stops to ask
      if (!keyNeedsTrust(probe)) {
        setBusy(false)
        await begin(req, 'form')
      } else {
        waiting.current = { req, back: 'form' }
        setStage('hostkey')
        setBusy(false)
      }
    } catch (e) {
      setError(errorText(e))
      setBusy(false)
    }
  }

  /** A fix: the install that was sent, with this change, sent again. For another server its key is read first. */
  const resend = async (change: (r: InstallRequest) => InstallRequest, probe = false) => {
    const before = held.current
    if (!before || busy) return
    const next = change(before)
    if (!probe) return begin(next, 'fix')
    setBusy(true)
    setFixError(null)
    try {
      const seen = await api.probe({ host: next.host, sshPort: next.sshPort, sshUser: next.sshUser })
      setKey(seen)
      setAckChanged(false)
      const req = withHostKey(next, seen.fingerprint)
      if (keyNeedsTrust(seen)) {
        waiting.current = { req, back: 'installing' }
        setStage('hostkey')
        setBusy(false)
        return
      }
      setBusy(false)
      await begin(req, 'fix')
    } catch (e) {
      setFixError(errorText(e))
      setBusy(false)
    }
  }

  /** Back to the form with what was sent, but not the login: the person types that again. */
  const changeDetails = () => {
    held.current = null
    waiting.current = null
    setJobId(null)
    setError(null)
    setFixError(null)
    setStage('form')
  }

  // what the panel is told of it: where it was sent and how it signs in, and none of the secrets
  const sent = held.current ? { sshPort: held.current.sshPort, sshUser: held.current.sshUser, hostKey: held.current.hostKey } : null
  const finished = job && job.state !== 'running'
  const failed = finished && job.state === 'failed'
  const fix = failed ? fixFor(job) : null
  const header = failed ? jobHeader(t, fmt, job, '', now) : null
  const title =
    stage !== 'installing'
      ? t('add.title')
      : job?.state === 'ok'
        ? t('job.installDone')
        : header
          ? header.title
          : t('job.install.running', { target: job?.target || named || suggested })

  const footer =
    stage === 'form' ? (
      <>
        <Button onClick={onClose}>{t('common.cancel')}</Button>
        <Button variant="primary" type="submit" form="add-node" busy={busy}>
          {t('add.submit')}
        </Button>
      </>
    ) : stage === 'hostkey' ? (
      <>
        <Button
          onClick={() => {
            const back = waiting.current?.back ?? 'form'
            waiting.current = null
            setStage(back)
          }}
        >
          {t('add.back')}
        </Button>
        <Button
          variant="danger"
          disabled={!key || !keyTrusted(key, ackChanged)}
          busy={busy}
          onClick={() => {
            const w = waiting.current
            if (w && key) void begin(withHostKey(w.req, key.fingerprint), w.back === 'form' ? 'form' : 'fix')
          }}
        >
          {t('add.trust')}
        </Button>
      </>
    ) : finished && job.state === 'ok' && job.nodeId ? (
      <Button variant="primary" onClick={() => onOpenNode(job.nodeId!)}>
        {t('job.openNode')}
      </Button>
    ) : finished && installOutcome(job) === 'kept' && job.nodeId ? (
      // its server waits for a cleanup, so another try can't start yet; the details can still be changed
      <>
        <Button onClick={onClose}>{t('common.close')}</Button>
        <Button onClick={changeDetails}>{t('add.changeDetails')}</Button>
        <Button variant="primary" onClick={() => onOpenNode(job.nodeId!)}>
          {t('job.openUnfinished')}
        </Button>
      </>
    ) : finished ? (
      // the install as it was sent can go again as it was, and the details can be changed: the login is typed again
      <>
        <Button onClick={onClose}>{t('common.close')}</Button>
        <Button onClick={changeDetails}>{t('add.changeDetails')}</Button>
        {held.current && (fix?.kind === 'again' || fix?.kind === 'port') && (
          <Button variant="primary" busy={busy} onClick={() => void resend(forAnotherTry)}>
            {t('job.tryAgain')}
          </Button>
        )}
      </>
    ) : (
      <Button onClick={onClose}>{t('common.close')}</Button>
    )

  return (
    <Dialog
      onClose={onClose}
      closeLabel={stage === 'installing' && !finished ? t('job.closeKeepsGoing') : t('common.close')}
      title={title}
      description={stage === 'form' ? t('add.desc') : stage === 'installing' && job ? (header ? header.line : jobLine(t, fmt, job, now)) : undefined}
      footer={footer}
    >
      {stage === 'form' && (
        <form id="add-node" onSubmit={submit} className="grid grid-cols-6 gap-3" autoComplete="off">
          <LoginFields
            form={login}
            onChange={setLogin}
            host={
              <Field label={t('add.host')} className="col-span-4">
                {(p) => (
                  <Input
                    {...p}
                    dir="ltr"
                    className="font-mono"
                    aria-describedby="domestic-hint"
                    required
                    value={form.host}
                    onChange={set('host')}
                    placeholder="203.0.113.10"
                    spellCheck={false}
                    autoCapitalize="off"
                  />
                )}
              </Field>
            }
            afterHost={
              <p id="domestic-hint" className="col-span-6 -mt-1 text-xs text-faint">
                {t('job.domesticHint')}
              </p>
            }
          />
          <Field label={t('add.name')} className="col-span-6">
            {(p) => <Input {...p} dir="auto" value={form.name} onChange={set('name')} placeholder={suggested} maxLength={MAX_NAME_LENGTH} />}
          </Field>
          <Disclosure title={t('add.advanced')} className="col-span-6 border-t border-border pt-3">
            <div className="flex flex-col gap-4">
              <Field label={t('add.entryPort')} hint={t('add.entryPortHint')}>
                {(p) => <Input {...p} dir="ltr" className="font-mono" inputMode="numeric" value={form.entryPort} onChange={set('entryPort')} />}
              </Field>
            </div>
          </Disclosure>
          {error && (
            <div className="col-span-6">
              <ErrorBox>{error}</ErrorBox>
            </div>
          )}
          <p className="col-span-6 flex items-start gap-2 text-xs text-faint">
            <KeyRound className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            {t('add.keyNote')}
          </p>
        </form>
      )}

      {stage === 'hostkey' && key && (
        <div className="flex flex-col gap-3">
          <HostKeyCheck host={(waiting.current?.req.host ?? form.host).trim()} probe={key} acked={ackChanged} onAck={setAckChanged} />
          {(error || fixError) && <ErrorBox>{error ?? fixError}</ErrorBox>}
        </div>
      )}

      {stage === 'installing' && (
        <div className="flex flex-col gap-4">
          <JobSteps job={job} />
          {job?.state === 'ok' && <p className="text-[13px] text-muted">{entryOn === false ? t('add.doneNoEntry') : t('add.doneNote')}</p>}
          {!finished && <p className="border-t border-border pt-3 text-[13px] text-muted">{t('job.keepsGoingInstall')}</p>}
          {failed && (
            <FixPanel job={job} sent={sent} busy={busy} onFix={(change, probe) => void resend(change, probe)} onOpenNode={(id) => onOpenNode(id)} />
          )}
          {/* why the install could not be sent again, whichever way it was asked for */}
          {failed && fixError && <ErrorBox>{fixError}</ErrorBox>}
        </div>
      )}
    </Dialog>
  )
}
