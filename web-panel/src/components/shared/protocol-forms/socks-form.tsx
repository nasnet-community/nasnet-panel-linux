import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { HiOutlinePlus, HiOutlineTrash } from "react-icons/hi"
import type { SOCKSSettings } from "@/lib/types"

interface SOCKSFormProps {
    settings?: SOCKSSettings
    onChange: (settings: SOCKSSettings) => void
}

export function SOCKSForm({ settings, onChange }: SOCKSFormProps) {
    const data = settings || { auth: "noauth", accounts: [] }

    const addAccount = () => {
        onChange({
            ...data,
            auth: "password",
            accounts: [...(data.accounts || []), { user: "", pass: "" }],
        })
    }

    return (
        <div className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Authentication</Label>
                    <Select
                        value={data.auth || "noauth"}
                        onValueChange={(value) => onChange({ ...data, auth: value as SOCKSSettings["auth"] })}
                    >
                        <SelectTrigger className="w-full">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="noauth">No Authentication</SelectItem>
                            <SelectItem value="password">Password</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
                <div className="space-y-2">
                    <Label>UDP Relay IP</Label>
                    <Input
                        placeholder="127.0.0.1"
                        value={data.ip || ""}
                        onChange={(e) => onChange({ ...data, ip: e.target.value })}
                    />
                </div>
            </div>

            <div className="flex items-center justify-between">
                <div>
                    <Label>Enable UDP</Label>
                    <p className="text-xs text-muted-foreground">Allow UDP relay</p>
                </div>
                <Switch
                    checked={data.udp ?? false}
                    onCheckedChange={(checked) => onChange({ ...data, udp: checked })}
                />
            </div>

            {data.auth === "password" && (
                <div className="space-y-3 border-t pt-4">
                    <div className="flex items-center justify-between">
                        <h4 className="font-medium">Accounts</h4>
                        <Button type="button" size="sm" variant="outline" onClick={addAccount}>
                            <HiOutlinePlus className="w-4 h-4 mr-1" /> Add Account
                        </Button>
                    </div>
                    {(data.accounts || []).map((acc, index) => (
                        <div key={index} className="flex gap-2 items-end">
                            <div className="flex-1 space-y-1">
                                <Label className="text-xs">Username</Label>
                                <Input
                                    value={acc.user}
                                    onChange={(e) => {
                                        const accounts = [...(data.accounts || [])]
                                        accounts[index] = { ...acc, user: e.target.value }
                                        onChange({ ...data, accounts })
                                    }}
                                />
                            </div>
                            <div className="flex-1 space-y-1">
                                <Label className="text-xs">Password</Label>
                                <Input
                                    type="password"
                                    value={acc.pass}
                                    onChange={(e) => {
                                        const accounts = [...(data.accounts || [])]
                                        accounts[index] = { ...acc, pass: e.target.value }
                                        onChange({ ...data, accounts })
                                    }}
                                />
                            </div>
                            <Button
                                type="button"
                                size="icon"
                                variant="ghost"
                                className="text-red-500 h-10 w-10 md:h-9 md:w-9"
                                onClick={() => {
                                    const accounts = [...(data.accounts || [])]
                                    accounts.splice(index, 1)
                                    onChange({ ...data, accounts })
                                }}
                            >
                                <HiOutlineTrash className="w-4 h-4" />
                            </Button>
                        </div>
                    ))}
                </div>
            )}
        </div>
    )
}
