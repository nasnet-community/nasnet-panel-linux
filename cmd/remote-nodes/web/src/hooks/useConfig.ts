import { createContext, useContext } from 'react'
import type { AppConfig } from '../lib/api'

/** The plugin's config (limits, entry state, the Xray version it installs), once it has loaded. */
export const ConfigContext = createContext<AppConfig | null>(null)

export const useConfig = () => useContext(ConfigContext)
