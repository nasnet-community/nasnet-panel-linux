import * as React from "react"
import { useEffect, useMemo, useCallback, useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { SlidersHorizontal, FileCode, ArrowRightLeft, Wrench } from "lucide-react"
import { outboundSchema, type OutboundFormData } from "@/lib/validations/outbound-schema"
import { OUTBOUND_PRESETS } from "@/lib/presets/outbound-presets"
import { SECURITY_TYPES, OUTBOUND_PROTOCOLS } from "@/lib/types"
import { shortNetworkLabel } from "@/components/connection-dialog/labels"
import { parseConfigLink } from "@/lib/link-parser"
import { toast } from "sonner"
import type { Outbound } from "@/lib/types"
import type { RailItem } from "@/components/connection-dialog/dialog-rail"
import type { StackToken } from "@/components/connection-dialog/stack-summary"
import { flattenErrors, labelForPath, type FlatError } from "@/lib/form-errors"

export type TabId = "general" | "protocol" | "transport" | "advanced"
export const TAB_ORDER: TabId[] = ["general", "protocol", "transport", "advanced"]

export interface TabError extends FlatError {
    tab: TabId
    label: string
}

export interface SectionVisibility {
    network: boolean
    transportSettings: boolean
    security: boolean
}

// Protocols that dial a remote server over xray streamSettings.
const TRANSPORT_PROTOCOLS = ["vless", "vmess", "trojan", "shadowsocks", "socks", "http"]
// Protocols that need address + port on the General tab.
export const NEEDS_ADDRESS_PROTOCOLS = [...TRANSPORT_PROTOCOLS, "hysteria2"]

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

export function getTabForErrorPath(path: string): TabId {
    if (
        path.startsWith("tls_settings") || path.startsWith("reality_settings") ||
        path.startsWith("transport_settings") || path === "network" || path === "security"
    ) return "transport"
    if (path.startsWith("sockopt_settings") || path.startsWith("mux_settings") ||
        path.startsWith("proxy_settings") || path.startsWith("finalmask") || path === "send_through") return "advanced"
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
        path.startsWith("loopback_settings") ||
        path.startsWith("hysteria_settings")
    ) return "protocol"
    return "general"
}

const labelOf = (list: readonly { value: string; label: string }[], value: string) =>
    list.find((x) => x.value === value)?.label ?? value

export function useOutboundForm(
    mode: "create" | "edit",
    outbound: Outbound | null | undefined,
    open: boolean,
) {
    const [activeTab, setActiveTab] = useState<TabId>("general")
    const [appliedPresetId, setAppliedPresetId] = useState<string | null>(null)

    const form = useForm<OutboundFormData, unknown, OutboundFormData>({
        resolver: zodResolver(outboundSchema),
        mode: "onTouched",
        defaultValues,
    })

    const { watch, reset, setValue, formState: { errors } } = form

    // Guards for the protocol-switch effect below. Opening the dialog (or
    // applying a template / importing a link) drives a reset whose re-render
    // changes the watched protocol; without these that render would look like
    // a user switch and wipe the sibling settings we just loaded — marking
    // the form dirty before the user touched anything.
    const initializingRef = React.useRef(false)
    const expectedProtocolRef = React.useRef<string | undefined>(undefined)
    const lastProtocolRef = React.useRef<string | undefined>(undefined)

    const beginInitialization = useCallback((loadedProtocol: string) => {
        initializingRef.current = true
        expectedProtocolRef.current = loadedProtocol
        lastProtocolRef.current = loadedProtocol
    }, [])

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
                security: outbound.protocol === "hysteria2" ? "tls" : outbound.security || "none",
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
            beginInitialization(outbound.protocol || "freedom")
        } else {
            reset(defaultValues)
            beginInitialization(defaultValues.protocol)
        }
        setActiveTab("general")
        setAppliedPresetId(null)
    }, [open, mode, outbound, reset, beginInitialization])

    const protocol = watch("protocol")

    // On a USER protocol switch: clear stale protocol-specific settings so
    // the save payload reflects the active protocol only. Must not fire while
    // a reset is settling (see initializingRef above).
    useEffect(() => {
        if (!open) {
            initializingRef.current = false
            expectedProtocolRef.current = undefined
            lastProtocolRef.current = undefined
            return
        }
        if (protocol === "hysteria2") {
            setValue("security", "tls")
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

    const tag = watch("tag")
    const address = watch("address")
    const port = watch("port")
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
    const wireguardSettings = watch("wireguard_settings")
    const sockoptSettings = watch("sockopt_settings")
    const muxSettings = watch("mux_settings")
    const proxySettings = watch("proxy_settings")
    const sendThrough = watch("send_through")
    const finalmask = watch("finalmask")

    const usesStream = TRANSPORT_PROTOCOLS.includes(protocol)
    const needsAddress = NEEDS_ADDRESS_PROTOCOLS.includes(protocol)

    const sectionVisibility = useMemo<SectionVisibility>(() => ({
        network: usesStream,
        transportSettings: usesStream && !!network,
        security: (usesStream && !!security && security !== "none") || protocol === "hysteria2",
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

    const protocolLabel = labelOf(OUTBOUND_PROTOCOLS, protocol)

    const rail = useMemo<RailItem[]>(() => {
        const generalParts = [tag || "—"]
        if (needsAddress && address) generalParts.push(`${address}:${port || "—"}`)

        const protoParts = [protocolLabel]
        if (protocol === "freedom" && freedomSettings?.domainStrategy) protoParts.push(freedomSettings.domainStrategy)
        if (protocol === "blackhole" && blackholeSettings?.responseType) protoParts.push(blackholeSettings.responseType)
        if (protocol === "vless" && vlessSettings?.flow) protoParts.push(vlessSettings.flow.replace("xtls-rprx-", ""))
        if (protocol === "vmess" && vmessSettings?.security) protoParts.push(vmessSettings.security)
        if (protocol === "shadowsocks" && shadowsocksSettings?.method) protoParts.push(shadowsocksSettings.method.replace("2022-blake3-", ""))
        if (protocol === "loopback" && loopbackSettings?.inboundTag) protoParts.push(loopbackSettings.inboundTag)
        if (protocol === "wireguard") {
            const peers = wireguardSettings?.peers?.length ?? 0
            protoParts.push(peers > 0 ? `${peers} peer${peers > 1 ? "s" : ""}` : "no peers")
        }

        let transportSummary: string
        let transportDisabled = false
        if (protocol === "hysteria2") {
            transportSummary = ["QUIC", "TLS", tlsSettings?.serverName].filter(Boolean).join(" · ")
        } else if (!usesStream) {
            transportSummary = `Not used by ${protocolLabel}`
            transportDisabled = true
        } else {
            const parts = [shortNetworkLabel(network)]
            parts.push(!security || security === "none" ? "no security" : labelOf(SECURITY_TYPES, security))
            if (security === "tls" && tlsSettings?.serverName) parts.push(tlsSettings.serverName)
            else if (security === "reality" && realitySettings?.serverNames?.[0]) parts.push(realitySettings.serverNames[0])
            else if (transportSettings?.path && transportSettings.path !== "/") parts.push(transportSettings.path)
            transportSummary = parts.join(" · ")
        }

        const advParts = [muxSettings?.enabled ? "Mux on" : "Mux off"]
        if (proxySettings?.tag) advParts.push(`via ${proxySettings.tag}`)
        if (sockoptSettings) advParts.push("sockopt")
        if (sendThrough) advParts.push(`from ${sendThrough}`)
        if (finalmask && (finalmask.tcp || finalmask.udp || finalmask.quicParams)) advParts.push("FinalMask")

        return [
            { id: "general", label: "General", icon: SlidersHorizontal, summary: generalParts.join(" · "), errorCount: errorCounts.general },
            { id: "protocol", label: "Protocol", icon: FileCode, summary: protoParts.join(" · "), errorCount: errorCounts.protocol },
            { id: "transport", label: "Transport", icon: ArrowRightLeft, summary: transportSummary, errorCount: errorCounts.transport, disabled: transportDisabled },
            { id: "advanced", label: "Advanced", icon: Wrench, summary: advParts.join(" · "), errorCount: errorCounts.advanced },
        ]
    }, [tag, address, port, needsAddress, protocol, protocolLabel, usesStream, network, security, transportSettings,
        tlsSettings, realitySettings, freedomSettings, blackholeSettings, vlessSettings, vmessSettings,
        shadowsocksSettings, loopbackSettings, wireguardSettings, sockoptSettings, muxSettings, proxySettings,
        sendThrough, finalmask, errorCounts])

    const stack = useMemo<StackToken[]>(() => {
        const tokens: StackToken[] = [{ label: protocol, tab: "protocol" }]
        if (protocol === "hysteria2") {
            tokens.push({ label: "quic", tab: "transport" }, { label: "tls", tab: "transport" })
        } else if (usesStream) {
            if (network) tokens.push({ label: network, tab: "transport" })
            tokens.push({ label: !security || security === "none" ? "none" : security, tab: "transport", dim: !security || security === "none" })
        } else if (protocol === "freedom" && freedomSettings?.domainStrategy) {
            tokens.push({ label: freedomSettings.domainStrategy, tab: "protocol", dim: true })
        } else if (protocol === "blackhole") {
            tokens.push({ label: blackholeSettings?.responseType || "none", tab: "protocol", dim: true })
        } else if (protocol === "loopback" && loopbackSettings?.inboundTag) {
            tokens.push({ label: loopbackSettings.inboundTag, tab: "protocol", dim: true })
        }
        if (protocol === "vless" && vlessSettings?.flow) {
            tokens.push({ label: vlessSettings.flow.replace("xtls-rprx-", ""), tab: "protocol", dim: true })
        }
        if (needsAddress && address) tokens.push({ label: `${address}:${port || "—"}`, tab: "general", dim: true })
        if (proxySettings?.tag) tokens.push({ label: `via ${proxySettings.tag}`, tab: "advanced", dim: true })
        return tokens
    }, [protocol, usesStream, network, security, freedomSettings, blackholeSettings, loopbackSettings, vlessSettings, needsAddress, address, port, proxySettings])

    // Apply preset: reset to the preset's defaults, keeping tag/remark.
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
        beginInitialization(preset.defaults.protocol ?? defaultValues.protocol)
        setAppliedPresetId(presetId)
    }, [form, reset, beginInitialization])

    // Import a share link: parse and fill every tab. Existing tag/remark win
    // over the link's remark only when the user already typed them.
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
        } as OutboundFormData, { keepDefaultValues: true })
        beginInitialization(result.outbound.protocol ?? defaultValues.protocol)
        setAppliedPresetId(null)

        toast.success(`Imported ${result.outbound.protocol?.toUpperCase()} config`)
        return { success: true }
    }, [form, reset, beginInitialization])

    // After a failed submit: open the first tab (in rail order) with an error
    // and try to focus its field. Reads formState live, not a stale closure.
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
        needsAddress,
        applyPreset,
        appliedPresetId,
        importConfigLink,
        navigateToFirstError,
        // Convenience: watched values for child components
        protocol,
        network,
        security,
    }
}

export type OutboundFormReturn = ReturnType<typeof useOutboundForm>
