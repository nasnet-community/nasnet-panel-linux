import { Checkbox } from "@/components/ui/checkbox"
import type { MtprotoSettings, MtprotoModes } from "@/lib/types/cores"
import {
    TextSetting,
    NumberSetting,
    SwitchSetting,
    SelectSetting,
    ListSetting,
    SettingRow,
} from "./settings-fields"
import { FormSection } from "./shared-fields"

const MODE_LABELS: { key: keyof MtprotoModes; label: string; hint: string }[] = [
    {
        key: "classic",
        label: "Classic",
        hint: "Plain obfuscation. Detectable by modern DPI — legacy clients only.",
    },
    { key: "secure", label: "Secure (dd)", hint: "Random-padding obfuscation." },
    { key: "tls", label: "FakeTLS (ee)", hint: "Looks like a real TLS visit to the SNI below." },
]

/** Mbps <-> bytes/sec, the unit telemt's rate limiter takes. */
const MBPS_TO_BPS = 1_000_000 / 8
const toMbps = (bps: number | undefined) =>
    bps && bps > 0 ? Math.round((bps / MBPS_TO_BPS) * 10) / 10 : 0
const fromMbps = (mbps: number) => (mbps > 0 ? Math.round(mbps * MBPS_TO_BPS) : 0)

export function MtprotoForm({
    value,
    onChange,
    builtIn,
    errors = {},
}: {
    value: MtprotoSettings
    onChange: (v: MtprotoSettings) => void
    /** the tunnel lives on the panel's built-in node */
    builtIn: boolean
    errors?: Record<string, string>
}) {
    const set = (patch: Partial<MtprotoSettings>) => onChange({ ...value, ...patch })
    const modes: MtprotoModes = value.modes ?? { classic: true, secure: true, tls: true }
    const setMode = (key: keyof MtprotoModes, on: boolean) =>
        set({ modes: { ...modes, [key]: on } })

    const extraDomains = value.tls_domains_extra ?? []
    const hasDomain = (value.tls_domain ?? "").trim() !== "" || extraDomains.length > 0
    // FakeTLS with no SNI cannot produce a client secret, so it is simply not
    // offered — mirror that here instead of letting the checkbox lie.
    const tlsOffered = !!modes.tls && hasDomain
    const linkMode = value.link_mode ?? "auto"
    const effectiveLinkMode =
        linkMode !== "auto"
            ? linkMode
            : tlsOffered
              ? "tls"
              : modes.secure
                ? "secure"
                : modes.classic
                  ? "classic"
                  : null

    return (
        <div className="space-y-5">
            <FormSection
                title="Camouflage modes"
                description="What the relay accepts on the wire. The client link encodes the mode, so this also decides what users are handed."
            >
                <div className="space-y-2">
                    {MODE_LABELS.map(({ key, label, hint }) => (
                        <label
                            key={key}
                            className="flex cursor-pointer items-center gap-3 rounded-lg border p-2.5"
                        >
                            <Checkbox
                                checked={modes[key] ?? false}
                                onCheckedChange={(c) => setMode(key, c === true)}
                            />
                            <div>
                                <div className="text-sm font-medium">{label}</div>
                                <div className="text-xs text-muted-foreground">{hint}</div>
                            </div>
                        </label>
                    ))}
                </div>
                {errors.modes && <p className="text-xs text-destructive">{errors.modes}</p>}

                {modes.tls && (
                    <>
                        <TextSetting
                            label="FakeTLS SNI domain"
                            value={value.tls_domain || ""}
                            onChange={(v) => set({ tls_domain: v })}
                            placeholder="e.g. www.cloudflare.com"
                            mono
                            error={errors.tls_domain}
                            description="The site the proxy impersonates. Pick one that is popular and reachable from your users' network."
                        />
                        <ListSetting
                            label="Extra SNI domains"
                            values={extraDomains}
                            onChange={(v) => set({ tls_domains_extra: v })}
                            placeholder="another.example.com"
                            description="Optional. Each domain gets its own client link, so users can switch if one is blocked."
                        />
                        <TextSetting
                            label="Mask host"
                            value={value.mask_host || ""}
                            onChange={(v) => set({ mask_host: v })}
                            placeholder="defaults to the SNI domain"
                            mono
                            description="Where a failed probe is forwarded, so a scanner sees a real site instead of a closed port."
                        />
                        <SelectSetting
                            label="Unknown SNI"
                            value={value.unknown_sni_action ?? "accept"}
                            onChange={(v) =>
                                set({ unknown_sni_action: v as "accept" | "drop" })
                            }
                            options={[
                                { value: "accept", label: "Accept — forward to the mask host" },
                                { value: "drop", label: "Drop the connection" },
                            ]}
                            description="Accept is safer: dropping is itself a signal to a probe."
                        />
                        <SwitchSetting
                            label="Mask bad clients"
                            checked={value.mask ?? true}
                            onChange={(v) => set({ mask: v })}
                            description="Proxy unauthenticated clients to the mask host rather than closing on them."
                        />
                        <SwitchSetting
                            label="TLS fingerprint emulation"
                            checked={value.tls_emulation ?? true}
                            onChange={(v) => set({ tls_emulation: v })}
                            description="Mirror the real site's TLS handshake shape."
                        />
                    </>
                )}
            </FormSection>

            <FormSection
                title="Client links"
                description="An MTProto secret carries its mode: classic is bare, secure is dd-prefixed, FakeTLS is ee-prefixed with the SNI."
            >
                <SelectSetting
                    label="Issue links for"
                    value={linkMode}
                    onChange={(v) => set({ link_mode: v as MtprotoSettings["link_mode"] })}
                    options={[
                        { value: "auto", label: "Auto — strongest enabled mode" },
                        { value: "tls", label: "FakeTLS (ee)" },
                        { value: "secure", label: "Secure (dd)" },
                        { value: "classic", label: "Classic" },
                    ]}
                    error={errors.link_mode}
                    description={
                        effectiveLinkMode
                            ? `Users will get a ${effectiveLinkMode} link.`
                            : "No mode is currently usable — users would get no link."
                    }
                />
                <SwitchSetting
                    label="Offer every mode"
                    checked={value.link_all_modes ?? false}
                    onChange={(v) => set({ link_all_modes: v })}
                    description="Hand out one link per enabled mode (and per SNI domain) so a user blocked on one can fall back."
                />
            </FormSection>

            <FormSection title="Routing">
                {builtIn ? (
                    <div className="rounded-lg border border-primary/30 bg-primary/5 p-3 text-xs text-muted-foreground">
                        On a router's built-in node the relay reaches Telegram through the VPN pool, so the
                        middle proxy and ad tag are unavailable: their connections would bypass it.
                    </div>
                ) : (
                    <>
                        <SwitchSetting
                            label="Telegram middle proxy"
                            checked={value.middle_proxy ?? false}
                            onChange={(v) => set({ middle_proxy: v })}
                            description="Route via Telegram's official middle proxies (required for promoted channels). Needs direct reachability to Telegram's ME servers — it will not come up on a network that blocks them."
                        />
                        <TextSetting
                            label="Ad tag"
                            value={value.adtag_default || ""}
                            onChange={(v) => set({ adtag_default: v })}
                            placeholder="promoted-channel tag (optional)"
                            mono
                            error={errors.adtag_default}
                            description="Only delivered through the middle proxy — ignored without it."
                        />
                    </>
                )}
            </FormSection>

            <FormSection
                title="Limits"
                description="Applied to every user on this tunnel. A subscription's own speed limit takes precedence when set."
            >
                <SettingRow
                    label="Speed cap"
                    description="0 leaves the tunnel uncapped."
                >
                    <div className="grid grid-cols-2 gap-3">
                        <NumberSetting
                            label="Upload (Mbps)"
                            value={toMbps(value.rate_limit_up_bps)}
                            onChange={(v) => set({ rate_limit_up_bps: fromMbps(v) })}
                            min={0}
                        />
                        <NumberSetting
                            label="Download (Mbps)"
                            value={toMbps(value.rate_limit_down_bps)}
                            onChange={(v) => set({ rate_limit_down_bps: fromMbps(v) })}
                            min={0}
                        />
                    </div>
                </SettingRow>
                <NumberSetting
                    label="Device-limit window (seconds)"
                    value={value.unique_ip_window_secs ?? 0}
                    onChange={(v) => set({ unique_ip_window_secs: v })}
                    min={0}
                    description="How long an IP keeps counting against a user's device limit. 0 uses the relay's default."
                />
            </FormSection>

            <FormSection title="Advanced">
                <SelectSetting
                    label="Relay log level"
                    value={value.log_level ?? "normal"}
                    onChange={(v) => set({ log_level: v as MtprotoSettings["log_level"] })}
                    options={[
                        { value: "silent", label: "Silent" },
                        { value: "normal", label: "Normal" },
                        { value: "verbose", label: "Verbose" },
                        { value: "debug", label: "Debug" },
                    ]}
                    description="Raise to debug handshake failures on the node."
                />
                <NumberSetting
                    label="Listen backlog"
                    value={value.listen_backlog ?? 0}
                    onChange={(v) => set({ listen_backlog: v })}
                    min={0}
                    description="0 uses the relay's default."
                />
                <SettingRow
                    label="Loopback ports"
                    description="0 auto-assigns (metrics 14300+, egress relay 12300+). The relay only runs on a router's built-in node; with Xray egress, Xray's SOCKS inbound takes that port."
                >
                    <div className="grid grid-cols-2 gap-3">
                        <NumberSetting
                            label="Metrics port"
                            value={value.metrics_port ?? 0}
                            onChange={(v) => set({ metrics_port: v })}
                            min={0}
                        />
                        <NumberSetting
                            label="Relay port"
                            value={value.socks_port ?? 0}
                            onChange={(v) => set({ socks_port: v })}
                            min={0}
                        />
                    </div>
                </SettingRow>
            </FormSection>
        </div>
    )
}
