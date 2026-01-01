interface AppConfig {
  basePath: string
  appName: string
}

export function getConfig(): AppConfig {
  return window.__CONFIG__ || { basePath: "", appName: "NasNet Panel" }
}

export function getApiBaseUrl(): string {
  return getConfig().basePath
}
