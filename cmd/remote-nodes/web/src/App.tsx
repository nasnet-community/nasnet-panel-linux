import { useEffect, useState } from 'react'
import { Moon, RemoteNodesLogo, Sun } from './components/icons'
import { JobTray } from './components/jobs'
import { RouteLink } from './components/link'
import { Button, ErrorBox } from './components/ui'
import { useConfigState } from './hooks/useConfig'
import { useRoute } from './hooks/useRoute'
import { LANGS, useErrorText, useLang } from './i18n'
import { NodeScreen } from './screens/NodeScreen'
import { RootScreen } from './screens/RootScreen'

function ThemeToggle() {
  const { t } = useLang()
  const [theme, setTheme] = useState(() => document.documentElement.dataset.theme ?? 'dark')
  const toggle = () => {
    const next = theme === 'dark' ? 'light' : 'dark'
    document.documentElement.dataset.theme = next
    try {
      localStorage.setItem('theme', next)
    } catch {
      // Storage may be blocked; the choice then lasts for this page view.
    }
    setTheme(next)
  }
  return (
    <Button variant="ghost" size="icon" onClick={toggle} aria-label={theme === 'dark' ? t('app.themeToLight') : t('app.themeToDark')}>
      {theme === 'dark' ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
    </Button>
  )
}

/** Switches to the other language, named in its own script. */
function LanguageButton() {
  const { lang, setLang } = useLang()
  const other = lang === 'en' ? 'fa' : 'en'
  const { name, short } = LANGS[other]
  return (
    <Button variant="ghost" size="icon" lang={other} aria-label={name} title={name} className={other === 'fa' ? 'text-[13px] font-semibold' : 'text-xs font-semibold'} onClick={() => setLang(other)}>
      {short}
    </Button>
  )
}

export function App() {
  const route = useRoute()
  const { t } = useLang()
  const errorText = useErrorText()
  const config = useConfigState()

  // a new page starts at the top, a new tab on the same page doesn't
  const page = route.page === 'node' ? `node-${route.id}` : 'root'
  useEffect(() => window.scrollTo(0, 0), [page])

  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-20 border-b border-border bg-surface">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-3 px-4">
          <RouteLink to={{ page: 'root', tab: 'nodes' }} className="flex shrink-0 items-center gap-2.5 font-semibold">
            <RemoteNodesLogo size={30} />
            <span dir="ltr">Remote Nodes</span>
          </RouteLink>
          <span className="hidden truncate text-xs text-faint md:inline">{t('app.subtitle')}</span>
          <div className="ms-auto flex items-center gap-1 sm:gap-2">
            <JobTray />
            {config.data && (
              <span dir="ltr" className="hidden text-xs text-faint sm:inline">
                v{config.data.version}
              </span>
            )}
            <LanguageButton />
            <ThemeToggle />
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-4 py-6">
        {config.error != null && !config.data && <ErrorBox>{errorText(config.error)}</ErrorBox>}
        {route.page === 'root' ? <RootScreen tab={route.tab} config={config.data} onConfigChanged={config.reload} /> : <NodeScreen key={route.id} id={route.id} tab={route.tab} />}
      </main>
    </div>
  )
}
