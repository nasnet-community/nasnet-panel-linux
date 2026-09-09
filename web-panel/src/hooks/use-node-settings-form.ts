import { useState, useEffect, useLayoutEffect, useCallback, useRef } from "react"
import { useForm, type UseFormReturn } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { toast } from "sonner"
import {
    nodeSettingsSchema,
    type NodeSettingsFormData,
} from "@/lib/validations/node-settings-schema"
import {
    updateNode,
    getXrayConfig,
    updateXrayConfig,
    getNodeSSHStatus,
    updateNodeSSHConfig,
} from "@/lib/admin-api"
import { setNodeMaintenance } from "@/lib/api/maintenance"
import type { Node, SSHStatus } from "@/lib/types"

type Field = keyof NodeSettingsFormData
type XrayConfig = Record<string, unknown> & { log?: Record<string, unknown> }
const xrayFields: Field[] = ["loglevel", "log_access", "log_error", "dnsLog"]
const sshFields: Field[] = ["ssh_enabled", "ssh_port"]
const maintenanceFields: Field[] = ["maintenance_mode", "maintenance_message"]
const remoteFields = [...xrayFields, ...sshFields]

export interface NodeSettingsForm {
    form: UseFormReturn<NodeSettingsFormData>
    isDirty: boolean
    isSaving: boolean
    dirtyFields: Field[]
    savedValues: NodeSettingsFormData
    save: () => Promise<Field | undefined>
    reset: () => void
    xrayLoading: boolean
    xrayError: string | null
    sshLoading: boolean
    sshError: string | null
    sshStatus: SSHStatus | null
    fullXrayConfig: XrayConfig | null
    fetchXrayConfig: () => Promise<void>
    fetchSSHStatus: () => Promise<void>
}

export function getNodeSettingsDefaults(node: Node): NodeSettingsFormData {
    return {
        name: node.name, ip: node.ip, country_code: node.country_code || "", datacenter: node.datacenter || "",
        is_active: node.is_active, agent_port: node.agent_port || 8080, api_port: node.api_port || 10085,
        is_stealth: node.is_stealth || false, is_persistent_stealth: node.is_persistent_stealth || false,
        loglevel: (node.log_level as NodeSettingsFormData["loglevel"]) || "warning",
        log_access: node.log_access || "", log_error: node.log_error || "", dnsLog: node.log_dns || false,
        enable_access_log: node.enable_access_log || false,
        bandwidth_enabled: node.bandwidth_settings?.enabled || false,
        bandwidth_interface: node.bandwidth_settings?.interface || "eth0",
        bandwidth_total_bw: node.bandwidth_settings?.total_bw ?? 1000,
        starlink_enabled: node.starlink_settings?.enabled || false,
        starlink_dish_address: node.starlink_settings?.dish_address || "192.168.100.1:9200",
        crash_recovery_enabled: node.crash_recovery_settings?.enabled || false,
        crash_recovery_command: node.crash_recovery_settings?.command || "",
        crash_recovery_command_timeout: node.crash_recovery_settings?.command_timeout || 60,
        crash_recovery_cooldown: node.crash_recovery_settings?.cooldown || 30,
        crash_recovery_max_attempts: node.crash_recovery_settings?.max_attempts ?? 3,
        ssh_enabled: false, ssh_port: 22,
        maintenance_mode: node.maintenance_mode || false, maintenance_message: node.maintenance_message || "",
    }
}

// Only send edited fields. Unrelated server settings may have changed since this form loaded.
export function nodeSettingsPatch(values: NodeSettingsFormData, fields: Field[]): Partial<Node> {
    const patch: Partial<Node> = {}
    const direct = ["name", "ip", "country_code", "datacenter", "is_active", "enable_access_log"] as const
    for (const field of direct) {
        if (fields.includes(field)) Object.assign(patch, { [field]: field === "country_code" ? values[field]?.toUpperCase() : values[field] })
    }
    if (fields.some(f => f.startsWith("starlink_"))) patch.starlink_settings = {
        enabled: values.starlink_enabled, dish_address: values.starlink_dish_address || "192.168.100.1:9200",
    }
    if (fields.some(f => f.startsWith("crash_recovery_"))) patch.crash_recovery_settings = {
        enabled: values.crash_recovery_enabled, command: values.crash_recovery_command || "",
        command_timeout: values.crash_recovery_command_timeout, cooldown: values.crash_recovery_cooldown,
        max_attempts: values.crash_recovery_max_attempts,
    }
    return patch
}

function parseConfig(content: string): XrayConfig {
    const parsed = JSON.parse(content)
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("The server returned an invalid Xray configuration")
    return parsed
}

export function useNodeSettingsForm(node: Node, options?: { onRefresh?: () => void }): NodeSettingsForm {
    const [isSaving, setIsSaving] = useState(false)
    const savingRef = useRef(false)
    const [xrayLoading, setXrayLoading] = useState(false)
    const [sshLoading, setSshLoading] = useState(false)
    const [xrayError, setXrayError] = useState<string | null>(null)
    const [sshError, setSshError] = useState<string | null>(null)
    const [sshStatus, setSshStatus] = useState<SSHStatus | null>(null)
    const [fullXrayConfig, setFullXrayConfig] = useState<XrayConfig | null>(null)
    const [savedValues, setSavedValues] = useState(() => getNodeSettingsDefaults(node))
    const baseline = useRef(savedValues)
    const nodeRef = useRef(node)
    const onRefreshRef = useRef(options?.onRefresh)
    useLayoutEffect(() => {
        nodeRef.current = node
        onRefreshRef.current = options?.onRefresh
    }, [node, options?.onRefresh])
    const xrayRequest = useRef(0)
    const sshRequest = useRef(0)
    const form = useForm<NodeSettingsFormData>({ resolver: zodResolver(nodeSettingsSchema), defaultValues: savedValues })
    const { dirtyFields: formDirtyFields, isDirty } = form.formState
    const dirtyFields = Object.keys(formDirtyFields).filter(k => formDirtyFields[k as Field]) as Field[]

    // Rebase server defaults, preserving edits even when the corresponding control is unmounted.
    const rebase = useCallback((next: NodeSettingsFormData, acknowledged?: Partial<NodeSettingsFormData>) => {
        const current = form.getValues()
        const previous = baseline.current
        baseline.current = next
        setSavedValues(next)
        form.reset(next)
        for (const field of Object.keys(current) as Field[]) {
            if (current[field] !== previous[field] && (!acknowledged || current[field] !== acknowledged[field])) {
                form.setValue(field, current[field], { shouldDirty: true })
            }
        }
    }, [form])

    const previousNodeID = useRef(node.id)
    useEffect(() => {
        const next = getNodeSettingsDefaults(node)
        if (previousNodeID.current !== node.id) {
            previousNodeID.current = node.id
            baseline.current = next
            setSavedValues(next)
            form.reset(next)
            setFullXrayConfig(null)
            setSshStatus(null)
        } else {
            // Remote defaults are owned by their respective fetches, not node polling.
            for (const field of remoteFields) Object.assign(next, { [field]: baseline.current[field] })
            rebase(next)
        }
    }, [node, form, rebase])

    const fetchXrayConfig = useCallback(async () => {
        const currentNode = nodeRef.current
        if (!currentNode.is_online) return
        const request = ++xrayRequest.current
        setXrayLoading(true)
        setXrayError(null)
        try {
            const res = await getXrayConfig(currentNode.id)
            if (request !== xrayRequest.current || currentNode.id !== nodeRef.current.id) return
            if (!res.success || !res.data) throw new Error(res.error || "Unable to load Xray settings")
            const parsed = parseConfig(res.data)
            setFullXrayConfig(parsed)
            const log = parsed.log || {}
            rebase({ ...baseline.current,
                loglevel: (log.loglevel || "warning") as NodeSettingsFormData["loglevel"],
                log_access: typeof log.access === "string" ? log.access : "",
                log_error: typeof log.error === "string" ? log.error : "", dnsLog: log.dnsLog === true,
            })
        } catch (err) {
            if (request === xrayRequest.current && currentNode.id === nodeRef.current.id) setXrayError(err instanceof Error ? err.message : "Unable to load Xray settings")
        } finally {
            if (request === xrayRequest.current) setXrayLoading(false)
        }
    }, [rebase])

    const fetchSSHStatus = useCallback(async () => {
        const currentNode = nodeRef.current
        if (!currentNode.is_online || currentNode.is_stealth) return
        const request = ++sshRequest.current
        setSshLoading(true)
        setSshError(null)
        try {
            const res = await getNodeSSHStatus(currentNode.id)
            if (request !== sshRequest.current || currentNode.id !== nodeRef.current.id) return
            if (!res.success || !res.data) throw new Error(res.error || "Unable to load SSH status")
            setSshStatus(res.data)
            rebase({ ...baseline.current, ssh_enabled: res.data.enabled, ssh_port: res.data.port })
        } catch (err) {
            if (request === sshRequest.current && currentNode.id === nodeRef.current.id) setSshError(err instanceof Error ? err.message : "Unable to load SSH status")
        } finally {
            if (request === sshRequest.current) setSshLoading(false)
        }
    }, [rebase])

    useEffect(() => {
        void fetchXrayConfig()
        void fetchSSHStatus()
        return () => { xrayRequest.current++; sshRequest.current++ }
    }, [node.id, node.is_online, node.is_stealth, fetchXrayConfig, fetchSSHStatus])

    const save = useCallback(async (): Promise<Field | undefined> => {
        if (savingRef.current) return
        savingRef.current = true
        if (!await form.trigger()) {
            savingRef.current = false
            toast.error("Check the highlighted settings before saving")
            return (Object.keys(form.getValues()) as Field[]).find(f => form.getFieldState(f).invalid)
        }
        const submitted = form.getValues()
        const values = nodeSettingsSchema.parse(submitted)
        const fields = (Object.keys(values) as Field[]).filter(f => values[f] !== baseline.current[f])
        if (!fields.length) { savingRef.current = false; form.reset(baseline.current); return }
        const nodeId = nodeRef.current.id
        setIsSaving(true)
        const failures: string[] = []
        let completed = 0
        const run = async (label: string, group: Field[], action: () => Promise<{ success: boolean; error?: string }>) => {
            const changed = fields.filter(f => group.includes(f))
            if (!changed.length) return
            try {
                const res = await action()
                if (!res.success) throw new Error(res.error || `Unable to save ${label.toLowerCase()}`)
                if (nodeId !== nodeRef.current.id) return
                const persisted = Object.fromEntries(changed.map(f => [f, values[f]]))
                const acknowledged = Object.fromEntries(changed.map(f => [f, submitted[f]]))
                rebase({ ...baseline.current, ...persisted }, acknowledged)
                completed++
            } catch (err) {
                failures.push(`${label}: ${err instanceof Error ? err.message : "save failed"}`)
            }
        }
        try {
            const nodeFields = fields.filter(f => ![...remoteFields, ...maintenanceFields].includes(f))
            await run("Node configuration", nodeFields, () => updateNode(nodeId, nodeSettingsPatch(values, nodeFields)))
            await run("Xray logging", xrayFields, async () => {
                if (!nodeRef.current.is_online) throw new Error("Reconnect the agent before saving")
                // Fetch at save time and modify only dirty log properties, preserving current routing and other edits.
                const latest = await getXrayConfig(nodeId)
                if (!latest.success || !latest.data) throw new Error(latest.error || "Unable to read the current configuration")
                const config = parseConfig(latest.data)
                const log = { ...config.log }
                const mapping = { loglevel: "loglevel", log_access: "access", log_error: "error", dnsLog: "dnsLog" } as const
                for (const field of xrayFields as (keyof typeof mapping)[]) if (fields.includes(field)) log[mapping[field]] = values[field]
                const updated = { ...config, log }
                const response = await updateXrayConfig(nodeId, JSON.stringify(updated, null, 2))
                if (response.success && nodeId === nodeRef.current.id) setFullXrayConfig(updated)
                return response
            })
            await run("Maintenance", maintenanceFields, () => setNodeMaintenance(nodeId, {
                enabled: values.maintenance_mode, message: values.maintenance_message,
            }))
            await run("SSH", sshFields, async () => {
                if (!nodeRef.current.is_online || nodeRef.current.is_stealth) throw new Error("SSH requires an online server")
                if (!sshStatus || sshError) throw new Error("Refresh SSH status before saving")
                return updateNodeSSHConfig(nodeId, values.ssh_enabled, values.ssh_port)
            })
            if (failures.length) {
                toast.error(completed ? "Some settings could not be saved" : "Settings could not be saved", {
                    description: `${failures.join(". ")}. Unsaved changes are kept for retry.`, duration: 10000,
                })
            } else {
                toast.success("Settings saved")
            }
            if (completed) onRefreshRef.current?.()
        } finally {
            savingRef.current = false
            setIsSaving(false)
        }
    }, [form, rebase, sshStatus, sshError])

    return { form, isDirty, isSaving, dirtyFields, savedValues, save, reset: () => form.reset(baseline.current),
        xrayLoading, xrayError, sshLoading, sshError, sshStatus, fullXrayConfig, fetchXrayConfig, fetchSSHStatus }
}
