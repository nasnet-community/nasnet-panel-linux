import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { HiOutlinePlus, HiOutlinePencil, HiOutlineTrash } from "react-icons/hi"
import type { ReverseProxy } from "@/lib/types"

interface ReverseProxyTableProps {
    reverseProxies: ReverseProxy[]
    onEdit: (rp: ReverseProxy) => void
    onDelete: (rp: ReverseProxy) => void
    onCreate: () => void
}

export function ReverseProxyTable({
    reverseProxies,
    onEdit,
    onDelete,
    onCreate,
}: ReverseProxyTableProps) {
    const isEmpty = reverseProxies.length === 0

    return (
        <div>
            {/* Header */}
            <div className="flex items-center justify-between mb-4">
                <div>
                    <h3 className="text-lg font-semibold">Reverse Proxies</h3>
                    <p className="text-sm text-muted-foreground">Configure bridge and portal reverse proxy entries</p>
                </div>
                <Button size="sm" onClick={onCreate}>
                    <HiOutlinePlus className="w-4 h-4 mr-2" />
                    Add Reverse
                </Button>
            </div>

            <div className="rounded-2xl border bg-card/50 backdrop-blur-sm border-white/5 overflow-hidden">
                {!isEmpty ? (
                    <div className="overflow-x-auto">
                        <Table>
                            <TableHeader>
                                <TableRow className="bg-muted/50">
                                    <TableHead className="w-10 text-center">#</TableHead>
                                    <TableHead className="w-[90px]">Type</TableHead>
                                    <TableHead className="max-w-[140px]">Tag</TableHead>
                                    <TableHead className="max-w-[180px]">Domain</TableHead>
                                    <TableHead className="max-w-[180px]">Interconnection</TableHead>
                                    <TableHead className="max-w-[180px]">Target</TableHead>
                                    <TableHead className="w-[80px]"></TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {reverseProxies.map((rp, idx) => (
                                    <TableRow key={rp.id} className="group">
                                        {/* # */}
                                        <TableCell className="w-10 text-center text-muted-foreground text-xs font-mono tabular-nums">
                                            {idx + 1}
                                        </TableCell>

                                        {/* Type */}
                                        <TableCell>
                                            {rp.type === "bridge" ? (
                                                <Badge className="bg-blue-500/10 text-blue-400 border border-blue-500/20">
                                                    Bridge
                                                </Badge>
                                            ) : (
                                                <Badge className="bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                                                    Portal
                                                </Badge>
                                            )}
                                        </TableCell>

                                        {/* Tag */}
                                        <TableCell className="max-w-[140px]">
                                            <span className="font-mono text-sm truncate block">{rp.tag}</span>
                                        </TableCell>

                                        {/* Domain */}
                                        <TableCell className="max-w-[180px]">
                                            <span className="font-mono text-xs text-muted-foreground truncate block">{rp.domain}</span>
                                        </TableCell>

                                        {/* Interconnection */}
                                        <TableCell className="max-w-[180px]">
                                            <span className="text-xs font-mono truncate block">
                                                {rp.type === "bridge"
                                                    ? rp.interconnection_tag || <span className="text-muted-foreground">—</span>
                                                    : rp.interconnection_tags?.length
                                                        ? rp.interconnection_tags.join(", ")
                                                        : <span className="text-muted-foreground">—</span>
                                                }
                                            </span>
                                        </TableCell>

                                        {/* Target */}
                                        <TableCell className="max-w-[180px]">
                                            <span className="text-xs font-mono truncate block">
                                                {rp.type === "bridge"
                                                    ? rp.outbound_tag || <span className="text-muted-foreground">—</span>
                                                    : rp.inbound_tags?.length
                                                        ? rp.inbound_tags.join(", ")
                                                        : <span className="text-muted-foreground">—</span>
                                                }
                                            </span>
                                        </TableCell>

                                        {/* Actions */}
                                        <TableCell>
                                            <div className="flex gap-1 justify-end">
                                                <Button
                                                    variant="ghost"
                                                    size="icon"
                                                    onClick={() => onEdit(rp)}
                                                    className="opacity-0 group-hover:opacity-100"
                                                >
                                                    <HiOutlinePencil className="w-4 h-4" />
                                                </Button>
                                                <Button
                                                    variant="ghost"
                                                    size="icon"
                                                    onClick={() => onDelete(rp)}
                                                    className="opacity-0 group-hover:opacity-100 text-red-500 hover:text-red-600 hover:bg-red-100/50"
                                                >
                                                    <HiOutlineTrash className="w-4 h-4" />
                                                </Button>
                                            </div>
                                        </TableCell>
                                    </TableRow>
                                ))}
                            </TableBody>
                        </Table>
                    </div>
                ) : (
                    <div className="flex flex-col items-center justify-center py-12 text-muted-foreground gap-4">
                        <p>No reverse proxies configured.</p>
                        <Button size="sm" onClick={onCreate}>
                            <HiOutlinePlus className="w-4 h-4 mr-2" />
                            Add Reverse
                        </Button>
                    </div>
                )}
            </div>
        </div>
    )
}
