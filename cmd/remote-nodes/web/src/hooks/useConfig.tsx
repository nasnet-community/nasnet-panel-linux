import { createContext, useContext, useMemo, type ReactNode } from 'react'
import { api, type AppConfig } from '../lib/api'
import { usePoll } from './usePoll'

interface ConfigState {
  data: AppConfig | null
  /** what the last load threw */
  error: unknown
  reload: () => void
}

const ConfigContext = createContext<ConfigState>({ data: null, error: null, reload: () => {} })

/** Loads the plugin's config (limits, entry state, the Xray version it installs) and keeps it fresh. */
export function ConfigProvider({ children }: { children: ReactNode }) {
  const { data, error, reload } = usePoll(api.config, 30000)
  const value = useMemo(() => ({ data, error, reload: () => void reload() }), [data, error, reload])
  return <ConfigContext.Provider value={value}>{children}</ConfigContext.Provider>
}

/** The config once it has loaded, else null. */
export const useConfig = () => useContext(ConfigContext).data

/** The config with its load error and a way to load it again. */
export const useConfigState = () => useContext(ConfigContext)
