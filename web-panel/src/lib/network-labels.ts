import type {
    InterfaceRole,
    LANDevice,
    NetworkInterfaceView,
    UplinkSlot,
    UplinkView,
} from "@/lib/types/network"

/** netif.Source → what an operator would call the socket. */
const ATTACHMENT_LABELS: Record<string, string> = {
    loopback: "Loopback",
    eth_onboard: "Onboard Ethernet",
    eth_pci: "PCIe Ethernet",
    eth_usb: "USB Ethernet",
    eth_platform: "Built-in Ethernet",
    wifi_pci: "Wi-Fi (PCIe)",
    wifi_usb: "Wi-Fi (USB)",
    tether_android: "Android tether",
    tether_iphone: "iPhone tether",
    wwan_usb: "Cellular (USB)",
    wwan_pcie: "Cellular (PCIe)",
    virt_bridge: "Bridge",
    virt_vlan: "VLAN",
    virt_bond: "Bond",
    virt_other: "Virtual",
    unknown: "Unrecognised",
}

export function attachmentLabel(source: string): string {
    return ATTACHMENT_LABELS[source] ?? source
}

export const ROLE_LABELS: Record<InterfaceRole, string> = {
    unassigned: "Unassigned",
    wan: "Uplink",
    lan: "LAN",
    lan_member: "LAN member",
    mgmt: "Management",
}

const SLOT_LABELS: Record<Exclude<UplinkSlot, "">, string> = {
    domestic: "Domestic ISP",
    secondary: "Secondary 1",
    secondary2: "Secondary 2",
    secondary3: "Secondary 3",
    secondary4: "Secondary 4",
}

export function roleLabel(role: InterfaceRole, slot: UplinkSlot): string {
    if (role === "wan" && slot) return SLOT_LABELS[slot]
    return ROLE_LABELS[role] ?? role
}

function addressRank(cidr: string): number {
    if (cidr.startsWith("169.254.")) return 3
    if (cidr.startsWith("fe80:")) return 4
    if (cidr.includes(".")) return 0
    if (cidr.startsWith("fec0:") || cidr.startsWith("fd") || cidr.startsWith("fc")) return 2
    return 1
}

export interface AddressGroups {
    /** The one address worth reading at a glance — routable IPv4 wins. */
    primary: string | null
    /** Everything else, most useful first. Collapsed in the UI. */
    extra: string[]
}

/** Every interface carries an fe80:: address and often a site-local one. Showing
 *  all three per row buried the address that actually identifies the box. */
export function groupAddresses(addrs: string[] | null | undefined): AddressGroups {
    const sorted = [...(addrs ?? [])].sort((a, b) => addressRank(a) - addressRank(b))
    return { primary: sorted[0] ?? null, extra: sorted.slice(1) }
}

export type LinkTone = "up" | "warn" | "down" | "absent"

// The verdict outranks the carrier bit: a green dot on a dead WAN is a lie.
export function linkTone(iface: NetworkInterfaceView, uplink?: UplinkView): LinkTone {
    if (!iface.present) return "absent"
    if (uplink?.verdict) {
        switch (uplink.verdict) {
            case "up":
                return "up"
            case "degraded":
            case "forced-up":
                return "warn"
            default:
                return "down"
        }
    }
    return iface.carrier ? "up" : "down"
}

export function linkLabel(iface: NetworkInterfaceView, uplink?: UplinkView): string {
    if (!iface.present) return "absent"
    if (uplink?.verdict) return uplink.verdict.replace("-", " ")
    return iface.carrier ? "up" : iface.oper_state || "down"
}

export function isSecondarySlot(slot: UplinkSlot): boolean {
    return slot.startsWith("secondary")
}

/** The one role a router is broken without. Management is optional, so it gets
 *  no nag — the role picker explains it where the choice is made. */
export function missingRoleHint(interfaces: NetworkInterfaceView[]): string | null {
    if (interfaces.some((i) => i.role === "lan")) return null
    // The bridge and its DHCP run whether or not a port holds the role; what is
    // missing is somewhere to plug in.
    return "No LAN port yet — nothing can plug in. Give the port your switch or access point uses the LAN role."
}

/** Warnings the page already states from structured fields. The backend emits
 *  the takeover and uplink-count strings too, and rendering both showed the
 *  same sentence twice. Unrecognised warnings still pass through. */
const COVERED_WARNINGS = [/^network not managed by nasnet yet/i, /uplink\(s\) assigned/i]

export function uncoveredWarnings(warnings: string[] | null | undefined): string[] {
    return (warnings ?? []).filter((w) => !COVERED_WARNINGS.some((re) => re.test(w)))
}

/** Where a device's displayed name came from. The operator's question about any
 *  row is "is this what I think it is", and the answer is mostly provenance: a
 *  name you typed is worth more than one the device claims for itself. */
export type NameProvenance = "named" | "claimed" | "vendor" | "unknown"

export const NAME_PROVENANCE_NOTES: Record<NameProvenance, string> = {
    named: "You named this device.",
    claimed: "The device asked to be called this. Clients choose their own hostname.",
    vendor: "No name offered. This is who registered the MAC address prefix.",
    unknown: "No name and no registered vendor. A randomized MAC has no vendor.",
}

export function deviceName(d: LANDevice): { name: string; from: NameProvenance } {
    if (d.label) return { name: d.label, from: "named" }
    if (d.hostname) return { name: d.hostname, from: "claimed" }
    if (d.vendor) return { name: d.vendor, from: "vendor" }
    return { name: d.mac, from: "unknown" }
}

/** Plain-language age. Precision past a minute is noise: the bridge only ages
 *  entries every few minutes anyway. */
export function lastSeenLabel(seconds: number | undefined): string {
    if (seconds === undefined) return "Not seen on the bridge"
    if (seconds < 60) return "Seconds ago"
    const mins = Math.floor(seconds / 60)
    if (mins < 60) return `${mins} minute${mins === 1 ? "" : "s"} ago`
    const hours = Math.floor(mins / 60)
    return `${hours} hour${hours === 1 ? "" : "s"} ago`
}
