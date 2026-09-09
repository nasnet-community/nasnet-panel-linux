import * as React from "react"
import { useEffect, useMemo, useCallback, useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { SlidersHorizontal, FileCode, ArrowRightLeft, Wrench } from "lucide-react"
import { inboundSchema, type InboundFormData } from "@/lib/validations/inbound-schema"
import { INBOUND_PRESETS } from "@/lib/presets/inbound-presets"
import { SECURITY_TYPES, INBOUND_PROTOCOLS } from "@/lib/types"
import type { Inbound } from "@/lib/types"
import type { RailItem } from "@/components/connection-dialog/dialog-rail"
import { shortNetworkLabel } from "@/components/connection-dialog/labels"
import type { StackToken } from "@/components/connection-dialog/stack-summary"
import { flattenErrors, labelForPath, type FlatError } from "@/lib/form-errors"

export type TabId = "general" | "protocol" | "transport" | "advanced"
export const TAB_ORDER: TabId[] = ["general", "protocol", "transport", "advanced"]

export interface TabError extends FlatError {
    tab: TabId
    label: string
}

export interface SectionVisibility {
    /** Network + security selects. */
    network: boolean
    /** Per-network transport settings (path, host, gRPC service name…). */
    transportSettings: boolean
    /** TLS / Reality form. */
    security: boolean
}

// Protocols that carry xray streamSettings (network/security are meaningful).
const STREAM_PROTOCOLS = ["vless", "vmess", "trojan", "shadowsocks", "socks", "http", "mixed", "dokodemo-door"]
// Raw-byte protocols: TCP only, TLS allowed, Reality not.
export const RAW_ONLY_PROTOCOLS = ["socks", "http", "mixed", "dokodemo-door"]

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

export function getTabForErrorPath(path: string): TabId {
    if (
        path.startsWith("tls_settings") || path.startsWith("reality_settings") ||
        path.startsWith("transport_settings") || path === "network" || path === "security"
    ) return "transport"
    if (path.startsWith("vless_settings") || path.startsWith("shadowsocks_settings") ||
        path.startsWith("wireguard_settings") || path.startsWith("http_settings") ||
        path.startsWith("socks_settings") || path.startsWith("vmess_settings") ||
        path.startsWith("trojan_settings") || path.startsWith("dokodemo_settings") ||
        path.startsWith("hysteria_settings")) return "protocol"
    if (path.startsWith("sniffing_settings") || path.startsWith("sockopt_settings") || path.startsWith("finalmask")) return "advanced"
    return "general"
}

const labelOf = (list: readonly { value: string; label: string }[], value: string) =>
    list.find((x) => x.value === value)?.label ?? value

export function useInboundForm(
    mode: "create" | "edit",
    inbound: Inbound | null | undefined,
    open: boolean,
) {
    const [activeTab, setActiveTab] = useState<TabId>("general")
    const [appliedPresetId, setAppliedPresetId] = useState<string | null>(null)

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
        setAppliedPresetId(null)
    }, [open, mode, inbound, reset])

    // Watch key values for summaries, visibility and the stack header
    const tag = watch("tag")
    const listen = watch("listen")
    const port = watch("port")
    const protocol = watch("protocol")
    const network = watch("network")
    const security = watch("security")
    const transportSettings = watch("transport_settings")
    const tlsSettings = watch("tls_settings")
    const realitySettings = watch("reality_settings")
    const vlessSettings = watch("vless_settings")
    const shadowsocksSettings = watch("shadowsocks_settings")
    const sniffingSettings = watch("sniffing_settings")
    const sockoptSettings = watch("sockopt_settings")
    const finalmask = watch("finalmask")

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
        if (RAW_ONLY_PROTOCOLS.includes(protocol)) {
            setValue("network", "tcp", { shouldDirty: true })
            if (security === "reality") setValue("security", "none", { shouldDirty: true })
        } else if (protocol === "hysteria2") {
            setValue("network", "", { shouldDirty: true })
            setValue("security", "tls", { shouldDirty: true })
        } else if (protocol === "wireguard") {
            setValue("network", "tcp", { shouldDirty: true })
            setValue("security", "none", { shouldDirty: true })
        }
        // Shadowsocks keeps whatever network/security the user had configured:
        // the backend accepts it over ws/grpc/tcp like vless/vmess/trojan.
    }, [open, protocol, security, setValue])

    // Derived visibility
    const usesStream = STREAM_PROTOCOLS.includes(protocol)
    const needsProtocolSettings = [
        "vless", "trojan", "shadowsocks", "wireguard",
        "http", "socks", "mixed", "dokodemo-door", "hysteria2",
    ].includes(protocol)

    const sectionVisibility = useMemo<SectionVisibility>(() => ({
        network: usesStream,
        transportSettings: usesStream && network !== "",
        security: (usesStream && security !== "none") || protocol === "hysteria2",
    }), [usesStream, network, security, protocol])

    // Errors → tabs
    const errorList = useMemo<TabError[]>(
        () => flattenErrors(errors).map((e) => ({ ...e, tab: getTabForErrorPath(e.path), label: labelForPath(e.path) })),
        [errors],
    )
    const errorCounts = useMemo(() => {
        const counts: Record<TabId, number> = { general: 0, protocol: 0, transport: 0, advanced: 0 }
        for (const e of errorList) counts[e.tab]++
        return counts
    }, [errorList])

    // Rail summaries
    const rail = useMemo<RailItem[]>(() => {
        const endpoint = `${listen || "0.0.0.0"}:${port || "—"}`
        const protocolLabel = labelOf(INBOUND_PROTOCOLS, protocol)

        const protoParts = [protocolLabel]
        if (protocol === "vless") {
            if (vlessSettings?.flow) protoParts.push(vlessSettings.flow.replace("xtls-rprx-", ""))
            const fb = vlessSettings?.fallbacks?.length ?? 0
            if (fb > 0) protoParts.push(`${fb} fallback${fb > 1 ? "s" : ""}`)
        }
        if (protocol === "shadowsocks" && shadowsocksSettings?.method) {
            protoParts.push(shadowsocksSettings.method.replace("2022-blake3-", ""))
        }
        if (protocol === "vmess") protoParts.push("clients via subscriptions")

        let transportSummary: string
        if (protocol === "wireguard") {
            transportSummary = "Not used by WireGuard"
        } else if (protocol === "hysteria2") {
            transportSummary = ["QUIC", "TLS", tlsSettings?.serverName].filter(Boolean).join(" · ")
        } else {
            const parts = [shortNetworkLabel(network)]
            parts.push(security === "none" ? "no security" : labelOf(SECURITY_TYPES, security))
            if (security === "tls" && tlsSettings?.serverName) parts.push(tlsSettings.serverName)
            else if (security === "reality" && realitySettings?.serverNames?.[0]) parts.push(realitySettings.serverNames[0])
            else if (transportSettings?.path && transportSettings.path !== "/") parts.push(transportSettings.path)
            transportSummary = parts.join(" · ")
        }

        const advParts = [sniffingSettings?.enabled ? "Sniffing on" : "Sniffing off"]
        if (sockoptSettings) advParts.push("sockopt")
        if (finalmask && (finalmask.tcp || finalmask.udp || finalmask.quicParams)) advParts.push("FinalMask")

        return [
            { id: "general", label: "General", icon: SlidersHorizontal, summary: `${tag || "—"} · ${endpoint}`, errorCount: errorCounts.general },
            { id: "protocol", label: "Protocol", icon: FileCode, summary: protoParts.join(" · "), errorCount: errorCounts.protocol, disabled: !needsProtocolSettings },
            { id: "transport", label: "Transport", icon: ArrowRightLeft, summary: transportSummary, errorCount: errorCounts.transport, disabled: protocol === "wireguard" },
            { id: "advanced", label: "Advanced", icon: Wrench, summary: advParts.join(" · "), errorCount: errorCounts.advanced },
        ]
    }, [tag, listen, port, protocol, network, security, transportSettings, tlsSettings, realitySettings,
        vlessSettings, shadowsocksSettings, sniffingSettings, sockoptSettings, finalmask, errorCounts, needsProtocolSettings])

    // Header stack tokens
    const stack = useMemo<StackToken[]>(() => {
        const tokens: StackToken[] = [{ label: protocol, tab: "protocol" }]
        if (protocol === "hysteria2") {
            tokens.push({ label: "quic", tab: "transport" }, { label: "tls", tab: "transport" })
        } else if (usesStream) {
            if (network) tokens.push({ label: network, tab: "transport" })
            tokens.push({ label: security === "none" ? "none" : security, tab: "transport", dim: security === "none" })
        }
        if (protocol === "vless" && vlessSettings?.flow) {
            tokens.push({ label: vlessSettings.flow.replace("xtls-rprx-", ""), tab: "protocol", dim: true })
        }
        tokens.push({ label: `${listen || "0.0.0.0"}:${port || "—"}`, tab: "general", dim: true })
        return tokens
    }, [protocol, usesStream, network, security, vlessSettings, listen, port])

    // Apply preset: full reset to the preset's defaults, keeping the identity
    // fields the user may already have typed.
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
            sniffing_settings: preset.defaults.sniffing_settings || defaultValues.sniffing_settings,
        } as InboundFormData, { keepDefaultValues: true })
        // The reset above changes protocol; make sure the switch effect treats
        // it as initialization rather than a user switch that wipes settings.
        initializingRef.current = true
        expectedProtocolRef.current = preset.defaults.protocol ?? defaultValues.protocol
        lastProtocolRef.current = expectedProtocolRef.current
        setAppliedPresetId(presetId)
    }, [form, reset])

    // After a failed submit: jump to the first tab (in rail order) that has an
    // error and try to focus its first field. Reads formState live so it is
    // not stale relative to the trigger() that just ran.
    const navigateToFirstError = useCallback(() => {
        const live = flattenErrors(form.formState.errors)
        if (live.length === 0) return
        for (const tab of TAB_ORDER) {
            const first = live.find((e) => getTabForErrorPath(e.path) === tab)
            if (first) {
                setActiveTab(tab)
                try {
                    form.setFocus(first.path as Parameters<typeof form.setFocus>[0])
                } catch {
                    // unregistered nested path; the tab switch is enough
                }
                return
            }
        }
    }, [form])

    return {
        form,
        activeTab,
        setActiveTab,
        rail,
        stack,
        errorList,
        errorCounts,
        sectionVisibility,
        usesStream,
        applyPreset,
        appliedPresetId,
        navigateToFirstError,
        // Convenience: watched values for child components
        protocol,
        network,
        security,
    }
}

export type InboundFormReturn = ReturnType<typeof useInboundForm>
