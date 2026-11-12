import { KeyRound, Pencil, RefreshCw } from '../components/icons'
import { Head } from '../components/section'
import { Button, Card, Disclosure } from '../components/ui'
import { useLang } from '../i18n'
import type { Local } from '../lib/api'
import { CopyField, ServerName } from './NodeSettingsTab'

interface Props {
  local: Local
  onChangeName: () => void
  onNewPort: () => void
  onChangeTarget: () => void
  onRotate: () => void
  /** a server name to confirm; empty is the Reality target's own */
  onChangeSni: (sni: string) => void
  onTurnOff: () => void
}

/** What can be changed on the router's own entry. Each change is a button that opens its own dialog; none saves from the page. */
export function RouterSettingsTab({ local, onChangeName, onNewPort, onChangeTarget, onRotate, onChangeSni, onTurnOff }: Props) {
  const { t } = useLang()
  const source = local.hostSource === 'own' ? t('router.source.own') : local.hostSource === 'cloud' ? t('router.source.cloud') : ''

  return (
    <div className="flex flex-col gap-4">
      <Card className="flex flex-col gap-2 p-4">
        <Head title={t('router.set.address')} hint={t('router.set.address.hint')}>
          <Button size="sm" onClick={onChangeName}>
            <Pencil className="h-3.5 w-3.5" /> {t('router.set.address.change')}
          </Button>
        </Head>
        <div className="flex flex-wrap items-center gap-2 text-[13px]">
          {local.host ? (
            <b dir="ltr" className="break-all font-mono">
              {local.host}
            </b>
          ) : (
            <span className="text-warn-text">{t('router.set.address.none')}</span>
          )}
          {source && <span className="text-xs text-faint">{source}</span>}
        </div>
        <p className="text-xs text-faint">{t('router.set.address.note')}</p>
      </Card>

      <Card className="flex flex-col gap-2 p-4">
        <Head title={t('set.port')} hint={t('router.set.port.hint')}>
          <Button size="sm" onClick={onNewPort}>
            <RefreshCw className="h-3.5 w-3.5" /> {t('router.set.port.new')}
          </Button>
        </Head>
        <div className="flex flex-wrap items-center gap-2 text-[13px]">
          {t('set.port')}{' '}
          {local.port !== null ? (
            <b dir="ltr" className="font-mono">
              {local.port}
            </b>
          ) : (
            <span className="text-muted">{t('router.set.port.none')}</span>
          )}
        </div>
        <p className="text-xs text-faint">{t('router.set.port.note')}</p>
      </Card>

      <Card className="flex flex-col gap-2 p-4">
        <Head title={t('set.target')} hint={t('router.set.target.hint')}>
          <Button size="sm" onClick={onChangeTarget}>
            <Pencil className="h-3.5 w-3.5" /> {t('set.target.change')}
          </Button>
        </Head>
        <div className="flex flex-wrap items-center gap-2 text-[13px]">
          {local.handshakeTarget ? <b dir="ltr">{local.handshakeTarget}</b> : <span className="text-muted">{t('router.set.target.none')}</span>}
        </div>
      </Card>

      <Card className="flex flex-col gap-3 p-4">
        <Head title={t('set.keys')} hint={t('router.set.keys.hint')}>
          <Button size="sm" onClick={onRotate}>
            <KeyRound className="h-3.5 w-3.5" /> {t('set.keys.rotate')}
          </Button>
        </Head>
        <div className="grid gap-2">
          <CopyField label={t('set.pubkey')} value={local.publicKey} copyLabel={t('set.copyPub')} />
          <CopyField label={t('set.shortid')} value={local.shortId} copyLabel={t('set.copyShort')} />
        </div>
        <p className="text-xs text-faint">{t('router.set.keys.note')}</p>
      </Card>

      {/* a server name is the target's own until there is a target */}
      {local.handshakeTarget && (
        <Card className="p-4">
          <Disclosure title={t('set.advanced')} hint={t('set.advanced.hint')}>
            {/* a new key when the entry changes, so the form starts from what it has */}
            <ServerName key={`${local.sni}|${local.sniCustom}`} node={local} locked={false} onSave={onChangeSni} />
          </Disclosure>
        </Card>
      )}

      <Card className="flex flex-col gap-2 p-4">
        <Head title={t('router.set.off')} hint={t('router.set.off.hint')}>
          <Button size="sm" variant="danger" onClick={onTurnOff}>
            {t('router.set.off.action')}
          </Button>
        </Head>
        <p className="text-xs text-faint">{t('router.off.note')}</p>
      </Card>
    </div>
  )
}
