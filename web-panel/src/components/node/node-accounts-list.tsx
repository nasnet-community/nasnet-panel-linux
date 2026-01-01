import { useState } from "react"
import {
    useAccountsByNode,
    useDeleteAccount,
    useDisableAccount,
    useEnableAccount,
    useAccountLink
} from "@/lib/queries/use-accounts"
import {
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow
} from "@/components/ui/table"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
    HiOutlineSearch,
    HiOutlineRefresh,
    HiOutlineUsers,
    HiOutlineDotsVertical,
    HiOutlineTrash,
    HiOutlineBan,
    HiOutlineCheckCircle,
    HiOutlineLink,
    HiOutlinePlus
} from "react-icons/hi"
import { cn, formatBytes, copyToClipboard } from "@/lib/utils"
import { CreateAccountDialog } from "./create-account-dialog"
import { toast } from "sonner"
import type { Account } from "@/lib/api/accounts"

interface NodeAccountsListProps {
    nodeId: number
    isOnline: boolean
}

export function NodeAccountsList({ nodeId, isOnline }: NodeAccountsListProps) {
    const { data: accounts, isLoading, refetch, isRefetching } = useAccountsByNode(nodeId)
    const [searchTerm, setSearchTerm] = useState("")
    const [createDialogOpen, setCreateDialogOpen] = useState(false)

    // Mutations
    const deleteMutation = useDeleteAccount(nodeId)
    const disableMutation = useDisableAccount(nodeId)
    const enableMutation = useEnableAccount(nodeId)
    const linkMutation = useAccountLink()

    if (isLoading && !accounts) {
        return (
            <div className="space-y-4">
                <div className="flex justify-between">
                    <Skeleton className="h-10 w-48" />
                    <Skeleton className="h-10 w-24" />
                </div>
                <Skeleton className="h-[200px] w-full" />
            </div>
        )
    }

    const filteredAccounts = accounts?.filter(acc =>
        acc.email.toLowerCase().includes(searchTerm.toLowerCase()) ||
        acc.uuid.toLowerCase().includes(searchTerm.toLowerCase()) ||
        (acc.inbound?.tag && acc.inbound.tag.toLowerCase().includes(searchTerm.toLowerCase()))
    ) || []

    const totalAccounts = accounts?.length || 0
    const activeAccounts = accounts?.filter(a => a.status === 'active').length || 0
    const totalDataUsed = accounts?.reduce((acc, a) => acc + a.data_used, 0) || 0

    const handleCopyLink = (id: number) => {
        linkMutation.mutate(id, {
            onSuccess: async (res) => {
                if (res.success && res.data?.link) {
                    await copyToClipboard(res.data.link)
                    toast.success("Link copied to clipboard")
                }
            }
        })
    }

    const handleDelete = (id: number) => {
        if (confirm("Are you sure you want to delete this account?")) {
            deleteMutation.mutate(id)
        }
    }

    return (
        <div className="space-y-6">
            <div className="grid gap-4 md:grid-cols-3">
                <Card>
                    <CardHeader className="py-4">
                        <CardTitle className="text-sm font-medium text-muted-foreground">Total Accounts</CardTitle>
                        <div className="text-2xl font-bold">{totalAccounts}</div>
                    </CardHeader>
                </Card>
                <Card>
                    <CardHeader className="py-4">
                        <CardTitle className="text-sm font-medium text-muted-foreground">Active Accounts</CardTitle>
                        <div className="text-2xl font-bold">{activeAccounts}</div>
                    </CardHeader>
                </Card>
                <Card>
                    <CardHeader className="py-4">
                        <CardTitle className="text-sm font-medium text-muted-foreground">Total Data Used</CardTitle>
                        <div className="text-2xl font-bold">{formatBytes(totalDataUsed)}</div>
                    </CardHeader>
                </Card>
            </div>

            <Card>
                <CardHeader className="pb-3">
                    <div className="flex items-center justify-between">
                        <div>
                            <CardTitle className="flex items-center gap-2">
                                <HiOutlineUsers className="w-5 h-5" />
                                Accounts
                            </CardTitle>
                            <CardDescription>
                                Manage accounts configured on this node
                            </CardDescription>
                        </div>
                        <div className="flex items-center gap-2">
                            <div className="relative w-64 hidden sm:block">
                                <HiOutlineSearch className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                                <Input
                                    placeholder="Search accounts..."
                                    className="pl-9"
                                    value={searchTerm}
                                    onChange={(e) => setSearchTerm(e.target.value)}
                                />
                            </div>
                            <Button variant="default" size="sm" onClick={() => setCreateDialogOpen(true)}>
                                <HiOutlinePlus className="w-4 h-4 mr-2" />
                                Create Account
                            </Button>
                            <Button variant="outline" size="icon" onClick={() => refetch()} disabled={isRefetching} aria-label="Refresh accounts">
                                <HiOutlineRefresh className={cn("w-4 h-4", isRefetching && "animate-spin")} />
                            </Button>
                        </div>
                    </div>
                    {/* Mobile Search */}
                    <div className="mt-4 sm:hidden relative">
                        <HiOutlineSearch className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                        <Input
                            placeholder="Search accounts..."
                            className="pl-9 w-full"
                            value={searchTerm}
                            onChange={(e) => setSearchTerm(e.target.value)}
                        />
                    </div>
                </CardHeader>
                <CardContent>
                    <div className="rounded-md border">
                        <Table>
                            <TableHeader>
                                <TableRow>
                                    <TableHead>Email / UUID</TableHead>
                                    <TableHead>Inbound</TableHead>
                                    <TableHead>Data Used</TableHead>
                                    <TableHead>Status</TableHead>
                                    <TableHead className="w-[50px]"></TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {filteredAccounts.length === 0 ? (
                                    <TableRow>
                                        <TableCell colSpan={5} className="text-center py-6 text-muted-foreground">
                                            No accounts found
                                        </TableCell>
                                    </TableRow>
                                ) : (
                                    filteredAccounts.map((account) => (
                                        <TableRow key={account.id}>
                                            <TableCell>
                                                <div className="flex flex-col">
                                                    <span className="font-medium">{account.email}</span>
                                                    <span className="text-xs text-muted-foreground font-mono truncate max-w-[150px]" title={account.uuid}>
                                                        {account.uuid}
                                                    </span>
                                                </div>
                                            </TableCell>
                                            <TableCell>
                                                <div className="flex flex-col">
                                                    <Badge variant="outline" className="w-fit">{account.inbound?.tag || account.inbound_id}</Badge>
                                                    <span className="text-xs text-muted-foreground uppercase">{account.inbound?.protocol}</span>
                                                </div>
                                            </TableCell>
                                            <TableCell>
                                                <span className="font-mono">{formatBytes(account.data_used)}</span>
                                            </TableCell>
                                            <TableCell>
                                                <Badge variant={account.status === 'active' ? 'success' : 'secondary'}>
                                                    {account.status}
                                                </Badge>
                                            </TableCell>
                                            <TableCell>
                                                <DropdownMenu>
                                                    <DropdownMenuTrigger asChild>
                                                        <Button variant="ghost" size="icon">
                                                            <HiOutlineDotsVertical className="w-4 h-4" />
                                                        </Button>
                                                    </DropdownMenuTrigger>
                                                    <DropdownMenuContent align="end">
                                                        <DropdownMenuLabel>Actions</DropdownMenuLabel>
                                                        <DropdownMenuSeparator />
                                                        <DropdownMenuItem onClick={() => handleCopyLink(account.id)}>
                                                            <HiOutlineLink className="w-4 h-4 mr-2" />
                                                            Copy Link
                                                        </DropdownMenuItem>
                                                        {account.status === 'active' ? (
                                                            <DropdownMenuItem onClick={() => disableMutation.mutate(account.id)}>
                                                                <HiOutlineBan className="w-4 h-4 mr-2" />
                                                                Disable
                                                            </DropdownMenuItem>
                                                        ) : (
                                                            <DropdownMenuItem onClick={() => enableMutation.mutate(account.id)}>
                                                                <HiOutlineCheckCircle className="w-4 h-4 mr-2" />
                                                                Enable
                                                            </DropdownMenuItem>
                                                        )}
                                                        <DropdownMenuSeparator />
                                                        <DropdownMenuItem onClick={() => handleDelete(account.id)} className="text-red-500 hover:text-red-600">
                                                            <HiOutlineTrash className="w-4 h-4 mr-2" />
                                                            Delete
                                                        </DropdownMenuItem>
                                                    </DropdownMenuContent>
                                                </DropdownMenu>
                                            </TableCell>
                                        </TableRow>
                                    ))
                                )}
                            </TableBody>
                        </Table>
                    </div>
                </CardContent>
            </Card>

            <CreateAccountDialog
                nodeId={nodeId}
                open={createDialogOpen}
                onOpenChange={setCreateDialogOpen}
            />
        </div>
    )
}
