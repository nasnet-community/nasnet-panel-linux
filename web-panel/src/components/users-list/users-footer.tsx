import { ChevronLeft, ChevronRight } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { PER_PAGE_OPTIONS } from "./use-users-view"

/** Page numbers with gaps: 1 2 3 … 8. */
export function pageList(page: number, pages: number): (number | "gap")[] {
    if (pages <= 7) return Array.from({ length: pages }, (_, i) => i + 1)
    const nums = [...new Set([1, 2, page - 1, page, page + 1, pages])].filter((n) => n >= 1 && n <= pages).sort((a, b) => a - b)
    const out: (number | "gap")[] = []
    nums.forEach((n, i) => {
        if (i > 0 && n - nums[i - 1] > 1) out.push("gap")
        out.push(n)
    })
    return out
}

export function UsersFooter({ page, perPage, total, onPage, onPerPage }: {
    page: number
    perPage: number
    total: number
    onPage: (page: number) => void
    onPerPage: (n: number) => void
}) {
    const pages = Math.max(1, Math.ceil(total / perPage))
    const from = total === 0 ? 0 : (page - 1) * perPage + 1
    const to = Math.min(total, page * perPage)
    return (
        <div className="flex flex-wrap items-center justify-between gap-3 pb-8">
            <p className="text-[13px] text-muted-foreground">
                <span className="font-mono text-foreground/90">{from}–{to}</span> of <span className="font-mono text-foreground/90">{total}</span>
            </p>
            <div className="flex items-center gap-5">
                <div className="flex items-center gap-2 text-[13px] text-muted-foreground">
                    Rows per page
                    <Select value={String(perPage)} onValueChange={(v) => onPerPage(Number(v))}>
                        <SelectTrigger className="h-8 w-[72px] text-[13px]" aria-label="Rows per page"><SelectValue /></SelectTrigger>
                        <SelectContent>
                            {PER_PAGE_OPTIONS.map((n) => <SelectItem key={n} value={String(n)}>{n}</SelectItem>)}
                        </SelectContent>
                    </Select>
                </div>
                <nav aria-label="Pages" className="flex items-center gap-1 font-mono text-[13px]">
                    <Button variant="outline" size="icon" className="size-8" disabled={page <= 1} onClick={() => onPage(page - 1)} aria-label="Previous page">
                        <ChevronLeft className="size-4" />
                    </Button>
                    {pageList(page, pages).map((n, i) =>
                        n === "gap" ? (
                            <span key={`gap-${i}`} className="w-6 text-center text-muted-foreground">…</span>
                        ) : (
                            <Button
                                key={n}
                                variant={n === page ? "secondary" : "ghost"}
                                size="icon"
                                className="size-8 font-mono"
                                aria-current={n === page ? "page" : undefined}
                                onClick={() => onPage(n)}
                            >
                                {n}
                            </Button>
                        ),
                    )}
                    <Button variant="outline" size="icon" className="size-8" disabled={page >= pages} onClick={() => onPage(page + 1)} aria-label="Next page">
                        <ChevronRight className="size-4" />
                    </Button>
                </nav>
            </div>
        </div>
    )
}

export function PhonePager({ page, perPage, total, onPage }: { page: number; perPage: number; total: number; onPage: (page: number) => void }) {
    const pages = Math.max(1, Math.ceil(total / perPage))
    if (pages <= 1) return null
    return (
        <nav aria-label="Pages" className="flex items-center justify-center gap-4 py-2">
            <Button variant="outline" size="icon" className="size-10" disabled={page <= 1} onClick={() => onPage(page - 1)} aria-label="Previous page"><ChevronLeft className="size-4" /></Button>
            <span className="font-mono text-[13px] text-text-secondary">{page} / {pages}</span>
            <Button variant="outline" size="icon" className="size-10" disabled={page >= pages} onClick={() => onPage(page + 1)} aria-label="Next page"><ChevronRight className="size-4" /></Button>
        </nav>
    )
}
