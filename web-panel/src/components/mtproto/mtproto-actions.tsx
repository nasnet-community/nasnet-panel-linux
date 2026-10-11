import { useState, type ReactNode } from "react"
import { useNavigate } from "react-router"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { useRefreshNodes, useRestartMtprotoTelemt, useSetMtprotoProxyStopped } from "@/lib/queries"
import {
    changeMtprotoAddress,
    changeMtprotoLinkPort,
    removeMtprotoNode,
    repairMtprotoNode,
    rotateMtprotoKeys,
    updateMtprotoNode,
    type MtprotoJob,
    type MtprotoNode,
} from "@/lib/api/mtproto"
import type { Node } from "@/lib/types"
import { LinkPortPrompt } from "./link-port-prompt"
import { AddressPrompt } from "@/components/settings-page/address-prompt"
import { LoginPrompt, type SshLogin } from "./login-prompt"
import { MtprotoJobDialog } from "./mtproto-job-dialog"
import { startMtprotoJob } from "./mtproto-labels"

/** after runs only when the job succeeded. */
type JobSpec = { title: string; job: Promise<string>; after?: () => void }

async function jobId(p: Promise<{ success: boolean; data?: { job_id: string }; error?: string }>, what: string): Promise<string> {
    const res = await p
    if (!res.success || !res.data?.job_id) throw new Error(res.error || `Could not start: ${what}`)
    return res.data.job_id
}

export interface MtprotoActions {
    /** link-only actions are off while it is down */
    linkUp: boolean
    restartProxy: () => Promise<void>
    restarting: boolean
    /** Stop holds across restarts until Start */
    stopProxy: () => Promise<void>
    startProxy: () => void
    /** "stop" or "start" while one is on its way */
    toggling: "stop" | "start" | null
    updateAgent: () => Promise<void>
    updateProxy: () => Promise<void>
    rotateKeys: () => Promise<void>
    /** asks for the new port, then moves the link as a job */
    changeLinkPort: () => void
    /** asks for the new address, then moves the link there as a job */
    changeAddress: () => void
    repair: () => void
    remove: () => void
    /** render once wherever the hook is used */
    dialogs: ReactNode
}

/** Every maintenance action on an MTProto node, with its confirm, login and progress dialogs. */
export function useMtprotoActions(node: Node, r: MtprotoNode | undefined, onDone?: () => void): MtprotoActions {
    const navigate = useNavigate()
    const confirm = useConfirm()
    const restart = useRestartMtprotoTelemt(node.id)
    const stopped = useSetMtprotoProxyStopped(node.id)
    const refreshNodes = useRefreshNodes()
    const [job, setJob] = useState<JobSpec | null>(null)
    const [finished, setFinished] = useState<MtprotoJob | null>(null)
    const [login, setLogin] = useState<null | "repair" | "remove">(null)
    const [portOpen, setPortOpen] = useState(false)
    const [addrOpen, setAddrOpen] = useState(false)
    const linkUp = (r?.link?.up ?? 0) > 0

    const run = (spec: JobSpec) => {
        setFinished(null)
        setJob(spec)
    }

    // restarts, updates and rotations run on click; users only reconnect
    async function update(kind: "agent" | "telemt") {
        run({ title: kind === "agent" ? "Updating the agent" : "Updating the proxy", job: startMtprotoJob(() => jobId(updateMtprotoNode(node.id, kind), "update")) })
    }

    async function rotateKeys() {
        run({ title: "Rotating the link keys", job: startMtprotoJob(() => jobId(rotateMtprotoKeys(node.id), "rotation")) })
    }

    async function restartProxy() {
        restart.mutate()
    }

    async function stopProxy() {
        const users = r?.active_users ?? 0
        const ok = await confirm({
            title: `Stop the proxy on ${node.name}?`,
            description: `${users > 0 ? `The ${users} ${users === 1 ? "user" : "users"} connected now lose their connection, and n` : "N"}obody can connect until you start it again. It stays stopped across restarts.`,
            confirmLabel: "Stop proxy",
            variant: "destructive",
        })
        if (ok) stopped.mutate(true)
    }

    function onLogin(kind: "repair" | "remove", l: SshLogin) {
        setLogin(null)
        const { force, ...creds } = l
        if (kind === "repair") {
            run({ title: "Repairing the node", job: startMtprotoJob(() => jobId(repairMtprotoNode(node.id, creds), "repair")) })
            return
        }
        run({
            title: "Removing the node",
            job: startMtprotoJob(() => jobId(removeMtprotoNode(node.id, { ...creds, force }), "removal")),
            after: () => {
                navigate("/nodes")
                refreshNodes()
            },
        })
    }

    const dialogs = (
        <>
            {r && (
                <LinkPortPrompt
                    open={portOpen}
                    onOpenChange={setPortOpen}
                    r={r}
                    onSubmit={(port) => {
                        setPortOpen(false)
                        run({ title: `Moving the link to port ${port}`, job: startMtprotoJob(() => jobId(changeMtprotoLinkPort(node.id, port), "port change")) })
                    }}
                />
            )}
            <AddressPrompt
                open={addrOpen}
                onOpenChange={setAddrOpen}
                current={node.ip}
                description="The panel tries the new address first and moves the link only if the agent answers there."
                warning="Every user's proxy link contains this address, so after the change each user needs their new link."
                onSubmit={(address) => run({ title: `Moving the link to ${address}`, job: startMtprotoJob(() => jobId(changeMtprotoAddress(node.id, address), "address change")) })}
            />
            <LoginPrompt
                open={login === "repair"}
                onOpenChange={(o) => !o && setLogin(null)}
                title="Repair the node"
                description="Re-runs the install on the node and keeps its keys. Sign in to the node to do it."
                defaultUser={r?.ssh_user ?? "root"}
                confirmLabel="Repair"
                onSubmit={(l) => onLogin("repair", l)}
            />
            <LoginPrompt
                open={login === "remove"}
                onOpenChange={(o) => !o && setLogin(null)}
                title={`Remove ${node.name}`}
                description={linkUp
                    ? "The agent removes itself over the link. No login is needed."
                    : "The link is down, so the agent has to be removed over SSH."}
                defaultUser={r?.ssh_user ?? "root"}
                confirmLabel="Remove"
                allowForce
                optional={linkUp}
                onSubmit={(l) => onLogin("remove", l)}
            />
            {job && (
                <MtprotoJobDialog
                    open
                    onOpenChange={(o) => {
                        if (o) return
                        const after = finished?.state === "ok" ? job.after : undefined
                        setJob(null)
                        if (after) after()
                        else onDone?.()
                    }}
                    title={job.title}
                    job={job.job}
                    onFinished={setFinished}
                />
            )}
        </>
    )

    return {
        linkUp,
        restartProxy,
        restarting: restart.isPending,
        stopProxy,
        startProxy: () => stopped.mutate(false),
        toggling: stopped.isPending ? (stopped.variables ? "stop" : "start") : null,
        updateAgent: () => update("agent"),
        updateProxy: () => update("telemt"),
        rotateKeys,
        changeLinkPort: () => setPortOpen(true),
        changeAddress: () => setAddrOpen(true),
        repair: () => setLogin("repair"),
        remove: () => setLogin("remove"),
        dialogs,
    }
}
