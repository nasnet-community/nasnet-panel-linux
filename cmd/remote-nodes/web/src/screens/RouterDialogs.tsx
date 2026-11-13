import { useState } from 'react'
import { Badge, Button, Confirm, Dialog, ErrorBox, Field, Input, useToast } from '../components/ui'
import { useErrorText, useLang } from '../i18n'
import { local as localApi, type HandshakeCheck, type Local } from '../lib/api'
import { normalizeTarget, validServerName } from '../lib/node'
import { Impact, LinksNotice } from './NodeDialogs'

/** Where a changed entry goes. */
type Changed = (fresh: Local) => void

/** What every dialog here gets: the entry as it is, a way out, and where the changed entry goes. */
interface Props {
  local: Local
  onClose: () => void
  onChanged: Changed
}

/** A confirmation's call: the changed entry goes to the page with a toast, a refusal stays in the dialog in the page's words. */
function useApply(onChanged: Changed) {
  const errorText = useErrorText()
  const toast = useToast()
  return (call: () => Promise<Local>, done: string) => async () => {
    let fresh: Local
    try {
      fresh = await call()
    } catch (e) {
      // Confirm shows what is thrown
      throw new Error(errorText(e))
    }
    toast(done)
    onChanged(fresh)
  }
}

/** How many links a change makes new, and that each has to be shared again. */
function LinksImpact({ n }: { n: number }) {
  const { t } = useLang()
  return <Impact n={n}>{t(n ? 'links.some' : 'router.links.none', { n })}</Impact>
}

export function TurnOnDialog({ onClose, onChanged }: Omit<Props, 'local'>) {
  const { t } = useLang()
  const apply = useApply(onChanged)
  return (
    <Confirm title={t('router.on.title')} action={t('router.turnOn')} onClose={onClose} onConfirm={apply(() => localApi.setEnabled(true), t('router.toast.on'))}>
      <p>{t('router.on.text')}</p>
      <p>{t('router.on.note')}</p>
    </Confirm>
  )
}

export function TurnOffDialog({ local, onClose, onChanged }: Props) {
  const { t } = useLang()
  const apply = useApply(onChanged)
  const n = local.subscriptionCount
  return (
    <Confirm danger title={t('router.off.title')} action={t('router.turnOff')} onClose={onClose} onConfirm={apply(() => localApi.setEnabled(false), t('router.toast.off'))}>
      <Impact n={n}>{t(n ? 'router.off.some' : 'router.off.none', { n })}</Impact>
      <p>{t('router.off.note')}</p>
    </Confirm>
  )
}

export function NewPortDialog({ local, onClose, onChanged }: Props) {
  const { t } = useLang()
  const apply = useApply(onChanged)
  return (
    <Confirm title={t('router.port.title')} action={t('router.port.action')} onClose={onClose} onConfirm={apply(localApi.newPort, t('router.toast.port'))}>
      <LinksImpact n={local.subscriptionCount} />
      <p>{t('router.port.note')}</p>
    </Confirm>
  )
}

export function RotateKeysDialog({ local, onClose, onChanged }: Props) {
  const { t } = useLang()
  const apply = useApply(onChanged)
  return (
    <Confirm danger title={t('router.rotate.title')} action={t('rotate.action')} onClose={onClose} onConfirm={apply(localApi.rotateKeys, t('router.toast.keys'))}>
      <LinksImpact n={local.subscriptionCount} />
      <p>{t('router.rotate.note')}</p>
    </Confirm>
  )
}

/** Confirms a new server name (the empty one is the Reality target's own). */
export function RouterServerNameDialog({ local, sni, onClose, onChanged }: Props & { sni: string }) {
  const { t } = useLang()
  const apply = useApply(onChanged)
  return (
    <Confirm title={t('router.sni.title')} action={t('sni.action')} onClose={onClose} onConfirm={apply(() => localApi.settings({ sni }), t('router.toast.sni'))}>
      <p>{sni ? t('sni.now', { sni }) : t('sni.same', { target: local.handshakeTarget.split(':')[0] })}</p>
      <LinksImpact n={local.subscriptionCount} />
    </Confirm>
  )
}

/** The name every link carries: one of the person's own, or back to the router's IP Cloud name. */
export function OwnNameDialog({ local, onClose, onChanged }: Props) {
  const { t } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const [name, setName] = useState(local.ownHost)
  const [busy, setBusy] = useState<'save' | 'clear' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const wanted = name.trim().toLowerCase()
  const problem = wanted && !validServerName(wanted) ? t('router.name.invalid') : null
  const ready = !!wanted && !problem && wanted !== local.ownHost

  const save = async (host: string, how: 'save' | 'clear') => {
    setBusy(how)
    setError(null)
    try {
      const fresh = await localApi.settings({ host })
      toast(host ? t('router.toast.name', { name: host }) : t('router.toast.cloudName'))
      onChanged(fresh)
      onClose()
    } catch (e) {
      setError(errorText(e))
      setBusy(null)
    }
  }

  return (
    <Dialog
      onClose={onClose}
      title={t('router.name.title')}
      description={local.domesticAddr ? t('router.name.desc', { addr: local.domesticAddr }) : t('router.name.descNoAddr')}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="primary" type="submit" form="own-name" busy={busy === 'save'} disabled={!ready || !!busy}>
            {t('settings.apply')}
          </Button>
        </>
      }
    >
      <form
        id="own-name"
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault()
          if (ready && !busy) void save(wanted, 'save')
        }}
      >
        <Field label={t('router.name.label')} error={problem} hint={t('router.name.hint')}>
          {(p) => (
            <Input {...p} dir="ltr" className="font-mono" value={name} onChange={(e) => setName(e.target.value)} spellCheck={false} autoCapitalize="off" placeholder="home.example.com" />
          )}
        </Field>
        {local.ownHost && (
          <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border pt-3 text-[13px] text-muted">
            <span className="min-w-[180px] flex-1">{local.cloudName ? t('router.name.cloud', { name: local.cloudName }) : t('router.name.noCloud')}</span>
            <Button size="sm" busy={busy === 'clear'} disabled={!!busy} onClick={() => void save('', 'clear')}>
              {t('router.name.useCloud')}
            </Button>
          </div>
        )}
        <LinksNotice router />
        {error && <ErrorBox>{error}</ErrorBox>}
      </form>
    </Dialog>
  )
}

/** Change the Reality target in steps: type it, have the router check it, then apply it. Or leave the choice to the router. */
export function RouterTargetDialog({ local, onClose, onChanged }: Props) {
  const { t, fmt } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const [input, setInput] = useState(local.handshakeTarget)
  const [check, setCheck] = useState<{ target: string; result: HandshakeCheck } | null>(null)
  const [busy, setBusy] = useState<'check' | 'save' | 'choose' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const wanted = normalizeTarget(input)
  // a result is for what was checked, not for what is typed now
  const result = check && check.target === wanted ? check.result : null

  const runCheck = async () => {
    setBusy('check')
    setError(null)
    setCheck(null)
    try {
      setCheck({ target: wanted, result: await localApi.handshakeCheck(wanted) })
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(null)
    }
  }
  const save = async (handshakeTarget: string, how: 'save' | 'choose') => {
    setBusy(how)
    setError(null)
    try {
      const fresh = await localApi.settings({ handshakeTarget })
      toast(handshakeTarget ? t('router.toast.target', { target: handshakeTarget }) : t('router.toast.choosing'))
      onChanged(fresh)
      onClose()
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
          <Button variant="primary" busy={busy === 'save'} disabled={!result?.ok || wanted === local.handshakeTarget || !!busy} onClick={() => void save(wanted, 'save')}>
            {t('settings.apply')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('hs.label')} error={invalid ? t('hs.invalid', { example: 'www.example.com:443' }) : null} hint={t('router.hs.hint', { tls: 'TLS 1.3' })}>
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
              {result.ok && <span className="text-xs text-faint">{t('router.hs.facts', { tls: ['TLS 1.3', 'X25519'].join(' · '), size: fmt.bytes(result.handshakeBytes) })}</span>}
            </div>
            {!result.ok && result.error && <ErrorBox>{result.error}</ErrorBox>}
            {wanted === local.handshakeTarget && <p className="text-xs text-faint">{t('hs.same')}</p>}
          </div>
        )}
        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border pt-3 text-[13px] text-muted">
          <span className="min-w-[180px] flex-1">{t('router.hs.chooseNote')}</span>
          <Button size="sm" busy={busy === 'choose'} disabled={!!busy} onClick={() => void save('', 'choose')}>
            {t('router.hs.choose')}
          </Button>
        </div>
        <LinksNotice router />
        {error && <ErrorBox>{error}</ErrorBox>}
      </div>
    </Dialog>
  )
}
