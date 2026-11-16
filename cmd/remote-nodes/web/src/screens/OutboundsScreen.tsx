import { useState, type FormEvent } from 'react'
import { Activity, Info, Pencil, Plus, Trash2, Waypoints } from '../components/icons'
import { Head } from '../components/section'
import { Badge, Button, Card, ErrorBox, Field, Input, Spinner, btn, cx, useToast } from '../components/ui'
import { usePoll } from '../hooks/usePoll'
import { useErrorText, useLang } from '../i18n'
import { api, outbounds as outboundsApi, type Outbound, type OutboundCheck, type TestResult } from '../lib/api'
import { checkUrlProblem, kindText, serverText, stateOf, trafficOf, trafficText, usersOf, usersText } from '../lib/outbounds'
import { sortByName, upDown } from '../lib/subscriptions'
import { AddOutboundDialog, EditOutboundDialog, RemoveOutboundDialog, TestAnswer } from './OutboundDialogs'
import { Name } from './SubscriptionParts'

type Modal = { kind: 'add' } | { kind: 'edit' | 'remove'; id: number }

/** A row's test: running, or what came back. */
type RowTest = { busy: true } | { busy: false; result: TestResult } | { busy: false; error: string }

/** Add outbound; at the limit it stays focusable and says why, as Add node does. Hangs its tooltip from a `relative` toolbar. */
function AddButton({ count, max, onAdd }: { count: number; max: number; onAdd: () => void }) {
  const { t } = useLang()
  const full = max > 0 && count >= max
  return (
    <div className="group ms-auto flex flex-wrap items-center justify-end gap-3">
      {max > 0 && <span className="text-xs text-faint">{t('ob.count', { n: count, max })}</span>}
      {full && (
        <Badge tone="idle" icon={<Info className="h-3.5 w-3.5" />}>
          {t('nodes.limitReached')}
        </Badge>
      )}
      <button
        type="button"
        aria-disabled={full || undefined}
        aria-describedby={full ? 'ob-cap-tip' : undefined}
        onClick={() => !full && onAdd()}
        className={btn('primary', 'md', 'aria-disabled:cursor-not-allowed aria-disabled:opacity-50 aria-disabled:hover:bg-accent')}
      >
        <Plus className="h-4 w-4" /> {t('ob.add')}
      </button>
      {full && (
        <div
          id="ob-cap-tip"
          role="tooltip"
          className="absolute inset-x-0 top-full z-30 mt-1.5 hidden rounded-md border border-border-strong bg-surface px-3 py-2.5 text-[13px] leading-normal shadow-lg group-focus-within:block group-hover:block sm:inset-x-auto sm:end-0 sm:w-[300px]"
        >
          <b>{t('ob.limit.title', { max })}</b> {t('ob.limit.why')}
        </div>
      )}
    </div>
  )
}

/** How an outbound is doing: a badge, and under it why it is down in the core's words. `full`: on a card, with room to say what the fallback does. */
function StateCell({ o, full = false }: { o: Outbound; full?: boolean }) {
  const { t } = useLang()
  const s = stateOf(o, t)
  return (
    <div data-state={s.key} className="flex min-w-0 flex-col gap-1">
      <Badge tone={s.tone}>{s.label}</Badge>
      {full && s.key === 'fallback' && <span className="text-xs text-muted">{t('ob.state.fallbackNote')}</span>}
      {s.detail && (
        <span dir="auto" title={s.detail} className={cx('line-clamp-2 break-words text-xs', s.key === 'down' ? 'text-bad-text' : 'text-warn-text', !full && 'max-w-[16rem]')}>
          {s.detail}
        </span>
      )}
    </div>
  )
}

/** The kind under a name: protocol, transport, security, in Latin whichever the page's language. */
const KindLine = ({ o }: { o: Outbound }) => (
  <span className="block text-xs font-normal text-muted">
    <span dir="ltr">{kindText(o)}</span>
  </span>
)

const Server = ({ o }: { o: Outbound }) => (
  <span dir="ltr" title={serverText(o.host, o.port)} className="inline-block max-w-full truncate align-bottom font-mono text-xs">
    {serverText(o.host, o.port)}
  </span>
)

/** What came back from a row's Test, read out when it arrives. */
function TestLine({ test }: { test: RowTest | undefined }) {
  return (
    <div role="status" className="min-w-0">
      {test && !test.busy && ('result' in test ? <TestAnswer result={test.result} /> : <span dir="auto" className="break-words text-xs text-bad-text">{test.error}</span>)}
    </div>
  )
}

/** Test, Edit and Remove. Remove on one in use says who uses it instead. */
function RowActions({ o, test, onTest, onEdit, onRemove, className }: { o: Outbound; test: RowTest | undefined; onTest: () => void; onEdit: () => void; onRemove: () => void; className?: string }) {
  const { t } = useLang()
  const edit = t('ob.edit.label', { name: o.name })
  const remove = t('ob.remove.label', { name: o.name })
  return (
    <div className={cx('flex items-center justify-end gap-1', className)}>
      <Button size="sm" aria-label={t('ob.test.label', { name: o.name })} busy={test?.busy} onClick={onTest}>
        {!test?.busy && <Activity className="h-3.5 w-3.5" />}
        {t('ob.test')}
      </Button>
      <Button size="sm" variant="ghost" aria-label={edit} title={edit} onClick={onEdit}>
        <Pencil className="h-4 w-4" />
      </Button>
      <Button size="sm" variant="ghost" aria-label={remove} title={remove} onClick={onRemove}>
        <Trash2 className="h-4 w-4" />
      </Button>
    </div>
  )
}

/** The address the health check fetches through each outbound in use. Empty is the hub's default, which the field shows. */
function CheckCard({ check, onSaved }: { check: OutboundCheck; onSaved: (c: OutboundCheck) => void }) {
  const { t } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const [url, setUrl] = useState(check.url)
  const [busy, setBusy] = useState<'save' | 'default' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const wanted = url.trim()
  const problem = checkUrlProblem(wanted) ? t('ob.check.invalid') : null
  const dirty = wanted !== check.url

  const save = async (next: string, how: 'save' | 'default') => {
    setBusy(how)
    setError(null)
    try {
      const fresh = await outboundsApi.setCheck(next)
      setUrl(fresh.url)
      toast(fresh.url ? t('ob.check.saved', { url: fresh.url }) : t('ob.check.reset'))
      onSaved(fresh)
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(null)
    }
  }

  return (
    <Card className="flex flex-col gap-3 p-4">
      <Head title={t('ob.check.title')} hint={t('ob.check.hint')} />
      <form
        className="flex flex-col gap-2 sm:flex-row sm:items-start"
        noValidate
        onSubmit={(e: FormEvent) => {
          e.preventDefault()
          if (dirty && !problem && !busy) void save(wanted, 'save')
        }}
      >
        <Field label={t('ob.check.label')} hint={t('ob.check.default', { url: check.defaultUrl })} error={problem} className="min-w-0 flex-1">
          {(p) => (
            <Input
              {...p}
              dir="ltr"
              type="url"
              inputMode="url"
              className="font-mono"
              value={url}
              placeholder={check.defaultUrl}
              onChange={(e) => setUrl(e.target.value)}
              spellCheck={false}
              autoCapitalize="off"
              autoComplete="off"
            />
          )}
        </Field>
        <div className="flex gap-2 sm:mt-[26px]">
          <Button type="submit" variant="primary" busy={busy === 'save'} disabled={!dirty || !!problem || !!busy}>
            {t('common.save')}
          </Button>
          {check.url && (
            <Button busy={busy === 'default'} disabled={!!busy} onClick={() => void save('', 'default')}>
              {t('ob.check.useDefault')}
            </Button>
          )}
        </div>
      </form>
      <p className="text-xs text-faint">{t('ob.check.every', { n: check.seconds })}</p>
      {error && <ErrorBox>{error}</ErrorBox>}
    </Card>
  )
}

/**
 * The Outbounds tab: the servers traffic can leave through instead of the router's VPN, how each is doing,
 * who uses it, and the address their health check fetches. A table on wide screens, cards below.
 */
export function OutboundsScreen() {
  const { t, fmt, lang } = useLang()
  const errorText = useErrorText()
  const list = usePoll(outboundsApi.list, 10000)
  // only for the names under "Used by"
  const nodes = usePoll(api.nodes, 30000)
  const [modal, setModal] = useState<Modal | null>(null)
  const [tests, setTests] = useState<Record<number, RowTest>>({})

  const data = list.data
  const rows = data ? sortByName(data.outbounds, lang) : []
  const users = (o: Outbound) => usersOf(o, nodes.data, t)
  const count = data?.outbounds.length ?? 0
  const max = data?.limit ?? 0
  const changed = () => void list.reload()
  const close = () => setModal(null)
  const add = () => setModal({ kind: 'add' })

  const runTest = async (o: Outbound) => {
    setTests((m) => ({ ...m, [o.id]: { busy: true } }))
    try {
      const result = await outboundsApi.test(o.id)
      setTests((m) => ({ ...m, [o.id]: { busy: false, result } }))
    } catch (e) {
      setTests((m) => ({ ...m, [o.id]: { busy: false, error: errorText(e) } }))
    }
  }

  const actions = (o: Outbound, className?: string) => (
    <RowActions
      o={o}
      test={tests[o.id]}
      className={className}
      onTest={() => void runTest(o)}
      onEdit={() => setModal({ kind: 'edit', id: o.id })}
      onRemove={() => setModal({ kind: 'remove', id: o.id })}
    />
  )
  const open = modal && modal.kind !== 'add' ? data?.outbounds.find((o) => o.id === modal.id) : undefined

  return (
    <div className="flex flex-col gap-4">
      <div className="relative flex flex-wrap items-center justify-between gap-3">
        <p className="text-[13px] text-muted">{t('ob.intro')}</p>
        <AddButton count={count} max={max} onAdd={add} />
      </div>

      {list.error != null && <ErrorBox>{errorText(list.error)}</ErrorBox>}
      {!data && list.error == null && <Spinner />}

      {data && rows.length === 0 && (
        <Card className="flex flex-col items-center gap-2 px-6 py-10 text-center">
          <Waypoints className="h-6 w-6 text-muted" />
          <h2 className="text-base font-semibold">{t('ob.empty.title')}</h2>
          <p className="max-w-md text-[13px] text-muted">{t('ob.empty.text')}</p>
          <Button variant="primary" className="mt-1" onClick={add}>
            <Plus className="h-4 w-4" /> {t('ob.addFirst')}
          </Button>
        </Card>
      )}

      {rows.length > 0 && (
        <>
          {/* a table from 1024 px: below that its six columns don't fit beside each other */}
          <Card className="hidden overflow-hidden lg:block">
            {/* relative, so the hidden text of a cell scrolls with the table instead of widening the page */}
            <div className="relative overflow-x-auto">
              <table aria-label={t('tabs.outbounds')} className="w-full text-[13px]">
                <thead className="border-b border-border text-xs text-muted">
                  <tr>
                    <th scope="col" className="py-2.5 pe-3 ps-4 text-start font-medium">
                      {t('ob.col.outbound')}
                    </th>
                    <th scope="col" className="px-3 py-2.5 text-start font-medium">
                      {t('ob.col.server')}
                    </th>
                    <th scope="col" className="px-3 py-2.5 text-start font-medium">
                      {t('ob.col.state')}
                    </th>
                    <th scope="col" className="px-3 py-2.5 text-start font-medium">
                      {t('ob.col.traffic')}
                    </th>
                    <th scope="col" className="px-3 py-2.5 text-start font-medium">
                      {t('ob.col.usedBy')}
                    </th>
                    <th scope="col" className="px-3 py-2.5">
                      <span className="sr-only">{t('ob.col.actions')}</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((o) => (
                    <tr key={o.id} className="border-b border-border align-top last:border-0">
                      <th scope="row" className="py-3 pe-3 ps-4 text-start font-medium">
                        <Name className="max-w-[200px]">{o.name}</Name>
                        <KindLine o={o} />
                      </th>
                      <td className="max-w-[220px] px-3 py-3">
                        <Server o={o} />
                      </td>
                      <td className="px-3 py-3">
                        <StateCell o={o} />
                        <TestLine test={tests[o.id]} />
                      </td>
                      <td className="tabular whitespace-nowrap px-3 py-3">
                        <div>{trafficText(fmt, o)}</div>
                        {trafficOf(o) > 0 && <div className="text-xs text-faint">{upDown(t, fmt, o.state.bytesUp, o.state.bytesDown)}</div>}
                      </td>
                      <td className={cx('max-w-[200px] px-3 py-3', users(o).length ? '' : 'text-faint')}>
                        <span dir="auto">{usersText(t, lang, users(o))}</span>
                      </td>
                      <td className="px-3 py-2.5">{actions(o)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>

          {/* cards on phones and tablets */}
          <ul aria-label={t('tabs.outbounds')} className="grid gap-3 sm:grid-cols-2 lg:hidden">
            {rows.map((o) => (
              <li key={o.id} className="flex min-w-0 flex-col gap-3 rounded-lg border border-border bg-card p-4">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="font-medium">
                      <Name>{o.name}</Name>
                    </div>
                    <KindLine o={o} />
                  </div>
                </div>
                <StateCell o={o} full />
                <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 text-[13px]">
                  <dt className="text-muted">{t('ob.col.server')}</dt>
                  <dd className="min-w-0">
                    <Server o={o} />
                  </dd>
                  <dt className="text-muted">{t('ob.col.traffic')}</dt>
                  <dd className="tabular">{trafficText(fmt, o)}</dd>
                  <dt className="text-muted">{t('ob.col.usedBy')}</dt>
                  <dd dir="auto" className={cx('min-w-0 break-words', users(o).length ? '' : 'text-faint')}>
                    {usersText(t, lang, users(o))}
                  </dd>
                </dl>
                <div className="mt-auto flex flex-wrap items-end justify-between gap-2">
                  <TestLine test={tests[o.id]} />
                  {actions(o, '-my-1 ms-auto')}
                </div>
              </li>
            ))}
          </ul>

          <p className="text-xs text-faint">{t('ob.legend')}</p>
        </>
      )}

      {/* a new key when the hub's address changes, so the field starts from it */}
      {data && <CheckCard key={data.check.url} check={data.check} onSaved={(check) => list.setData((d) => (d ? { ...d, check } : d))} />}

      {modal?.kind === 'add' && <AddOutboundDialog onClose={close} onAdded={changed} />}
      {modal?.kind === 'edit' && open && (
        <EditOutboundDialog
          outbound={open}
          onClose={close}
          onSaved={(o) => {
            // an answer was for the settings it had before
            setTests(({ [o.id]: _gone, ...rest }) => rest)
            changed()
          }}
        />
      )}
      {modal?.kind === 'remove' && open && (
        <RemoveOutboundDialog outbound={open} users={users(open)} usersText={usersText(t, lang, users(open))} onClose={close} onRemoved={changed} onRefused={changed} />
      )}
    </div>
  )
}
