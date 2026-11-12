import { useEffect, useMemo, useState } from 'react'
import { Check, Copy, QrCode as QrIcon } from '../components/icons'
import { Button, Dialog, Notice, copyText, useToast } from '../components/ui'
import { useErrorText, useLang } from '../i18n'
import { subscriptions, type PlaceId } from '../lib/api'
import { browserStore, clearHandout, handoutLeft, markHandout, readHandout, startHandout, type Handout } from '../lib/handout'
import { portableLink, sortByName } from '../lib/subscriptions'
import type { AttentionItem } from './Attention'
import { ShareDialog } from './ShareDialog'

interface HandoutPlace {
  id: PlaceId
  name: string
}

/**
 * The list of new links to hand out for one place, after a change that made every link of it new:
 * who is on the list, a button that copies each one's link and one that shows its QR code, and a tick
 * on those that are done.
 */
function HandoutDialog({
  place,
  list,
  wait,
  onChange,
  onClose,
}: {
  place: HandoutPlace
  list: Handout
  /** why no link can be made right now, when none can */
  wait: string | null
  /** the list with one more tick, or null once it is forgotten */
  onChange: (next: Handout | null) => void
  onClose: () => void
}) {
  const { t, lang } = useLang()
  const errorText = useErrorText()
  const [busy, setBusy] = useState<number | null>(null)
  const [errors, setErrors] = useState<Record<number, string>>({})
  const [qr, setQr] = useState<{ id: number; name: string } | null>(null)
  const left = handoutLeft(list)
  // by name, as every list of subscriptions is
  const subs = useMemo(() => sortByName(list.subs, lang), [list.subs, lang])
  const sent = (id: number) => onChange(markHandout(browserStore(), place.id, list, id))

  const copy = async (id: number) => {
    setBusy(id)
    setErrors(({ [id]: _gone, ...rest }) => rest)
    try {
      await copyText(portableLink((await subscriptions.linkAt(id, place.id)).vless))
      sent(id)
    } catch (e) {
      setErrors((all) => ({ ...all, [id]: errorText(e) }))
    } finally {
      setBusy(null)
    }
  }

  return (
    <Dialog
      onClose={onClose}
      title={t('handout.title')}
      description={t('handout.desc', { place: place.name })}
      footer={
        <>
          <Button
            onClick={() => {
              clearHandout(browserStore(), place.id)
              onChange(null)
              onClose()
            }}
          >
            {t('handout.forget')}
          </Button>
          <Button variant="primary" onClick={onClose}>
            {t('handout.done')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {wait && <Notice>{wait}</Notice>}
        <p role="status" className="tabular text-[13px] text-muted">
          {t('handout.progress', { done: list.subs.length - left, total: list.subs.length })}
        </p>
        <ul aria-label={t('handout.title')} className="flex flex-col divide-y divide-border rounded-md border border-border">
          {subs.map((s) => {
            const done = list.done.includes(s.id)
            return (
              <li key={s.id} className="flex flex-col gap-1 px-3 py-2">
                <div className="flex min-h-9 items-center gap-2">
                  <span dir="auto" className="min-w-0 flex-1 truncate text-[13px] font-medium">
                    {s.name}
                  </span>
                  {done && (
                    <span className="inline-flex items-center gap-1 text-xs text-ok-text">
                      <Check className="h-3.5 w-3.5" /> {t('handout.sent')}
                    </span>
                  )}
                  <Button size="sm" variant={done ? 'ghost' : 'secondary'} busy={busy === s.id} disabled={!!wait || busy !== null} aria-label={t('handout.copyOf', { name: s.name })} onClick={() => void copy(s.id)}>
                    {busy !== s.id && <Copy className="h-3.5 w-3.5" />} {t('subs.share.copy')}
                  </Button>
                  <Button size="sm" variant="ghost" disabled={!!wait} aria-label={t('handout.qrOf', { name: s.name })} onClick={() => setQr(s)}>
                    <QrIcon className="h-4 w-4" />
                  </Button>
                </div>
                {errors[s.id] && (
                  <p role="alert" dir="auto" className="text-xs text-bad-text">
                    {errors[s.id]}
                  </p>
                )}
              </li>
            )
          })}
        </ul>
        <p className="text-xs text-faint">{t('handout.kept')}</p>
      </div>

      {qr && (
        <ShareDialog target={{ id: qr.id, name: qr.name, places: [{ id: place.id, name: place.name, unreachable: false }] }} onShared={() => sent(qr.id)} onClose={() => setQr(null)} />
      )}
    </Dialog>
  )
}

/**
 * A place's list of new links to hand out, kept in the browser: how to start one after its links
 * changed, the note that says some are still waiting, and the dialog the note opens.
 */
export function useHandout(place: HandoutPlace, wait: string | null) {
  const { t } = useLang()
  const toast = useToast()
  const [list, setList] = useState<Handout | null>(() => readHandout(browserStore(), place.id))
  const [open, setOpen] = useState(false)
  const left = list ? handoutLeft(list) : 0

  // every link sent and the list put away: there is nothing left to remember. (A copy that finishes
  // after the list was closed lands here too.)
  useEffect(() => {
    if (!list || left > 0 || open) return
    clearHandout(browserStore(), place.id)
    setList(null)
    toast(t('handout.toast.allSent'))
  }, [list, left, open])

  const note: AttentionItem | null =
    list && left > 0
      ? {
          tone: 'info',
          title: t('handout.note.title'),
          detail: t('handout.note.detail', { n: left, place: place.name }),
          acts: [{ label: t('handout.note.open'), primary: true, run: () => setOpen(true) }],
        }
      : null

  return {
    /** Starts the list from the subscriptions that are on the place now; `show` opens it at once. */
    start: (subs: { id: number; name: string }[], show: boolean) => {
      const next = startHandout(browserStore(), place.id, subs)
      setList(next)
      setOpen(show && next !== null)
    },
    note,
    dialog: open && list ? <HandoutDialog place={place} list={list} wait={wait} onChange={setList} onClose={() => setOpen(false)} /> : null,
  }
}
