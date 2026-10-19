import { useCallback, useEffect, useRef, useState, type ReactNode } from "react"
import { Check, ChevronDown, type LucideIcon } from "lucide-react"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { cn } from "@/lib/utils"

export interface SettingsSectionMeta {
    id: string
    title: string
    icon: LucideIcon
    /** a group in it holds unsaved edits */
    dirty?: boolean
    danger?: boolean
    /** something in it needs attention, said in a few words */
    note?: string
}

const sectionId = (id: string) => `settings-section-${id}`

/** The section whose heading last passed the top of the screen. */
function useActiveSection(rootRef: React.RefObject<HTMLDivElement | null>, ids: string[]) {
    const [active, setActive] = useState(ids[0] ?? "")
    // a section picked from the list stays picked while the page glides to it
    const pinned = useRef(0)
    const key = ids.join(",")
    useEffect(() => {
        let frame = 0
        let atEnd = false
        const measure = () => {
            frame = 0
            if (Date.now() < pinned.current) return
            let current = ids[0] ?? ""
            for (const id of ids) {
                const el = document.getElementById(sectionId(id))
                if (el && el.getBoundingClientRect().top <= 140) current = id
            }
            // the last section may be too short to reach the top
            if (atEnd && ids.length) current = ids[ids.length - 1]
            setActive(current)
        }
        // whichever box scrolls the page (the layout's <main>, or the window on a phone)
        const onScroll = (e: Event) => {
            const box = e.target instanceof HTMLElement ? e.target : document.scrollingElement as HTMLElement | null
            if (!box || !rootRef.current || !box.contains(rootRef.current)) return
            atEnd = box.scrollHeight > box.clientHeight + 4 && box.scrollTop + box.clientHeight >= box.scrollHeight - 4
            if (!frame) frame = requestAnimationFrame(measure)
        }
        measure()
        document.addEventListener("scroll", onScroll, { capture: true, passive: true })
        return () => {
            document.removeEventListener("scroll", onScroll, { capture: true })
            if (frame) cancelAnimationFrame(frame)
        }
        // ids are compared by value
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [rootRef, key])
    const pick = useCallback((id: string) => {
        pinned.current = Date.now() + 900
        setActive(id)
    }, [])
    return [active, pick] as const
}

/**
 * Keeps el at the top of the screen while its parent passes by. CSS sticky
 * can't: the layout's <main> has overflow set but doesn't scroll itself.
 */
function useStayInView(ref: React.RefObject<HTMLElement | null>, gap: number) {
    useEffect(() => {
        let frame = 0
        const place = () => {
            frame = 0
            const el = ref.current
            const parent = el?.parentElement
            if (!el || !parent || !el.offsetParent) return
            const box = parent.getBoundingClientRect()
            const shift = Math.max(0, Math.min(gap - box.top, box.height - el.offsetHeight))
            el.style.transform = shift ? `translateY(${shift}px)` : ""
        }
        const schedule = () => { if (!frame) frame = requestAnimationFrame(place) }
        place()
        document.addEventListener("scroll", schedule, { capture: true, passive: true })
        window.addEventListener("resize", schedule)
        return () => {
            document.removeEventListener("scroll", schedule, { capture: true })
            window.removeEventListener("resize", schedule)
            if (frame) cancelAnimationFrame(frame)
        }
    }, [ref, gap])
}

/**
 * One scrolling settings page: the section list sits on the right when there's
 * room, becomes chips when there isn't, and a dropdown on a phone.
 */
export function SettingsPage({ sections, children, footer }: { sections: SettingsSectionMeta[]; children: ReactNode; footer?: ReactNode }) {
    const rootRef = useRef<HTMLDivElement>(null)
    const pickerRef = useRef<HTMLDivElement>(null)
    const chipsRef = useRef<HTMLElement>(null)
    const listRef = useRef<HTMLDivElement>(null)
    const [active, pick] = useActiveSection(rootRef, sections.map(s => s.id))
    useStayInView(pickerRef, 8)
    useStayInView(chipsRef, 0)
    useStayInView(listRef, 16)
    const jump = useCallback((id: string) => {
        pick(id)
        document.getElementById(sectionId(id))?.scrollIntoView({ behavior: "smooth", block: "start" })
    }, [pick])
    const current = sections.find(s => s.id === active) ?? sections[0]
    const anyDirty = sections.some(s => s.dirty)

    return (
        <div ref={rootRef} className="@container min-w-0">
            {current && (
                <div ref={pickerRef} className="relative z-20 -mx-1 bg-background px-1 pb-3 pt-1 @min-[560px]:hidden">
                    <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                            <button
                                type="button"
                                aria-label={`Jump to a section. Now on ${current.title}`}
                                className="group flex h-11 w-full items-center gap-2.5 rounded-lg border border-border bg-card px-3 text-left text-sm font-medium transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                            >
                                <current.icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                                <span className="min-w-0 flex-1 truncate">{current.title}</span>
                                {anyDirty && (
                                    <span className="flex items-center gap-1.5 text-xs font-normal text-amber-500">
                                        <span className="size-1.5 rounded-full bg-amber-500" />Unsaved
                                    </span>
                                )}
                                <ChevronDown className="size-4 shrink-0 text-muted-foreground transition-transform group-data-[state=open]:rotate-180" aria-hidden />
                            </button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="start" className="w-(--radix-dropdown-menu-trigger-width) p-1">
                            {sections.map(s => (
                                <DropdownMenuItem
                                    key={s.id}
                                    onSelect={() => jump(s.id)}
                                    className={cn("h-10 gap-2.5", s.id === current.id && "bg-muted font-medium", s.danger && "text-red-500 focus:text-red-500")}
                                >
                                    <s.icon className={cn("size-4", s.danger ? "text-red-500" : "text-muted-foreground")} aria-hidden />
                                    <span className="flex min-w-0 flex-1 flex-col">
                                        {s.title}
                                        {s.note && <span className="truncate text-xs text-amber-500">{s.note}</span>}
                                    </span>
                                    {s.dirty && <span className="size-1.5 rounded-full bg-amber-500" aria-label="Unsaved changes" />}
                                    <Check className={cn("size-4", s.id === current.id ? "opacity-100" : "opacity-0")} aria-hidden />
                                </DropdownMenuItem>
                            ))}
                        </DropdownMenuContent>
                    </DropdownMenu>
                </div>
            )}
            <nav ref={chipsRef} aria-label="Settings sections" className="relative z-20 mb-4 hidden gap-1.5 overflow-x-auto border-b border-border/60 bg-background py-2.5 no-scrollbar @min-[560px]:flex @min-[820px]:hidden">
                {sections.map(s => (
                    <button
                        key={s.id}
                        type="button"
                        onClick={() => jump(s.id)}
                        aria-current={s.id === active ? "true" : undefined}
                        className={cn(
                            "flex shrink-0 items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                            s.id === active ? "border-border bg-muted text-foreground" : "border-border/60 text-muted-foreground hover:text-foreground",
                        )}
                    >
                        <s.icon className="size-3.5" aria-hidden />{s.title}
                        {(s.dirty || s.note) && <span className="size-1.5 rounded-full bg-amber-500" aria-label={s.note ?? "Unsaved changes"} title={s.note} />}
                    </button>
                ))}
            </nav>
            <div className="grid min-w-0 gap-8 @min-[820px]:grid-cols-[minmax(0,720px)_180px]">
                <div className="min-w-0 space-y-7">{children}{footer}</div>
                <nav aria-label="On this page" className="hidden self-stretch @min-[820px]:block">
                    <div ref={listRef} className="space-y-0.5">
                        <p className="mb-2 pl-3 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">On this page</p>
                        {sections.map(s => (
                            <button
                                key={s.id}
                                type="button"
                                onClick={() => jump(s.id)}
                                aria-current={s.id === active ? "true" : undefined}
                                className={cn(
                                    "flex w-full items-center gap-2 border-l-2 py-1.5 pl-3 pr-2 text-left text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                                    s.id === active ? "border-foreground font-medium text-foreground" : "border-border text-muted-foreground hover:text-foreground",
                                )}
                            >
                                <s.icon className="size-3.5 shrink-0 self-start mt-[3px]" aria-hidden />
                                <span className="flex min-w-0 flex-1 flex-col">
                                    {s.title}
                                    {s.note && <span className="text-xs font-normal text-amber-500">{s.note}</span>}
                                </span>
                                {s.dirty && <span className="size-1.5 self-start mt-[7px] rounded-full bg-amber-500" aria-label="Unsaved changes" />}
                            </button>
                        ))}
                    </div>
                </nav>
            </div>
        </div>
    )
}

/** A section of the page: a small heading, then its groups. */
export function SettingsSection({ id, title, icon: Icon, children, hideTitle }: { id: string; title: string; icon: LucideIcon; children: ReactNode; hideTitle?: boolean }) {
    return (
        <section id={sectionId(id)} aria-label={title} className="min-w-0 scroll-mt-16 space-y-2.5">
            {!hideTitle && (
                <h2 className="flex items-center gap-2 px-0.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                    <Icon className="size-3.5" aria-hidden />{title}
                </h2>
            )}
            {children}
        </section>
    )
}
