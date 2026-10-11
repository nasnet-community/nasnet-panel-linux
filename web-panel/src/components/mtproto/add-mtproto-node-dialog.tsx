import { useState } from "react"
import { Loader2 } from "lucide-react"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { toast } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { installMtprotoNode, probeMtprotoHost, type DisguiseResult, type InstallMtprotoBody, type MtprotoJob } from "@/lib/api/mtproto"
import { MtprotoJobDialog } from "./mtproto-job-dialog"
import { looksLikeDomain } from "./mtproto-labels"
import { CoverChecks } from "./overview/cover-site-card"

export interface AddMtprotoNodeDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    /** Called with the new node's id once the install has finished. */
    onInstalled: (nodeId: number) => void
    /** Back from the first step, e.g. to the node kind picker. */
    onBack?: () => void
}

type Step = "server" | "details"

/** Adds an MTProto node; the host key it answers with on first contact is pinned for every later login. */
export function AddMtprotoNodeDialog({ open, onOpenChange, onInstalled, onBack }: AddMtprotoNodeDialogProps) {
    const [step, setStep] = useState<Step>("server")
    const [host, setHost] = useState("")
    const [sshPort, setSshPort] = useState("22")
    const [sshUser, setSshUser] = useState("root")
    const [fingerprint, setFingerprint] = useState("")
    const [probing, setProbing] = useState(false)
    const [probeError, setProbeError] = useState("")

    const [name, setName] = useState("")
    const [domain, setDomain] = useState("")
    const [method, setMethod] = useState<"password" | "key">("password")
    const [password, setPassword] = useState("")
    const [key, setKey] = useState("")
    const [passphrase, setPassphrase] = useState("")
    const [hubPort, setHubPort] = useState("")
    const [capturePort, setCapturePort] = useState("")
    const [maxStreams, setMaxStreams] = useState("")
    const [advanced, setAdvanced] = useState(false)

    // the install request signs in and tests the cover site from the node before the job starts
    const [testing, setTesting] = useState(false)
    const [installError, setInstallError] = useState("")
    const [cover, setCover] = useState<{ domain: string; check: DisguiseResult } | null>(null)

    const [jobOpen, setJobOpen] = useState(false)
    const [installJob, setInstallJob] = useState<Promise<string> | null>(null)

    const portOk = /^\d{1,5}$/.test(sshPort) && Number(sshPort) >= 1 && Number(sshPort) <= 65535
    const secretOk = method === "password" ? password !== "" : key.trim() !== ""
    const detailsOk = name.trim() !== "" && looksLikeDomain(domain) && secretOk
    const coverFailed = cover?.domain === domain.trim().toLowerCase() ? cover.check : null

    function reset() {
        setStep("server")
        setHost("")
        setSshPort("22")
        setSshUser("root")
        setFingerprint("")
        setProbeError("")
        setName("")
        setDomain("")
        setPassword("")
        setKey("")
        setPassphrase("")
        setHubPort("")
        setCapturePort("")
        setMaxStreams("")
        setAdvanced(false)
        setInstallError("")
        setCover(null)
        setInstallJob(null)
    }

    async function probe() {
        setProbing(true)
        setProbeError("")
        try {
            const res = await probeMtprotoHost({ host: host.trim(), ssh_port: Number(sshPort), ssh_user: sshUser.trim() || "root" })
            if (!res.success || !res.data?.fingerprint) throw new Error(res.error || "The node did not answer")
            setFingerprint(res.data.fingerprint)
            if (!name) setName(host.trim())
            setStep("details")
        } catch (e) {
            setProbeError(e instanceof Error ? e.message : String(e))
        } finally {
            setProbing(false)
        }
    }

    async function install() {
        const b: InstallMtprotoBody = {
            name: name.trim(),
            host: host.trim(),
            ssh_port: Number(sshPort),
            ssh_user: sshUser.trim() || "root",
            host_key: fingerprint,
            disguise: domain.trim().toLowerCase(),
        }
        if (method === "password") b.ssh_password = password
        else {
            b.ssh_key = key
            if (passphrase) b.ssh_passphrase = passphrase
        }
        if (hubPort) b.hub_port = Number(hubPort)
        if (capturePort) b.capture_port = Number(capturePort)
        if (maxStreams) b.max_streams = Number(maxStreams)
        setTesting(true)
        setInstallError("")
        setCover(null)
        try {
            const res = await installMtprotoNode(b)
            if (!res.success || !res.data?.job_id) throw new Error(res.error || "Could not start the install")
            // the secrets left with the request and are not kept
            setPassword("")
            setKey("")
            setPassphrase("")
            setInstallJob(Promise.resolve(res.data.job_id))
            setJobOpen(true)
        } catch (e) {
            // a failed cover test keeps everything typed, the login too: only the site needs changing
            const check = e instanceof ApiError ? (e.body as { data?: { cover?: DisguiseResult } } | undefined)?.data?.cover : undefined
            if (check) setCover({ domain: b.disguise, check })
            else setInstallError(e instanceof Error ? e.message : String(e))
        } finally {
            setTesting(false)
        }
    }

    function finished(job: MtprotoJob) {
        if (job.state === "ok" && job.node_id) {
            reset()
            onOpenChange(false)
            onInstalled(job.node_id)
        } else if (job.node_id) {
            toast.warning("The node was added but is not connected yet", {
                description: "Open it to repair the connection or remove it.",
            })
        }
    }

    return (
        <>
            <Dialog
                open={open && !jobOpen}
                onOpenChange={(next) => {
                    // once the test passes the install starts, so it can't be walked away from mid-test
                    if (testing) return
                    if (!next) reset()
                    onOpenChange(next)
                }}
            >
                <DialogContent className="sm:max-w-lg">
                    <DialogHeader>
                        <DialogTitle>Add an MTProto node</DialogTitle>
                        <DialogDescription>
                            {step === "server" && "Where the node will run."}
                            {step === "details" && "How to sign in, and what to present."}
                        </DialogDescription>
                    </DialogHeader>

                    {step === "server" && (
                        <div className="space-y-4">
                            <div className="grid grid-cols-[1fr_6rem] gap-3">
                                <div className="space-y-1.5">
                                    <Label htmlFor="mtproto-host">Address</Label>
                                    <Input id="mtproto-host" placeholder="203.0.113.10 or vps.example.com" value={host} onChange={(e) => setHost(e.target.value)} />
                                </div>
                                <div className="space-y-1.5">
                                    <Label htmlFor="mtproto-ssh-port">SSH port</Label>
                                    <Input id="mtproto-ssh-port" inputMode="numeric" value={sshPort} onChange={(e) => setSshPort(e.target.value)} />
                                </div>
                            </div>
                            <div className="space-y-1.5">
                                <Label htmlFor="mtproto-ssh-user">User</Label>
                                <Input id="mtproto-ssh-user" value={sshUser} onChange={(e) => setSshUser(e.target.value)} autoComplete="off" />
                            </div>
                            {probeError && (
                                <p role="alert" className="text-sm text-red-500">{probeError}</p>
                            )}
                        </div>
                    )}

                    {step === "details" && (
                        <div className="space-y-4">
                            <div className="space-y-1.5">
                                <Label htmlFor="mtproto-name">Name</Label>
                                <Input id="mtproto-name" value={name} onChange={(e) => setName(e.target.value)} />
                            </div>
                            <div className="space-y-1.5">
                                <Label htmlFor="mtproto-domain">Cover site</Label>
                                <Input id="mtproto-domain" placeholder="www.example.ir" value={domain} onChange={(e) => setDomain(e.target.value)} spellCheck={false} aria-invalid={!!coverFailed} />
                                {coverFailed ? (
                                    <div className="space-y-2 pt-1">
                                        <p role="alert" className="text-sm text-red-500">
                                            {cover!.domain} doesn't work as a cover site from this node. Try another.
                                        </p>
                                        <CoverChecks d={coverFailed} />
                                        {coverFailed.err && <p className="text-xs text-muted-foreground">{coverFailed.err}</p>}
                                    </div>
                                ) : (
                                    <p className="text-xs text-muted-foreground">
                                        A real HTTPS site the node can reach. It is tested from the node before anything is installed.
                                    </p>
                                )}
                            </div>
                            <div className="flex gap-2" role="group" aria-label="Sign in with">
                                <Button type="button" size="sm" variant={method === "password" ? "default" : "outline"} onClick={() => setMethod("password")}>
                                    Password
                                </Button>
                                <Button type="button" size="sm" variant={method === "key" ? "default" : "outline"} onClick={() => setMethod("key")}>
                                    Private key
                                </Button>
                            </div>
                            {method === "password" ? (
                                <div className="space-y-1.5">
                                    <Label htmlFor="mtproto-password">Password</Label>
                                    <Input id="mtproto-password" type="password" autoComplete="off" value={password} onChange={(e) => setPassword(e.target.value)} />
                                    <p className="text-xs text-muted-foreground">Used once for the install. It is never stored.</p>
                                </div>
                            ) : (
                                <div className="space-y-3">
                                    <div className="space-y-1.5">
                                        <Label htmlFor="mtproto-key">Private key</Label>
                                        <Textarea id="mtproto-key" rows={5} spellCheck={false} className="font-mono text-xs" value={key} onChange={(e) => setKey(e.target.value)} />
                                    </div>
                                    <div className="space-y-1.5">
                                        <Label htmlFor="mtproto-passphrase">Passphrase (if the key has one)</Label>
                                        <Input id="mtproto-passphrase" type="password" autoComplete="off" value={passphrase} onChange={(e) => setPassphrase(e.target.value)} />
                                    </div>
                                </div>
                            )}
                            <button type="button" className="text-sm text-muted-foreground underline" onClick={() => setAdvanced((v) => !v)}>
                                {advanced ? "Hide" : "Show"} advanced
                            </button>
                            {advanced && (
                                <div className="grid grid-cols-3 gap-3">
                                    <div className="space-y-1.5">
                                        <Label htmlFor="mtproto-hub-port">Link port</Label>
                                        <Input id="mtproto-hub-port" inputMode="numeric" placeholder="8443" value={hubPort} onChange={(e) => setHubPort(e.target.value)} />
                                    </div>
                                    <div className="space-y-1.5">
                                        <Label htmlFor="mtproto-capture-port">Capture port</Label>
                                        <Input id="mtproto-capture-port" inputMode="numeric" placeholder="9443" value={capturePort} onChange={(e) => setCapturePort(e.target.value)} />
                                    </div>
                                    <div className="space-y-1.5">
                                        <Label htmlFor="mtproto-max-streams">Max streams</Label>
                                        <Input id="mtproto-max-streams" inputMode="numeric" placeholder="5000" value={maxStreams} onChange={(e) => setMaxStreams(e.target.value)} />
                                    </div>
                                </div>
                            )}
                            {installError && (
                                <p role="alert" className="text-sm text-red-500">{installError}</p>
                            )}
                        </div>
                    )}

                    <DialogFooter>
                        {step === "server" && onBack && (
                            <Button type="button" variant="ghost" onClick={() => { reset(); onBack() }}>
                                Back
                            </Button>
                        )}
                        {step !== "server" && (
                            <Button type="button" variant="ghost" disabled={testing} onClick={() => setStep("server")}>
                                Back
                            </Button>
                        )}
                        {step === "server" && (
                            <Button type="button" disabled={!host.trim() || !portOk || probing} onClick={probe}>
                                {probing && <Loader2 className="mr-2 size-4 animate-spin" />}
                                Check node
                            </Button>
                        )}
                        {step === "details" && (
                            <Button type="button" disabled={!detailsOk || testing} onClick={install}>
                                {testing && <Loader2 className="mr-2 size-4 animate-spin" />}
                                {testing ? "Testing the cover site…" : "Install"}
                            </Button>
                        )}
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <MtprotoJobDialog
                open={jobOpen}
                onOpenChange={(next) => {
                    setJobOpen(next)
                    if (!next) onOpenChange(false)
                }}
                title="Installing the MTProto node"
                description="This takes about a minute."
                job={installJob}
                onFinished={finished}
            />
        </>
    )
}
