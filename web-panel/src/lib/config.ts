interface AppConfig {
  basePath: string
  appName: string
}

declare global {
  interface Window {
    __CONFIG__?: AppConfig
  }
}

const DEFAULT_CONFIG: AppConfig = { basePath: "", appName: "NasNet Panel" }

export function getConfig(): AppConfig {
  // In dev the placeholder in index.html is never substituted, so __CONFIG__ is
  // the raw string — truthy, but with no basePath. Only trust a real object.
  const cfg = window.__CONFIG__
  if (!cfg || typeof cfg !== "object") return DEFAULT_CONFIG
  return cfg
}

export function getApiBaseUrl(): string {
  return getConfig().basePath
}
