import type { Inbound } from "@/lib/types"
import type { InboundDetail } from "./desktop-inbound-row"

// The handful of fields that actually identify a listener. Order matters: the
// first one rides in the row, the rest wait in the panel's Details tab.
export function buildInboundDetails(inbound: Inbound): InboundDetail[] {
    const details: InboundDetail[] = []
    if (inbound.security === "tls" && inbound.tls_settings?.serverName) {
        details.push({ label: "SNI", value: inbound.tls_settings.serverName })
    }
    if (inbound.security === "reality" && inbound.reality_settings?.serverNames?.[0]) {
        details.push({ label: "SNI", value: inbound.reality_settings.serverNames[0] })
    }
    if (inbound.transport_settings?.path) {
        details.push({ label: "Path", value: inbound.transport_settings.path })
    }
    if (inbound.transport_settings?.host) {
        details.push({ label: "Host", value: inbound.transport_settings.host })
    }
    if (inbound.transport_settings?.serviceName) {
        details.push({ label: "Service", value: inbound.transport_settings.serviceName })
    }
    return details
}
