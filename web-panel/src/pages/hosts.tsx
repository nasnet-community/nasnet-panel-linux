import { useState, useCallback, useEffect } from "react"
import { PageHeader } from "@/components/shared/page-header"
import { motion } from "framer-motion"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { PaginationNav } from "@/components/ui/pagination-nav"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuCheckboxItem,
    DropdownMenuTrigger,
    DropdownMenuSeparator,
    DropdownMenuLabel,
} from "@/components/ui/dropdown-menu"
import {
    HiOutlinePlus,
    HiOutlineSearch,
    HiOutlinePencil,
    HiOutlineTrash,
    HiOutlineDuplicate,
    HiOutlineLink,
    HiOutlineX,
    HiOutlineTemplate,
    HiOutlineTag,
} from "react-icons/hi"
import {
    useHostList,
    useDeleteHostMutation,
    useDuplicateHostMutation,
    useToggleHostMutation,
    useBulkUpdateHostsMutation,
    useApplyHostTemplateMutation,
    useHostTags,
    useHostTemplates,
    useNodes,
    useNodeInbounds,
} from "@/lib/queries"
import { useHostsStore } from "@/store/hosts-store"
import { HostSettingsDialog } from "@/components/host/host-settings-dialog"
import { BulkEditDialog } from "@/components/host/bulk-edit-dialog"
import { HostTemplateDialog } from "@/components/host/host-template-dialog"
import { HostFilterPanel } from "@/components/host/host-filter-panel"
import { HostGroupSection } from "@/components/host/host-group-section"
import { ServerHostCard } from "@/components/host/server-host-card"
import { InfoHostCard } from "@/components/host/info-host-card"
import { SwipeableHostCard } from "@/components/host/swipeable-host-card"
import { ConfirmDialogProvider, useConfirm } from "@/components/ui/confirm-dialog"
import { groupHostsByNode } from "@/lib/host-grouping"
import type { HostWithRelations } from "@/lib/types"

const PAGE_SIZE_OPTIONS = [10, 20, 50, 100]

function HostsPageContent() {
    const {
        statusFilter,
        hostType,
        page, setPage,
        search,
        nodeFilter,
        inboundFilter,
        tagFilter, setTagFilter,
        sortBy, sortOrder,
        perPage, setPerPage,
        collapsedGroups, toggleGroupCollapsed,
        selectedHosts, toggleSelectHost, selectAll, clearSelection,
        isMultiSelectMode, enterMultiSelectMode, exitMultiSelectMode,
        editDialog, openEditDialog, closeEditDialog,
    } = useHostsStore()

    const confirm = useConfirm()
    const [bulkEditOpen, setBulkEditOpen] = useState(false)
    const [templateDialogOpen, setTemplateDialogOpen] = useState(false)
    const [bulkTagInput, setBulkTagInput] = useState("")
    const [showBulkTagPopover, setShowBulkTagPopover] = useState(false)
    const [openSwipeId, setOpenSwipeId] = useState<number | null>(null)

    // Compute query params
    const queryParams = {
        page,
        perPage,
        search: search || undefined,
        nodeId: nodeFilter !== "all" ? parseInt(nodeFilter) : undefined,
        inboundId: inboundFilter !== "all" ? parseInt(inboundFilter) : undefined,
        disabled: statusFilter === "enabled" ? false : statusFilter === "disabled" ? true : undefined,
        hostType: hostType !== "all" ? hostType as "server" | "info" : undefined,
        tag: tagFilter !== "all" ? tagFilter : undefined,
        sortBy: sortBy !== "priority" ? sortBy : undefined,
        sortOrder: sortBy !== "priority" ? sortOrder : undefined,
    }

    const { data, isLoading } = useHostList(queryParams)
    const { data: nodes } = useNodes()
    const { data: inbounds } = useNodeInbounds(nodeFilter !== "all" ? parseInt(nodeFilter) : 0)
    const { data: hostTags = [] } = useHostTags()
    const { data: templates = [] } = useHostTemplates()

    const deleteMutation = useDeleteHostMutation()
    const duplicateMutation = useDuplicateHostMutation()
    const toggleMutation = useToggleHostMutation()
    const bulkUpdateMutation = useBulkUpdateHostsMutation()
    const applyTemplateMutation = useApplyHostTemplateMutation()

    const hosts = data?.hosts ?? []
    const total = data?.total ?? 0
    const totalPages = Math.ceil(total / perPage)

    // Auto-correct page when it exceeds total pages (e.g. after filter/delete changes)
    useEffect(() => {
        if (totalPages > 0 && page > totalPages) {
            setPage(totalPages)
        } else if (!isLoading && hosts.length === 0 && page > 1) {
            setPage(1)
        }
    }, [totalPages, page, setPage, isLoading, hosts.length])

    const groups = groupHostsByNode(hosts)

    async function handleDelete(host: HostWithRelations) {
        const ok = await confirm({
            title: "Delete Host",
            description: `Are you sure you want to delete "${host.remark || host.address || `Host #${host.id}`}"? This cannot be undone.`,
            confirmLabel: "Delete",
            variant: "destructive",
        })
        if (ok) deleteMutation.mutate(host.id)
    }

    async function handleBulkDelete() {
        const ids = Array.from(selectedHosts)
        const ok = await confirm({
            title: "Delete Hosts",
            description: `Are you sure you want to delete ${ids.length} host(s)? This cannot be undone.`,
            confirmLabel: "Delete All",
            variant: "destructive",
        })
        if (ok) {
            await Promise.all(ids.map(id => deleteMutation.mutateAsync(id)))
            clearSelection()
        }
    }

    async function handleBulkToggle(disable: boolean) {
        const ids = Array.from(selectedHosts)
        await Promise.all(ids.map(id => toggleMutation.mutateAsync({ id, isDisabled: !disable })))
        clearSelection()
    }

    function handleBulkAddTag() {
        const tag = bulkTagInput.trim().toLowerCase()
        if (!tag) return
        const ids = Array.from(selectedHosts)
        const hostsToUpdate = hosts.filter(h => ids.includes(h.id))
        hostsToUpdate.forEach(h => {
            const currentTags = h.tags || []
            if (!currentTags.includes(tag)) {
                bulkUpdateMutation.mutate({ ids: [h.id], fields: { tags: [...currentTags, tag] } })
            }
        })
        setBulkTagInput("")
        setShowBulkTagPopover(false)
        clearSelection()
    }

    function handleApplyTemplate(templateId: number) {
        const ids = Array.from(selectedHosts)
        applyTemplateMutation.mutate({ templateId, hostIds: ids }, {
            onSuccess: () => clearSelection(),
        })
    }

    const handleSwipeOpen = useCallback((id: number) => {
        setOpenSwipeId(id)
    }, [])

    const handleLongPress = useCallback((id: number) => {
        enterMultiSelectMode()
        toggleSelectHost(id)
    }, [enterMultiSelectMode, toggleSelectHost])

    const handleCtrlClick = useCallback((id: number) => {
        if (!isMultiSelectMode) enterMultiSelectMode()
        toggleSelectHost(id)
    }, [isMultiSelectMode, enterMultiSelectMode, toggleSelectHost])

    function isInfoHost(host: HostWithRelations): boolean {
        return !!host.plan_id && !host.inbound_id
    }

    // Compute showing range
    const showStart = total > 0 ? (page - 1) * perPage + 1 : 0
    const showEnd = Math.min(page * perPage, total)

    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            {/* Header */}
            <PageHeader
                title="Hosts"
                description={`${total} hosts configured`}
                actions={
                    <>
                        <Button
                            variant="outline"
                            size="sm"
                            className="gap-1.5"
                            onClick={() => setTemplateDialogOpen(true)}
                        >
                            <HiOutlineTemplate className="h-4 w-4" />
                            Templates
                        </Button>
                        <Button
                            size="sm"
                            className="gap-1.5"
                            onClick={() => openEditDialog(null)}
                        >
                            <HiOutlinePlus className="h-4 w-4" />
                            Add Host
                        </Button>
                    </>
                }
            />

            {/* Filter Panel */}
            <HostFilterPanel
                nodes={nodes}
                inbounds={inbounds}
                hostTags={hostTags}
            />

            {/* Floating bulk action island */}
            {selectedHosts.size > 0 && (
                <div className="fixed bottom-6 left-1/2 -translate-x-1/2 z-50 animate-in slide-in-from-bottom-4 fade-in duration-200">
                    <div className="flex items-center gap-1 px-2 py-1.5 rounded-xl border border-border/60 bg-background/95 backdrop-blur-xl shadow-2xl shadow-black/20 dark:shadow-black/50 ring-1 ring-white/10">
                        {/* Selection count pill */}
                        <div className="flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-primary/10 mr-1">
                            <div className="h-1.5 w-1.5 rounded-full bg-primary animate-pulse" />
                            <span className="text-xs font-semibold text-primary tabular-nums">{selectedHosts.size}</span>
                            <span className="text-xs text-primary/70">selected</span>
                        </div>

                        <div className="h-5 w-px bg-border/60 mx-0.5" />

                        <Button variant="ghost" size="sm" className="h-8 text-xs gap-1.5 rounded-lg" onClick={() => setBulkEditOpen(true)}>
                            <HiOutlinePencil className="h-3.5 w-3.5" />
                            Edit
                        </Button>
                        <Button variant="ghost" size="sm" className="h-8 text-xs rounded-lg" onClick={() => handleBulkToggle(false)}>
                            Enable
                        </Button>
                        <Button variant="ghost" size="sm" className="h-8 text-xs rounded-lg" onClick={() => handleBulkToggle(true)}>
                            Disable
                        </Button>

                        {/* Bulk Tag */}
                        <div className="relative">
                            <Button variant="ghost" size="sm" className="h-8 text-xs gap-1.5 rounded-lg" onClick={() => setShowBulkTagPopover(!showBulkTagPopover)}>
                                <HiOutlineTag className="h-3.5 w-3.5" />
                                Tag
                            </Button>
                            {showBulkTagPopover && (
                                <div className="absolute bottom-10 left-1/2 -translate-x-1/2 z-50 p-2.5 rounded-xl border bg-popover/95 backdrop-blur-xl shadow-xl space-y-2 min-w-[200px]">
                                    <Input
                                        value={bulkTagInput}
                                        onChange={(e) => setBulkTagInput(e.target.value)}
                                        onKeyDown={(e) => { if (e.key === "Enter") handleBulkAddTag() }}
                                        placeholder="Tag name..."
                                        className="h-8 text-xs"
                                        autoFocus
                                    />
                                    <div className="flex gap-1.5">
                                        <Button size="sm" className="h-7 text-xs flex-1" onClick={handleBulkAddTag} disabled={!bulkTagInput.trim()}>Add Tag</Button>
                                        <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={() => setShowBulkTagPopover(false)}>Cancel</Button>
                                    </div>
                                </div>
                            )}
                        </div>

                        {/* Apply Template */}
                        {templates.length > 0 && (
                            <DropdownMenu>
                                <DropdownMenuTrigger asChild>
                                    <Button variant="ghost" size="sm" className="h-8 text-xs gap-1.5 rounded-lg">
                                        <HiOutlineTemplate className="h-3.5 w-3.5" />
                                        Template
                                    </Button>
                                </DropdownMenuTrigger>
                                <DropdownMenuContent align="center" side="top" className="mb-2">
                                    <DropdownMenuLabel className="text-xs">Apply Template</DropdownMenuLabel>
                                    <DropdownMenuSeparator />
                                    {templates.map((t) => (
                                        <DropdownMenuCheckboxItem
                                            key={t.id}
                                            checked={false}
                                            onCheckedChange={() => handleApplyTemplate(t.id)}
                                        >
                                            {t.name}
                                        </DropdownMenuCheckboxItem>
                                    ))}
                                </DropdownMenuContent>
                            </DropdownMenu>
                        )}

                        <div className="h-5 w-px bg-border/60 mx-0.5" />

                        <Button variant="ghost" size="sm" className="h-8 text-xs text-destructive hover:text-destructive hover:bg-destructive/10 rounded-lg" onClick={handleBulkDelete}>
                            <HiOutlineTrash className="h-3.5 w-3.5" />
                        </Button>
                        <Button variant="ghost" size="icon" className="h-8 w-8 rounded-lg text-muted-foreground hover:text-foreground" onClick={clearSelection}>
                            <HiOutlineX className="h-3.5 w-3.5" />
                        </Button>
                    </div>
                </div>
            )}

            {/* Content */}
            {isLoading && hosts.length === 0 ? (
                /* Skeleton loading */
                <div className="space-y-4">
                    {Array.from({ length: 2 }).map((_, gi) => (
                        <div key={gi}>
                            {/* Group header skeleton */}
                            <div className="flex items-center gap-2 px-1 py-2">
                                <Skeleton className="h-4 w-4" />
                                <Skeleton className="h-4 w-24" />
                                <Skeleton className="h-4 w-6" />
                            </div>
                            {/* Card grid skeleton */}
                            <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-3">
                                {Array.from({ length: 3 }).map((_, ci) => (
                                    <div key={ci} className="border rounded-lg p-4 border-l-4 border-l-blue-500/30 space-y-3">
                                        <div className="flex justify-between">
                                            <Skeleton className="h-4 w-32" />
                                            <Skeleton className="h-5 w-10" />
                                        </div>
                                        <div className="flex gap-1.5">
                                            <Skeleton className="h-5 w-12" />
                                            <Skeleton className="h-5 w-10" />
                                            <Skeleton className="h-5 w-8" />
                                        </div>
                                        <Skeleton className="h-3 w-40" />
                                        <Skeleton className="h-3 w-28" />
                                    </div>
                                ))}
                            </div>
                        </div>
                    ))}
                </div>
            ) : !isLoading && hosts.length === 0 ? (
                /* Empty state */
                <div className="flex flex-col items-center justify-center p-12 border border-dashed rounded-lg text-center">
                    <HiOutlineLink className="h-10 w-10 text-muted-foreground/50 mb-3" />
                    <p className="text-sm font-medium text-muted-foreground">No hosts found</p>
                    <p className="text-xs text-muted-foreground/70 mt-1">Create a host or adjust your filters.</p>
                    <Button
                        size="sm"
                        className="mt-4 gap-1.5"
                        onClick={() => openEditDialog(null)}
                    >
                        <HiOutlinePlus className="h-4 w-4" />
                        Add Host
                    </Button>
                </div>
            ) : (
                /* Grouped card grid */
                <div className="space-y-2">
                    {groups.map((group) => (
                        <HostGroupSection
                            key={group.key}
                            groupKey={group.key}
                            label={group.label}
                            countryCode={group.countryCode}
                            count={group.hosts.length}
                            isCollapsed={collapsedGroups.includes(group.key)}
                            onToggle={() => toggleGroupCollapsed(group.key)}
                        >
                            {/* Desktop: card grid */}
                            <div className="hidden md:grid grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-3">
                                {group.hosts.map((host, index) => (
                                    <motion.div
                                        key={host.id}
                                        initial={{ opacity: 0, y: 8 }}
                                        animate={{ opacity: 1, y: 0 }}
                                        transition={{ duration: 0.15, delay: index * 0.03 }}
                                    >
                                        {isInfoHost(host) ? (
                                            <InfoHostCard
                                                host={host}
                                                isSelected={selectedHosts.has(host.id)}
                                                isMultiSelectMode={isMultiSelectMode}
                                                onToggleSelect={() => toggleSelectHost(host.id)}
                                                onEdit={() => openEditDialog(host)}
                                                onDelete={() => handleDelete(host)}
                                                onDuplicate={() => duplicateMutation.mutate(host.id)}
                                                onToggle={() => toggleMutation.mutate({ id: host.id, isDisabled: host.is_disabled })}
                                                onCtrlClick={() => handleCtrlClick(host.id)}
                                            />
                                        ) : (
                                            <ServerHostCard
                                                host={host}
                                                isSelected={selectedHosts.has(host.id)}
                                                isMultiSelectMode={isMultiSelectMode}
                                                onToggleSelect={() => toggleSelectHost(host.id)}
                                                onEdit={() => openEditDialog(host)}
                                                onDelete={() => handleDelete(host)}
                                                onDuplicate={() => duplicateMutation.mutate(host.id)}
                                                onToggle={() => toggleMutation.mutate({ id: host.id, isDisabled: host.is_disabled })}
                                                onTagClick={(tag) => setTagFilter(tag)}
                                                onCtrlClick={() => handleCtrlClick(host.id)}
                                            />
                                        )}
                                    </motion.div>
                                ))}
                            </div>

                            {/* Mobile: swipeable list */}
                            <div className="md:hidden space-y-2">
                                {group.hosts.map((host) => (
                                    <SwipeableHostCard
                                        key={host.id}
                                        host={host}
                                        isSelected={selectedHosts.has(host.id)}
                                        isMultiSelectMode={isMultiSelectMode}
                                        shouldClose={openSwipeId !== host.id && openSwipeId !== null}
                                        onOpen={handleSwipeOpen}
                                        onToggleSelect={() => toggleSelectHost(host.id)}
                                        onLongPress={handleLongPress}
                                        onEdit={() => openEditDialog(host)}
                                        onDelete={() => handleDelete(host)}
                                        onDuplicate={() => duplicateMutation.mutate(host.id)}
                                        onToggle={() => toggleMutation.mutate({ id: host.id, isDisabled: host.is_disabled })}
                                        onTagClick={(tag) => setTagFilter(tag)}
                                        onEnterMultiSelect={() => handleCtrlClick(host.id)}
                                    />
                                ))}
                            </div>
                        </HostGroupSection>
                    ))}
                </div>
            )}

            {/* Pagination with page size selector */}
            <div className="flex items-center justify-between gap-4 flex-wrap">
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    {total > 0 && <span>Showing {showStart}-{showEnd} of {total}</span>}
                    <Select value={String(perPage)} onValueChange={(v) => setPerPage(parseInt(v))}>
                        <SelectTrigger className="h-8 w-[80px] text-xs">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {PAGE_SIZE_OPTIONS.map((n) => (
                                <SelectItem key={n} value={String(n)}>{n} / page</SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </div>
                {totalPages > 1 && (
                    <PaginationNav
                        page={page}
                        hasNextPage={page < totalPages}
                        totalPages={totalPages}
                        onPageChange={setPage}
                        showingCount={hosts.length}
                    />
                )}
            </div>

            {/* Dialogs */}
            <HostSettingsDialog
                open={editDialog.open}
                onOpenChange={(open) => { if (!open) closeEditDialog() }}
                host={editDialog.host}
                showInboundSelector
                onSuccess={closeEditDialog}
            />
            <BulkEditDialog
                open={bulkEditOpen}
                onOpenChange={setBulkEditOpen}
                selectedIds={Array.from(selectedHosts)}
                onSuccess={clearSelection}
            />
            <HostTemplateDialog
                open={templateDialogOpen}
                onOpenChange={setTemplateDialogOpen}
            />
        </div>
    )
}

export default function HostsPage() {
    return (
        <ConfirmDialogProvider>
            <HostsPageContent />
        </ConfirmDialogProvider>
    )
}
