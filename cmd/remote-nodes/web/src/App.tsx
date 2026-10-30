import { useState } from 'react'
import { Moon, RemoteNodesLogo, Sun } from './components/icons'
import { Button } from './components/ui'
import { usePoll } from './hooks/usePoll'
import { api } from './lib/api'

function ThemeToggle() {
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
    <Button variant="ghost" size="icon" onClick={toggle} aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}>
      {theme === 'dark' ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
    </Button>
  )
}

export function App() {
  const config = usePoll(api.config, 30000)

  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-20 border-b border-border bg-surface">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-3 px-4">
          {/* "#/" alone resolves against <base href> and reloads the page when the panel's URL has no trailing slash */}
          <a
            href="#/"
            onClick={(e) => {
              e.preventDefault()
              window.location.hash = '/'
            }}
            className="flex shrink-0 items-center gap-2.5 font-semibold"
          >
            <RemoteNodesLogo size={30} />
            <span dir="ltr">Remote Nodes</span>
          </a>
          <span className="hidden truncate text-xs text-faint md:inline">Entry nodes whose traffic leaves through this router</span>
          <div className="ms-auto flex items-center gap-1 sm:gap-2">
            {config.data && (
              <span dir="ltr" className="hidden text-xs text-faint sm:inline">
                v{config.data.version}
              </span>
            )}
            <ThemeToggle />
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-4 py-6" />
    </div>
  )
}
