import { useState, useEffect, useRef, useCallback } from "react"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Search, X, UserPlus, Loader2 } from "lucide-react"
import { FaTelegram } from "react-icons/fa"
import { listUsers } from "@/lib/admin-api"
import { cn } from "@/lib/utils"
import type { User } from "@/lib/types"

interface UserSearchSelectProps {
    value: User | null
    onChange: (user: User | null) => void
    onCreateNew?: () => void
    placeholder?: string
}

export function UserSearchSelect({ value, onChange, onCreateNew, placeholder = "Search users..." }: UserSearchSelectProps) {
    const [search, setSearch] = useState("")
    const [users, setUsers] = useState<User[]>([])
    const [loading, setLoading] = useState(false)
    const [open, setOpen] = useState(false)
    const containerRef = useRef<HTMLDivElement>(null)
    const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined)

    const fetchUsers = useCallback(async (query: string) => {
        setLoading(true)
        try {
            const res = await listUsers({ search: query, per_page: 20, page: 1 })
            if (res.success && res.data) {
                setUsers(res.data.users)
            }
        } finally {
            setLoading(false)
        }
    }, [])

    useEffect(() => {
        if (debounceRef.current) clearTimeout(debounceRef.current)
        if (!open) return

        debounceRef.current = setTimeout(() => {
            fetchUsers(search)
        }, 300)

        return () => {
            if (debounceRef.current) clearTimeout(debounceRef.current)
        }
    }, [search, open, fetchUsers])

    useEffect(() => {
        function handleClickOutside(e: MouseEvent) {
            if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
                setOpen(false)
            }
        }
        document.addEventListener("mousedown", handleClickOutside)
        return () => document.removeEventListener("mousedown", handleClickOutside)
    }, [])

    if (value) {
        return (
            <div className="flex items-center gap-3 p-3 rounded-lg border bg-card/50">
                <div className="w-9 h-9 rounded-full bg-primary/10 flex items-center justify-center shrink-0">
                    <span className="text-sm font-bold text-primary">
                        {(value.username || value.first_name || "U").charAt(0).toUpperCase()}
                    </span>
                </div>
                <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium truncate">{value.username || `User #${value.id}`}</p>
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        {value.first_name && <span>{value.first_name} {value.last_name}</span>}
                        {value.telegram_id > 0 && (
                            <div className="flex items-center gap-1">
                                <FaTelegram className="w-3 h-3 text-[#229ED9]" />
                                <span className="font-mono">{value.telegram_id}</span>
                            </div>
                        )}
                    </div>
                </div>
                <Button variant="ghost" size="icon" className="h-8 w-8 shrink-0" onClick={() => onChange(null)}>
                    <X className="w-4 h-4" />
                </Button>
            </div>
        )
    }

    return (
        <div ref={containerRef} className="relative">
            <div className="relative">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                <Input
                    placeholder={placeholder}
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    onFocus={() => setOpen(true)}
                    className="pl-9"
                />
                {loading && (
                    <Loader2 className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 animate-spin text-muted-foreground" />
                )}
            </div>

            {open && (
                <div className="absolute z-50 top-full left-0 right-0 mt-1 rounded-lg border bg-popover shadow-lg">
                    <ScrollArea className="max-h-[240px]">
                        {users.length === 0 && !loading ? (
                            <div className="p-4 text-center text-sm text-muted-foreground">
                                {search ? "No users found" : "Type to search users"}
                            </div>
                        ) : (
                            <div className="py-1">
                                {users.map((user) => (
                                    <button
                                        key={user.id}
                                        type="button"
                                        className={cn(
                                            "w-full flex items-center gap-3 px-3 py-2 hover:bg-muted/50 transition-colors text-left"
                                        )}
                                        onClick={() => {
                                            onChange(user)
                                            setOpen(false)
                                            setSearch("")
                                        }}
                                    >
                                        <div className="w-8 h-8 rounded-full bg-primary/10 flex items-center justify-center shrink-0">
                                            <span className="text-xs font-bold text-primary">
                                                {(user.username || user.first_name || "U").charAt(0).toUpperCase()}
                                            </span>
                                        </div>
                                        <div className="flex-1 min-w-0">
                                            <p className="text-sm font-medium truncate">{user.username || `User #${user.id}`}</p>
                                            <p className="text-xs text-muted-foreground truncate">
                                                {user.first_name} {user.last_name}
                                            </p>
                                        </div>
                                        {user.telegram_id > 0 && (
                                            <Badge variant="outline" className="text-[10px] px-1.5 shrink-0">
                                                <FaTelegram className="w-3 h-3 mr-1 text-[#229ED9]" />
                                                {user.telegram_id}
                                            </Badge>
                                        )}
                                    </button>
                                ))}
                            </div>
                        )}
                    </ScrollArea>
                    {onCreateNew && (
                        <>
                            <div className="border-t" />
                            <button
                                type="button"
                                className="w-full flex items-center gap-2 px-3 py-2.5 text-sm text-primary hover:bg-muted/50 transition-colors"
                                onClick={() => {
                                    onCreateNew()
                                    setOpen(false)
                                }}
                            >
                                <UserPlus className="w-4 h-4" />
                                Create New User
                            </button>
                        </>
                    )}
                </div>
            )}
        </div>
    )
}
