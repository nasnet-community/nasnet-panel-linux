import { useRef, useState } from "react"
import { KeyRound, Upload } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { CopyableText } from "@/components/ui/copyable-text"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ResponsiveDialog } from "@/components/ui/responsive-dialog"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { useGenerateVPNKeypair, useParseVPNInput, useSaveVPNProfile } from "@/lib/queries/use-network"
import { detectFormat } from "@/lib/vpn-labels"
import type { VPNProfile, WireGuardConfig } from "@/lib/types/network"
import { toast } from "sonner"

const FORMAT_LABEL: Record<"uri" | "conf", string> = {
    uri: "Read as a WireGuard link",
    conf: "Read as a WireGuard config file",
}

interface Props {
    open: boolean
    onOpenChange: (open: boolean) => void
    /** Editing an existing profile; omitted when adding one. */
    profile?: VPNProfile | null
}

const EMPTY_MANUAL: WireGuardConfig = {
    private_key: "",
    address: "",
    dns: "",
    peer: { public_key: "", preshared_key: "", allowed_ips: ["0.0.0.0/0"], endpoint: "" },
}

export function VpnAddDialog({ open, onOpenChange, profile }: Props) {
    const save = useSaveVPNProfile()
    const parse = useParseVPNInput()
    const keypair = useGenerateVPNKeypair()
    const fileRef = useRef<HTMLInputElement>(null)

    const [name, setName] = useState(profile?.name ?? "")
    const [raw, setRaw] = useState("")
    const [manual, setManual] = useState<WireGuardConfig>(profile?.config ?? EMPTY_MANUAL)
    const [mode, setMode] = useState<"paste" | "manual">(profile ? "manual" : "paste")
    const [preview, setPreview] = useState<WireGuardConfig | null>(null)
    const [error, setError] = useState("")

    const format = detectFormat(raw)

    async function check(text: string) {
        setError("")
        setPreview(null)
        if (!text.trim()) return
        try {
            const res = await parse.mutateAsync(text)
            setPreview(res.config)
            if (!name && res.config.suggested_name) setName(res.config.suggested_name)
        } catch (e) {
            setError(e instanceof Error ? e.message : "This is not a WireGuard config")
        }
    }

    async function readFile(file: File) {
        const text = await file.text()
        setRaw(text)
        void check(text)
    }

    async function makeKeys() {
        try {
            const keys = await keypair.mutateAsync()
            setManual({ ...manual, private_key: keys.private_key })
            toast.success("Key generated — give the public key to your VPN server")
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Failed to generate a key")
        }
    }

    async function submit() {
        setError("")
        try {
            await save.mutateAsync({
                id: profile?.id,
                name: name.trim(),
                ...(mode === "paste" ? { raw } : { config: manual }),
            })
            toast.success(profile ? "VPN saved" : "VPN added")
            onOpenChange(false)
            setRaw("")
            setPreview(null)
            setName("")
            setManual(EMPTY_MANUAL)
        } catch (e) {
            setError(e instanceof Error ? e.message : "Failed to save the VPN")
        }
    }

    const ready = name.trim() !== "" && (mode === "paste" ? raw.trim() !== "" : manual.private_key !== "")

    return (
        <ResponsiveDialog
            open={open}
            onOpenChange={onOpenChange}
            title={profile ? "Edit VPN" : "Add a VPN"}
            description="Paste what your provider gave you, or fill it in by hand."
            onSave={() => void submit()}
            saveLabel={profile ? "Save" : "Add"}
            saveDisabled={!ready || save.isPending}
            className="sm:max-w-2xl"
        >
            <div className="space-y-4">
                <div className="space-y-1.5">
                    <Label htmlFor="vpn-name">Name</Label>
                    <Input
                        id="vpn-name"
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                        placeholder="Frankfurt"
                    />
                </div>

                <Tabs value={mode} onValueChange={(v) => setMode(v as "paste" | "manual")}>
                    <TabsList>
                        <TabsTrigger value="paste">Paste or upload</TabsTrigger>
                        <TabsTrigger value="manual">Enter by hand</TabsTrigger>
                    </TabsList>

                    <TabsContent value="paste" className="mt-4 space-y-3">
                        <Textarea
                            aria-label="WireGuard link or config file"
                            value={raw}
                            onChange={(e) => setRaw(e.target.value)}
                            onBlur={() => void check(raw)}
                            placeholder={"wireguard://…\n\nor\n\n[Interface]\nPrivateKey = …"}
                            className="min-h-[10rem] font-mono text-xs"
                        />
                        <div className="flex flex-wrap items-center gap-2">
                            <input
                                ref={fileRef}
                                type="file"
                                accept=".conf,.txt,text/plain"
                                className="hidden"
                                onChange={(e) => {
                                    const f = e.target.files?.[0]
                                    if (f) void readFile(f)
                                }}
                            />
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                onClick={() => fileRef.current?.click()}
                            >
                                <Upload className="mr-1.5 h-3.5 w-3.5" />
                                Upload a .conf file
                            </Button>
                            {format && (
                                <span className="text-text-tertiary text-xs">
                                    {FORMAT_LABEL[format]}
                                </span>
                            )}
                        </div>

                        {/* What the parser dropped or filled in, in its own words —
                            finding out from behaviour is how a config looks broken. */}
                        {preview?.notices?.map((n) => (
                            <p key={n} className="text-text-tertiary text-xs">
                                {n}
                            </p>
                        ))}
                        {preview && (
                            <div className="border-border-subtle rounded-lg border p-3 text-xs">
                                <p className="mb-1 font-medium">This connects to</p>
                                <p className="font-mono break-all">{preview.peer.endpoint}</p>
                                <p className="text-text-tertiary mt-1">
                                    as {preview.address} · carries{" "}
                                    {preview.peer.allowed_ips.join(", ")}
                                </p>
                            </div>
                        )}
                    </TabsContent>

                    <TabsContent value="manual" className="mt-4 space-y-3">
                        <div className="space-y-1.5">
                            <Label htmlFor="vpn-priv">Your private key</Label>
                            <div className="flex gap-2">
                                <Input
                                    id="vpn-priv"
                                    value={manual.private_key}
                                    onChange={(e) =>
                                        setManual({ ...manual, private_key: e.target.value })
                                    }
                                    className="font-mono text-xs"
                                />
                                <Button
                                    type="button"
                                    variant="outline"
                                    onClick={() => void makeKeys()}
                                    disabled={keypair.isPending}
                                >
                                    <KeyRound className="mr-1.5 h-3.5 w-3.5" />
                                    Generate
                                </Button>
                            </div>
                            {/* Generating one is only useful if you can hand the other
                                half to the server. */}
                            {keypair.data && (
                                <div className="space-y-1 pt-1">
                                    <p className="text-text-tertiary text-xs">
                                        Give this public key to your VPN server:
                                    </p>
                                    <CopyableText
                                        text={keypair.data.public_key}
                                        className="font-mono text-xs"
                                    />
                                </div>
                            )}
                        </div>

                        <div className="grid gap-3 sm:grid-cols-2">
                            <div className="space-y-1.5">
                                <Label htmlFor="vpn-addr">Your address in the tunnel</Label>
                                <Input
                                    id="vpn-addr"
                                    value={manual.address}
                                    onChange={(e) =>
                                        setManual({ ...manual, address: e.target.value })
                                    }
                                    placeholder="10.66.0.2/32"
                                    className="font-mono text-xs"
                                />
                            </div>
                            <div className="space-y-1.5">
                                <Label htmlFor="vpn-endpoint">Server address and port</Label>
                                <Input
                                    id="vpn-endpoint"
                                    value={manual.peer.endpoint}
                                    onChange={(e) =>
                                        setManual({
                                            ...manual,
                                            peer: { ...manual.peer, endpoint: e.target.value },
                                        })
                                    }
                                    placeholder="vpn.example.com:51820"
                                    className="font-mono text-xs"
                                />
                            </div>
                        </div>

                        <div className="space-y-1.5">
                            <Label htmlFor="vpn-pub">Server public key</Label>
                            <Input
                                id="vpn-pub"
                                value={manual.peer.public_key}
                                onChange={(e) =>
                                    setManual({
                                        ...manual,
                                        peer: { ...manual.peer, public_key: e.target.value },
                                    })
                                }
                                className="font-mono text-xs"
                            />
                        </div>

                        <div className="grid gap-3 sm:grid-cols-2">
                            <div className="space-y-1.5">
                                <Label htmlFor="vpn-psk">Preshared key (optional)</Label>
                                <Input
                                    id="vpn-psk"
                                    value={manual.peer.preshared_key ?? ""}
                                    onChange={(e) =>
                                        setManual({
                                            ...manual,
                                            peer: { ...manual.peer, preshared_key: e.target.value },
                                        })
                                    }
                                    className="font-mono text-xs"
                                />
                            </div>
                            <div className="space-y-1.5">
                                <Label htmlFor="vpn-dns">Resolver inside the tunnel (optional)</Label>
                                <Input
                                    id="vpn-dns"
                                    value={manual.dns ?? ""}
                                    onChange={(e) => setManual({ ...manual, dns: e.target.value })}
                                    placeholder="1.1.1.1"
                                    className="font-mono text-xs"
                                />
                            </div>
                        </div>
                    </TabsContent>
                </Tabs>

                {error && (
                    <Alert variant="warning">
                        <AlertDescription>{error}</AlertDescription>
                    </Alert>
                )}
            </div>
        </ResponsiveDialog>
    )
}
