import {
    Activity, Archive, Bell, Box, ClipboardList, Globe, History, House, IdCard, KeyRound,
    Link as LinkIcon, Router, Server, Settings, ShieldCheck, Users, Waypoints, type LucideIcon,
} from "lucide-react"

export type NavBadgeKey = "alerts"

export interface NavItem {
    label: string
    href: string
    icon: LucideIcon
    badgeKey?: NavBadgeKey
    /** Other route prefixes that count as this item. */
    also?: string[]
    mobileLabel?: string
    children?: NavItem[]
}

export interface NavSection {
    label?: string
    items: NavItem[]
}

export const NAV_SECTIONS: NavSection[] = [
    {
        items: [{ label: "Dashboard", href: "/dashboard", icon: House, mobileLabel: "Home" }],
    },
    {
        label: "Customers",
        items: [
            { label: "Users", href: "/users", icon: Users },
            {
                label: "Subscriptions", href: "/subscriptions", icon: KeyRound, mobileLabel: "Subs",
                children: [{ label: "Accounts", href: "/accounts", icon: IdCard }],
            },
        ],
    },
    {
        label: "Infrastructure",
        items: [
            {
                label: "Router", href: "/router", icon: Router, also: ["/network"],
                children: [{ label: "Traffic Flow", href: "/router/flow", icon: Waypoints }],
            },
            {
                label: "Nodes", href: "/nodes", icon: Server, also: ["/server"],
                children: [
                    { label: "Hosts", href: "/hosts", icon: LinkIcon },
                    { label: "Xray Core", href: "/xray-binaries", icon: Box },
                ],
            },
            {
                label: "Certificates", href: "/certificates", icon: ShieldCheck,
                children: [{ label: "Domains", href: "/domains", icon: Globe }],
            },
        ],
    },
    {
        label: "Monitoring",
        items: [
            { label: "Alerts", href: "/alerts", icon: Bell, badgeKey: "alerts" },
            { label: "Access Logs", href: "/access-logs", icon: Activity },
            { label: "Access History", href: "/access-history", icon: History },
            { label: "Audit Log", href: "/audit", icon: ClipboardList },
        ],
    },
]

export const SETTINGS_ITEM: NavItem = {
    label: "Settings", href: "/settings", icon: Settings, also: ["/backup"],
}

// Reached from Settings and ⌘K rather than the sidebar.
const EXTRA_PAGES: NavItem[] = [
    { label: "Database Backup", href: "/backup", icon: Archive },
]

export const ALL_NAV_ITEMS: NavItem[] = [
    ...NAV_SECTIONS.flatMap((s) => s.items.flatMap((i) => [i, ...(i.children ?? [])])),
    SETTINGS_ITEM,
]

export const PALETTE_PAGES: NavItem[] = [...ALL_NAV_ITEMS, ...EXTRA_PAGES]

export const MOBILE_TABS: NavItem[] = ["/dashboard", "/router", "/subscriptions", "/nodes"]
    .map((href) => ALL_NAV_ITEMS.find((i) => i.href === href)!)

// `g` then the letter jumps to the page.
export const GO_KEYS: Record<string, string> = {
    d: "/dashboard", u: "/users", s: "/subscriptions", n: "/nodes", r: "/router",
}

export function goKeyFor(href: string): string | undefined {
    return Object.keys(GO_KEYS).find((key) => GO_KEYS[key] === href)
}

export function routeMatches(pathname: string, prefix: string): boolean {
    return pathname === prefix || pathname.startsWith(`${prefix}/`)
}

export function isItemActive(item: NavItem, pathname: string): boolean {
    return [item.href, ...(item.also ?? [])].some((prefix) => routeMatches(pathname, prefix))
}

export function isChildActive(item: NavItem, pathname: string): boolean {
    return (item.children ?? []).some((child) => isItemActive(child, pathname))
}

export function isGroupActive(item: NavItem, pathname: string): boolean {
    return isItemActive(item, pathname) || isChildActive(item, pathname)
}
