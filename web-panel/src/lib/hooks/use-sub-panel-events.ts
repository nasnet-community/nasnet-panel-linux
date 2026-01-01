import { useEffect, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { getApiBaseUrl } from '@/lib/config'
import { getSubAuthToken } from "@/lib/sub-auth"

const API_BASE_URL = getApiBaseUrl()

export type SubEventStatus = "idle" | "connecting" | "open" | "closed"

export function useSubPanelEvents(
    uuid: string,
    opts?: { enabled?: boolean },
): SubEventStatus {
    const enabled = opts?.enabled ?? true
    const queryClient = useQueryClient()
    const [status, setStatus] = useState<SubEventStatus>("idle")

    useEffect(() => {
        if (!enabled) {
            setStatus("idle")
            return
        }

        let url = `${API_BASE_URL}/api/v1/public/sub/${uuid}/events`
        // Append auth token for SSE (EventSource can't set headers). Re-read here
        // so a token saved during the password gate is picked up when `enabled`
        // flips true after authentication.
        const token = getSubAuthToken(uuid)
        if (token) {
            url += `?auth=${encodeURIComponent(token)}`
        }

        setStatus("connecting")
        const es = new EventSource(url, { withCredentials: true })

        es.onopen = () => setStatus("open")

        es.addEventListener("update", (event) => {
            try {
                const data = JSON.parse((event as MessageEvent).data)
                queryClient.setQueryData(["sub-panel", uuid], data)
            } catch {
                // Ignore parse errors
            }
        })

        es.onerror = () => {
            // EventSource auto-reconnects while CONNECTING; CLOSED means it gave up.
            setStatus(es.readyState === EventSource.CLOSED ? "closed" : "connecting")
        }

        return () => es.close()
    }, [uuid, queryClient, enabled])

    return status
}
