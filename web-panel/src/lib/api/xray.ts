import { api } from "../api"

export interface XrayPlatformInfo {
  cached: boolean
  size: number
  checksum: string
}

export interface XrayVersionInfo {
  version: string
  is_default: boolean
  platforms: Record<string, XrayPlatformInfo> // keyed by arch: "amd64" | "arm64"
}

// These endpoints return raw objects (not the {success,data} envelope), so use
// the raw client methods.
export async function listXrayVersions(): Promise<{ versions: XrayVersionInfo[] }> {
  return api.getRaw<{ versions: XrayVersionInfo[] }>("/api/v1/xray/versions")
}

// uploadXrayBinary PUTs the raw binary; the server detects arch from the ELF.
export async function uploadXrayBinary(version: string, file: File): Promise<{ version: string; arch: string }> {
  return api.putRaw<{ version: string; arch: string }>(
    `/api/v1/xray/binary?version=${encodeURIComponent(version)}`,
    file,
  )
}

export async function deleteXrayVersion(version: string): Promise<void> {
  await api.delete(`/api/v1/xray/binary?version=${encodeURIComponent(version)}`)
}

export async function triggerXrayDownload(version: string): Promise<void> {
  await api.post(`/api/v1/xray/download?version=${encodeURIComponent(version)}`, {})
}
