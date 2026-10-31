import { useState, type FormEvent } from 'react'
import { KeyRound } from '../components/icons'
import { JobSteps } from '../components/job'
import { useJobs } from '../components/jobs'
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
import { Button, Dialog, Disclosure, ErrorBox, Field, Input, useToast } from '../components/ui'
import { useConfig } from '../hooks/useConfig'
import { useNow } from '../hooks/usePoll'
import { useErrorText, useLang, type T } from '../i18n'
import { MAX_NAME_LENGTH, api, type Node, type ProbeResult } from '../lib/api'
import { jobLine } from '../lib/jobs'

type Stage = 'form' | 'hostkey' | 'installing'

interface Props {
  /** the nodes there are now, to pick a name that isn't taken; null while the list loads */
  nodes: Node[] | null
  onClose: () => void
  /** The install job started: the node is in the list, installing. */
  onStarted: () => void
  /** The install job ended; the node may be saved even if it failed late. */
  onInstalled: () => void
  onOpenNode: (id: number) => void
}

/** The username is not a secret, so the next add starts from it. */
let lastUser = 'root'

// Mounted only while open: closing it however you like drops everything typed, the login included.
export function AddNodeDialog({ open, ...props }: Props & { open: boolean }) {
  return open ? <AddNode {...props} /> : null
}

/** "Node 3": the first number whose name is free. */
function suggestName(nodes: Node[], t: T) {
  let n = nodes.length + 1
  while (nodes.some((x) => x.name === t('node.defaultName', { n }))) n += 1
  return t('node.defaultName', { n })
}

/** The port subscriptions connect to: 1 to 65535, and not the one SSH uses. */
const badPort = (p: number, ssh: number) => !Number.isInteger(p) || p < 1 || p > 65535 || p === ssh

function AddNode({ nodes, onClose, onStarted, onInstalled, onOpenNode }: Props) {
  const { t, fmt } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const jobs = useJobs()
  const now = useNow()
  const entryOn = useConfig()?.entry.on
  const suggested = suggestName(nodes ?? [], t)
  const [form, setForm] = useState({ host: '', name: '', entryPort: '443' })
  const [login, setLogin] = useState<LoginForm>(() => emptyLogin(22, lastUser))
  const [stage, setStage] = useState<Stage>('form')
  const [key, setKey] = useState<ProbeResult | null>(null)
  const [ackChanged, setAckChanged] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [jobId, setJobId] = useState<string | null>(null)
  // the name the install goes with, fixed at submit: the suggestion moves on once the node is in the list
  const [named, setNamed] = useState('')
  const job = jobs.get(jobId)

  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }))

  const install = async (fingerprint: string, name: string) => {
    setBusy(true)
    setError(null)
    try {
      const { jobId } = await api.install({
        ...toLogin(login, fingerprint),
        host: form.host.trim(),
        name,
        entryPort: Number(form.entryPort),
      })
      // the secrets have gone; nothing needs them again
      setLogin(withoutSecrets)
      setJobId(jobId)
      setStage('installing')
      onStarted()
      // reported here even when the dialog is closed by then
      jobs.watch(jobId, (j) => {
        if (j.state === 'ok') toast(t('job.install.ok', { target: name }))
        else toast(t('job.installFailed'), 'error')
        onInstalled()
      })
    } catch (e) {
      setError(errorText(e))
      setStage('form')
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
      // a first key is pinned as it is, like ssh does; only a changed one stops to ask
      if (!keyNeedsTrust(probe)) {
        setBusy(false)
        await install(probe.fingerprint, chosen)
      } else {
        setStage('hostkey')
        setBusy(false)
      }
    } catch (e) {
      setError(errorText(e))
      setBusy(false)
    }
  }

  const finished = job && job.state !== 'running'
  const title =
    stage !== 'installing'
      ? t('add.title')
      : job?.state === 'ok'
        ? t('job.installDone')
        : job?.state === 'failed'
          ? t('job.installFailed')
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
        <Button onClick={() => setStage('form')}>{t('add.back')}</Button>
        <Button variant="danger" disabled={!key || !keyTrusted(key, ackChanged)} busy={busy} onClick={() => key && install(key.fingerprint, named)}>
          {t('add.trust')}
        </Button>
      </>
    ) : finished && job.nodeId ? (
      <>
        {job.state === 'failed' && <Button onClick={onClose}>{t('common.close')}</Button>}
        <Button variant="primary" onClick={() => onOpenNode(job.nodeId!)}>
          {t('job.openNode')}
        </Button>
      </>
    ) : finished ? (
      <>
        <Button onClick={onClose}>{t('common.close')}</Button>
        <Button
          variant="primary"
          onClick={() => {
            setJobId(null)
            setStage('form')
          }}
        >
          {t('add.tryAgain')}
        </Button>
      </>
    ) : (
      <Button onClick={onClose}>{t('common.close')}</Button>
    )

  return (
    <Dialog
      onClose={onClose}
      closeLabel={stage === 'installing' && !finished ? t('job.closeKeepsGoing') : t('common.close')}
      title={title}
      description={stage === 'form' ? t('add.desc') : stage === 'installing' && job ? jobLine(t, fmt, job, now) : undefined}
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
            <Field label={t('add.entryPort')} hint={t('add.entryPortHint')}>
              {(p) => <Input {...p} dir="ltr" className="font-mono" inputMode="numeric" value={form.entryPort} onChange={set('entryPort')} />}
            </Field>
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
          <HostKeyCheck host={form.host.trim()} probe={key} acked={ackChanged} onAck={setAckChanged} />
          {error && <ErrorBox>{error}</ErrorBox>}
        </div>
      )}

      {stage === 'installing' && (
        <div className="flex flex-col gap-4">
          <JobSteps job={job} />
          {job?.state === 'ok' && <p className="text-[13px] text-muted">{entryOn === false ? t('add.doneNoEntry') : t('add.doneNote')}</p>}
          {job?.state === 'failed' && job.nodeId ? <p className="text-[13px] text-muted">{t('add.savedNote')}</p> : null}
          {!finished && <p className="border-t border-border pt-3 text-[13px] text-muted">{t('job.keepsGoingInstall')}</p>}
        </div>
      )}
    </Dialog>
  )
}
