import { getApiBaseUrl } from '@/lib/config'

const API_BASE_URL = getApiBaseUrl()

function storageKey(uuid: string): string {
    return `sub_auth_${uuid.slice(0, 8)}`
}

export function getSubAuthToken(uuid: string): string | null {
    if (typeof window === "undefined") return null
    try {
        return localStorage.getItem(storageKey(uuid))
    } catch {
        return null
    }
}

export function setSubAuthToken(uuid: string, token: string): void {
    if (typeof window === "undefined") return
    try {
        localStorage.setItem(storageKey(uuid), token)
    } catch {
        // Ignore storage errors
    }
}

export function clearSubAuthToken(uuid: string): void {
    if (typeof window === "undefined") return
    try {
        localStorage.removeItem(storageKey(uuid))
    } catch {
        // Ignore storage errors
    }
}

export async function verifySubPassword(
    uuid: string,
    password: string,
    remember: boolean
): Promise<{ success: boolean; token?: string; error?: string }> {
    try {
        const res = await fetch(`${API_BASE_URL}/api/v1/public/sub/${uuid}/auth`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            credentials: "include",
            body: JSON.stringify({ password, remember }),
        })
        const json = await res.json()
        if (json.success && json.data?.token) {
            setSubAuthToken(uuid, json.data.token)
            return { success: true, token: json.data.token }
        }
        // Backend may return a structured error object (e.g. {message, scope, since}
        // for 503 MAINTENANCE). Coerce to string so callers can render safely.
        let errMsg: string
        if (typeof json.error === "string") {
            errMsg = json.error
        } else if (json.error && typeof json.error === "object") {
            errMsg = json.error.message || "Invalid password"
        } else {
            errMsg = "Invalid password"
        }
        return { success: false, error: errMsg }
    } catch {
        return { success: false, error: "Network error" }
    }
}
