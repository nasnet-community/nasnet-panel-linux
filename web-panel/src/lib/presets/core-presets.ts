// Default `settings` object per protocol, applied when a protocol is picked in
// the create dialog. Values mirror the agent config defaults.

import type { CoreProtocol, CoreSettings } from "@/lib/types/cores"

export const CORE_PRESETS: Record<CoreProtocol, CoreSettings> = {
    ssh: {
        strategy: "accept",
    },
    mtproto: {
        // FakeTLS is the only mode that survives modern DPI, and it needs an SNI
        // domain to encode into the client secret — so ship one by default
        // rather than silently downgrading new tunnels to the "dd" mode.
        modes: { classic: false, secure: true, tls: true },
        tls_domain: "www.cloudflare.com",
        tls_domains_extra: [],
        mask_host: "",
        unknown_sni_action: "accept",
        mask: true,
        tls_emulation: true,
        link_mode: "auto",
        link_all_modes: false,
        middle_proxy: false,
        adtag_default: "",
        unique_ip_window_secs: 0,
        rate_limit_up_bps: 0,
        rate_limit_down_bps: 0,
        log_level: "normal",
        listen_backlog: 0,
        metrics_port: 0,
        socks_port: 0,
    },
}

/** Suggested default listen port per protocol (0 = admin must choose). */
export const CORE_DEFAULT_PORTS: Record<CoreProtocol, number> = {
    ssh: 2222,
    mtproto: 8443,
}

/** Deep-clone a preset so the form never mutates the shared constant. */
export function presetFor(protocol: CoreProtocol): CoreSettings {
    return structuredClone(CORE_PRESETS[protocol])
}
