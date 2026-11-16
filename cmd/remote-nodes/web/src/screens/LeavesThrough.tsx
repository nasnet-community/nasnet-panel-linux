import { useEffect, useRef, useState } from 'react'
import { RouteLink } from '../components/link'
import { Head } from '../components/section'
import { Badge, Button, Card, Confirm, ErrorBox, Notice, Select, useToast } from '../components/ui'
import { usePoll } from '../hooks/usePoll'
import { useErrorText, useLang } from '../i18n'
import { outbounds as outboundsApi } from '../lib/api'
import { routeChoices, routeName, serverText, stateOf } from '../lib/outbounds'

/**
 * "Leaves through" on a node's or This router's Settings tab: the router's VPN or one of the outbounds,
 * with how the chosen one is doing. Saving goes through a confirmation (`onSave`), as the bridge restarts.
 */
export function LeavesThroughCard({ outboundId, where, onSave }: { outboundId: number; where: 'node' | 'router'; onSave: (outboundId: number, target: string) => void }) {
  const { t, lang } = useLang()
  const errorText = useErrorText()
  const list = usePoll(outboundsApi.list, 10000)
  const outs = list.data?.outbounds ?? null
  const [choice, setChoice] = useState(outboundId)
  const first = useRef(true)
  // back to what the hub has once a change is saved, or made elsewhere; the new outbound's state is
  // asked for again, since it has only just been put in use
  useEffect(() => {
    setChoice(outboundId)
    if (first.current) first.current = false
    else void list.reload()
  }, [outboundId])

  const current = outs?.find((o) => o.id === outboundId)
  const choices = routeChoices(outs ?? [], t, lang)
  // an outbound removed while still chosen stays in the list, under words that say so
  if (outboundId && outs && !current) choices.push({ id: outboundId, label: t('via.gone') })
  const state = current ? stateOf(current, t) : null

  return (
    <Card className="flex flex-col gap-3 p-4">
      <Head title={t('via.title')} hint={t(where === 'node' ? 'via.hint.node' : 'via.hint.router')} />
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <Select aria-label={t('via.title')} className="min-w-0 sm:max-w-sm" value={choice} disabled={!outs} onChange={(e) => setChoice(Number(e.target.value))}>
          {choices.map((c) => (
            <option key={c.id} value={c.id}>
              {c.label}
            </option>
          ))}
        </Select>
        <Button variant="primary" disabled={!outs || choice === outboundId} onClick={() => onSave(choice, routeName(t, choice, outs))}>
          {t('via.save')}
        </Button>
      </div>
      {current && state && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[13px]">
          <Badge tone={state.tone}>{state.label}</Badge>
          {/* the kind is in the choice already */}
          <span className="font-mono text-xs text-muted" dir="ltr">
            {serverText(current.host, current.port)}
          </span>
        </div>
      )}
      {current && state?.key === 'down' && <Notice tone="danger">{t('via.down', { name: current.name })}</Notice>}
      {current && state?.key === 'fallback' && <Notice tone="warn">{t('via.fallback', { name: current.name })}</Notice>}
      {outs && outs.length === 0 && (
        <p className="text-xs text-faint">
          {t('via.none')}{' '}
          <RouteLink to={{ page: 'root', tab: 'outbounds' }} className="font-medium text-foreground underline underline-offset-2">
            {t('via.openOutbounds')}
          </RouteLink>
        </p>
      )}
      {list.error != null && !outs && <ErrorBox>{errorText(list.error)}</ErrorBox>}
    </Card>
  )
}

/** Asks before a new "Leaves through": the router's Xray restarts, so open connections drop for a moment. */
export function RouteDialog({ title, place, target, call, onClose }: { title: string; place: string; target: string; call: () => Promise<unknown>; onClose: () => void }) {
  const { t } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  return (
    <Confirm
      title={title}
      action={t('via.action')}
      onClose={onClose}
      onConfirm={async () => {
        try {
          await call()
        } catch (e) {
          // Confirm shows what is thrown
          throw new Error(errorText(e))
        }
        toast(t('via.toast', { place, target }))
      }}
    >
      <p>{t('via.to', { target })}</p>
      <Notice tone="warn">{t('via.drop')}</Notice>
    </Confirm>
  )
}
