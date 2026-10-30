import { Tabs } from '../components/ui'
import { navigate, type RootTab } from '../hooks/useRoute'
import { useLang } from '../i18n'
import type { AppConfig } from '../lib/api'
import { AttentionList } from './Attention'
import { EntryCard, entryAttention } from './Entry'
import { Soon } from './Soon'

/** The first page: whether entry works, what needs attention, then the Nodes, Subscriptions and Settings tabs. */
export function RootScreen({ tab, config }: { tab: RootTab; config: AppConfig | null }) {
  const { t } = useLang()
  const tabs: [RootTab, string][] = [
    ['nodes', t('tabs.nodes')],
    ['subscriptions', t('tabs.subscriptions')],
    ['settings', t('tabs.settings')],
  ]
  return (
    <div className="flex flex-col gap-5">
      {config && <EntryCard entry={config.entry} />}
      {config && <AttentionList list={entryAttention(config.entry, t)} />}
      <Tabs label={t('tabs.sections')} idBase="root" tabs={tabs} value={tab} onChange={(next) => navigate({ page: 'root', tab: next })} />
      <div role="tabpanel" id="root-panel" aria-labelledby={`root-${tab}`}>
        <Soon title={tabs.find(([key]) => key === tab)![1]} />
      </div>
    </div>
  )
}
