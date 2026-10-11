import { useRef, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { setNodeMaintenance } from "@/lib/api/maintenance"
import { queryKeys } from "@/lib/queries/keys"
import { toast } from "@/lib/toast"
import { useUpdateMtprotoNodeSettings, useUpdateMtprotoSettings } from "@/lib/queries/use-mtproto"
import { MAX_STREAM_LIMIT, MIN_STREAM_LIMIT, type MtprotoNode, type MtprotoSettingsBody } from "@/lib/api/mtproto"
import type { Node } from "@/lib/types"
/** The fields the settings page edits. */
export type MtprotoField = "name" | "is_active" | "maintenance_mode" | "maintenance_message" | "max_streams" | "ssh_port" | "ssh_user"

/** invalid names the first field that failed validation; error is a failed save */
export interface MtprotoSaveResult { ok: boolean; invalid?: MtprotoField; error?: string }

/** What the inputs hold: numbers stay text so a half-typed value isn't lost. */
export interface MtprotoValues {
    name: string
    is_active: boolean
    maintenance_mode: boolean
    maintenance_message: string
    max_streams: string
    ssh_port: string
    ssh_user: string
}

const FIELDS: MtprotoField[] = ["name", "is_active", "maintenance_mode", "maintenance_message", "ssh_port", "ssh_user", "max_streams"]
const NOTICE: MtprotoField[] = ["maintenance_mode", "maintenance_message"]
// these go through PATCH /mtproto/nodes/:id, the rest through the node update
const REMOTE: MtprotoField[] = ["max_streams", "ssh_port", "ssh_user"]
const SSH_USER = /^[A-Za-z_][A-Za-z0-9_.-]{0,31}$/

const whole = (v: string) => (/^\d+$/.test(v.trim()) ? Number(v.trim()) : NaN)

export function validate(field: MtprotoField, v: MtprotoValues[MtprotoField]): string | undefined {
    const s = String(v).trim()
    switch (field) {
        case "name":
            return s ? undefined : "Enter a name."
        case "max_streams": {
            const n = whole(s)
            return n >= MIN_STREAM_LIMIT && n <= MAX_STREAM_LIMIT ? undefined : `Pick a limit from ${MIN_STREAM_LIMIT} to ${MAX_STREAM_LIMIT.toLocaleString("en-US")}.`
        }
        case "ssh_port": {
            const n = whole(s)
            return n >= 1 && n <= 65535 ? undefined : "Pick a port from 1 to 65535."
        }
        case "ssh_user":
            return SSH_USER.test(s) ? undefined : "Up to 32 letters, digits, _ . or -, starting with a letter or _."
        case "maintenance_message":
            return s.length <= 2000 ? undefined : "Use at most 2000 characters."
        default:
            return undefined
    }
}

function same(field: MtprotoField, a: MtprotoValues[MtprotoField], b: MtprotoValues[MtprotoField]): boolean {
    if (field === "is_active" || field === "maintenance_mode") return a === b
    if (field === "max_streams" || field === "ssh_port") return whole(String(a)) === whole(String(b))
    return String(a).trim() === String(b).trim()
}

// what the panel last accepted for a node field, until the page reloads the node
type Ack<V> = { value: V; over: V } | undefined

export function useMtprotoSettingsForm(node: Node, r: MtprotoNode | undefined, onSaved?: () => void) {
    const confirm = useConfirm()
    const qc = useQueryClient()
    const nodeSave = useUpdateMtprotoNodeSettings(node.id)
    const mtprotoSave = useUpdateMtprotoSettings(node.id)
    const [draft, setDraft] = useState<Partial<MtprotoValues>>({})
    const [touched, setTouched] = useState<Partial<Record<MtprotoField, boolean>>>({})
    const [attempted, setAttempted] = useState(false)
    const [isSaving, setIsSaving] = useState(false)
    const [nameAck, setNameAck] = useState<Ack<string>>()
    const [activeAck, setActiveAck] = useState<Ack<boolean>>()
    const [noticeAck, setNoticeAck] = useState<Ack<{ mode: boolean; message: string }>>()
    const saving = useRef(false)

    // an ack only stands while the node prop still shows the old value
    const saved: { [K in MtprotoField]: MtprotoValues[K] | undefined } = {
        name: nameAck && nameAck.over === node.name ? nameAck.value : node.name,
        is_active: activeAck && activeAck.over === node.is_active ? activeAck.value : node.is_active,
        ...(() => {
            const now = { mode: !!node.maintenance_mode, message: node.maintenance_message ?? "" }
            const held = noticeAck && noticeAck.over.mode === now.mode && noticeAck.over.message === now.message ? noticeAck.value : now
            return { maintenance_mode: held.mode, maintenance_message: held.message }
        })(),
        max_streams: r ? String(r.max_streams) : undefined,
        ssh_port: r ? String(r.ssh_port) : undefined,
        ssh_user: r?.ssh_user,
    }
    const values: MtprotoValues = {
        name: draft.name ?? saved.name ?? "",
        is_active: draft.is_active ?? saved.is_active ?? false,
        maintenance_mode: draft.maintenance_mode ?? saved.maintenance_mode ?? false,
        maintenance_message: draft.maintenance_message ?? saved.maintenance_message ?? "",
        max_streams: draft.max_streams ?? saved.max_streams ?? "",
        ssh_port: draft.ssh_port ?? saved.ssh_port ?? "",
        ssh_user: draft.ssh_user ?? saved.ssh_user ?? "",
    }
    const dirtyFields = FIELDS.filter((f) => draft[f] !== undefined && saved[f] !== undefined && !same(f, values[f], saved[f]!))
    const errors: Partial<Record<MtprotoField, string>> = {}
    for (const f of dirtyFields) {
        const e = validate(f, values[f])
        if (e) errors[f] = e
    }
    const shownErrors: Partial<Record<MtprotoField, string>> = {}
    for (const f of dirtyFields) if (errors[f] && (attempted || touched[f])) shownErrors[f] = errors[f]

    const set = <K extends MtprotoField>(field: K, value: MtprotoValues[K]) => setDraft((d) => ({ ...d, [field]: value }))
    const touch = (field: MtprotoField) => setTouched((t) => (t[field] ? t : { ...t, [field]: true }))
    const forget = (fields: MtprotoField[]) => setDraft((d) => {
        const next = { ...d }
        for (const f of fields) delete next[f]
        return next
    })
    const reset = () => {
        setDraft({})
        setTouched({})
        setAttempted(false)
    }
    /** puts these fields back to what the node has */
    const discard = (fields: MtprotoField[]) => {
        forget(fields)
        setTouched((t) => {
            const next = { ...t }
            for (const f of fields) delete next[f]
            return next
        })
        setAttempted(false)
    }

    /** Saves what changed among these fields (all when omitted). */
    const save = async (only?: MtprotoField[]): Promise<MtprotoSaveResult> => {
        if (saving.current) return { ok: false }
        setAttempted(true)
        const scope = only ? dirtyFields.filter((f) => only.includes(f)) : dirtyFields
        const invalid = scope.find((f) => errors[f])
        if (invalid) return { ok: false, invalid }
        if (!scope.length) return { ok: true }
        const disabling = scope.includes("is_active") && !values.is_active
        if (disabling && !(await confirm({
            title: `Take ${node.name} out of service?`,
            description: "The link closes, so every Telegram connection through this node drops, and its proxy links leave users' subscriptions until you turn it back on.",
            confirmLabel: "Take out of service",
            variant: "destructive",
        }))) return { ok: false }

        saving.current = true
        setIsSaving(true)
        let done = 0
        const failed: string[] = []
        const why = (e: unknown) => failed.push(e instanceof Error ? e.message : "The save failed.")
        // each part toasts its own result; a failed one stays edited for a retry
        try {
            if (scope.includes("name")) {
                const name = values.name.trim()
                try {
                    await nodeSave.mutateAsync({ name })
                    setNameAck({ value: name, over: node.name })
                    forget(["name"])
                    done++
                } catch (e) { why(e) }
            }
            if (scope.includes("is_active")) {
                try {
                    await nodeSave.mutateAsync({ is_active: values.is_active })
                    setActiveAck({ value: values.is_active, over: node.is_active })
                    forget(["is_active"])
                    done++
                } catch (e) { why(e) }
            }
            if (scope.some((f) => NOTICE.includes(f))) {
                const notice = { mode: values.maintenance_mode, message: values.maintenance_message.trim() }
                try {
                    const res = await setNodeMaintenance(node.id, { enabled: notice.mode, message: notice.message })
                    if (!res.success) throw new Error(res.error || "The notice wasn't saved.")
                    toast.success(notice.mode === !!node.maintenance_mode ? "Notice saved" : notice.mode ? "Maintenance notice is on" : "Maintenance notice is off")
                    setNoticeAck({ value: notice, over: { mode: !!node.maintenance_mode, message: node.maintenance_message ?? "" } })
                    forget(NOTICE)
                    qc.invalidateQueries({ queryKey: queryKeys.nodeDetails(node.id) })
                    done++
                } catch (e) {
                    toast.error("The notice wasn't saved", { description: e instanceof Error ? e.message : undefined })
                    why(e)
                }
            }
            const remote = scope.filter((f) => REMOTE.includes(f))
            if (remote.length) {
                const body: MtprotoSettingsBody = {}
                if (remote.includes("max_streams")) body.max_streams = whole(values.max_streams)
                if (remote.includes("ssh_port")) body.ssh_port = whole(values.ssh_port)
                if (remote.includes("ssh_user")) body.ssh_user = values.ssh_user.trim()
                try {
                    await mtprotoSave.mutateAsync(body)
                    forget(remote)
                    done++
                } catch (e) { why(e) }
            }
            if (done) {
                setAttempted(false)
                onSaved?.()
            }
            return failed.length ? { ok: false, error: failed.join(" ") } : { ok: true }
        } finally {
            saving.current = false
            setIsSaving(false)
        }
    }

    return { values, saved, dirtyFields, errors, shownErrors, isDirty: dirtyFields.length > 0, isSaving, set, touch, reset, discard, save }
}

export type MtprotoSettingsForm = ReturnType<typeof useMtprotoSettingsForm>
