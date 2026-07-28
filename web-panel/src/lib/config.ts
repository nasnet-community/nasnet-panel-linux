interface AppConfig {
  basePath: string
  appName: string
}

declare global {
  interface Window {
    __CONFIG__?: AppConfig
  }
}

export function getConfig(): AppConfig {
  return window.__CONFIG__ || { basePath: "", appName: "NasNet Panel" }
}

export function getApiBaseUrl(): string {
  return getConfig().basePath
}
