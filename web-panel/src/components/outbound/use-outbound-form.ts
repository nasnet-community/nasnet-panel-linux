import * as React from "react"
import { useEffect, useMemo, useCallback, useState } from "react"
import { useForm, type UseFormReturn } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { outboundSchema, type OutboundFormData } from "@/lib/validations/outbound-schema"
import { OUTBOUND_PRESETS } from "@/lib/presets/outbound-presets"
import { NETWORK_TYPES, SECURITY_TYPES } from "@/lib/types"
import { parseConfigLink } from "@/lib/link-parser"
import { toast } from "sonner"
import type { Outbound } from "@/lib/types"

export type TabId = "general" | "network" | "transport" | "security" | "protocol" | "advanced"

export interface TabDefinition {
    id: TabId
    label: string
    visible: boolean
    hasErrors: boolean
    badges: string[]
}

const TRANSPORT_PROTOCOLS = ["vless", "vmess", "trojan", "shadowsocks", "socks", "http"]

const defaultValues: OutboundFormData = {
    tag: "",
    remark: "",
    protocol: "freedom",
    address: "",
    port: 0,
    network: "tcp",
    security: "none",
    freedom_settings: { domainStrategy: "AsIs" },
}

// Map error paths to their respective tab
function getTabForErrorPath(path: string): TabId {
    if (path.startsWith("tls_settings") || path.startsWith("reality_settings")) return "security"
    if (path.startsWith("transport_settings")) return "transport"
    if (path.startsWith("sockopt_settings")) return "advanced"
    if (path === "network" || path === "security") return "network"
    if (
        path.startsWith("freedom_settings") ||
        path.startsWith("blackhole_settings") ||
        path.startsWith("vmess_settings") ||
        path.startsWith("vless_settings") ||
        path.startsWith("trojan_settings") ||
        path.startsWith("shadowsocks_settings") ||
        path.startsWith("wireguard_settings") ||
        path.startsWith("http_settings") ||
        path.startsWith("socks_settings") ||
        path.startsWith("dns_settings") ||
        path.startsWith("loopback_settings")
    ) return "protocol"
    return "general"
}

export function useOutboundForm(
    mode: "create" | "edit",
    outbound: Outbound | null | undefined,
    open: boolean,
) {
    const [activeTab, setActiveTab] = useState<TabId>("general")

    const form = useForm<OutboundFormData, unknown, OutboundFormData>({
        resolver: zodResolver(outboundSchema),
        mode: "onTouched",
        defaultValues,
    })

    const { watch, reset, setValue, formState: { errors } } = form

    // Reset form when dialog opens
    useEffect(() => {
        if (!open) return

        if (mode === "edit" && outbound) {
            reset({
                tag: outbound.tag || "",
                remark: outbound.remark || "",
                protocol: outbound.protocol || "freedom",
                address: outbound.address || "",
                port: outbound.port || 0,
                network: outbound.network || "tcp",
                security: outbound.security || "none",
                tls_settings: outbound.tls_settings,
                reality_settings: outbound.reality_settings,
                transport_settings: outbound.transport_settings,
                sockopt_settings: outbound.sockopt_settings,
                freedom_settings: outbound.freedom_settings,
                blackhole_settings: outbound.blackhole_settings,
                vmess_settings: outbound.vmess_settings,
                vless_settings: outbound.vless_settings,
                trojan_settings: outbound.trojan_settings,
                shadowsocks_settings: outbound.shadowsocks_settings,
                wireguard_settings: outbound.wireguard_settings,
                http_settings: outbound.http_settings,
                socks_settings: outbound.socks_settings,
                dns_settings: outbound.dns_settings as OutboundFormData["dns_settings"],
                loopback_settings: outbound.loopback_settings,
                hysteria_settings: outbound.hysteria_settings,
                mux_settings: outbound.mux_settings,
                proxy_settings: outbound.proxy_settings,
                send_through: outbound.send_through,
                finalmask: outbound.finalmask,
            })
        } else {
            reset(defaultValues)
        }
        setActiveTab("general")
    }, [open, mode, outbound, reset])

    // Watch key values for tab visibility & badges
    const protocol = watch("protocol")

    // Clear stale protocol-specific settings when the user switches
    // protocol so the save payload reflects the active protocol only.
    const lastProtocolRef = React.useRef<string | undefined>(undefined)
    useEffect(() => {
        if (!open) {
            lastProtocolRef.current = undefined
            return
        }
        if (lastProtocolRef.current === undefined) {
            lastProtocolRef.current = protocol
            return
        }
        if (lastProtocolRef.current === protocol) return
        lastProtocolRef.current = protocol
        const allKeys = [
            "freedom_settings", "blackhole_settings", "vless_settings",
            "vmess_settings", "trojan_settings", "shadowsocks_settings",
            "wireguard_settings", "http_settings", "socks_settings",
            "dns_settings", "loopback_settings", "hysteria_settings",
        ] as const
        const keep: Record<string, string | undefined> = {
            freedom: "freedom_settings",
            blackhole: "blackhole_settings",
            vless: "vless_settings",
            vmess: "vmess_settings",
            trojan: "trojan_settings",
            shadowsocks: "shadowsocks_settings",
            wireguard: "wireguard_settings",
            http: "http_settings",
            socks: "socks_settings",
            dns: "dns_settings",
            loopback: "loopback_settings",
            hysteria2: "hysteria_settings",
        }
        const keepKey = keep[protocol]
        for (const key of allKeys) {
            if (key !== keepKey) {
                setValue(key as typeof allKeys[number], undefined, { shouldDirty: true })
            }
        }
    }, [open, protocol, setValue])
    const network = watch("network")
    const security = watch("security")
    const transportSettings = watch("transport_settings")
    const tlsSettings = watch("tls_settings")
    const realitySettings = watch("reality_settings")
    const freedomSettings = watch("freedom_settings")
    const blackholeSettings = watch("blackhole_settings")
    const vlessSettings = watch("vless_settings")
    const vmessSettings = watch("vmess_settings")
    const shadowsocksSettings = watch("shadowsocks_settings")
    const loopbackSettings = watch("loopback_settings")
    const sockoptSettings = watch("sockopt_settings")

    // Derived visibility
    const needsTransport = TRANSPORT_PROTOCOLS.includes(protocol)

    // Tab visibility
    const tabVisibility = useMemo(() => ({
        general: true,
        network: needsTransport,
        transport: needsTransport && network !== "tcp",
        security: (needsTransport && security !== "none") || protocol === "hysteria2",
        protocol: true,
        advanced: true,
    }), [needsTransport, network, security, protocol])

    // Auto-reset to general when active tab becomes hidden
    useEffect(() => {
        if (!tabVisibility[activeTab]) {
            setActiveTab("general")
        }
    }, [tabVisibility, activeTab])

    // Collect error paths and map to tabs
    const tabErrors = useMemo(() => {
        const result: Record<TabId, boolean> = {
            general: false,
            network: false,
            transport: false,
            security: false,
            protocol: false,
            advanced: false,
        }
        const flatErrors = Object.keys(errors)
        for (const path of flatErrors) {
            const tab = getTabForErrorPath(path)
            result[tab] = true
        }
        return result
    }, [errors])

    // Tab badge summaries
    const tabBadges = useMemo(() => {
        const badges: Record<TabId, string[]> = {
            general: [],
            network: [],
            transport: [],
            security: [],
            protocol: [],
            advanced: [],
        }

        // Network tab badges
        if (needsTransport && network) {
            const networkLabel = NETWORK_TYPES.find(n => n.value === network)?.label || network
            badges.network.push(networkLabel)
            if (security && security !== "none") {
                const securityLabel = SECURITY_TYPES.find(s => s.value === security)?.label || security
                badges.network.push(securityLabel)
            }
        }

        // Transport tab badges
        if (transportSettings?.path) {
            badges.transport.push(transportSettings.path)
        }

        // Security tab badges
        if (security === "tls" && tlsSettings?.serverName) {
            badges.security.push(tlsSettings.serverName)
        }
        if (security === "reality" && realitySettings?.serverNames?.[0]) {
            badges.security.push(realitySettings.serverNames[0])
        }

        // Protocol tab badges
        if (protocol === "freedom" && freedomSettings?.domainStrategy) {
            badges.protocol.push(freedomSettings.domainStrategy)
        }
        if (protocol === "blackhole" && blackholeSettings?.responseType) {
            badges.protocol.push(blackholeSettings.responseType)
        }
        if (protocol === "vless" && vlessSettings?.flow) {
            badges.protocol.push(vlessSettings.flow)
        }
        if (protocol === "vmess" && vmessSettings?.security) {
            badges.protocol.push(vmessSettings.security)
        }
        if (protocol === "shadowsocks" && shadowsocksSettings?.method) {
            const shortMethod = shadowsocksSettings.method.replace("2022-blake3-", "")
            badges.protocol.push(shortMethod)
        }
        if (protocol === "loopback" && loopbackSettings?.inboundTag) {
            badges.protocol.push(loopbackSettings.inboundTag)
        }
        if (protocol === "dns") {
            badges.protocol.push("DNS")
        }
        if (protocol === "wireguard") {
            const peerCount = form.getValues("wireguard_settings")?.peers?.length || 0
            if (peerCount > 0) badges.protocol.push(`${peerCount} peer${peerCount > 1 ? "s" : ""}`)
        }

        // Advanced tab badges
        if (sockoptSettings) {
            badges.advanced.push("Sockopt")
        }

        return badges
    }, [needsTransport, network, security, transportSettings, tlsSettings, realitySettings,
        protocol, freedomSettings, blackholeSettings, vlessSettings, vmessSettings,
        shadowsocksSettings, loopbackSettings, sockoptSettings])

    // Build tab definitions array
    const tabs = useMemo((): TabDefinition[] => {
        const allTabs: { id: TabId; label: string }[] = [
            { id: "general", label: "General" },
            { id: "network", label: "Network" },
            { id: "transport", label: "Transport" },
            { id: "security", label: "Security" },
            { id: "protocol", label: "Protocol" },
            { id: "advanced", label: "Advanced" },
        ]
        return allTabs
            .filter(t => tabVisibility[t.id])
            .map(t => ({
                ...t,
                visible: true,
                hasErrors: tabErrors[t.id],
                badges: tabBadges[t.id],
            }))
    }, [tabVisibility, tabErrors, tabBadges])

    // Apply preset
    const applyPreset = useCallback((presetId: string) => {
        const preset = OUTBOUND_PRESETS.find(p => p.id === presetId)
        if (!preset) return

        const currentTag = form.getValues("tag")
        const currentRemark = form.getValues("remark")

        reset({
            ...defaultValues,
            ...preset.defaults,
            tag: currentTag,
            remark: currentRemark,
        } as OutboundFormData)
    }, [form, reset])

    // Import config link
    const importConfigLink = useCallback((link: string): { success: boolean; error?: string } => {
        const result = parseConfigLink(link)
        if (!result.success || !result.outbound) {
            return { success: false, error: result.error || "Failed to parse link" }
        }

        const currentTag = form.getValues("tag")
        const currentRemark = form.getValues("remark")

        reset({
            ...defaultValues,
            ...result.outbound,
            tag: currentTag || result.outbound.tag || "",
            remark: currentRemark || result.outbound.remark || "",
        } as OutboundFormData)

        toast.success(`Imported ${result.outbound.protocol?.toUpperCase()} config successfully`)
        return { success: true }
    }, [form, reset])

    // Navigate to first error tab on validation failure
    const navigateToFirstError = useCallback(() => {
        const errorPaths = Object.keys(errors)
        if (errorPaths.length === 0) return

        const tabOrder: TabId[] = ["general", "network", "transport", "security", "protocol", "advanced"]
        for (const tab of tabOrder) {
            if (tabErrors[tab] && tabVisibility[tab]) {
                setActiveTab(tab)
                return
            }
        }
    }, [errors, tabErrors, tabVisibility])

    return {
        form,
        activeTab,
        setActiveTab,
        tabs,
        tabVisibility,
        applyPreset,
        importConfigLink,
        navigateToFirstError,
        // Convenience: watched values for child components
        protocol,
        network,
        security,
    }
}

export type OutboundFormReturn = ReturnType<typeof useOutboundForm>
