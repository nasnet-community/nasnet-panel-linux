import { useEffect, useState } from "react"
import { Download, RefreshCw, SlidersHorizontal } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { QueryError } from "@/components/ui/query-error"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { CreateManualSubscriptionDialog } from "@/components/subscription/create-manual-subscription-dialog"
import { plural } from "@/components/user/format"
import { EmptyList } from "@/components/users-list/empty-list"
import { downloadUsersCsv } from "@/components/users-list/export"
import { FiltersPanel } from "@/components/users-list/filters-panel"
import { GroupChips, GroupTabs } from "@/components/users-list/group-tabs"
import { SelectionBar } from "@/components/users-list/selection-bar"
import { BAN_COPY, UNBAN_COPY, useUserRowActions } from "@/components/users-list/use-row-actions"
import { filterCount, toListParams, useUsersView } from "@/components/users-list/use-users-view"
import { UsersCards } from "@/components/users-list/users-cards"
import { PhonePager, UsersFooter } from "@/components/users-list/users-footer"
import { UsersTable } from "@/components/users-list/users-table"
import { PhoneSort, SearchBox, UsersToolbar } from "@/components/users-list/users-toolbar"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { useBulkBan, useBulkUnban, useUsers } from "@/lib/queries"
import { cn } from "@/lib/utils"
import { useUsersStore } from "@/store/users-store"
import type { UserListItem } from "@/lib/types"

export default function UsersPage() {
    const isMobile = useIsMobile()
    const { view, perPage, scopeKey, setGroup, setSearch, setFilters, clearFilters, setSort, setSortTo, setPage, setPerPage } = useUsersView()
    const pageSize = isMobile ? 25 : perPage
    const { data, isPending, isFetching, isError, error, refetch } = useUsers(toListParams(view, pageSize))
    const users = data?.users ?? []
    const total = data?.total ?? 0
    const pages = Math.max(1, Math.ceil(total / pageSize))

    // A filter change can leave the page past the end: step back to the last page.
    useEffect(() => {
        if (data && view.page > pages) setPage(pages, { replace: true })
    }, [data, view.page, pages, setPage])

    const { selected, toggle, setMany, clear } = useUsersStore()
    useEffect(() => {
        clear()
    }, [scopeKey, clear])
    const onToggle = (u: UserListItem) => toggle(u.id, u.is_banned)

    const row = useUserRowActions()
    const confirm = useConfirm()
    const bulkBan = useBulkBan()
    const bulkUnban = useBulkUnban()
    const banIds = [...selected].filter(([, banned]) => !banned).map(([id]) => id)
    const unbanIds = [...selected].filter(([, banned]) => banned).map(([id]) => id)
    const onBulkBan = async () => {
        if (banIds.length === 0) return
        const ok = await confirm({ title: `Ban ${plural(banIds.length, "user")}?`, description: BAN_COPY, confirmLabel: "Ban", variant: "destructive" })
        if (ok) bulkBan.mutate(banIds, { onSuccess: clear })
    }
    const onBulkUnban = async () => {
        if (unbanIds.length === 0) return
        const ok = await confirm({ title: `Unban ${plural(unbanIds.length, "user")}?`, description: UNBAN_COPY, confirmLabel: "Unban" })
        if (ok) bulkUnban.mutate(unbanIds, { onSuccess: clear })
    }

    const [sheetOpen, setSheetOpen] = useState(false)
    const empty = <EmptyList view={view} onClearSearch={() => setSearch("")} onClearFilters={clearFilters} />
    const errorCard = (
        <QueryError
            title="Couldn't load users"
            description={error instanceof Error ? error.message : "The request failed."}
            onRetry={() => refetch()}
            isRetrying={isFetching}
        />
    )
    const overlays = (
        <>
            <SelectionBar
                count={selected.size}
                canBan={banIds.length > 0}
                canUnban={unbanIds.length > 0}
                busy={bulkBan.isPending || bulkUnban.isPending}
                onBan={onBulkBan}
                onUnban={onBulkUnban}
                onExport={() => downloadUsersCsv(view, [...selected.keys()])}
                onClear={clear}
            />
            {row.dialogs}
            <CreateManualSubscriptionDialog />
        </>
    )

    if (isMobile) {
        return (
            <div className="flex flex-col gap-3 pb-[calc(env(safe-area-inset-bottom)+5.5rem)]">
                <div className="flex h-10 items-center justify-between">
                    <h1 className="text-[22px] font-bold tracking-tight">Users</h1>
                    <Button variant="outline" size="icon" className="relative size-10" onClick={() => setSheetOpen(true)} aria-label="Filters and sort">
                        <SlidersHorizontal className="size-[18px]" />
                        {filterCount(view) > 0 && <span className="absolute right-1.5 top-1.5 size-2 rounded-full bg-foreground" />}
                    </Button>
                </div>
                <SearchBox value={view.q} onChange={setSearch} placeholder="Search name, @username, ID or link" className="h-11 rounded-[10px]" />
                <GroupChips value={view.group} counts={data?.groups} onChange={setGroup} />
                {isError ? errorCard : <UsersCards users={users} loading={isPending} selected={selected} onToggle={onToggle} empty={empty} />}
                {!isError && <PhonePager page={view.page} perPage={pageSize} total={total} onPage={setPage} />}
                <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
                    <SheetContent side="bottom" className="max-h-[85vh] overflow-y-auto rounded-t-2xl">
                        <SheetHeader><SheetTitle>Filters</SheetTitle></SheetHeader>
                        <div className="flex flex-col gap-5 px-4 pb-6">
                            <PhoneSort view={view} onSortTo={setSortTo} />
                            <FiltersPanel value={view} onChange={setFilters} onClear={clearFilters} footer={<Button size="sm" onClick={() => setSheetOpen(false)}>Done</Button>} />
                        </div>
                    </SheetContent>
                </Sheet>
                {overlays}
            </div>
        )
    }

    return (
        <div className="flex flex-col gap-4 animate-in fade-in duration-300">
            <div className="flex items-center justify-between">
                <h1 className="text-2xl font-bold tracking-tight">Users</h1>
                <div className="flex gap-2">
                    <Button variant="outline" size="sm" className="h-9 gap-2" onClick={() => downloadUsersCsv(view)}>
                        <Download className="size-4" />Export CSV
                    </Button>
                    <Button variant="outline" size="icon" className="size-9" onClick={() => refetch()} disabled={isFetching} aria-label="Refresh list">
                        <RefreshCw className={cn("size-4", isFetching && "animate-spin")} />
                    </Button>
                </div>
            </div>
            <GroupTabs value={view.group} counts={data?.groups} onChange={setGroup} />
            <UsersToolbar view={view} onSearch={setSearch} onFilters={setFilters} onClearFilters={clearFilters} />
            {isError ? errorCard : (
                <UsersTable
                    users={users}
                    loading={isPending}
                    skeletonRows={pageSize}
                    group={view.group}
                    sort={view.sort}
                    order={view.order}
                    onSort={setSort}
                    selected={selected}
                    onToggle={onToggle}
                    onTogglePage={(list, select) => setMany(list.map((u) => [u.id, u.is_banned]), select)}
                    actions={row.actions}
                    empty={empty}
                />
            )}
            {!isError && total > 0 && <UsersFooter page={view.page} perPage={pageSize} total={total} onPage={setPage} onPerPage={setPerPage} />}
            {overlays}
        </div>
    )
}
