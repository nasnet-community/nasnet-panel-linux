import { useEffect, useId, useRef, useState, type ReactNode } from "react"
import { ArrowLeft, Check, ChevronRight, Loader2, ShieldCheck, TriangleAlert } from "lucide-react"
import { useQueryClient } from "@tanstack/react-query"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
    Sheet,
    SheetContent,
    SheetDescription,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet"
import {
    applyNetworkChange,
    confirmNetworkApply,
    getNetworkState,
    planNetworkChange,
    remainingSeconds,
    rollbackNetworkApply,
} from "@/lib/api/network"
import { ApiError } from "@/lib/api"
import { queryKeys } from "@/lib/queries/keys"
import {
    defaultWAN,
    describeWAN,
    normalizeWAN,
    recoveryURL,
    savedWAN,
    validateWAN,
} from "@/lib/wan-config"
import { cn } from "@/lib/utils"
import type {
    AssignRoleRequest,
    NetworkApply,
    NetworkInterfaceView,
    NetworkPlan,
    UplinkView,
    WANConfig,
} from "@/lib/types/network"

type Step =
    | "edit"
    | "review"
    | "applying"
    | "pending"
    | "recovering"
    | "kept"
    | "reverted"
    | "failed"

export interface WANEditorTarget {
    iface: NetworkInterfaceView
    request: AssignRoleRequest
}

function Field({
    id,
    label,
    error,
    children,
}: {
    id: string
    label: string
    error?: string
    children: ReactNode
}) {
    return (
        <div className="min-w-0 space-y-2">
            <label htmlFor={id} className="text-sm font-medium">
                {label}
            </label>
            {children}
            {error && (
                <p id={`${id}-error`} role="alert" className="text-status-danger text-xs">
                    {error}
                </p>
            )}
        </div>
    )
}

/** Mounted per edit, so cancel discards the draft and reopening reads saved intent. */
export function WANConfigSheet({
    target,
    live,
    onClose,
}: {
    target: WANEditorTarget
    live?: UplinkView
    onClose: () => void
}) {
    const { iface, request } = target
    const qc = useQueryClient()
    const id = useId()
    const initial = useRef(savedWAN(iface, request.slot)).current
    const original = useRef(iface.wan ?? defaultWAN(iface.slot)).current
    const [draft, setDraft] = useState<WANConfig>(initial)
    const [step, setStep] = useState<Step>("edit")
    const [errors, setErrors] = useState<Record<string, string>>({})
    const [plan, setPlan] = useState<NetworkPlan | null>(null)
    const [busy, setBusy] = useState(false)
    const [message, setMessage] = useState("")
    const [typed, setTyped] = useState("")
    const [applied, setApplied] = useState<NetworkApply | null>(null)
    const [now, setNow] = useState(Date.now())
    const requestID = useRef("")
    const alive = useRef(true)
    useEffect(() => {
        alive.current = true
        return () => {
            alive.current = false
        }
    }, [])
    const active = ["applying", "pending", "recovering"].includes(step)
    const final = ["kept", "reverted", "failed"].includes(step)
    const domestic = request.slot.startsWith("domestic")
    const normalized = normalizeWAN(draft)
    const changed =
        iface.role !== request.role ||
        iface.slot !== request.slot ||
        JSON.stringify(normalized) !== JSON.stringify(normalizeWAN(original))
    const left = applied ? remainingSeconds(applied.confirm_deadline_unix) : 0
    const alternate = recoveryURL(normalized, window.location.href)
    const confirmationRequired = plan?.verdicts.some((v) => v.level === "confirm")
    const rejected = plan?.verdicts.some((v) => v.level === "reject")
    const refresh = () => void qc.invalidateQueries({ queryKey: queryKeys.network })

    // Poll the operation identity after a lost POST response, and verify the
    // outcome after expiry. A local countdown alone cannot prove rollback.
    useEffect(() => {
        if (!active) return
        let stopped = false
        let timer: ReturnType<typeof setTimeout>
        const poll = async () => {
            try {
                const res = await getNetworkState()
                if (stopped || !res.success || !res.data) return
                const state = res.data
                if (state.pending_request_id === requestID.current && state.pending_plan_id) {
                    setApplied({
                        plan_id: state.pending_plan_id,
                        confirm_deadline_unix: state.confirm_deadline_unix,
                        ops: [],
                    })
                    if (
                        state.last_apply_phase === "applied" &&
                        state.last_apply_request_id === requestID.current
                    ) {
                        setStep(
                            remainingSeconds(state.confirm_deadline_unix) > 0
                                ? "pending"
                                : "recovering",
                        )
                        setMessage("")
                    }
                } else if (state.last_apply_request_id === requestID.current) {
                    if (state.last_apply_phase === "confirmed") {
                        setStep("kept")
                        refresh()
                    } else if (state.last_apply_phase === "rolled_back") {
                        setStep("reverted")
                        refresh()
                    } else if (state.last_apply_phase === "failed") {
                        setStep("failed")
                        setMessage(
                            state.last_apply_error || "The router could not apply this change.",
                        )
                        refresh()
                    }
                }
            } catch {
                /* Keep the recovery view through a temporary disconnect. */
            } finally {
                if (!stopped) timer = setTimeout(poll, 2000)
            }
        }
        void poll()
        const clock = setInterval(() => setNow(Date.now()), 1000)
        return () => {
            stopped = true
            clearTimeout(timer)
            clearInterval(clock)
        }
    }, [active, qc])

    useEffect(() => {
        if (step === "pending" && applied && remainingSeconds(applied.confirm_deadline_unix) === 0)
            setStep("recovering")
    }, [now, step, applied])

    const update = (patch: Partial<WANConfig>) => {
        setDraft((d) => ({ ...d, ...patch }))
        setErrors({})
        setMessage("")
    }
    const inputProps = (field: string) => ({
        id: `${id}-${field}`,
        "aria-invalid": !!errors[field],
        "aria-describedby": errors[field] ? `${id}-${field}-error` : undefined,
    })
    const [address = "", prefix = ""] = draft.static_address.split("/")

    async function review() {
        const findings = validateWAN(draft)
        setErrors(findings)
        if (Object.keys(findings).length) return
        setBusy(true)
        setMessage("")
        try {
            const result = await planNetworkChange({ ...request, wan: normalized })
            if (!result.success || !result.data)
                throw new Error(result.error || "Could not review this change.")
            setPlan(result.data)
            setStep("review")
        } catch (e) {
            setMessage(e instanceof Error ? e.message : "Could not reach the router. Try again.")
        } finally {
            setBusy(false)
        }
    }

    async function apply() {
        requestID.current = crypto.randomUUID()
        setMessage("")
        setStep("applying")
        setBusy(true)
        try {
            const result = await applyNetworkChange({
                ...request,
                wan: normalized,
                confirmed: typed === "CONFIRM",
                request_id: requestID.current,
            })
            if (!result.success || !result.data)
                throw new Error(result.error || "No apply response received.")
            if (!alive.current) return
            setApplied(result.data)
            setStep("pending")
            refresh()
        } catch (e) {
            if (!alive.current) return
            if (e instanceof ApiError && (e.status === 400 || e.status === 409)) {
                setStep("review")
                setMessage(e.message)
            } else {
                setStep("recovering")
                setMessage(
                    "The apply response was interrupted. Checking the router for the result. Keep this page open or reconnect using the address below.",
                )
            }
        } finally {
            if (alive.current) setBusy(false)
        }
    }

    async function settle(keep: boolean) {
        if (!applied) return
        setBusy(true)
        setMessage("")
        try {
            const result = await (keep
                ? confirmNetworkApply(applied.plan_id)
                : rollbackNetworkApply(applied.plan_id))
            if (!result.success) throw new Error(result.error || "Could not reach the router.")
            if (alive.current) {
                setStep(keep ? "kept" : "reverted")
                refresh()
            }
        } catch (e) {
            if (alive.current)
                setMessage(
                    `${e instanceof Error ? e.message : "Connection interrupted."} Checking the router; automatic recovery remains active.`,
                )
        } finally {
            if (alive.current) setBusy(false)
        }
    }

    const close = () => {
        if (step !== "applying" && !busy) onClose()
    }
    const before = describeWAN(normalizeWAN(original))
    const after = describeWAN(normalized)
    const changes = Object.keys(after).filter(
        (key) => before[key] !== after[key] || iface.role !== "wan",
    )
    const title =
        step === "edit"
            ? "Configure WAN"
            : step === "review"
              ? "Review WAN changes"
              : step === "kept"
                ? "WAN settings kept"
                : step === "reverted"
                  ? "Previous settings restored"
                  : step === "failed"
                    ? "Change needs attention"
                    : step === "pending"
                      ? "Keep this connection?"
                      : step === "recovering"
                        ? "Checking connection"
                        : "Applying WAN settings"

    return (
        <Sheet
            open
            onOpenChange={(open) => {
                if (!open) close()
            }}
        >
            <SheetContent
                data-wan-editor={iface.if_name}
                className="w-full gap-0 sm:max-w-xl"
                onEscapeKeyDown={(e) => {
                    if (step === "applying" || busy) e.preventDefault()
                }}
            >
                <SheetHeader className="border-border-subtle shrink-0 border-b p-5 pr-10 sm:p-6">
                    <p className="text-text-tertiary mb-2 flex flex-wrap items-center gap-1.5 text-xs">
                        Router <ChevronRight className="size-3" />{" "}
                        {iface.label || (domestic ? "Domestic WAN" : "Secondary WAN")}{" "}
                        <span className="font-mono">{iface.if_name}</span>
                    </p>
                    <SheetTitle className="text-xl tracking-tight">{title}</SheetTitle>
                    <SheetDescription>
                        {step === "edit"
                            ? "Set how this connection gets its address."
                            : step === "review"
                              ? "Only this WAN will change. Check the details before applying."
                              : step === "kept"
                                ? "Your new settings are saved and confirmed."
                                : step === "reverted"
                                  ? "The router restored your previous WAN configuration."
                                  : step === "failed"
                                    ? "The router reported a problem with this change."
                                    : "The router restores the previous settings unless you confirm within 90 seconds."}
                    </SheetDescription>
                </SheetHeader>

                <div className="min-h-0 flex-1 overflow-y-auto p-5 sm:p-6">
                    {message && (
                        <div
                            role="alert"
                            className="bg-status-warning-soft text-text-primary mb-5 rounded-lg p-3 text-sm break-words"
                        >
                            {message}
                        </div>
                    )}
                    {step === "edit" && (
                        <form
                            id={`${id}-form`}
                            onSubmit={(e) => {
                                e.preventDefault()
                                void review()
                            }}
                            className="space-y-6"
                        >
                            <div className="bg-surface-2 flex flex-wrap items-center justify-between gap-3 rounded-lg p-3.5">
                                <div>
                                    <p className="text-text-secondary text-xs">
                                        Current connection ·{" "}
                                        {original.method === "static"
                                            ? "Static IPv4"
                                            : original.method === "rawip"
                                              ? "Cellular"
                                              : "DHCP"}
                                    </p>
                                    <p className="mt-1 font-mono text-sm break-all">
                                        {(live?.addrs ?? iface.addrs)?.filter(
                                            (a) => !a.includes(":"),
                                        )[0] || "Waiting for an address"}
                                    </p>
                                    {live?.gateway && (
                                        <p className="text-text-tertiary mt-1 text-xs">
                                            Gateway {live.gateway}
                                        </p>
                                    )}
                                </div>
                                <span
                                    className={cn(
                                        "rounded-full px-2.5 py-1 text-xs font-medium",
                                        live?.healthy || iface.healthy
                                            ? "bg-status-success-soft text-status-success"
                                            : "bg-surface-3 text-text-secondary",
                                    )}
                                >
                                    {live?.healthy || iface.healthy ? "Connected" : "Checking"}
                                </span>
                            </div>
                            <fieldset className="space-y-4" disabled={busy}>
                                <legend className="mb-3 text-sm font-semibold">IP address</legend>
                                <div className="border-border-subtle bg-surface-2 grid grid-cols-2 gap-1 rounded-lg border p-1">
                                    {(
                                        [
                                            ["dhcp4", "Automatic (DHCP)"],
                                            ["static", "Static IPv4"],
                                        ] as const
                                    ).map(([method, label]) => (
                                        <label
                                            key={method}
                                            className={cn(
                                                "relative cursor-pointer rounded-md px-2 py-2.5 text-center text-sm transition-colors",
                                                draft.method === method
                                                    ? "bg-surface-1 text-text-primary shadow-sm"
                                                    : "text-text-secondary",
                                            )}
                                        >
                                            <input
                                                type="radio"
                                                name={`${id}-method`}
                                                value={method}
                                                checked={draft.method === method}
                                                onChange={() => update({ method })}
                                                className="peer sr-only"
                                            />
                                            <span className="peer-focus-visible:outline-ring rounded-sm peer-focus-visible:outline-2 peer-focus-visible:outline-offset-4">
                                                {label}
                                            </span>
                                        </label>
                                    ))}
                                </div>
                                <p className="text-text-secondary text-xs">
                                    {draft.method === "dhcp4"
                                        ? "The network supplies this WAN's address and gateway. Your DNS policy stays in place."
                                        : "Use the address and gateway assigned for this connection."}
                                </p>
                                {draft.method === "static" && (
                                    <>
                                        <div className="grid grid-cols-[minmax(0,1fr)_7rem] gap-3">
                                            <Field
                                                id={`${id}-address`}
                                                label="IPv4 address"
                                                error={errors.address}
                                            >
                                                <Input
                                                    {...inputProps("address")}
                                                    inputMode="decimal"
                                                    autoComplete="off"
                                                    value={address}
                                                    placeholder="10.0.2.20"
                                                    onChange={(e) =>
                                                        update({
                                                            static_address: `${e.target.value}/${prefix}`,
                                                        })
                                                    }
                                                />
                                            </Field>
                                            <Field
                                                id={`${id}-prefix`}
                                                label="Prefix length"
                                                error={errors.prefix}
                                            >
                                                <Input
                                                    {...inputProps("prefix")}
                                                    inputMode="numeric"
                                                    value={prefix}
                                                    placeholder="24"
                                                    onChange={(e) =>
                                                        update({
                                                            static_address: `${address}/${e.target.value}`,
                                                        })
                                                    }
                                                />
                                            </Field>
                                        </div>
                                        <Field
                                            id={`${id}-gateway`}
                                            label="Gateway"
                                            error={errors.gateway}
                                        >
                                            <Input
                                                {...inputProps("gateway")}
                                                inputMode="decimal"
                                                autoComplete="off"
                                                value={draft.static_gateway}
                                                placeholder="10.0.2.1"
                                                onChange={(e) =>
                                                    update({ static_gateway: e.target.value })
                                                }
                                            />
                                        </Field>
                                        <details
                                            className="text-sm"
                                            open={draft.gateway_on_link || undefined}
                                        >
                                            <summary className="text-text-secondary cursor-pointer py-1">
                                                Advanced addressing
                                            </summary>
                                            <label className="mt-3 flex items-start gap-2.5">
                                                <input
                                                    className="mt-1"
                                                    type="checkbox"
                                                    checked={draft.gateway_on_link}
                                                    onChange={(e) =>
                                                        update({
                                                            gateway_on_link: e.target.checked,
                                                        })
                                                    }
                                                />
                                                <span>
                                                    Gateway outside this subnet
                                                    <span className="text-text-tertiary mt-1 block text-xs leading-relaxed">
                                                        For ISP setups including /32 addresses. The
                                                        gateway must be directly reachable on this
                                                        link. Enable only when required by your ISP.
                                                    </span>
                                                </span>
                                            </label>
                                        </details>
                                    </>
                                )}
                            </fieldset>
                            <fieldset
                                className="border-border-subtle space-y-4 border-t pt-5"
                                disabled={busy}
                            >
                                <legend className="float-left mb-4 w-full text-sm font-semibold">
                                    DNS
                                </legend>
                                {domestic ? (
                                    <>
                                        <Field id={`${id}-dns-mode`} label="Domestic name lookups">
                                            <select
                                                id={`${id}-dns-mode`}
                                                className="border-input bg-background focus-visible:ring-ring h-10 w-full rounded-md border px-3 text-sm focus-visible:ring-2"
                                                value={draft.dns_mode}
                                                onChange={(e) =>
                                                    update({
                                                        dns_mode: e.target
                                                            .value as WANConfig["dns_mode"],
                                                    })
                                                }
                                            >
                                                <option value="default">Use router defaults</option>
                                                <option value="custom">Use custom servers</option>
                                            </select>
                                        </Field>
                                        {draft.dns_mode === "custom" && (
                                            <div className="grid gap-4 sm:grid-cols-2">
                                                {[0, 1].map((n) => (
                                                    <Field
                                                        key={n}
                                                        id={`${id}-dns${n + 1}`}
                                                        label={`DNS server ${n + 1}${n ? " (optional)" : ""}`}
                                                        error={errors[`dns${n + 1}`]}
                                                    >
                                                        <Input
                                                            {...inputProps(`dns${n + 1}`)}
                                                            inputMode="decimal"
                                                            autoComplete="off"
                                                            value={draft.dns_servers[n] ?? ""}
                                                            onChange={(e) => {
                                                                const servers = [
                                                                    draft.dns_servers[0] ?? "",
                                                                    draft.dns_servers[1] ?? "",
                                                                ]
                                                                servers[n] = e.target.value
                                                                update({ dns_servers: servers })
                                                            }}
                                                        />
                                                    </Field>
                                                ))}
                                            </div>
                                        )}
                                        <p className="text-text-secondary text-xs leading-relaxed">
                                            Domestic lookups use this WAN. Foreign lookups stay
                                            inside the VPN.
                                            {draft.dns_mode === "custom"
                                                ? " Servers are selected automatically; their order does not set a strict priority."
                                                : " Default resolver: 217.218.127.127. ISP-provided DNS is not used."}
                                        </p>
                                    </>
                                ) : (
                                    <div className="bg-surface-2 flex gap-3 rounded-lg p-4">
                                        <ShieldCheck className="text-text-secondary size-5 shrink-0" />
                                        <div>
                                            <p className="text-sm font-medium">
                                                Managed through the VPN
                                            </p>
                                            <p className="text-text-secondary mt-1 text-xs leading-relaxed">
                                                Foreign DNS stays inside the VPN. This secondary WAN
                                                does not use custom or ISP-provided DNS.
                                            </p>
                                        </div>
                                    </div>
                                )}
                            </fieldset>
                        </form>
                    )}

                    {step === "review" && (
                        <div className="space-y-5">
                            <div className="border-border-subtle overflow-hidden rounded-lg border">
                                <div className="bg-surface-2 grid grid-cols-2 gap-4 px-4 py-3 text-xs font-medium">
                                    <span>Saved settings</span>
                                    <span>After applying</span>
                                </div>
                                {iface.role !== request.role || iface.slot !== request.slot ? (
                                    <div className="border-border-subtle border-t p-4">
                                        <p className="text-text-tertiary mb-2 text-xs">
                                            WAN assignment
                                        </p>
                                        <div className="grid grid-cols-2 gap-4 text-sm">
                                            <span>
                                                {iface.role === "wan" ? iface.slot : "Unassigned"}
                                            </span>
                                            <span className="font-medium">{request.slot}</span>
                                        </div>
                                    </div>
                                ) : null}
                                {changes.map((key) => (
                                    <div key={key} className="border-border-subtle border-t p-4">
                                        <p className="text-text-tertiary mb-2 text-xs">{key}</p>
                                        <div className="grid grid-cols-2 gap-4 text-sm break-words">
                                            <span className="text-text-secondary">
                                                {before[key]}
                                            </span>
                                            <span className="font-medium">{after[key]}</span>
                                        </div>
                                    </div>
                                ))}
                            </div>
                            {request.evict_id && (
                                <p className="text-status-warning text-sm">
                                    This assignment also removes the interface currently holding
                                    this role.
                                </p>
                            )}
                            <div className="bg-surface-2 flex gap-3 rounded-lg p-4">
                                <ShieldCheck className="text-text-secondary size-5 shrink-0" />
                                <p className="text-text-secondary text-sm">
                                    Your connection may pause. Reconnect, then keep these settings
                                    within 90 seconds. Otherwise the router restores the previous
                                    configuration.
                                </p>
                            </div>
                            {plan?.verdicts.map((v, n) => (
                                <p
                                    role={v.level === "reject" ? "alert" : undefined}
                                    key={n}
                                    className={cn(
                                        "rounded-md p-3 text-sm",
                                        v.level === "reject"
                                            ? "bg-status-danger-soft text-status-danger"
                                            : "bg-status-warning-soft text-text-primary",
                                    )}
                                >
                                    {v.message}
                                </p>
                            ))}
                            {confirmationRequired && (
                                <Field
                                    id={`${id}-confirm`}
                                    label="Type CONFIRM to acknowledge the warning"
                                >
                                    <Input
                                        id={`${id}-confirm`}
                                        autoComplete="off"
                                        value={typed}
                                        onChange={(e) => setTyped(e.target.value)}
                                    />
                                </Field>
                            )}
                            <details className="text-text-tertiary text-xs">
                                <summary className="cursor-pointer">Technical changes</summary>
                                <ul className="mt-2 list-disc space-y-1 pl-4">
                                    {plan?.ops.map((op, n) => (
                                        <li key={n}>{op}</li>
                                    ))}
                                </ul>
                            </details>
                        </div>
                    )}

                    {active && (
                        <div className="space-y-6 py-3">
                            <div className="text-center">
                                <div className="bg-surface-2 mx-auto mb-5 flex size-14 items-center justify-center rounded-full">
                                    {step === "pending" ? (
                                        <ShieldCheck className="size-6" />
                                    ) : (
                                        <Loader2 className="size-6 animate-spin motion-reduce:animate-none" />
                                    )}
                                </div>
                                <p className="text-lg font-medium" role="status">
                                    {step === "pending"
                                        ? "The router is reachable"
                                        : "Waiting for the router"}
                                </p>
                                <p className="text-text-secondary mx-auto mt-2 max-w-sm text-sm leading-relaxed">
                                    {step === "pending"
                                        ? "Check that this connection works, then keep the settings."
                                        : "The connection may pause while the address changes. We will verify the result when the router responds."}
                                </p>
                            </div>
                            {applied && (
                                <div className="bg-surface-2 rounded-lg p-4 text-center">
                                    <p className="font-mono text-3xl tabular-nums">
                                        {left > 0 ? `${left}s` : "Checking…"}
                                    </p>
                                    <p className="text-text-secondary mt-1 text-xs">
                                        {left > 0
                                            ? "until automatic recovery"
                                            : "The confirmation window ended. Waiting for the router to report recovery."}
                                    </p>
                                </div>
                            )}
                            {alternate && (
                                <div className="border-border-subtle rounded-lg border p-4">
                                    <p className="text-sm font-medium">
                                        Reconnecting after an address change
                                    </p>
                                    <a
                                        className="text-primary mt-2 block text-sm break-all underline"
                                        href={alternate}
                                        target="_blank"
                                        rel="noreferrer"
                                    >
                                        Open panel at the new address
                                    </a>
                                    <p className="text-text-tertiary mt-2 text-xs">
                                        You may need to sign in again. The confirmation bar will
                                        appear there. You can also reconnect through your LAN or
                                        management port.
                                    </p>
                                </div>
                            )}
                        </div>
                    )}

                    {final && (
                        <div className="py-8 text-center">
                            <div className="bg-surface-2 mx-auto mb-5 flex size-14 items-center justify-center rounded-full">
                                {step === "failed" ? (
                                    <TriangleAlert className="text-status-warning size-6" />
                                ) : (
                                    <Check className="text-status-success size-6" />
                                )}
                            </div>
                            <p className="text-text-secondary text-sm">
                                {step === "kept"
                                    ? "This WAN now uses the new settings."
                                    : step === "reverted"
                                      ? "The router confirmed that the previous configuration was restored."
                                      : "Review the router's reported error before trying again."}
                            </p>
                        </div>
                    )}
                </div>

                <div className="border-border-subtle bg-background flex shrink-0 flex-wrap items-center justify-between gap-3 border-t p-4 sm:px-6">
                    <span className="text-text-tertiary text-xs">
                        {step === "edit"
                            ? "Nothing applied yet"
                            : step === "review"
                              ? "Ready to apply to one WAN"
                              : active
                                ? applied
                                    ? "Automatic recovery active"
                                    : "Checking apply status"
                                : "Router confirmed the result"}
                    </span>
                    <div className="ml-auto flex gap-2">
                        {step === "edit" && (
                            <>
                                <Button variant="outline" disabled={busy} onClick={close}>
                                    Cancel
                                </Button>
                                <Button
                                    type="submit"
                                    form={`${id}-form`}
                                    disabled={busy || !changed}
                                >
                                    {busy ? "Checking…" : "Review changes"}
                                </Button>
                            </>
                        )}
                        {step === "review" && (
                            <>
                                <Button
                                    variant="outline"
                                    onClick={() => {
                                        setStep("edit")
                                        setMessage("")
                                    }}
                                >
                                    <ArrowLeft className="size-3.5" /> Edit
                                </Button>
                                <Button
                                    disabled={
                                        rejected || (confirmationRequired && typed !== "CONFIRM")
                                    }
                                    onClick={() => void apply()}
                                >
                                    Apply changes
                                </Button>
                            </>
                        )}
                        {step === "pending" && (
                            <>
                                <Button
                                    variant="outline"
                                    disabled={busy || left <= 0}
                                    onClick={() => void settle(false)}
                                >
                                    Revert now
                                </Button>
                                <Button
                                    disabled={busy || left <= 0}
                                    onClick={() => void settle(true)}
                                >
                                    {busy ? "Please wait…" : "Keep settings"}
                                </Button>
                            </>
                        )}
                        {step === "recovering" && !busy && (
                            <Button variant="outline" onClick={onClose}>
                                Back to router
                            </Button>
                        )}
                        {final && <Button onClick={onClose}>Done</Button>}
                    </div>
                </div>
            </SheetContent>
        </Sheet>
    )
}
