import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react"
import { useLocation, useNavigate, useParams } from "react-router"
import { ArrowLeft, ChevronRight, Search, TriangleAlert, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { useUnsavedChanges } from "@/hooks/use-unsaved-changes"
import { useSettings, useSettingsStatus } from "@/lib/queries/use-settings"
import type { SettingsGrouped } from "@/lib/domain/setting"
import { SettingsPage as OnePage, SettingsSection, type SettingsSectionMeta } from "@/components/settings-page/settings-page"
import { SECTIONS, CARDS, cardById, cardsIn, matchCard, sectionNote, type Ctx, type SectionId, type Values } from "@/components/settings/catalog"
import { SettingsPageProvider, type Draft } from "@/components/settings/settings-card"
import { CardView } from "@/components/settings/card-view"
import { DangerZone } from "@/components/settings/danger-zone"
import { RouterDangerZone } from "@/components/settings/router-danger-zone"
import { SettingsHeader } from "@/components/settings/settings-header"
import { cn } from "@/lib/utils"

const DANGER = { id: "danger", title: "Danger zone", icon: TriangleAlert, description: "Erase customer data, wipe or nuke this router" } as const

function DangerCards() {
    return (
        <>
            <DangerZone />
            <RouterDangerZone />
        </>
    )
}
type PageSection = SectionId | "danger"

function flatten(grouped?: SettingsGrouped): Values {
    const out: Values = {}
    for (const list of Object.values(grouped ?? {})) for (const s of list) out[s.key] = s.value
    return out
}

/** Scrolls a card into view and rings it for a moment. */
function flashCard(id: string) {
    const el = document.getElementById(`group-${id}`)
    if (!el) return false
    window.scrollTo({ top: el.getBoundingClientRect().top + window.scrollY - 24, behavior: "smooth" })
    el.animate(
        [{ boxShadow: "0 0 0 2px var(--ring)" }, { boxShadow: "0 0 0 2px var(--ring)", offset: 0.4 }, { boxShadow: "0 0 0 2px transparent" }],
        { duration: 1800, easing: "ease-out" },
    )
    return true
}

export default function SettingsRoute() {
    const { section: param } = useParams<{ section?: string }>()
    const location = useLocation()
    const navigate = useNavigate()
    const phone = useIsMobile()
    const { data: grouped, isLoading, refetch } = useSettings()
    const { data: status } = useSettingsStatus()
    const values = useMemo(() => flatten(grouped), [grouped])
    const ctx = useMemo<Ctx>(() => ({ values, status }), [values, status])
    const loaded = !!grouped
    const [query, setQuery] = useState("")
    const [dirty, setDirty] = useState<ReadonlySet<string>>(() => new Set())
    const [drafts, setDrafts] = useState<Record<string, Draft>>({})
    const setDraft = useCallback((id: string, fn: (d: Draft | undefined) => Draft | undefined) => {
        setDrafts(prev => {
            const next = fn(prev[id])
            if (next === prev[id]) return prev
            const out = { ...prev }
            if (next) out[id] = next
            else delete out[id]
            return out
        })
    }, [])
    const unsaved = useUnsavedChanges(dirty.size > 0)
    const section: PageSection | undefined = param === "danger" ? "danger" : SECTIONS.find(s => s.id === param)?.id

    const report = useCallback((id: string, isDirty: boolean) => {
        setDirty(prev => {
            if (prev.has(id) === isDirty) return prev
            const next = new Set(prev)
            if (isDirty) next.add(id)
            else next.delete(id)
            return next
        })
    }, [])

    const goto = useCallback((cardId: string) => {
        const card = cardById(cardId)
        setQuery("")
        if (phone && card && card.section !== section) {
            navigate(`/settings/${card.section}#${cardId}`)
            return
        }
        setTimeout(() => flashCard(cardId), 50)
    }, [phone, section, navigate])

    // /settings#<card> from ⌘K and other pages
    useEffect(() => {
        let id = ""
        try { id = decodeURIComponent(location.hash.slice(1)) } catch { /* a mangled link */ }
        const card = cardById(id)
        if (!loaded || !card) return
        if (phone && card.section !== section) {
            navigate(`/settings/${card.section}#${id}`, { replace: true })
            return
        }
        const t = setTimeout(() => flashCard(id), 80)
        return () => clearTimeout(t)
    }, [location.hash, loaded, phone, section, navigate])

    // /settings/<section> on a wide screen is the same page, scrolled there
    useEffect(() => {
        if (phone || !loaded || !section || location.hash) return
        document.getElementById(`settings-section-${section}`)?.scrollIntoView({ block: "start" })
    }, [phone, loaded, section, location.hash])

    const matches = useMemo(() => new Map(CARDS.map(c => [c.id, matchCard(c, query, values)])), [query, values])
    const searching = query.trim() !== ""
    const hits = useMemo(() => {
        let cards = 0, rows = 0
        for (const m of matches.values()) if (m) { cards++; rows += m === "all" ? 1 : m.size }
        return { cards, rows }
    }, [matches])

    if (isLoading) return <SettingsSkeleton />
    // a failed background refetch keeps the cached settings; only a page with none shows the error
    if (!grouped) {
        return (
            <div className="space-y-4">
                <h1 className="text-2xl font-bold tracking-tight sm:text-3xl">Settings</h1>
                <p className="text-sm text-muted-foreground">Settings didn't load.</p>
                <Button variant="outline" onClick={() => refetch()}>Try again</Button>
            </div>
        )
    }

    const cardsOf = (id: SectionId) => cardsIn(id).filter(c => matches.get(c.id))
        .map(c => <CardView key={c.id} card={c} match={matches.get(c.id)!} />)
    const searchBox = <SearchBox query={query} onChange={setQuery} hits={searching ? hits : null} />

    const found = SECTIONS.filter(s => cardsIn(s.id).some(c => matches.get(c.id)))
    let body: ReactNode
    if (searching && !found.length) {
        body = <p className="rounded-xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">Nothing matches “{query.trim()}”.</p>
    } else if (searching && phone) {
        body = <div className="@container space-y-7">{found.map(s => (
            <SettingsSection key={s.id} id={s.id} title={s.title} icon={s.icon}>{cardsOf(s.id)}</SettingsSection>
        ))}</div>
    } else if (phone && !section) {
        body = <SectionList ctx={ctx} dirty={dirty} onOpen={id => navigate(`/settings/${id}`)} />
    } else if (phone && section) {
        const meta = section === "danger" ? DANGER : SECTIONS.find(s => s.id === section)!
        body = (
            <div className="space-y-4">
                <Button variant="ghost" size="sm" className="-ml-2 h-8 gap-1.5 px-2 text-muted-foreground" onClick={() => navigate("/settings")}>
                    <ArrowLeft className="size-4" />Settings
                </Button>
                <div className="flex items-center gap-3">
                    <span className={cn("flex size-9 shrink-0 items-center justify-center rounded-lg", section === "danger" ? "bg-red-500/10 text-red-500" : "bg-muted text-muted-foreground")}>
                        <meta.icon className="size-[18px]" aria-hidden />
                    </span>
                    <div className="min-w-0">
                        <h2 className="text-lg font-semibold leading-6">{meta.title}</h2>
                        <p className="text-sm text-muted-foreground">{meta.description}</p>
                    </div>
                </div>
                <div className="@container space-y-2.5">{section === "danger" ? <DangerCards /> : cardsOf(section)}</div>
            </div>
        )
    } else {
        // a search keeps this layout and narrows it to the cards that match
        const metas: SettingsSectionMeta[] = [
            ...found.map(s => ({
                id: s.id, title: s.title, icon: s.icon,
                note: sectionNote(s.id, ctx),
                dirty: cardsIn(s.id).some(c => dirty.has(c.id)),
            })),
            ...(searching ? [] : [{ id: DANGER.id, title: DANGER.title, icon: DANGER.icon, danger: true }]),
        ]
        body = (
            <OnePage sections={metas}>
                {found.map(s => (
                    <SettingsSection key={s.id} id={s.id} title={s.title} icon={s.icon}>{cardsOf(s.id)}</SettingsSection>
                ))}
                {!searching && (
                    <SettingsSection id={DANGER.id} title={DANGER.title} icon={DANGER.icon} hideTitle>
                        <div className="space-y-2.5"><DangerCards /></div>
                    </SettingsSection>
                )}
            </OnePage>
        )
    }

    return (
        <SettingsPageProvider value={{ ctx, query, goto, report, drafts, setDraft }}>
            <div className="space-y-5 animate-in fade-in duration-300">
                <SettingsHeader ctx={ctx} phone={phone} />
                {searchBox}
                {body}
            </div>
            <Dialog open={unsaved.showDialog} onOpenChange={o => { if (!o) unsaved.cancelNavigation() }}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Leave with unsaved changes?</DialogTitle>
                        <DialogDescription>Edits you haven't saved are lost.</DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={unsaved.cancelNavigation}>Keep editing</Button>
                        <Button variant="destructive" onClick={unsaved.confirmNavigation}>Discard and leave</Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </SettingsPageProvider>
    )
}

function SearchBox({ query, onChange, hits }: { query: string; onChange: (q: string) => void; hits: { cards: number; rows: number } | null }) {
    return (
        <div className="space-y-2">
            <div className="relative max-w-md">
                <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden />
                <Input
                    value={query} onChange={e => onChange(e.target.value)} placeholder="Search settings"
                    aria-label="Search settings" className="h-10 pl-9 pr-9" spellCheck={false}
                    onKeyDown={e => { if (e.key === "Escape") onChange("") }}
                />
                {query && (
                    <Button variant="ghost" size="icon" className="absolute right-1 top-1/2 size-8 -translate-y-1/2" aria-label="Clear search" onClick={() => onChange("")}>
                        <X className="size-3.5" />
                    </Button>
                )}
            </div>
            {hits && hits.cards > 0 && (
                <p className="text-xs text-muted-foreground">
                    {hits.rows === 1 ? "1 match" : `${hits.rows} matches`} in {hits.cards === 1 ? "1 card" : `${hits.cards} cards`}
                </p>
            )}
        </div>
    )
}

/** Phones: the sections as a list; each opens as its own page. */
function SectionList({ ctx, dirty, onOpen }: { ctx: Ctx; dirty: ReadonlySet<string>; onOpen: (id: PageSection) => void }) {
    const rows = [...SECTIONS.map(s => ({ ...s, note: sectionNote(s.id, ctx) })), { ...DANGER, note: undefined }]
    return (
        <nav aria-label="Settings sections" className="overflow-hidden rounded-xl border border-border/60 bg-card">
            {rows.map((s, i) => {
                const danger = s.id === "danger"
                const unsaved = !danger && cardsIn(s.id as SectionId).some(c => dirty.has(c.id))
                return (
                    <button
                        key={s.id} type="button" onClick={() => onOpen(s.id)}
                        className={cn("flex w-full items-center gap-3 px-3.5 py-3 text-left transition-colors active:bg-muted/60", i > 0 && "border-t border-border/40")}
                    >
                        <span className={cn("flex size-9 shrink-0 items-center justify-center rounded-lg",
                            danger ? "bg-red-500/10 text-red-500" : s.note ? "bg-amber-500/10 text-amber-500" : "bg-muted text-muted-foreground")}>
                            <s.icon className="size-[18px]" aria-hidden />
                        </span>
                        <span className="min-w-0 flex-1">
                            <span className={cn("block text-[15px] font-medium", danger && "text-red-500")}>{s.title}</span>
                            <span className={cn("block text-[13px] leading-[17px]", s.note ? "text-amber-600 dark:text-amber-500" : "text-muted-foreground")}>{s.note ?? s.description}</span>
                        </span>
                        {unsaved && <span className="flex items-center gap-1.5 text-xs text-amber-500"><span className="size-1.5 rounded-full bg-amber-500" />Unsaved</span>}
                        <ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                    </button>
                )
            })}
        </nav>
    )
}

function SettingsSkeleton() {
    return (
        <div className="space-y-5">
            <div className="space-y-2">
                <Skeleton className="h-8 w-40" />
                <Skeleton className="h-4 w-64" />
            </div>
            <Skeleton className="h-10 max-w-md" />
            <div className="max-w-[720px] space-y-2.5">
                {[0, 1, 2].map(i => (
                    <div key={i} className="overflow-hidden rounded-xl border border-border/60">
                        <Skeleton className="h-12 rounded-none" />
                        <div className="space-y-3 p-4">
                            <Skeleton className="h-4 w-3/4" />
                            <Skeleton className="h-4 w-1/2" />
                        </div>
                    </div>
                ))}
            </div>
        </div>
    )
}
