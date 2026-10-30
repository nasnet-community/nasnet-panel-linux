import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { JobsProvider } from './components/jobs'
import { ToastProvider } from './components/ui'
import { ConfigProvider } from './hooks/useConfig'
import { LangProvider } from './i18n'
import './index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <LangProvider>
      <ToastProvider>
        <ConfigProvider>
          <JobsProvider>
            <App />
          </JobsProvider>
        </ConfigProvider>
      </ToastProvider>
    </LangProvider>
  </StrictMode>,
)
