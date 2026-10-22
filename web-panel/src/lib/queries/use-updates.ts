import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { updatesApi, type ApplyRequest, type UpdateCheck } from "@/lib/api/updates"
import { toast } from "@/lib/toast"
import { queryKeys } from "./keys"

/** Settings → Updates. Polls faster while a job runs. */
export function useUpdates() {
    return useQuery({
        queryKey: queryKeys.updates(),
        queryFn: () => updatesApi.status(),
        staleTime: 10_000,
        refetchInterval: q => (q.state.data?.job && q.state.data.job.phase !== "done" ? 3_000 : 60_000),
    })
}

function useRefresh() {
    const qc = useQueryClient()
    return () => {
        void qc.invalidateQueries({ queryKey: queryKeys.settingsStatus() })
        return qc.invalidateQueries({ queryKey: queryKeys.updates() })
    }
}

export function checkToast(c: UpdateCheck) {
    if (c.error) return toast.warning("Update check failed", { description: c.error })
    if (c.available && c.latest) return toast.info(`${c.latest.version} is available`, { description: "Update from Settings → Updates." })
    if (c.blocked) return toast.info(`${c.latest?.version} needs a step first`, { description: c.blocked })
    return toast.success("Up to date", { description: `${c.latest?.version ?? "This"} is the newest ${c.channel} release.` })
}

export function useCheckUpdates() {
    const refresh = useRefresh()
    return useMutation({
        mutationFn: () => updatesApi.check(),
        onSuccess: c => { checkToast(c); return refresh() },
        onError: (e: Error) => toast.error("Update check failed", { description: e.message }),
    })
}

/** Starts an update, or schedules it when at is set. */
export function useApplyUpdate() {
    const refresh = useRefresh()
    return useMutation({
        mutationFn: (req: ApplyRequest) => updatesApi.apply(req),
        onSuccess: res => {
            if (res.schedule) {
                const at = new Date(res.schedule.at)
                toast.info(`Update scheduled for ${at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`, {
                    description: `${at.toLocaleDateString([], { weekday: "short", day: "numeric", month: "short" })}. Cancel it in Settings → Updates.`,
                })
            }
            return refresh()
        },
        onError: (e: Error) => toast.error("The update didn't start", { description: e.message }),
    })
}

export function useCancelSchedule() {
    const refresh = useRefresh()
    return useMutation({
        mutationFn: () => updatesApi.cancelSchedule(),
        onSuccess: s => { toast.success("Scheduled update cancelled", { description: `${s.version} won't install then.` }); return refresh() },
        onError: (e: Error) => toast.error("Couldn't cancel the update", { description: e.message }),
    })
}

export function useRollback() {
    const refresh = useRefresh()
    return useMutation({
        mutationFn: () => updatesApi.rollback(),
        onSuccess: () => refresh(),
        onError: (e: Error) => toast.error("The rollback didn't start", { description: e.message }),
    })
}
