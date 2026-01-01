import { useState, useEffect, useCallback, useRef } from "react"
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
import type { Node, SSHStatus } from "@/lib/types"

export interface NodeSettingsForm {
    form: UseFormReturn<NodeSettingsFormData>
    isDirty: boolean
    isSaving: boolean
    save: () => Promise<void>
    reset: () => void
    xrayLoading: boolean
    sshLoading: boolean
    sshStatus: SSHStatus | null
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    fullXrayConfig: any
    fetchXrayConfig: () => Promise<void>
    fetchSSHStatus: () => Promise<void>
}

interface UseNodeSettingsFormOptions {
    onRefresh?: () => void
}

function getDefaultValues(node: Node): NodeSettingsFormData {
    return {
        // General
        name: node.name,
        ip: node.ip,
        country_code: node.country_code || "",
        datacenter: node.datacenter || "",
        is_active: node.is_active,
        // Agent
        agent_port: node.agent_port || 8080,
        api_port: node.api_port || 10085,
        is_stealth: node.is_stealth || false,
        is_persistent_stealth: node.is_persistent_stealth || false,
        // Xray Logging (use DB value as default, updated async from agent)
        loglevel: (node.log_level as NodeSettingsFormData["loglevel"]) || "warning",
        log_access: "",
        log_error: "",
        dnsLog: false,
        enable_access_log: node.enable_access_log || false,
        // Bandwidth shaping
        bandwidth_enabled: node.bandwidth_settings?.enabled || false,
        bandwidth_interface: node.bandwidth_settings?.interface || "eth0",
        bandwidth_total_bw: node.bandwidth_settings?.total_bw || 1000,
        // Starlink monitoring
        starlink_enabled: node.starlink_settings?.enabled || false,
        starlink_dish_address: node.starlink_settings?.dish_address || "192.168.100.1:9200",
        // Crash recovery command
        crash_recovery_enabled: node.crash_recovery_settings?.enabled || false,
        crash_recovery_command: node.crash_recovery_settings?.command || "",
        crash_recovery_command_timeout: node.crash_recovery_settings?.command_timeout || 60,
        crash_recovery_cooldown: node.crash_recovery_settings?.cooldown || 30,
        crash_recovery_max_attempts: node.crash_recovery_settings?.max_attempts ?? 3,
        // SSH (defaults, updated async)
        ssh_enabled: false,
        ssh_port: 22,
    }
}

export function useNodeSettingsForm(node: Node, options?: UseNodeSettingsFormOptions): NodeSettingsForm {
    const [isSaving, setIsSaving] = useState(false)
    const [xrayLoading, setXrayLoading] = useState(false)
    const [sshLoading, setSshLoading] = useState(false)
    const [sshStatus, setSshStatus] = useState<SSHStatus | null>(null)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const fullXrayConfigRef = useRef<any>(null)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const [fullXrayConfig, setFullXrayConfig] = useState<any>(null)
    const onRefreshRef = useRef(options?.onRefresh)
    onRefreshRef.current = options?.onRefresh

    const form = useForm<NodeSettingsFormData>({
        resolver: zodResolver(nodeSettingsSchema),
        defaultValues: getDefaultValues(node),
    })

    // Reset form when node prop changes (e.g., after external refresh)
    useEffect(() => {
        const currentValues = form.getValues()
        form.reset({
            ...getDefaultValues(node),
            // Preserve async-loaded fields if they've been set
            loglevel: currentValues.loglevel,
            log_access: currentValues.log_access,
            log_error: currentValues.log_error,
            dnsLog: currentValues.dnsLog,
            ssh_enabled: currentValues.ssh_enabled,
            ssh_port: currentValues.ssh_port,
        })
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [node.id, node.updated_at])

    // Fetch Xray config
    const fetchXrayConfig = useCallback(async () => {
        if (!node.is_online) return
        try {
            setXrayLoading(true)
            const res = await getXrayConfig(node.id)
            if (res.success && res.data) {
                const parsed = JSON.parse(res.data)
                fullXrayConfigRef.current = parsed
                setFullXrayConfig(parsed)
                if (parsed.log) {
                    const xrayValues = {
                        loglevel: (parsed.log.loglevel || "warning") as NodeSettingsFormData["loglevel"],
                        log_access: parsed.log.access || "",
                        log_error: parsed.log.error || "",
                        dnsLog: parsed.log.dnsLog || false,
                    }
                    // Update the form defaults with fetched values
                    form.reset({
                        ...form.getValues(),
                        ...xrayValues,
                    })
                }
            }
        } catch {
            // Silent fail — xray section will show loading/error state
        } finally {
            setXrayLoading(false)
        }
    }, [node.id, node.is_online, form])

    // Fetch SSH status
    const fetchSSHStatus = useCallback(async () => {
        try {
            setSshLoading(true)
            const res = await getNodeSSHStatus(node.id)
            if (res.success && res.data) {
                setSshStatus(res.data)
                const sshValues = {
                    ssh_enabled: res.data.enabled,
                    ssh_port: res.data.port,
                }
                form.reset({
                    ...form.getValues(),
                    ...sshValues,
                })
            }
        } catch {
            // Silent fail
        } finally {
            setSshLoading(false)
        }
    }, [node.id, form])

    // Initial data fetch
    useEffect(() => {
        fetchXrayConfig()
        fetchSSHStatus()
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [node.id])

    const save = useCallback(async () => {
        const isValid = await form.trigger()
        if (!isValid) {
            toast.error("Please fix validation errors before saving")
            return
        }

        const values = form.getValues()
        const dirtyFields = form.formState.dirtyFields
        setIsSaving(true)

        try {
            // Check if general, agent, or bandwidth fields changed
            const generalAgentDirty = dirtyFields.name || dirtyFields.ip ||
                dirtyFields.country_code || dirtyFields.datacenter ||
                dirtyFields.is_active ||
                dirtyFields.agent_port || dirtyFields.api_port ||
                dirtyFields.is_stealth || dirtyFields.is_persistent_stealth ||
                dirtyFields.bandwidth_enabled || dirtyFields.bandwidth_interface ||
                dirtyFields.starlink_enabled || dirtyFields.starlink_dish_address ||
                dirtyFields.bandwidth_total_bw ||
                dirtyFields.crash_recovery_enabled || dirtyFields.crash_recovery_command ||
                dirtyFields.crash_recovery_command_timeout || dirtyFields.crash_recovery_cooldown ||
                dirtyFields.crash_recovery_max_attempts ||
                dirtyFields.enable_access_log

            // Check if xray log fields changed
            const xrayDirty = dirtyFields.loglevel || dirtyFields.log_access ||
                dirtyFields.log_error || dirtyFields.dnsLog

            // Check if SSH fields changed
            const sshDirty = dirtyFields.ssh_enabled || dirtyFields.ssh_port

            // Warn if xray fields changed but we can't save them (agent was offline at load)
            if (xrayDirty && !fullXrayConfigRef.current) {
                toast.error("Cannot save Xray settings: config not loaded. Ensure the agent is online and refresh the page.")
                return
            }

            if (!generalAgentDirty && !xrayDirty && !sshDirty) {
                toast.info("No changes to save")
                return
            }

            // Save general/agent fields FIRST to prevent race condition.
            // updateXrayConfig also does a full node save (to persist log_level),
            // so it must run AFTER updateNode to avoid overwriting each other.
            if (generalAgentDirty) {
                const res = await updateNode(node.id, {
                    name: values.name,
                    ip: values.ip,
                    country_code: values.country_code,
                    datacenter: values.datacenter,
                    is_active: values.is_active,
                    agent_port: values.agent_port,
                    api_port: values.api_port,
                    is_stealth: values.is_stealth,
                    is_persistent_stealth: values.is_persistent_stealth,
                    enable_access_log: values.enable_access_log,
                    bandwidth_settings: {
                        enabled: values.bandwidth_enabled,
                        interface: values.bandwidth_interface || "eth0",
                        total_bw: values.bandwidth_total_bw,
                    },
                    starlink_settings: {
                        enabled: values.starlink_enabled,
                        dish_address: values.starlink_dish_address || "192.168.100.1:9200",
                    },
                    crash_recovery_settings: {
                        enabled: values.crash_recovery_enabled,
                        command: values.crash_recovery_command,
                        command_timeout: values.crash_recovery_command_timeout,
                        cooldown: values.crash_recovery_cooldown,
                        max_attempts: values.crash_recovery_max_attempts,
                    },
                })
                if (!res.success) throw new Error(res.error || "Failed to update node")
            }

            // Now save xray config and SSH in parallel (they don't conflict)
            const promises: Promise<void>[] = []

            if (xrayDirty && fullXrayConfigRef.current) {
                const updatedConfig = {
                    ...fullXrayConfigRef.current,
                    log: {
                        ...fullXrayConfigRef.current.log,
                        loglevel: values.loglevel,
                        access: values.log_access,
                        error: values.log_error,
                        dnsLog: values.dnsLog,
                    },
                }
                promises.push(
                    updateXrayConfig(node.id, JSON.stringify(updatedConfig, null, 2)).then((res) => {
                        if (res.success) {
                            fullXrayConfigRef.current = updatedConfig
                            setFullXrayConfig(updatedConfig)
                        } else {
                            throw new Error(res.error || "Failed to update Xray config")
                        }
                    })
                )
            }

            if (sshDirty) {
                promises.push(
                    updateNodeSSHConfig(node.id, values.ssh_enabled, values.ssh_port).then((res) => {
                        if (!res.success) throw new Error(res.error || "Failed to update SSH config")
                    })
                )
            }

            if (promises.length > 0) {
                await Promise.all(promises)
            }

            // Reset form to current values (clears dirty state)
            form.reset(values)
            toast.success("Settings saved successfully")

            if (xrayDirty) {
                toast.info("Xray changes require restart to take effect")
            }

            // Refresh the node data from server so parent has latest state
            onRefreshRef.current?.()
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Failed to save settings")
        } finally {
            setIsSaving(false)
        }
    }, [form, node.id])

    const reset = useCallback(() => {
        form.reset()
    }, [form])

    return {
        form,
        isDirty: form.formState.isDirty,
        isSaving,
        save,
        reset,
        xrayLoading,
        sshLoading,
        sshStatus,
        fullXrayConfig,
        fetchXrayConfig,
        fetchSSHStatus,
    }
}
