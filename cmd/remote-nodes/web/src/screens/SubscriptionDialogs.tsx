import { useState, type FormEvent, type ReactNode } from 'react'
import { Button, Confirm, Dialog, ErrorBox, Field, Input, Notice, cx, useToast } from '../components/ui'
import { useErrorText, useLang } from '../i18n'
import { ApiError, MAX_NAME_LENGTH, subscriptions, type Subscription, type SubscriptionPatch } from '../lib/api'
import { defaultNodeIds, limitText, localDay, readQuota, today, type NodeInfo } from '../lib/subscriptions'
import { ShareDialog, type ShareTarget } from './ShareDialog'

/** A subscription as the dialogs see it. */
export interface Named {
  id: number
  name: string
}

type Modal = { kind: 'add' } | { kind: 'share'; target: ShareTarget } | { kind: 'rename' | 'delete'; sub: Named } | { kind: 'quota' | 'reset'; sub: Subscription }

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
    <div className="grid gap-3 sm:grid-cols-2">
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
      <Field label={t('subs.quota.date')} hint={t('subs.quota.dateHint')} error={dateError}>
        {(p) => <Input {...p} type="date" dir="ltr" min={today()} value={form.day} onChange={(e) => onChange({ ...form, day: e.target.value })} />}
      </Field>
    </div>
  )
}

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

function Rename({ sub, onClose, onRenamed }: { sub: Named; onClose: () => void; onRenamed: () => void }) {
  const { t } = useLang()
  const toast = useToast()
  const [name, setName] = useState(sub.name)
  const [busy, setBusy] = useState(false)
  const errors = useSaveErrors()

  const submit = async () => {
    errors.clear()
    const trimmed = name.trim()
    if (!trimmed) return errors.setNameError(t('subs.add.errName'))
    if (trimmed === sub.name) return onClose()
    setBusy(true)
    try {
      const renamed = await subscriptions.rename(sub.id, trimmed)
      toast(t('subs.toast.renamed', { name: renamed.name }))
      onRenamed()
    } catch (e) {
      errors.set(e)
      setBusy(false)
    }
  }

  return (
    <Dialog
      onClose={onClose}
      title={t('subs.rename.title', { name: sub.name })}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="primary" type="submit" form="rename-subscription" busy={busy}>
            {t('subs.rename.save')}
          </Button>
        </>
      }
    >
      <NameForm id="rename-subscription" value={name} onChange={setName} error={errors.nameError} onSubmit={submit}>
        {errors.error && <ErrorBox>{errors.error}</ErrorBox>}
      </NameForm>
    </Dialog>
  )
}

function Quota({ sub, onClose, onSaved }: { sub: Subscription; onClose: () => void; onSaved: () => void }) {
  const { t, fmt } = useLang()
  const toast = useToast()
  const errorText = useErrorText()
  const initialDay = sub.expiresAt ? localDay(sub.expiresAt) : ''
  const [form, setForm] = useState<QuotaForm>({ gb: limitText(sub.trafficLimit), day: initialDay })
  const [bad, setBad] = useState<{ traffic: boolean; date: boolean }>({ traffic: false, date: false })
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    setError(null)
    // a date that is already past is let be when it is not the one being changed
    const dateChanged = form.day !== initialDay
    const read = readQuota(form.gb, dateChanged ? form.day : '')
    if (read.badLimit || read.badDate) return setBad({ traffic: read.badLimit, date: read.badDate })
    setBad({ traffic: false, date: false })
    const patch: SubscriptionPatch = {}
    if (read.limit !== (sub.trafficLimit > 0 ? sub.trafficLimit : null)) patch.trafficLimit = read.limit
    if (dateChanged) patch.expiresAt = form.day === '' ? null : readQuota('', form.day).expiresAt
    if (patch.trafficLimit === undefined && patch.expiresAt === undefined) return onClose()
    setBusy(true)
    try {
      const saved = await subscriptions.update(sub.id, patch)
      toast(t('subs.toast.quota', { name: saved.name }))
      onSaved()
    } catch (e) {
      setError(errorText(e))
      setBusy(false)
    }
  }

  return (
    <Dialog
      onClose={onClose}
      title={t('subs.quota.title', { name: sub.name })}
      description={t('subs.quota.desc')}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="primary" type="submit" form="subscription-quota" busy={busy}>
            {t('subs.quota.save')}
          </Button>
        </>
      }
    >
      <form
        id="subscription-quota"
        className="grid gap-3"
        autoComplete="off"
        noValidate
        onSubmit={(e: FormEvent) => {
          e.preventDefault()
          void submit()
        }}
      >
        <QuotaFields
          form={form}
          onChange={setForm}
          trafficError={bad.traffic ? t('subs.quota.errTraffic') : null}
          dateError={bad.date ? t('subs.quota.errDate') : null}
        />
        <p className="tabular text-[13px] text-muted">{t('subs.quota.usedNow', { used: fmt.bytes(sub.trafficUsed) })}</p>
        {error && <ErrorBox>{error}</ErrorBox>}
      </form>
    </Dialog>
  )
}

/**
 * The dialogs of the subscription screens, and how to open each: add, share, rename, delete, limits, reset.
 * `nodes` are the choices when adding and the names a created subscription's share dialog
 * shows; `onChanged` runs after anything was added, renamed or deleted, to reload the list.
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
      {modal.kind === 'quota' && (
        <Quota
          sub={modal.sub}
          onClose={close}
          onSaved={() => {
            onChanged()
            close()
          }}
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
      {modal.kind === 'rename' && (
        <Rename
          sub={modal.sub}
          onClose={close}
          onRenamed={() => {
            onChanged()
            close()
          }}
        />
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
    rename: (sub: Named) => setModal({ kind: 'rename', sub }),
    remove: (sub: Named) => setModal({ kind: 'delete', sub }),
    quota: (sub: Subscription) => setModal({ kind: 'quota', sub }),
    reset: (sub: Subscription) => setModal({ kind: 'reset', sub }),
  }
}
