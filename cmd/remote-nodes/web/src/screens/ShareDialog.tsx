import { useEffect, useState } from 'react'
import { Download, RefreshCw } from '../components/icons'
import { Button, Confirm, CopyButton, Dialog, ErrorBox, Notice, QrCode, Spinner, copyText, cx, useToast } from '../components/ui'
import { useErrorText, useLang } from '../i18n'
import { subscriptions, type Link, type PlaceId } from '../lib/api'
import { qrFileName, qrPng, saveBlob } from '../lib/qr'
import { portableLink } from '../lib/subscriptions'

/** A place a subscription has a link on: a node, or the router's own entry. */
export interface SharePlace {
  id: PlaceId
  name: string
  /** links for it don't work right now */
  unreachable: boolean
}

/** What the dialog needs of a subscription: its name and the places it has a link on. */
export interface ShareTarget {
  id: number
  name: string
  /** the places it is switched on for */
  places: SharePlace[]
  /** the place to show first; the first of `places` when left out */
  placeId?: PlaceId
}

/** A tab per place. The unreachable ones carry a red dot and say so to screen readers. */
function PlaceTabs({ places, value, onChange }: { places: SharePlace[]; value: PlaceId; onChange: (id: PlaceId) => void }) {
  const { t } = useLang()
  return (
    <div role="tablist" aria-label={t('subs.share.place')} className="flex w-full rounded-md border border-border bg-card2 p-0.5">
      {places.map((n) => (
        <button
          key={n.id}
          type="button"
          role="tab"
          aria-selected={n.id === value}
          onClick={() => onChange(n.id)}
          className={cx(
            'flex h-10 min-w-0 flex-1 items-center justify-center gap-1.5 whitespace-nowrap rounded-[6px] px-3 text-[13px] font-medium transition sm:h-7',
            'focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent',
            n.id === value ? 'bg-accent text-accent-fg' : 'text-muted hover:text-foreground',
          )}
        >
          <span dir="auto" className="truncate">
            {n.name}
          </span>
          {n.unreachable && (
            <>
              <span aria-hidden="true" className="h-1.5 w-1.5 shrink-0 rounded-full bg-bad" />
              <span className="sr-only"> {t(n.id === 'router' ? 'subs.notWorking' : 'subs.unreachable')}</span>
            </>
          )}
        </button>
      ))}
    </div>
  )
}

/**
 * The QR code and link of a subscription on one place, ready to send: as text, as an image of the code,
 * or every place's link in one paste. And a way to replace the link.
 */
export function ShareDialog({ target, onClose, onShared }: { target: ShareTarget; onClose: () => void; /** a link was copied or its code saved */ onShared?: () => void }) {
  const { t } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const [placeId, setPlaceId] = useState(target.placeId ?? target.places[0]?.id)
  // a link once fetched is kept, so going back to a place needs no call, and a replaced one stays replaced
  const [links, setLinks] = useState<Record<string, Link>>({})
  const [error, setError] = useState<string | null>(null)
  const [replacing, setReplacing] = useState(false)
  // the link is long: one line of it is enough to see that it is there
  const [whole, setWhole] = useState(false)
  const [busy, setBusy] = useState<'image' | 'all' | null>(null)
  const place = target.places.find((n) => n.id === placeId)
  const link = placeId === undefined ? undefined : links[placeId]
  const many = target.places.length > 1

  useEffect(() => {
    if (placeId === undefined || links[placeId]) return
    // a late answer for a place that is no longer shown is dropped
    let stale = false
    setError(null)
    subscriptions
      .linkAt(target.id, placeId)
      .then((l) => !stale && setLinks((all) => ({ ...all, [placeId]: l })))
      .catch((e) => !stale && setError(errorText(e)))
    return () => {
      stale = true
    }
  }, [placeId, target.id])

  const text = link ? portableLink(link.vless) : ''

  const saveImage = async () => {
    if (!place) return
    setBusy('image')
    try {
      saveBlob(await qrPng(text), qrFileName(target.name, place.name))
      onShared?.()
    } catch {
      toast(t('subs.share.imageFailed'), 'error')
    } finally {
      setBusy(null)
    }
  }

  // every place's link, one to a line: an app that takes several keeps them all, and can use another place
  const copyAll = async () => {
    setBusy('all')
    setError(null)
    try {
      const all: Record<string, Link> = { ...links }
      for (const p of target.places) all[p.id] ??= await subscriptions.linkAt(target.id, p.id)
      setLinks(all)
      await copyText(target.places.map((p) => portableLink(all[p.id].vless)).join('\n'))
      toast(t('subs.share.copiedAll', { n: target.places.length }))
      onShared?.()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(null)
    }
  }

  return (
    <Dialog onClose={onClose} title={t(many ? 'subs.share.titleMany' : 'subs.share.title', { name: target.name })} description={t(many ? 'subs.share.descMany' : 'subs.share.desc')}>
      {!place || placeId === undefined ? (
        <Notice>{t('subs.menu.shareOff')}</Notice>
      ) : (
        <div className="flex flex-col items-center gap-4">
          {many && <PlaceTabs places={target.places} value={placeId} onChange={setPlaceId} />}
          {error && (
            <div className="w-full">
              <ErrorBox>{error}</ErrorBox>
            </div>
          )}
          {link ? (
            <QrCode text={text} size={220} label={t('subs.share.qr', { name: target.name, node: place.name })} />
          ) : (
            !error && (
              <div className="flex h-[220px] w-[220px] items-center justify-center rounded-md bg-card2">
                <Spinner />
              </div>
            )
          )}
          {link && (
            <div className="flex w-full items-start gap-2 rounded-md border border-border bg-card2 p-2 ps-3">
              {/* a link reads left to right in any language, or its punctuation lands in the wrong places */}
              <div dir="ltr" className={cx('min-w-0 flex-1 py-1 text-start font-mono text-xs', whole ? 'break-all' : 'truncate')} data-link>
                {text}
              </div>
              <Button size="sm" variant="ghost" aria-expanded={whole} onClick={() => setWhole(!whole)}>
                {t(whole ? 'subs.share.hide' : 'subs.share.show')}
              </Button>
            </div>
          )}
          <div className="grid w-full gap-2 sm:grid-cols-2">
            {link ? (
              <CopyButton text={text} label={t('subs.share.copy')} variant="primary" size="md" className="w-full" onCopied={onShared} />
            ) : (
              <Button variant="primary" disabled>
                {t('subs.share.copy')}
              </Button>
            )}
            <Button disabled={!link || !!busy} busy={busy === 'image'} onClick={saveImage}>
              {busy !== 'image' && <Download className="h-4 w-4" />} {t('subs.share.saveImage')}
            </Button>
          </div>
          <p className="text-center text-xs text-faint">
            {place.id === 'router' && `${t('subs.share.noteRouter')} `}
            {t('subs.share.note')}
          </p>
          {many && (
            <div className="flex w-full flex-wrap items-center justify-between gap-2 border-t border-border pt-3">
              <span className="min-w-[180px] flex-1 text-xs text-faint">{t('subs.share.allNote')}</span>
              <Button size="sm" busy={busy === 'all'} disabled={!!busy} onClick={copyAll}>
                {t('subs.share.copyAll', { n: target.places.length })}
              </Button>
            </div>
          )}
          <div className="flex w-full flex-wrap items-center justify-between gap-2 border-t border-border pt-3">
            <span className="min-w-[180px] flex-1 text-xs text-faint">{t('subs.share.replaceNote')}</span>
            <Button size="sm" disabled={!link} onClick={() => setReplacing(true)}>
              <RefreshCw className="h-4 w-4" /> {t('subs.share.replace')}
            </Button>
          </div>
        </div>
      )}

      {replacing && place && placeId !== undefined && (
        <Confirm
          title={t('subs.replace.title', { name: target.name })}
          action={t('subs.replace.action')}
          onClose={() => setReplacing(false)}
          onConfirm={async () => {
            try {
              const next = await subscriptions.replaceLinkAt(target.id, placeId)
              setLinks((all) => ({ ...all, [placeId]: next }))
              toast(t('subs.toast.newLink', { name: target.name }))
            } catch (e) {
              // Confirm shows what is thrown, in the server's English unless it is worded here
              throw new Error(errorText(e))
            }
          }}
        >
          <p>{t('subs.replace.text')}</p>
          {many && <p>{t('subs.replace.only', { node: place.name })}</p>}
        </Confirm>
      )}
    </Dialog>
  )
}
