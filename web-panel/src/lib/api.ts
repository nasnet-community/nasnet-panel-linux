import { toast } from "sonner"
import { getApiBaseUrl } from './config'

const API_BASE_URL = getApiBaseUrl()
const BASE_PATH = getApiBaseUrl()

export interface ApiResponse<T = unknown> {
    success: boolean
    data?: T
    error?: string
    code?: string
    warning?: string
}

export interface UserInfo {
    id: number
    telegram_id: number
    username: string
    first_name: string
    last_name: string
    is_admin: boolean
    balance: number
}

export interface LoginResponse {
    user: UserInfo
    expires_at: string
}

export class ApiError extends Error {
    code?: string
    status?: number

    constructor(message: string, code?: string, status?: number) {
        super(message)
        this.code = code
        this.status = status
        this.name = "ApiError"
    }
}

class ApiClient {
    private baseUrl: string
    private isRefreshing = false
    private refreshPromise: Promise<boolean> | null = null
    private defaultTimeoutMs = 30000

    constructor(baseUrl: string) {
        this.baseUrl = baseUrl
    }

    private fetchWithTimeout(url: string, options: RequestInit, timeoutMs = this.defaultTimeoutMs): Promise<Response> {
        const controller = new AbortController()
        const timeout = setTimeout(() => controller.abort(), timeoutMs)
        return fetch(url, { ...options, signal: controller.signal }).finally(() => clearTimeout(timeout)).catch((error) => {
            if (error instanceof DOMException && error.name === "AbortError") {
                throw new ApiError("Request timed out", "TIMEOUT")
            }
            throw error
        })
    }

    private async parseJson<T>(response: Response): Promise<T> {
        try {
            return await response.json()
        } catch {
            throw new ApiError("Server returned invalid response", "INVALID_RESPONSE", response.status)
        }
    }

    private async request<T>(
        endpoint: string,
        options: RequestInit = {},
        skipRefresh = false
    ): Promise<ApiResponse<T>> {
        const url = `${this.baseUrl}${endpoint}`

        try {
            const response = await this.fetchWithTimeout(url, {
                ...options,
                credentials: "include", // Include cookies
                headers: {
                    "Content-Type": "application/json",
                    ...options.headers,
                },
            })

            const data: ApiResponse<T> = await this.parseJson(response)

            if (!response.ok) {
                // Handle token expiration (but skip if this is already a refresh attempt)
                if (response.status === 401 && !skipRefresh) {
                    // Try to refresh token
                    const refreshed = await this.refreshToken()
                    if (refreshed) {
                        // Retry the original request with skipRefresh=true to avoid infinite loop
                        return this.request<T>(endpoint, options, true)
                    } else {
                        // Refresh failed, redirect to login (skip for public pages like /sub/)
                        if (typeof window !== "undefined" && window.location.pathname !== `${BASE_PATH}/login` && !window.location.pathname.startsWith(`${BASE_PATH}/sub/`)) {
                            window.location.href = `${BASE_PATH}/login`
                            // Return a pending promise to prevent further errors while redirecting
                            return new Promise(() => { })
                        }
                    }
                }
                if (response.status === 503) {
                    const body = await response.clone().json().catch(() => null)
                    if (body && body.code === "MAINTENANCE") {
                        const msg = body?.error?.message || "Service maintenance in progress"
                        toast.warning(msg)
                        throw new ApiError(msg, "MAINTENANCE", 503)
                    }
                }
                throw new ApiError(data.error || `HTTP error ${response.status}`, data.code, response.status)
            }

            return data
        } catch (error) {
            if (error instanceof ApiError) {
                throw error
            }
            const message = error instanceof Error ? error.message : "Network error"
            throw new Error(message)
        }
    }

    async get<T>(endpoint: string): Promise<ApiResponse<T>> {
        return this.request<T>(endpoint, { method: "GET" })
    }

    async post<T>(endpoint: string, body?: unknown): Promise<ApiResponse<T>> {
        return this.request<T>(endpoint, {
            method: "POST",
            body: body ? JSON.stringify(body) : undefined,
        })
    }

    async put<T>(endpoint: string, body?: unknown): Promise<ApiResponse<T>> {
        return this.request<T>(endpoint, {
            method: "PUT",
            body: body ? JSON.stringify(body) : undefined,
        })
    }

    async patch<T>(endpoint: string, body?: unknown): Promise<ApiResponse<T>> {
        return this.request<T>(endpoint, {
            method: "PATCH",
            body: body ? JSON.stringify(body) : undefined,
        })
    }

    async delete<T>(endpoint: string): Promise<ApiResponse<T>> {
        return this.request<T>(endpoint, { method: "DELETE" })
    }

    // Raw request that returns the full JSON response including meta fields
    async getRaw<T = unknown>(endpoint: string): Promise<T> {
        const url = `${this.baseUrl}${endpoint}`

        const doFetch = async (skipRefresh = false): Promise<T> => {
            const response = await this.fetchWithTimeout(url, {
                method: "GET",
                credentials: "include",
                headers: { "Content-Type": "application/json" },
            })

            if (!response.ok && response.status === 401 && !skipRefresh) {
                const refreshed = await this.refreshToken()
                if (refreshed) return doFetch(true)
                if (typeof window !== "undefined" && window.location.pathname !== `${BASE_PATH}/login` && !window.location.pathname.startsWith(`${BASE_PATH}/sub/`)) {
                    window.location.href = `${BASE_PATH}/login`
                    return new Promise(() => {})
                }
            }

            if (!response.ok) {
                if (response.status === 503) {
                    const body = await response.clone().json().catch(() => null)
                    if (body && body.code === "MAINTENANCE") {
                        const msg = body?.error?.message || "Service maintenance in progress"
                        toast.warning(msg)
                        throw new ApiError(msg, "MAINTENANCE", 503)
                    }
                }
                const data = await this.parseJson<ApiResponse>(response)
                throw new ApiError(data.error || `HTTP error ${response.status}`, data.code, response.status)
            }

            return this.parseJson<T>(response)
        }

        return doFetch()
    }

    async postForm<T>(endpoint: string, formData: FormData): Promise<ApiResponse<T>> {
        const url = `${this.baseUrl}${endpoint}`

        try {
            const response = await this.fetchWithTimeout(url, {
                method: "POST",
                credentials: "include",
                body: formData,
                // Don't set Content-Type - browser will set it with boundary
            })

            const data: ApiResponse<T> = await this.parseJson<ApiResponse<T>>(response)

            if (!response.ok) {
                if (response.status === 401) {
                    const refreshed = await this.refreshToken()
                    if (refreshed) {
                        // Retry with fresh token
                        const retryResponse = await this.fetchWithTimeout(url, {
                            method: "POST",
                            credentials: "include",
                            body: formData,
                        })
                        return this.parseJson<ApiResponse<T>>(retryResponse)
                    } else {
                        // Refresh failed, redirect to login (skip for public pages like /sub/)
                        if (typeof window !== "undefined" && window.location.pathname !== `${BASE_PATH}/login` && !window.location.pathname.startsWith(`${BASE_PATH}/sub/`)) {
                            window.location.href = `${BASE_PATH}/login`
                            // Return a pending promise to prevent further errors while redirecting
                            return new Promise(() => { })
                        }
                    }
                }
                throw new ApiError(data.error || `HTTP error ${response.status}`, data.code, response.status)
            }

            return data
        } catch (error) {
            if (error instanceof ApiError) {
                throw error
            }
            const message = error instanceof Error ? error.message : "Network error"
            throw new Error(message)
        }
    }

    // putRaw sends a raw binary body (e.g. an uploaded file) and returns the
    // parsed JSON directly. Mirrors getRaw's 401-refresh handling.
    async putRaw<T = unknown>(endpoint: string, body: BodyInit): Promise<T> {
        const url = `${this.baseUrl}${endpoint}`

        const doFetch = async (skipRefresh = false): Promise<T> => {
            const response = await this.fetchWithTimeout(url, {
                method: "PUT",
                credentials: "include",
                headers: { "Content-Type": "application/octet-stream" },
                body,
            }, 120000)

            if (!response.ok && response.status === 401 && !skipRefresh) {
                const refreshed = await this.refreshToken()
                if (refreshed) return doFetch(true)
                if (typeof window !== "undefined" && window.location.pathname !== `${BASE_PATH}/login` && !window.location.pathname.startsWith(`${BASE_PATH}/sub/`)) {
                    window.location.href = `${BASE_PATH}/login`
                    return new Promise(() => {})
                }
            }

            if (!response.ok) {
                const data = await this.parseJson<ApiResponse>(response)
                throw new ApiError(data.error || `HTTP error ${response.status}`, data.code, response.status)
            }

            return this.parseJson<T>(response)
        }

        return doFetch()
    }

    // Auth-specific methods
    async login(username: string, password: string, rememberMe?: boolean): Promise<ApiResponse<LoginResponse>> {
        const response = await this.post<LoginResponse>("/api/v1/auth/admin-login", {
            username,
            password,
            remember_me: rememberMe,
        })
        return response
    }

    async logout(): Promise<void> {
        try {
            await this.post("/api/v1/auth/logout")
        } catch {
            // Ignore logout errors
        }
    }

    async getCurrentUser(): Promise<ApiResponse<UserInfo>> {
        return this.get<UserInfo>("/api/v1/auth/me")
    }

    private async refreshToken(): Promise<boolean> {
        // If already refreshing, all callers wait on the same promise
        if (this.refreshPromise) {
            return this.refreshPromise
        }

        this.isRefreshing = true
        this.refreshPromise = (async () => {
            try {
                const response = await this.fetchWithTimeout(`${this.baseUrl}/api/v1/auth/refresh`, {
                    method: "POST",
                    credentials: "include",
                    headers: {
                        "Content-Type": "application/json",
                    },
                }, 10000)
                return response.ok
            } catch {
                return false
            } finally {
                this.isRefreshing = false
                this.refreshPromise = null
            }
        })()

        return this.refreshPromise
    }
}

export const api = new ApiClient(API_BASE_URL)

// Helper function to handle API errors with toast
export async function apiWithToast<T>(
    promise: Promise<ApiResponse<T>>,
    options?: {
        loading?: string
        success?: string
        error?: string
    }
): Promise<T | null> {
    const { loading = "Loading...", success, error } = options || {}

    let loadingToastId: string | number | undefined
    try {
        if (loading) {
            loadingToastId = toast.loading(loading)
        }
        const response = await promise

        if (loadingToastId) {
            toast.dismiss(loadingToastId)
        }

        if (response.success && response.data) {
            if (success) toast.success(success)
            return response.data
        } else {
            throw new Error(response.error || "Request failed")
        }
    } catch (err) {
        if (loadingToastId) {
            toast.dismiss(loadingToastId)
        }
        const message = err instanceof Error ? err.message : "An error occurred"
        toast.error(error || message)
        return null
    }
}
