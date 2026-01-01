import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { AnimatePresence, motion } from "framer-motion"
import {
    HiOutlineSearch,
    HiOutlineX,
    HiOutlineAdjustments,
    HiOutlineInformationCircle,
} from "react-icons/hi"
import { useHostsStore } from "@/store/hosts-store"
import type { Node } from "@/lib/types"
import type { Inbound } from "@/lib/types"

interface HostFilterPanelProps {
    nodes?: Node[]
    inbounds?: Inbound[]
    hostTags: string[]
}

export function HostFilterPanel({ nodes, inbounds, hostTags }: HostFilterPanelProps) {
    const {
        statusFilter, setStatusFilter,
        hostType, setHostType,
        search, setSearch,
        nodeFilter, setNodeFilter,
        inboundFilter, setInboundFilter,
        tagFilter, setTagFilter,
        mobileFiltersOpen, setMobileFiltersOpen,
    } = useHostsStore()

    const [searchInput, setSearchInput] = useState(search)

    function handleSearch() {
        setSearch(searchInput)
    }

    function handleClearSearch() {
        setSearchInput("")
        setSearch("")
    }

    // Count active filters (excluding search and host type)
    const activeFilterCount = [
        nodeFilter !== "all",
        inboundFilter !== "all",
        tagFilter !== "all",
        statusFilter !== "all",
    ].filter(Boolean).length

    // Active filter chips
    type Chip = { label: string; onClear: () => void }
    const chips: Chip[] = []
    if (hostType !== "all") {
        chips.push({
            label: hostType === "server" ? "Server" : "Info",
            onClear: () => setHostType("all"),
        })
    }
    if (nodeFilter !== "all") {
        const node = nodes?.find(n => String(n.id) === nodeFilter)
        chips.push({
            label: node?.name || `Node ${nodeFilter}`,
            onClear: () => setNodeFilter("all"),
        })
    }
    if (inboundFilter !== "all") {
        const inbound = inbounds?.find(ib => String(ib.id) === inboundFilter)
        chips.push({
            label: inbound?.tag || `Inbound ${inboundFilter}`,
            onClear: () => setInboundFilter("all"),
        })
    }
    if (tagFilter !== "all") {
        chips.push({
            label: `#${tagFilter}`,
            onClear: () => setTagFilter("all"),
        })
    }
    if (statusFilter !== "all") {
        chips.push({
            label: statusFilter === "enabled" ? "Enabled" : "Disabled",
            onClear: () => setStatusFilter("all"),
        })
    }

    const filterControls = (
        <>
            {/* Host Type */}
            <div className="flex items-center gap-1 rounded-lg border p-0.5 w-fit">
                {(["all", "server", "info"] as const).map((t) => (
                    <button
                        key={t}
                        onClick={() => setHostType(t)}
                        className={`px-2.5 py-1 rounded-md text-xs font-medium transition-colors flex items-center gap-1 ${
                            hostType === t
                                ? "bg-primary text-primary-foreground"
                                : "text-muted-foreground hover:text-foreground"
                        }`}
                    >
                        {t === "info" && <HiOutlineInformationCircle className="h-3 w-3" />}
                        {t.charAt(0).toUpperCase() + t.slice(1)}
                    </button>
                ))}
            </div>

            {/* Node */}
            <Select value={nodeFilter} onValueChange={setNodeFilter}>
                <SelectTrigger className="h-9 text-sm w-full sm:w-[160px]">
                    <SelectValue placeholder="All Nodes" />
                </SelectTrigger>
                <SelectContent>
                    <SelectItem value="all">All Nodes</SelectItem>
                    {nodes?.map((n) => (
                        <SelectItem key={n.id} value={String(n.id)}>{n.name}</SelectItem>
                    ))}
                </SelectContent>
            </Select>

            {/* Inbound */}
            <Select value={inboundFilter} onValueChange={setInboundFilter}>
                <SelectTrigger className="h-9 text-sm w-full sm:w-[160px]">
                    <SelectValue placeholder="All Inbounds" />
                </SelectTrigger>
                <SelectContent>
                    <SelectItem value="all">All Inbounds</SelectItem>
                    {inbounds?.map((ib) => (
                        <SelectItem key={ib.id} value={String(ib.id)}>{ib.tag} (:{ib.port})</SelectItem>
                    ))}
                </SelectContent>
            </Select>

            {/* Tag */}
            {hostTags.length > 0 && (
                <Select value={tagFilter} onValueChange={setTagFilter}>
                    <SelectTrigger className="h-9 text-sm w-full sm:w-[140px]">
                        <SelectValue placeholder="All Tags" />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="all">All Tags</SelectItem>
                        {hostTags.map((t) => (
                            <SelectItem key={t} value={t}>{t}</SelectItem>
                        ))}
                    </SelectContent>
                </Select>
            )}

            {/* Status */}
            <div className="flex items-center gap-1 rounded-lg border p-0.5 w-fit">
                {(["all", "enabled", "disabled"] as const).map((s) => (
                    <button
                        key={s}
                        onClick={() => setStatusFilter(s)}
                        className={`px-2.5 py-1 rounded-md text-xs font-medium transition-colors ${
                            statusFilter === s
                                ? "bg-primary text-primary-foreground"
                                : "text-muted-foreground hover:text-foreground"
                        }`}
                    >
                        {s.charAt(0).toUpperCase() + s.slice(1)}
                    </button>
                ))}
            </div>
        </>
    )

    return (
        <div className="space-y-2">
            {/* Search + Mobile filter toggle */}
            <div className="flex items-center gap-2">
                <div className="relative flex-1 max-w-sm">
                    <HiOutlineSearch className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                    <Input
                        value={searchInput}
                        onChange={(e) => setSearchInput(e.target.value)}
                        onKeyDown={(e) => e.key === "Enter" && handleSearch()}
                        placeholder="Search remark or address..."
                        className="pl-9 pr-8 h-9 text-sm"
                    />
                    {searchInput && (
                        <button
                            onClick={handleClearSearch}
                            className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                        >
                            <HiOutlineX className="h-4 w-4" />
                        </button>
                    )}
                </div>

                {/* Mobile filter button */}
                <div className="md:hidden">
                    <Button
                        variant="outline"
                        size="sm"
                        className="h-9 gap-1.5 relative"
                        onClick={() => setMobileFiltersOpen(!mobileFiltersOpen)}
                    >
                        <HiOutlineAdjustments className="h-4 w-4" />
                        Filters
                        {activeFilterCount > 0 && (
                            <Badge className="h-4 w-4 p-0 flex items-center justify-center text-[9px] absolute -top-1.5 -right-1.5">
                                {activeFilterCount}
                            </Badge>
                        )}
                    </Button>
                </div>
            </div>

            {/* Active filter chips */}
            {chips.length > 0 && (
                <div className="flex flex-wrap gap-1.5">
                    {chips.map((chip) => (
                        <Badge
                            key={chip.label}
                            variant="secondary"
                            className="text-xs h-6 px-2 gap-1 cursor-pointer hover:bg-secondary/80"
                            onClick={chip.onClear}
                        >
                            {chip.label}
                            <HiOutlineX className="h-3 w-3" />
                        </Badge>
                    ))}
                </div>
            )}

            {/* Desktop: all filters inline */}
            <div className="hidden md:flex md:flex-wrap md:items-center gap-2">
                {filterControls}
            </div>

            {/* Mobile: collapsible panel */}
            <AnimatePresence initial={false}>
                {mobileFiltersOpen && (
                    <motion.div
                        key="mobile-filters"
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.2 }}
                        className="md:hidden overflow-hidden"
                    >
                        <div className="flex flex-col gap-2 p-3 rounded-lg border bg-muted/30">
                            {filterControls}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    )
}
