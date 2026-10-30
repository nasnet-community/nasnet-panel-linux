import { useState } from 'react'
import { Check, Copy, KeyRound, Pencil, RefreshCw } from '../components/icons'
import { Head } from '../components/section'
import { Badge, Button, Card, Disclosure, Field, Input, copyText } from '../components/ui'
import { useLang } from '../i18n'
import type { Node } from '../lib/api'
import { unreachable } from '../lib/health'
import { renewLeadDays, validServerName } from '../lib/node'

/** A key shown in full, with a button to copy it. */
function CopyField({ label, value, copyLabel }: { label: string; value: string; copyLabel: string }) {
  const { t } = useLang()
  const [copied, setCopied] = useState(false)
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-[13px] font-medium text-muted">{label}</span>
      <div className="flex items-center gap-2">
        <code dir="ltr" className="min-w-0 flex-1 break-all rounded-md border border-border bg-card2 px-3 py-2 font-mono text-xs">
          {value}
        </code>
        <Button
          size="icon"
          aria-label={copyLabel}
          onClick={async () => {
            await copyText(value)
            setCopied(true)
            setTimeout(() => setCopied(false), 1500)
          }}
        >
          {copied ? <Check className="h-4 w-4 text-ok-text" /> : <Copy className="h-4 w-4" />}
        </Button>
        <span className="sr-only" aria-live="polite">
          {copied ? t('common.copied') : ''}
        </span>
      </div>
    </div>
  )
}

/** The server name the handshake uses: the target's own, or one of its own. Saving it asks first. */
function ServerName({ node, locked, onSave }: { node: Node; locked: boolean; onSave: (sni: string) => void }) {
  const { t } = useLang()
  const [same, setSame] = useState(!node.sniCustom)
  const [name, setName] = useState(node.sniCustom ? node.sni : '')
  const target = node.handshakeTarget.split(':')[0]
  // empty stands for the target's own name
  const wanted = same ? '' : name.trim().toLowerCase()
  const problem = !same && name.trim() && !validServerName(name) ? t('sni.invalid') : null
  const now = node.sniCustom ? node.sni : ''
  const ready = !locked && !problem && wanted !== now && (same || !!wanted)
  return (
    <div className="flex flex-col gap-3 border-t border-border pt-3">
      <label className="flex min-h-[44px] items-start gap-2 text-[13px] sm:min-h-0">
        <input type="checkbox" className="mt-1 h-4 w-4" checked={same} disabled={locked} onChange={(e) => setSame(e.target.checked)} />
        <span>
          {t('sni.sameLabel')}
          <span dir="ltr" className="block text-start text-xs text-faint">
            {target}
          </span>
        </span>
      </label>
      <Field label={t('sni.label')} error={problem} hint={t('sni.hint')}>
        {(p) => (
          <Input
            {...p}
            dir="ltr"
            className="font-mono"
            value={same ? target : name}
            disabled={same || locked}
            onChange={(e) => setName(e.target.value)}
            spellCheck={false}
            autoCapitalize="off"
            placeholder="cdn.example.com"
          />
        )}
      </Field>
      <div className="flex justify-end">
        <Button size="sm" variant="primary" disabled={!ready} onClick={() => onSave(wanted)}>
          {t('sni.save')}
        </Button>
      </div>
    </div>
  )
}

interface Props {
  node: Node
  onChangePort: () => void
  onChangeTarget: () => void
  onRotate: () => void
  onRenew: () => void
  /** a server name to confirm; empty is the handshake target's own */
  onChangeName: (sni: string) => void
}

/** What can be changed on a node. Each change is a button that opens its own dialog; none saves from the page. */
export function NodeSettingsTab({ node, onChangePort, onChangeTarget, onRotate, onRenew, onChangeName }: Props) {
  const { t, fmt } = useLang()
  // the agent applies every change, so none can be made while the router can't reach it
  const down = unreachable(node)
  const why = down ? t('node.needsLink') : undefined
  const check = node.handshakeCheck

  return (
    <div className="flex flex-col gap-4">
      {down && <p className="text-xs text-faint">{t('settings.locked')}</p>}

      <Card className="flex flex-col gap-2 p-4">
        <Head title={t('set.connection')} hint={t('set.connection.hint')}>
          <Button size="sm" disabled={down} title={why} onClick={onChangePort}>
            <Pencil className="h-3.5 w-3.5" /> {t('set.port.change')}
          </Button>
        </Head>
        <div className="flex flex-wrap items-center gap-2 text-[13px]">
          {t('set.port')}{' '}
          <b dir="ltr" className="font-mono">
            {node.entryPort}
          </b>
        </div>
      </Card>

      <Card className="flex flex-col gap-2 p-4">
        <Head title={t('set.target')} hint={t('set.target.hint')}>
          <Button size="sm" disabled={down} title={why} onClick={onChangeTarget}>
            <Pencil className="h-3.5 w-3.5" /> {t('set.target.change')}
          </Button>
        </Head>
        <div className="flex flex-wrap items-center gap-2 text-[13px]">
          <b dir="ltr">{node.handshakeTarget}</b>
          {check && <Badge tone={check.ok ? 'online' : 'danger'}>{check.ok ? t('hs.working') : t('hs.failing')}</Badge>}
          {check && <span className="text-xs text-faint">{t('hs.checkedAgo', { ago: fmt.relative(check.checkedAt) })}</span>}
        </div>
      </Card>

      <Card className="flex flex-col gap-3 p-4">
        <Head title={t('set.keys')} hint={t('set.keys.hint')}>
          <Button size="sm" disabled={down} title={why} onClick={onRotate}>
            <KeyRound className="h-3.5 w-3.5" /> {t('set.keys.rotate')}
          </Button>
        </Head>
        <div className="grid gap-2">
          <CopyField label={t('set.pubkey')} value={node.publicKey} copyLabel={t('set.copyPub')} />
          <CopyField label={t('set.shortid')} value={node.shortId} copyLabel={t('set.copyShort')} />
        </div>
        <p className="text-xs text-faint">{t('set.keys.note')}</p>
      </Card>

      <Card className="flex flex-col gap-2 p-4">
        <Head title={t('set.cert')} hint={t('set.cert.hint')}>
          <Button size="sm" disabled={down} title={why} onClick={onRenew}>
            <RefreshCw className="h-3.5 w-3.5" /> {t('set.cert.renew')}
          </Button>
        </Head>
        <div className="flex flex-wrap items-center gap-2 text-[13px]">
          <span>
            {t('status.validUntil')} <b>{fmt.date(node.cert.notAfter)}</b>
            <span className="text-muted"> · {t('set.cert.renews', { n: renewLeadDays(node.cert) })}</span>
          </span>
        </div>
      </Card>

      <Card className="p-4">
        <Disclosure title={t('set.advanced')} hint={t('set.advanced.hint')}>
          {/* a new key when the node changes, so the form starts from what the node has */}
          <ServerName key={`${node.sni}|${node.sniCustom}`} node={node} locked={down} onSave={onChangeName} />
        </Disclosure>
      </Card>
    </div>
  )
}
