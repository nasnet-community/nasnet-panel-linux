import * as React from "react"
import { useEffect, useMemo, useCallback, useState } from "react"
import { useForm, type UseFormReturn } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { inboundSchema, type InboundFormData } from "@/lib/validations/inbound-schema"
import { INBOUND_PRESETS } from "@/lib/presets/inbound-presets"
import { NETWORK_TYPES, SECURITY_TYPES } from "@/lib/types"
import type { Inbound } from "@/lib/types"

export type TabId = "general" | "network" | "transport" | "security" | "protocol" | "advanced"

export interface TabDefinition {
    id: TabId
    label: string
    visible: boolean
    hasErrors: boolean
    badges: string[]
}

const defaultValues: InboundFormData = {
    tag: "",
    remark: "",
    listen: "0.0.0.0",
    port: 443,
    port_range: "",
    protocol: "vless",
    network: "xhttp",
    security: "none",
    sniffing_settings: {
        enabled: true,
        destOverride: ["http", "tls"],
        metadataOnly: false,
        routeOnly: false,
    },
}

// Map error paths to their respective tab
function getTabForErrorPath(path: string): TabId {
    if (path.startsWith("tls_settings") || path.startsWith("reality_settings")) return "security"
    if (path.startsWith("transport_settings")) return "transport"
    if (path.startsWith("vless_settings") || path.startsWith("shadowsocks_settings") ||
        path.startsWith("wireguard_settings") || path.startsWith("http_settings") ||
        path.startsWith("socks_settings") || path.startsWith("vmess_settings") ||
        path.startsWith("trojan_settings") || path.startsWith("dokodemo_settings") ||
        path.startsWith("hysteria_settings")) return "protocol"
    if (path.startsWith("sniffing_settings") || path.startsWith("sockopt_settings")) return "advanced"
    if (path === "network" || path === "security") return "network"
    return "general"
}

export function useInboundForm(
    mode: "create" | "edit",
    inbound: Inbound | null | undefined,
    open: boolean,
) {
    const [activeTab, setActiveTab] = useState<TabId>("general")

    const form = useForm<InboundFormData, unknown, InboundFormData>({
        resolver: zodResolver(inboundSchema),
        mode: "onTouched",
        defaultValues,
    })

    const { watch, setValue, reset, formState: { errors } } = form

    // Tell a dialog-open reset apart from a real protocol switch, so the effect
    // below doesn't wipe just-loaded settings. initializingRef holds until
    // protocol settles on the loaded value (expectedProtocolRef).
    const initializingRef = React.useRef(false)
    const expectedProtocolRef = React.useRef<string | undefined>(undefined)
    const lastProtocolRef = React.useRef<string | undefined>(undefined)

    // Reset form when dialog opens
    useEffect(() => {
        if (!open) return

        let loadedProtocol: string
        if (mode === "edit" && inbound) {
            reset({
                tag: inbound.tag || "",
                remark: inbound.remark || "",
                listen: inbound.listen || "0.0.0.0",
                port: inbound.port || 443,
                port_range: inbound.port_range || "",
                protocol: inbound.protocol as InboundFormData["protocol"] || "vless",
                network: inbound.network || "tcp",
                security: inbound.security || "none",
                tls_settings: inbound.tls_settings,
                reality_settings: inbound.reality_settings,
                transport_settings: inbound.transport_settings,
                sniffing_settings: inbound.sniffing_settings || {
                    enabled: true,
                    destOverride: ["http", "tls"],
                    metadataOnly: false,
                    routeOnly: false,
                },
                sockopt_settings: inbound.sockopt_settings,
                vless_settings: inbound.vless_settings,
                vmess_settings: inbound.vmess_settings,
                shadowsocks_settings: inbound.shadowsocks_settings,
                wireguard_settings: inbound.wireguard_settings,
                http_settings: inbound.http_settings,
                socks_settings: inbound.socks_settings,
                // Previously omitted; editing a Trojan/Dokodemo/Hysteria2
                // inbound dropped their settings on save and the AdvancedTab
                // showed FinalMask as off even when the inbound had it set.
                trojan_settings: inbound.trojan_settings,
                dokodemo_settings: inbound.dokodemo_settings,
                hysteria_settings: inbound.hysteria_settings,
                finalmask: inbound.finalmask,
            })
            loadedProtocol = inbound.protocol || "vless"
        } else {
            reset(defaultValues)
            loadedProtocol = defaultValues.protocol
        }
        // mark upcoming protocol-watch renders as init, not a user switch
        initializingRef.current = true
        expectedProtocolRef.current = loadedProtocol
        lastProtocolRef.current = loadedProtocol
        setActiveTab("general")
    }, [open, mode, inbound, reset])

    // Watch key values for tab visibility & badges
    const protocol = watch("protocol")
    const network = watch("network")
    const security = watch("security")
    const transportSettings = watch("transport_settings")
    const tlsSettings = watch("tls_settings")
    const realitySettings = watch("reality_settings")
    const vlessSettings = watch("vless_settings")
    const shadowsocksSettings = watch("shadowsocks_settings")
    const sniffingSettings = watch("sniffing_settings")

    // On a user protocol switch: clear stale *_settings (they'd pollute the JSONB
    // column) and reset network/security to protocol defaults. Skipped during
    // dialog-open init (initializingRef) so edits keep their loaded settings.
    useEffect(() => {
        if (!open) {
            initializingRef.current = false
            expectedProtocolRef.current = undefined
            lastProtocolRef.current = undefined
            return
        }
        if (initializingRef.current) {
            // still settling the reset; sync baseline, don't treat as a switch
            lastProtocolRef.current = protocol
            if (protocol === expectedProtocolRef.current) {
                initializingRef.current = false
            }
            return
        }
        if (lastProtocolRef.current === protocol) return
        lastProtocolRef.current = protocol
        const allKeys = [
            "vless_settings", "vmess_settings", "trojan_settings",
            "shadowsocks_settings", "wireguard_settings", "http_settings",
            "socks_settings", "dokodemo_settings", "hysteria_settings",
        ] as const
        const keep: Record<string, string | undefined> = {
            vless: "vless_settings",
            vmess: "vmess_settings",
            trojan: "trojan_settings",
            shadowsocks: "shadowsocks_settings",
            wireguard: "wireguard_settings",
            http: "http_settings",
            socks: "socks_settings",
            // Mixed (SOCKS+HTTP) reuses the SOCKS settings form/key.
            mixed: "socks_settings",
            "dokodemo-door": "dokodemo_settings",
            hysteria2: "hysteria_settings",
        }
        const keepKey = keep[protocol]
        for (const key of allKeys) {
            if (key !== keepKey) {
                setValue(key as typeof allKeys[number], undefined, { shouldDirty: true })
            }
        }
        // Protocol-appropriate network/security defaults.
        if (protocol === "socks" || protocol === "mixed" || protocol === "http" || protocol === "dokodemo-door") {
            setValue("network", "tcp", { shouldDirty: true })
            setValue("security", "none", { shouldDirty: true })
        } else if (protocol === "hysteria2") {
            setValue("network", "", { shouldDirty: true })
            setValue("security", "tls", { shouldDirty: true })
        } else if (protocol === "wireguard") {
            setValue("network", "tcp", { shouldDirty: true })
            setValue("security", "none", { shouldDirty: true })
        }
        // shadowsocks keeps user network/security: backend now accepts it over ws/grpc/tcp
    }, [open, protocol, setValue])

    // Derived visibility
    const needsTransport = ["vless", "vmess", "trojan", "shadowsocks"].includes(protocol)
    const needsSecurity = ["vless", "vmess", "trojan", "hysteria2"].includes(protocol)
    // Trojan needs Fallbacks; Dokodemo-door needs destination address/port;
    // Hysteria2 needs auth/timeout. Mixed reuses the SOCKS settings form.
    // VMess has no per-inbound form because its clients are managed
    // dynamically by the subscription system.
    const needsProtocolSettings = [
        "vless", "trojan", "shadowsocks", "wireguard",
        "http", "socks", "mixed", "dokodemo-door", "hysteria2",
    ].includes(protocol)

    // Tab visibility
    const tabVisibility = useMemo(() => ({
        general: true,
        network: needsTransport,
        transport: needsTransport && network !== "tcp",
        security: (needsSecurity && security !== "none") || protocol === "hysteria2",
        protocol: needsProtocolSettings,
        advanced: true,
    }), [needsTransport, needsSecurity, needsProtocolSettings, network, security, protocol])

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
        if (needsTransport) {
            const networkLabel = NETWORK_TYPES.find(n => n.value === network)?.label || network
            badges.network.push(networkLabel)
            if (security !== "none") {
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
        if (protocol === "vless" && vlessSettings?.flow) {
            badges.protocol.push(vlessSettings.flow)
        }
        if (protocol === "shadowsocks" && shadowsocksSettings?.method) {
            const shortMethod = shadowsocksSettings.method.replace("2022-blake3-", "")
            badges.protocol.push(shortMethod)
        }

        // Advanced tab badges
        if (sniffingSettings?.enabled) {
            badges.advanced.push("Sniffing")
        }

        return badges
    }, [needsTransport, network, security, transportSettings, tlsSettings, realitySettings,
        protocol, vlessSettings, shadowsocksSettings, sniffingSettings])

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
        const preset = INBOUND_PRESETS.find(p => p.id === presetId)
        if (!preset) return

        const currentTag = form.getValues("tag")
        const currentRemark = form.getValues("remark")

        reset({
            ...defaultValues,
            ...preset.defaults,
            tag: currentTag,
            remark: currentRemark,
            // Ensure sniffing comes from preset or defaults
            sniffing_settings: preset.defaults.sniffing_settings || defaultValues.sniffing_settings,
        } as InboundFormData)
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
        navigateToFirstError,
        // Convenience: watched values for child components
        protocol,
        network,
        security,
    }
}

export type InboundFormReturn = ReturnType<typeof useInboundForm>
