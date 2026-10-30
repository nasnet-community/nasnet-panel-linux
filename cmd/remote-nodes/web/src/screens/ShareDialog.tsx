import { useEffect, useState } from 'react'
import { RefreshCw } from '../components/icons'
import { Button, Confirm, CopyButton, Dialog, ErrorBox, Notice, QrCode, Spinner, cx, useToast } from '../components/ui'
import { useErrorText, useLang } from '../i18n'
import { subscriptions, type Link } from '../lib/api'
import { portableLink } from '../lib/subscriptions'

/** What the dialog needs of a subscription: its name and the nodes it has a link on. */
export interface ShareTarget {
  id: number
  name: string
  /** the nodes it is switched on for */
  nodes: { id: number; name: string; unreachable: boolean }[]
  /** the node to show first; the first of `nodes` when left out */
  nodeId?: number
}

/** One of the nodes, side by side. The unreachable ones carry a red dot and say so to screen readers. */
function NodePicker({ nodes, value, onChange }: { nodes: ShareTarget['nodes']; value: number; onChange: (id: number) => void }) {
  const { t } = useLang()
  return (
    <div role="group" aria-label={t('subs.share.node')} className="flex w-full rounded-md border border-border bg-card2 p-0.5">
      {nodes.map((n) => (
        <button
          key={n.id}
          type="button"
          aria-pressed={n.id === value}
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
              <span className="sr-only"> {t('subs.unreachable')}</span>
            </>
          )}
        </button>
      ))}
    </div>
  )
}

/** The QR code and link of a subscription on one node, ready to send, and a way to replace the link. */
export function ShareDialog({ target, onClose }: { target: ShareTarget; onClose: () => void }) {
  const { t } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const [nodeId, setNodeId] = useState(target.nodeId ?? target.nodes[0]?.id)
  // a link once fetched is kept, so going back to a node needs no call, and a replaced one stays replaced
  const [links, setLinks] = useState<Record<number, Link>>({})
  const [error, setError] = useState<string | null>(null)
  const [replacing, setReplacing] = useState(false)
  const node = target.nodes.find((n) => n.id === nodeId)
  const link = nodeId === undefined ? undefined : links[nodeId]

  useEffect(() => {
    if (nodeId === undefined || links[nodeId]) return
    // a late answer for a node that is no longer shown is dropped
    let stale = false
    setError(null)
    subscriptions
      .link(target.id, nodeId)
      .then((l) => !stale && setLinks((all) => ({ ...all, [nodeId]: l })))
      .catch((e) => !stale && setError(errorText(e)))
    return () => {
      stale = true
    }
  }, [nodeId, target.id])

  const text = link ? portableLink(link.vless) : ''

  return (
    <Dialog onClose={onClose} title={t('subs.share.title', { name: target.name })} description={t('subs.share.desc')}>
      {!node || nodeId === undefined ? (
        <Notice>{t('subs.menu.shareOff')}</Notice>
      ) : (
        <div className="flex flex-col items-center gap-4">
          <NodePicker nodes={target.nodes} value={nodeId} onChange={setNodeId} />
          {error && (
            <div className="w-full">
              <ErrorBox>{error}</ErrorBox>
            </div>
          )}
          {link ? (
            <QrCode text={text} size={220} label={t('subs.share.qr', { name: target.name, node: node.name })} />
          ) : (
            !error && (
              <div className="flex h-[220px] w-[220px] items-center justify-center rounded-md bg-card2">
                <Spinner />
              </div>
            )
          )}
          {link && (
            // a link reads left to right in any language, or its punctuation lands in the wrong places
            <div dir="ltr" className="w-full break-all rounded-md border border-border bg-card2 p-3 text-start font-mono text-xs" data-link>
              {text}
            </div>
          )}
          <div className="grid w-full gap-2">
            {link ? (
              <CopyButton text={text} label={t('subs.share.copy')} variant="primary" size="md" className="w-full" />
            ) : (
              <Button variant="primary" disabled>
                {t('subs.share.copy')}
              </Button>
            )}
          </div>
          <p className="text-center text-xs text-faint">{t('subs.share.note')}</p>
          <div className="flex w-full flex-wrap items-center justify-between gap-2 border-t border-border pt-3">
            <span className="min-w-[180px] flex-1 text-xs text-faint">{t('subs.share.replaceNote')}</span>
            <Button size="sm" disabled={!link} onClick={() => setReplacing(true)}>
              <RefreshCw className="h-4 w-4" /> {t('subs.share.replace')}
            </Button>
          </div>
        </div>
      )}

      {replacing && node && nodeId !== undefined && (
        <Confirm
          title={t('subs.replace.title', { name: target.name })}
          action={t('subs.replace.action')}
          onClose={() => setReplacing(false)}
          onConfirm={async () => {
            try {
              const next = await subscriptions.replaceLink(target.id, nodeId)
              setLinks((all) => ({ ...all, [nodeId]: next }))
              toast(t('subs.toast.newLink', { name: target.name }))
            } catch (e) {
              // Confirm shows what is thrown, in the server's English unless it is worded here
              throw new Error(errorText(e))
            }
          }}
        >
          <p>{t('subs.replace.text')}</p>
          {target.nodes.length > 1 && <p>{t('subs.replace.only', { node: node.name })}</p>}
        </Confirm>
      )}
    </Dialog>
  )
}
