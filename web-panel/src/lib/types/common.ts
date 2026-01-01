// Pagination
export interface PaginationMeta {
    page: number
    per_page: number
    total: number
    total_pages: number
}

export interface PaginatedResponse<T> {
    data: T[]
    meta: PaginationMeta
}

// Log Entry (from SSE structured log stream)
export interface LogEntry {
    timestamp: number  // Unix milliseconds
    level: string      // info, warning, error, debug
    log_type: string   // all, access, error
    message: string
    source: string
}
