import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Button } from "@/components/ui/button"
import { HiOutlinePlus, HiOutlineTrash } from "react-icons/hi"
import type { HTTPSettings } from "@/lib/types"

interface HTTPFormProps {
    settings?: HTTPSettings
    onChange: (settings: HTTPSettings) => void
}

export function HTTPForm({ settings, onChange }: HTTPFormProps) {
    const data = settings || { accounts: [] }

    const addAccount = () => {
        onChange({
            ...data,
            accounts: [...(data.accounts || []), { user: "", pass: "" }],
        })
    }

    return (
        <div className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <Label>Timeout (seconds)</Label>
                    <Input
                        type="number"
                        placeholder="300"
                        value={data.timeout || ""}
                        onChange={(e) => onChange({ ...data, timeout: parseInt(e.target.value) || undefined })}
                    />
                </div>
                <div className="flex items-center justify-between pt-6">
                    <Label>Allow Transparent</Label>
                    <Switch
                        checked={data.allowTransparent ?? false}
                        onCheckedChange={(checked) => onChange({ ...data, allowTransparent: checked })}
                    />
                </div>
            </div>

            <div className="space-y-3 border-t pt-4">
                <div className="flex items-center justify-between">
                    <h4 className="font-medium">Accounts (Optional)</h4>
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
        </div>
    )
}
