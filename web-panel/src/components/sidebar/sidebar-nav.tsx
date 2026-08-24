import { useState } from "react"
import { Link, useLocation } from "react-router"
import type { IconType } from "react-icons"
import { HiOutlineChevronDown } from "react-icons/hi"
import { cn } from "@/lib/utils"

export interface NavItem {
    label: string
    href: string
    icon: IconType | React.ComponentType<{ className?: string }>
    mobileQuickAccess?: boolean
    mobileLabel?: string
    collapsedLabel?: string
    badgeKey?: "chats"
    children?: NavItem[]
}

export interface NavSection {
    label?: string
    items: NavItem[]
}

export interface SidebarNavProps {
    sections: NavSection[]
    collapsed: boolean
    onNavClick?: () => void
    getBadge: (key: NonNullable<NavItem["badgeKey"]>) => number
}

function computeInitialExpanded(sections: NavSection[]): Set<string> {
    const parents = new Set<string>()
    for (const section of sections) {
        for (const item of section.items) {
            if (item.children && item.children.length > 0) {
                parents.add(item.href)
            }
        }
    }
    return parents
}

export function SidebarNav({ sections, collapsed, onNavClick, getBadge }: SidebarNavProps) {
    const { pathname } = useLocation()
    const [expandedParents, setExpandedParents] = useState<Set<string>>(() =>
        computeInitialExpanded(sections),
    )

    const toggleParent = (href: string) => {
        setExpandedParents((prev) => {
            const next = new Set(prev)
            if (next.has(href)) next.delete(href)
            else next.add(href)
            return next
        })
    }

    const isChildActive = (item: NavItem) =>
        item.children?.some((child) => isOn(pathname, child.href)) ?? false

    const isParentExpanded = (item: NavItem) =>
        expandedParents.has(item.href) || isOn(pathname, item.href) || isChildActive(item)

    return (
        <nav className={cn("flex-1 min-h-0 overflow-y-auto py-3", collapsed ? "px-1.5" : "px-3")}>
            {sections.map((section, idx) => {
                const items = collapsed
                    ? section.items.flatMap((item) =>
                          item.children ? [item, ...item.children] : [item],
                      )
                    : section.items
                return (
                    <div key={section.label ?? "top"} className={idx > 0 ? "mt-3" : ""}>
                        {section.label &&
                            (collapsed ? (
                                <div className="mx-2 my-2 border-t border-border/40" />
                            ) : (
                                <div className="px-3 mt-3 mb-1.5">
                                    <span className="font-mono text-[10.5px] font-semibold tracking-[0.12em] uppercase text-muted-foreground/70">
                                        // {section.label.toLowerCase()}
                                    </span>
                                </div>
                            ))}
                        <div className={collapsed ? "space-y-1" : "space-y-px"}>
                            {items.map((item) => (
                                <NavRow
                                    key={item.label}
                                    item={item}
                                    collapsed={collapsed}
                                    onClick={onNavClick}
                                    pathname={pathname}
                                    expanded={isParentExpanded(item)}
                                    onToggle={toggleParent}
                                    badge={item.badgeKey ? getBadge(item.badgeKey) : 0}
                                />
                            ))}
                        </div>
                    </div>
                )
            })}
        </nav>
    )
}

interface NavRowProps {
    item: NavItem
    collapsed: boolean
    onClick?: () => void
    pathname: string
    expanded: boolean
    onToggle: (href: string) => void
    badge: number
}

/** Most specific wins: a child's route belongs to the child, not to the parent
 *  that contains it, or two rows claim to be the current page at once. */
function isOn(pathname: string, href: string): boolean {
    return pathname === href || pathname.startsWith(`${href}/`)
}

function NavRow({ item, collapsed, onClick, pathname, expanded, onToggle, badge }: NavRowProps) {
    const childOwns = item.children?.some((c) => isOn(pathname, c.href)) ?? false
    const isActive = isOn(pathname, item.href) && !childOwns
    const hasChildren = !!item.children && item.children.length > 0 && !collapsed
    const showBadge = badge > 0

    if (collapsed) {
        return (
            <Link
                to={item.href}
                onClick={onClick}
                title={item.collapsedLabel ?? item.label}
                className={cn(
                    "relative flex items-center justify-center w-10 h-10 mx-auto rounded-lg transition-colors",
                    isActive
                        ? "bg-primary/10 text-primary"
                        : "text-muted-foreground hover:text-foreground hover:bg-muted/50",
                )}
            >
                <item.icon className="w-[18px] h-[18px]" />
                {showBadge && (
                    <span className="absolute -top-0.5 -right-0.5 bg-amber-500 text-white font-mono text-[9px] font-bold rounded-full min-w-[16px] h-4 flex items-center justify-center px-1 tabular-nums">
                        {badge}
                    </span>
                )}
            </Link>
        )
    }

    return (
        <>
            <div className="relative flex items-center">
                <Link
                    to={item.href}
                    onClick={onClick}
                    className={cn(
                        "flex flex-1 items-center gap-3 pl-4 pr-3 rounded-md transition-colors h-9 relative",
                        isActive
                            ? "bg-primary/10 text-primary font-medium"
                            : "text-muted-foreground hover:text-foreground hover:bg-muted/40",
                    )}
                >
                    {isActive && (
                        <span className="absolute left-0 top-1/2 -translate-y-1/2 w-[3px] h-5 rounded-r-full bg-primary" />
                    )}
                    <item.icon className="w-4 h-4 shrink-0" />
                    <span className="text-[13.5px] font-medium lowercase whitespace-nowrap">
                        {item.label}
                    </span>
                    {showBadge && (
                        <span className="ml-auto bg-amber-500 text-white font-mono text-[11px] font-bold rounded-full min-w-[20px] h-[16px] flex items-center justify-center px-1 tabular-nums">
                            {badge}
                        </span>
                    )}
                </Link>
                {hasChildren && (
                    <button
                        onClick={(e) => {
                            e.preventDefault()
                            onToggle(item.href)
                        }}
                        className="absolute right-1.5 top-1/2 -translate-y-1/2 w-5 h-5 flex items-center justify-center rounded text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors"
                        aria-label={expanded ? "Collapse" : "Expand"}
                    >
                        <HiOutlineChevronDown
                            className={cn(
                                "w-3 h-3 transition-transform duration-200",
                                expanded ? "rotate-0" : "-rotate-90",
                            )}
                        />
                    </button>
                )}
            </div>
            {hasChildren && (
                <div
                    className={cn(
                        "overflow-hidden transition-all duration-200",
                        expanded ? "max-h-40 opacity-100" : "max-h-0 opacity-0",
                    )}
                >
                    {item.children!.map((child) => {
                        const childActive = isOn(pathname, child.href)
                        return (
                            <Link
                                key={child.href}
                                to={child.href}
                                onClick={onClick}
                                className={cn(
                                    "relative flex items-center gap-2.5 h-8 rounded-md transition-colors",
                                    "pl-8 pr-3",
                                    childActive
                                        ? "bg-primary/10 text-primary font-medium"
                                        : "text-muted-foreground hover:text-foreground hover:bg-muted/40",
                                )}
                            >
                                {childActive && (
                                    <span className="absolute left-0 top-1/2 -translate-y-1/2 w-[3px] h-4 rounded-r-full bg-primary" />
                                )}
                                <span className="font-mono text-[12.5px] text-muted-foreground/60 select-none">└</span>
                                <span className="text-[12.5px] font-medium lowercase whitespace-nowrap">
                                    {child.label}
                                </span>
                            </Link>
                        )
                    })}
                </div>
            )}
        </>
    )
}
