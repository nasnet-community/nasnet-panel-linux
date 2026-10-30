import { ChevronLeft } from '../components/icons'
import { RouteLink } from '../components/link'
import { ErrorBox, Tabs } from '../components/ui'
import { usePoll } from '../hooks/usePoll'
import { navigate, type NodeTab } from '../hooks/useRoute'
import { useErrorText, useLang } from '../i18n'
import { api } from '../lib/api'
import { Soon } from './Soon'

/** A node's page: a way back, its name, and the Subscriptions, Status and Settings tabs. */
export function NodeScreen({ id, tab }: { id: string; tab: NodeTab }) {
  const { t } = useLang()
  const errorText = useErrorText()
  const node = usePoll(() => api.node(Number(id)), 10000, [id])
  const tabs: [NodeTab, string][] = [
    ['subscriptions', t('tabs.subscriptions')],
    ['status', t('tabs.status')],
    ['settings', t('tabs.settings')],
  ]
  return (
    <div className="flex flex-col gap-5">
      <RouteLink to={{ page: 'root', tab: 'nodes' }} className="inline-flex w-fit items-center gap-1.5 text-[13px] text-muted hover:text-foreground">
        <ChevronLeft className="h-4 w-4 rtl:-scale-x-100" />
        {t('node.all')}
      </RouteLink>
      {node.error != null && !node.data && <ErrorBox>{errorText(node.error)}</ErrorBox>}
      {node.data && (
        <div className="min-w-0">
          <h1 dir="auto" className="truncate text-lg font-semibold">
            {node.data.name}
          </h1>
          <div dir="ltr" className="truncate font-mono text-xs text-muted">
            {node.data.host}
          </div>
        </div>
      )}
      <Tabs label={t('tabs.node')} idBase="node" tabs={tabs} value={tab} onChange={(next) => navigate({ page: 'node', id, tab: next })} />
      <div role="tabpanel" id="node-panel" aria-labelledby={`node-${tab}`}>
        <Soon title={tabs.find(([key]) => key === tab)![1]} />
      </div>
    </div>
  )
}
