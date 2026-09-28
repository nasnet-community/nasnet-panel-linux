import { useEffect, useId, useRef, useState } from "react"
import { Link, useLocation } from "react-router"
import { ChevronDown } from "lucide-react"
import { cn } from "@/lib/utils"
import { Popover, PopoverAnchor, PopoverContent } from "@/components/ui/popover"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import {
    goKeyFor, isChildActive, isGroupActive, isItemActive, type NavItem, type NavSection,
} from "@/components/layouts/nav-config"
import type { NavBadges } from "@/components/layouts/use-nav-badges"
import { NavBadge, type NavBadgeValue } from "./nav-badge"

export type { NavItem, NavSection }

// Groups the user opened or closed by hand, keyed by parent href.
const OVERRIDES_KEY = "sidebar-expanded-groups"

function readOverrides(): Record<string, boolean> {
    try {
        const raw: unknown = JSON.parse(localStorage.getItem(OVERRIDES_KEY) ?? "{}")
        return raw && typeof raw === "object" && !Array.isArray(raw) ? (raw as Record<string, boolean>) : {}
    } catch {
        return {}
    }
}

export interface SidebarNavProps {
    sections: NavSection[]
    collapsed: boolean
    badges: NavBadges
}

export function SidebarNav({ sections, collapsed, badges }: SidebarNavProps) {
    const { pathname } = useLocation()
    const navRef = useRef<HTMLElement>(null)
    const [overrides, setOverrides] = useState(readOverrides)

    // A group holding the current page stays open. Otherwise the user's
    // choice wins, and by default only the current group is open.
    const isOpen = (item: NavItem) =>
        isChildActive(item, pathname) || (overrides[item.href] ?? isItemActive(item, pathname))

    const toggle = (item: NavItem) => {
        const next = { ...overrides, [item.href]: !isOpen(item) }
        setOverrides(next)
        try {
            localStorage.setItem(OVERRIDES_KEY, JSON.stringify(next))
        } catch {
            // storage blocked: the toggle still holds for this session
        }
    }

    useEffect(() => {
        navRef.current?.querySelector<HTMLElement>('[aria-current="page"]')?.scrollIntoView?.({ block: "nearest" })
    }, [pathname, collapsed])

    const badgeFor = (item: NavItem) => (item.badgeKey ? badges[item.badgeKey] : undefined)

    if (collapsed) {
        return (
            <nav ref={navRef} aria-label="Sections" className="flex-1 min-h-0 overflow-y-auto no-scrollbar py-1.5 flex flex-col items-center">
                {sections.map((section, idx) => (
                    <div key={section.label ?? "top"} className="flex flex-col items-center gap-0.5">
                        {idx > 0 && <div className="w-6 my-2 border-t border-border/40" />}
                        {section.items.map((item) =>
                            item.children?.length ? (
                                <RailFlyout key={item.href} item={item} pathname={pathname} />
                            ) : (
                                <RailLink key={item.href} item={item} pathname={pathname} badge={badgeFor(item)} />
                            ),
                        )}
                    </div>
                ))}
            </nav>
        )
    }

    return (
        <nav ref={navRef} aria-label="Sections" className="flex-1 min-h-0 overflow-y-auto px-3 py-1.5">
            {sections.map((section, idx) => (
                <div key={section.label ?? "top"} className={cn("space-y-px", idx > 0 && "mt-3")}>
                    {section.label && (
                        <div className="px-3 pb-1 text-[12px] font-medium leading-4 tracking-[0.08em] uppercase text-muted-foreground/80">
                            {section.label}
                        </div>
                    )}
                    {section.items.map((item) => (
                        <NavGroup
                            key={item.href}
                            item={item}
                            pathname={pathname}
                            badge={badgeFor(item)}
                            open={isOpen(item)}
                            onToggle={() => toggle(item)}
                        />
                    ))}
                </div>
            ))}
        </nav>
    )
}

function NavGroup({ item, pathname, badge, open, onToggle }: {
    item: NavItem
    pathname: string
    badge?: NavBadgeValue
    open: boolean
    onToggle: () => void
}) {
    const childrenId = useId()
    const hasChildren = !!item.children?.length
    const canToggle = hasChildren && !isChildActive(item, pathname)
    return (
        <>
            <div className="relative">
                {/* A child owns its route: the parent row stays quiet on /router/flow. */}
                <NavLinkRow item={item} active={isItemActive(item, pathname) && !isChildActive(item, pathname)} badge={badge} reserveToggle={hasChildren} />
                {canToggle && (
                    <button
                        type="button"
                        onClick={onToggle}
                        aria-label={`${open ? "Collapse" : "Expand"} ${item.label}`}
                        aria-expanded={open}
                        aria-controls={childrenId}
                        className="absolute right-1.5 top-1/2 -translate-y-1/2 size-5 flex items-center justify-center rounded text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors"
                    >
                        <ChevronDown className={cn("size-3.5 transition-transform duration-200", !open && "-rotate-90")} />
                    </button>
                )}
            </div>
            {hasChildren && (
                <div id={childrenId} hidden={!open} className="space-y-px">
                    {item.children!.map((child) => (
                        <NavLinkRow key={child.href} item={child} child active={isItemActive(child, pathname)} />
                    ))}
                </div>
            )}
        </>
    )
}

export function NavLinkRow({ item, active, badge, child = false, reserveToggle = false }: {
    item: NavItem
    active: boolean
    badge?: NavBadgeValue
    child?: boolean
    reserveToggle?: boolean
}) {
    const Icon = item.icon
    return (
        <Link
            to={item.href}
            aria-current={active ? "page" : undefined}
            className={cn(
                "relative flex items-center rounded-md transition-colors",
                child ? "h-7 gap-2.5 pl-[42px] pr-3" : cn("h-[30px] gap-3 pl-3", reserveToggle ? "pr-8" : "pr-2.5"),
                active
                    ? "bg-primary/10 text-primary font-medium"
                    : "text-muted-foreground hover:text-foreground hover:bg-muted/40",
            )}
        >
            {active && (
                <span className={cn("absolute left-0 top-1/2 -translate-y-1/2 w-[3px] rounded-r-full bg-primary", child ? "h-4" : "h-[18px]")} />
            )}
            <Icon className={cn("shrink-0", child ? "size-4" : "size-[18px]")} strokeWidth={1.75} />
            <span className={cn("font-medium lowercase whitespace-nowrap", child ? "text-[14px]" : "text-[14.5px]")}>
                {item.label}
            </span>
            <NavBadge value={badge} className="ml-auto min-w-5 h-[18px] px-1.5 text-[11.5px]" />
        </Link>
    )
}

function railIconClass(active: boolean) {
    return cn(
        "relative size-10 shrink-0 flex items-center justify-center rounded-lg transition-colors",
        active ? "bg-primary/10 text-primary" : "text-muted-foreground hover:text-foreground hover:bg-muted/50",
    )
}

function GoKeys({ href }: { href: string }) {
    const key = goKeyFor(href)
    if (!key) return null
    return (
        <span className="ml-2 inline-flex gap-0.5 font-mono text-[10.5px] normal-case text-muted-foreground">
            <kbd className="rounded border border-border px-1">G</kbd>
            <kbd className="rounded border border-border px-1 uppercase">{key}</kbd>
        </span>
    )
}

export function RailLink({ item, pathname, badge }: { item: NavItem; pathname: string; badge?: NavBadgeValue }) {
    const active = isItemActive(item, pathname)
    const Icon = item.icon
    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <Link to={item.href} aria-label={item.label} aria-current={active ? "page" : undefined} className={railIconClass(active)}>
                    <Icon className="size-5" strokeWidth={1.75} />
                    <NavBadge value={badge} className="absolute -top-0.5 -right-0.5 min-w-4 h-4 px-1 text-[10px] ring-2 ring-card" />
                </Link>
            </TooltipTrigger>
            <TooltipContent side="right" className="flex items-center lowercase">
                {item.label}
                <GoKeys href={item.href} />
            </TooltipContent>
        </Tooltip>
    )
}

function RailFlyout({ item, pathname }: { item: NavItem; pathname: string }) {
    const [open, setOpen] = useState(false)
    const closeTimer = useRef<number | undefined>(undefined)
    const contentRef = useRef<HTMLDivElement>(null)
    const Icon = item.icon

    const show = () => {
        window.clearTimeout(closeTimer.current)
        setOpen(true)
    }
    const hide = () => {
        window.clearTimeout(closeTimer.current)
        closeTimer.current = window.setTimeout(() => setOpen(false), 150)
    }
    useEffect(() => () => window.clearTimeout(closeTimer.current), [])
    useEffect(() => setOpen(false), [pathname])

    const rowClass = (active: boolean) => cn(
        "flex h-[30px] items-center gap-2.5 rounded-md px-2.5 text-[14px] font-medium lowercase transition-colors",
        active ? "bg-primary/10 text-primary" : "text-foreground/90 hover:bg-muted",
    )

    return (
        <Popover open={open} onOpenChange={setOpen}>
            <PopoverAnchor asChild>
                <Link
                    to={item.href}
                    aria-label={item.label}
                    aria-current={isItemActive(item, pathname) ? "page" : undefined}
                    aria-expanded={open}
                    className={railIconClass(isGroupActive(item, pathname))}
                    onPointerEnter={show}
                    onPointerLeave={hide}
                    onFocus={show}
                    onBlur={hide}
                    onKeyDown={(e) => {
                        if (e.key !== "ArrowRight") return
                        e.preventDefault()
                        setOpen(true)
                        requestAnimationFrame(() => contentRef.current?.querySelector<HTMLElement>("a")?.focus())
                    }}
                >
                    <Icon className="size-5" strokeWidth={1.75} />
                </Link>
            </PopoverAnchor>
            <PopoverContent
                ref={contentRef}
                side="right"
                align="start"
                sideOffset={10}
                className="w-52 p-1.5"
                onOpenAutoFocus={(e) => e.preventDefault()}
                onCloseAutoFocus={(e) => e.preventDefault()}
                onPointerEnter={show}
                onPointerLeave={hide}
                onFocus={show}
                onBlur={hide}
            >
                <Link to={item.href} className={cn(rowClass(isItemActive(item, pathname) && !isChildActive(item, pathname)), "justify-between font-semibold")}>
                    {item.label}
                    <GoKeys href={item.href} />
                </Link>
                <div className="my-1 h-px bg-border" />
                {item.children!.map((child) => {
                    const ChildIcon = child.icon
                    const childActive = isItemActive(child, pathname)
                    return (
                        <Link key={child.href} to={child.href} aria-current={childActive ? "page" : undefined} className={rowClass(childActive)}>
                            <ChildIcon className="size-4 shrink-0 text-muted-foreground" strokeWidth={1.75} />
                            {child.label}
                        </Link>
                    )
                })}
            </PopoverContent>
        </Popover>
    )
}
