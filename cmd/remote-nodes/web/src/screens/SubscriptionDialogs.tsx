import { useState, type FormEvent, type ReactNode } from 'react'
import { RotateCcw } from '../components/icons'
import { Button, Confirm, Dialog, ErrorBox, Field, Input, Notice, cx, useToast } from '../components/ui'
import { useErrorText, useLang } from '../i18n'
import { ApiError, MAX_NAME_LENGTH, subscriptions, type Subscription, type SubscriptionPatch } from '../lib/api'
import { defaultNodeIds, extendDay, limitText, localDay, readQuota, today, type Editable, type NodeInfo } from '../lib/subscriptions'
import { ShareDialog, type ShareTarget } from './ShareDialog'

/** A subscription as the dialogs see it. */
export interface Named {
  id: number
  name: string
}

type Modal = { kind: 'add' } | { kind: 'share'; target: ShareTarget } | { kind: 'delete' | 'reset'; sub: Named } | { kind: 'edit'; sub: Editable }

/** The share dialog's target for a subscription the API returned: the nodes it is on. */
export function shareTarget(s: Subscription, nodes: NodeInfo[]): ShareTarget {
  return {
    id: s.id,
    name: s.name,
    nodes: s.nodes.flatMap((n) => {
      const info = nodes.find((x) => x.id === n.nodeId)
      return n.enabled && info ? [{ id: info.id, name: info.name, unreachable: info.unreachable }] : []
    }),
  }
}

/** A name field and what to do with it, for adding and for renaming. */
function NameForm({
  id,
  value,
  onChange,
  error,
  onSubmit,
  children,
}: {
  id: string
  value: string
  onChange: (name: string) => void
  /** what is wrong with the name; it replaces the hint */
  error: string | null
  onSubmit: () => void
  children?: ReactNode
}) {
  const { t } = useLang()
  return (
    <form
      id={id}
      className="grid gap-3"
      autoComplete="off"
      // the page words what is wrong itself; the browser's own bubble for a date's min would come first
      noValidate
      onSubmit={(e: FormEvent) => {
        e.preventDefault()
        onSubmit()
      }}
    >
      <Field label={t('subs.name')} hint={t('subs.add.nameHint')} error={error}>
        {(p) => <Input {...p} dir="auto" value={value} onChange={(e) => onChange(e.target.value)} maxLength={MAX_NAME_LENGTH} />}
      </Field>
      {children}
    </form>
  )
}

/** Fails a save the way the API did: the name already used belongs under the name field, anything else in a box. */
function useSaveErrors() {
  const errorText = useErrorText()
  const [nameError, setNameError] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  return {
    nameError,
    error,
    clear: () => {
      setNameError(null)
      setError(null)
    },
    set: (e: unknown) => (e instanceof ApiError && e.code === 'name_taken' ? setNameError(errorText(e)) : setError(errorText(e))),
    setNameError,
  }
}

/** The traffic limit and the end date, as the text of two fields. Read when the form is saved. */
interface QuotaForm {
  gb: string
  day: string
}

/** The two optional fields of a subscription's limits; an empty one means none. */
function QuotaFields({
  form,
  onChange,
  trafficError,
  dateError,
}: {
  form: QuotaForm
  onChange: (next: QuotaForm) => void
  trafficError: string | null
  dateError: string | null
}) {
  const { t } = useLang()
  return (
    <div className="grid gap-3 sm:grid-cols-[minmax(0,4fr)_minmax(0,6fr)]">
      <Field label={t('subs.quota.traffic')} hint={t('subs.quota.trafficHint')} error={trafficError}>
        {(p) => (
          <div className="flex items-center gap-2">
            <Input
              {...p}
              inputMode="decimal"
              autoComplete="off"
              dir="ltr"
              className="min-w-0 flex-1"
              placeholder={t('subs.quota.none')}
              value={form.gb}
              onChange={(e) => onChange({ ...form, gb: e.target.value })}
            />
            <span className="text-[13px] text-muted">{t('subs.quota.trafficUnit')}</span>
          </div>
        )}
      </Field>
      <div className="flex flex-col gap-1.5">
        <Field label={t('subs.quota.date')} hint={t('subs.quota.dateHint')} error={dateError}>
          {(p) => <Input {...p} type="date" dir="ltr" min={today()} value={form.day} onChange={(e) => onChange({ ...form, day: e.target.value })} />}
        </Field>
        {/* the usual terms, a click each: from the date set, or from today once it has passed */}
        <div role="group" aria-label={t('subs.quota.presets')} className="flex flex-wrap gap-1.5">
          {([{ days: 30 }, { days: 90 }, { years: 1 }] as const).map((by) => (
            <button key={'days' in by ? by.days : 'y'} type="button" className={CHIP} onClick={() => onChange({ ...form, day: extendDay(form.day, by) })}>
              {'days' in by ? t('subs.quota.plusDays', { n: by.days }) : t('subs.quota.plusYears', { n: by.years })}
            </button>
          ))}
          {form.day !== '' && (
            <button type="button" className={CHIP} onClick={() => onChange({ ...form, day: '' })}>
              {t('subs.quota.noEnd')}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

const CHIP = cx(
  'inline-flex min-h-11 items-center rounded-full border border-border px-3 text-xs font-medium text-muted transition sm:min-h-7 sm:px-2.5',
  'hover:bg-track hover:text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent',
)

// Mounted only while open, so each time starts from empty fields.
function AddSubscription({
  nodes,
  left,
  onClose,
  onAdded,
}: {
  /** null while the list loads */
  nodes: NodeInfo[] | null
  /** places left under the limit; null until the limit is known */
  left: number | null
  onClose: () => void
  onAdded: (created: Subscription) => void
}) {
  const { t } = useLang()
  const toast = useToast()
  const [name, setName] = useState('')
  // null until a box is touched: the nodes that work are ticked, whenever the list arrives
  const [picked, setPicked] = useState<Set<number> | null>(null)
  const [quota, setQuota] = useState<QuotaForm>({ gb: '', day: '' })
  const [quotaError, setQuotaError] = useState<{ traffic: boolean; date: boolean }>({ traffic: false, date: false })
  const [busy, setBusy] = useState(false)
  const errors = useSaveErrors()
  const ticked = picked ?? new Set(defaultNodeIds(nodes ?? []))

  const submit = async () => {
    errors.clear()
    setQuotaError({ traffic: false, date: false })
    const trimmed = name.trim()
    if (!trimmed) return errors.setNameError(t('subs.add.errName'))
    const read = readQuota(quota.gb, quota.day)
    if (read.badLimit || read.badDate) return setQuotaError({ traffic: read.badLimit, date: read.badDate })
    setBusy(true)
    try {
      const nodeIds = (nodes ?? []).filter((n) => ticked.has(n.id) && !n.installing).map((n) => n.id)
      const created = await subscriptions.create({
        name: trimmed,
        nodeIds,
        ...(read.limit !== null && { trafficLimit: read.limit }),
        ...(read.expiresAt !== null && { expiresAt: read.expiresAt }),
      })
      toast(t('subs.toast.added', { name: created.name }))
      onAdded(created)
    } catch (e) {
      errors.set(e)
      setBusy(false)
    }
  }

  const toggle = (id: number, on: boolean) => {
    const next = new Set(ticked)
    if (on) next.add(id)
    else next.delete(id)
    setPicked(next)
  }

  return (
    <Dialog
      onClose={onClose}
      title={t('subs.add.title')}
      description={left === null ? undefined : t('subs.add.desc', { n: left })}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="primary" type="submit" form="add-subscription" busy={busy}>
            {t('subs.add')}
          </Button>
        </>
      }
    >
      <NameForm id="add-subscription" value={name} onChange={setName} error={errors.nameError} onSubmit={submit}>
        {nodes && nodes.length === 0 ? (
          <Notice>{t('subs.add.noNodes')}</Notice>
        ) : (
          nodes && (
            <fieldset className="flex flex-col">
              <legend className="mb-1 text-[13px] font-medium text-muted">{t('subs.add.nodes')}</legend>
              {nodes.map((n) => (
                <label key={n.id} className={cx('flex min-h-11 items-center gap-2.5 text-[13px] sm:min-h-8', n.installing && 'cursor-not-allowed text-muted')}>
                  <input
                    type="checkbox"
                    className="h-4 w-4 shrink-0"
                    checked={ticked.has(n.id) && !n.installing}
                    disabled={n.installing}
                    onChange={(e) => toggle(n.id, e.target.checked)}
                  />
                  <span>
                    <span dir="auto">{n.name}</span>
                    {n.unreachable && <span className="text-xs text-faint"> {t('subs.unreachable')}</span>}
                    {n.installing && <span className="text-xs text-faint"> {t('subs.add.installing')}</span>}
                  </span>
                </label>
              ))}
              <p className="mt-1 text-xs text-faint">{t('subs.add.nodesHint')}</p>
            </fieldset>
          )
        )}
        <QuotaFields
          form={quota}
          onChange={setQuota}
          trafficError={quotaError.traffic ? t('subs.quota.errTraffic') : null}
          dateError={quotaError.date ? t('subs.quota.errDate') : null}
        />
        {errors.error && <ErrorBox>{errors.error}</ErrorBox>}
      </NameForm>
    </Dialog>
  )
}

/**
 * Edit: the name, the traffic limit and the end date in one go. Only what changed is sent, so an
 * unchanged form asks the hub nothing, and a subscription whose date has passed can be renamed
 * without that date being refused. A stopped one says why, and what starts it again.
 */
function EditSubscription({ sub, onClose, onSaved, onReset }: { sub: Editable; onClose: () => void; onSaved: () => void; onReset: () => void }) {
  const { t, fmt } = useLang()
  const toast = useToast()
  const initialDay = sub.expiresAt ? localDay(sub.expiresAt) : ''
  const [name, setName] = useState(sub.name)
  const [form, setForm] = useState<QuotaForm>({ gb: limitText(sub.trafficLimit), day: initialDay })
  const [bad, setBad] = useState<{ traffic: boolean; date: boolean }>({ traffic: false, date: false })
  const [busy, setBusy] = useState(false)
  const errors = useSaveErrors()

  const submit = async () => {
    errors.clear()
    setBad({ traffic: false, date: false })
    const trimmed = name.trim()
    if (!trimmed) return errors.setNameError(t('subs.add.errName'))
    // a date that is already past is let be when it is not the one being changed
    const dateChanged = form.day !== initialDay
    const read = readQuota(form.gb, dateChanged ? form.day : '')
    if (read.badLimit || read.badDate) return setBad({ traffic: read.badLimit, date: read.badDate })
    const patch: SubscriptionPatch = {}
    if (trimmed !== sub.name) patch.name = trimmed
    if (read.limit !== (sub.trafficLimit > 0 ? sub.trafficLimit : null)) patch.trafficLimit = read.limit
    if (dateChanged) patch.expiresAt = form.day === '' ? null : read.expiresAt
    if (!Object.keys(patch).length) return onClose()
    setBusy(true)
    try {
      const saved = await subscriptions.update(sub.id, patch)
      const renamedOnly = Object.keys(patch).length === 1 && patch.name !== undefined
      toast(renamedOnly ? t('subs.toast.renamed', { name: saved.name }) : t('subs.toast.saved', { name: saved.name }))
      onSaved()
    } catch (e) {
      errors.set(e)
      setBusy(false)
    }
  }

  return (
    <Dialog
      onClose={onClose}
      title={t('subs.edit.title', { name: sub.name })}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="primary" type="submit" form="edit-subscription" busy={busy}>
            {t('subs.edit.save')}
          </Button>
        </>
      }
    >
      {sub.stopped === 'limit' && <Notice tone="warn" className="mb-3">{t('subs.edit.stoppedLimit', { name: sub.name })}</Notice>}
      {sub.stopped === 'expired' && sub.expiresAt && (
        <Notice tone="warn" className="mb-3">
          {t('subs.edit.stoppedExpired', { name: sub.name, date: fmt.date(sub.expiresAt) })}
        </Notice>
      )}
      <NameForm id="edit-subscription" value={name} onChange={setName} error={errors.nameError} onSubmit={submit}>
        <QuotaFields
          form={form}
          onChange={setForm}
          trafficError={bad.traffic ? t('subs.quota.errTraffic') : null}
          dateError={bad.date ? t('subs.quota.errDate') : null}
        />
        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border pt-3">
          <span className="tabular text-[13px] text-muted">{t('subs.edit.used', { used: fmt.bytes(sub.trafficUsed) })}</span>
          <Button size="sm" variant="ghost" onClick={onReset}>
            <RotateCcw className="h-4 w-4" /> {t('subs.edit.reset')}
          </Button>
        </div>
        {errors.error && <ErrorBox>{errors.error}</ErrorBox>}
      </NameForm>
    </Dialog>
  )
}

/**
 * The dialogs of the subscription screens, and how to open each: add, share, edit, reset usage, delete.
 * `nodes` are the choices when adding and the names a created subscription's share dialog
 * shows; `onChanged` runs after anything was added, edited, reset or deleted, to reload the list.
 */
export function useSubscriptionDialogs({ nodes, count, max, onChanged }: { nodes: NodeInfo[] | null; count: number; max: number; onChanged: () => void }) {
  const { t } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const [modal, setModal] = useState<Modal | null>(null)
  const close = () => setModal(null)

  const dialogs = modal && (
    <>
      {modal.kind === 'add' && (
        <AddSubscription
          nodes={nodes}
          left={max > 0 ? Math.max(0, max - count) : null}
          onClose={close}
          onAdded={(created) => {
            onChanged()
            // its link is ready at once, so the next thing is to hand it over
            const target = shareTarget(created, nodes ?? [])
            setModal(target.nodes.length ? { kind: 'share', target } : null)
          }}
        />
      )}
      {modal.kind === 'share' && <ShareDialog target={modal.target} onClose={close} />}
      {modal.kind === 'edit' && (
        <EditSubscription
          sub={modal.sub}
          onClose={close}
          onSaved={() => {
            onChanged()
            close()
          }}
          // the confirm takes the dialog's place, and the list shows the new count after it
          onReset={() => setModal({ kind: 'reset', sub: modal.sub })}
        />
      )}
      {modal.kind === 'reset' && (
        <Confirm
          title={t('subs.reset.title', { name: modal.sub.name })}
          action={t('subs.reset.action')}
          onClose={close}
          onConfirm={async () => {
            try {
              await subscriptions.resetUsage(modal.sub.id)
            } catch (e) {
              throw new Error(errorText(e))
            }
            toast(t('subs.toast.reset', { name: modal.sub.name }))
            onChanged()
          }}
        >
          <p>{t('subs.reset.text')}</p>
        </Confirm>
      )}
      {modal.kind === 'delete' && (
        <Confirm
          danger
          title={t('subs.delete.title', { name: modal.sub.name })}
          action={t('subs.menu.delete')}
          onClose={close}
          onConfirm={async () => {
            try {
              await subscriptions.remove(modal.sub.id)
            } catch (e) {
              // Confirm shows what is thrown, in the server's English unless it is worded here
              throw new Error(errorText(e))
            }
            toast(t('subs.toast.deleted', { name: modal.sub.name }))
            onChanged()
          }}
        >
          <p>{t('subs.delete.text')}</p>
        </Confirm>
      )}
    </>
  )

  return {
    dialogs,
    add: () => setModal({ kind: 'add' }),
    share: (target: ShareTarget) => setModal({ kind: 'share', target }),
    edit: (sub: Editable) => setModal({ kind: 'edit', sub }),
    reset: (sub: Named) => setModal({ kind: 'reset', sub }),
    remove: (sub: Named) => setModal({ kind: 'delete', sub }),
  }
}
