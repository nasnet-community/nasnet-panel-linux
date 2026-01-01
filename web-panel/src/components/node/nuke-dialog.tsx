import { useState, useRef, useEffect, useCallback } from "react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import { NukeProgress } from "@/components/node/nuke-progress"
import { nukeNode, type NukePhaseResult, type NukeReport } from "@/lib/api/nuke"
import { cn } from "@/lib/utils"
import {
  AlertTriangleIcon,
  ArrowRightIcon,
  ArrowLeftIcon,
  ZapIcon,
  EyeIcon,
  CheckCircle2Icon,
  XCircleIcon,
  AlertCircleIcon,
  ClockIcon,
  Loader2Icon,
} from "lucide-react"

export interface NukeDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  nodeId: number
  nodeName: string
  plannedPhases: string[]
}

type Step = "preview" | "confirm" | "running" | "done"

function formatMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

type ResultLabel = "SUCCESS" | "PARTIAL" | "FAILED"

function resultLabel(r: NukeReport): ResultLabel {
  if (r.result === "NUKE_RESULT_SUCCESS") return "SUCCESS"
  if (r.result === "NUKE_RESULT_PARTIAL") return "PARTIAL"
  return "FAILED"
}

// ──────────────────────────────────────────────────────────────────────────────
// Sub-components
// ──────────────────────────────────────────────────────────────────────────────

function ModalShell({
  accent,
  children,
  className,
}: {
  accent: "neutral" | "red"
  children: React.ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        "relative flex min-h-0 flex-col overflow-hidden",
        // Left accent bar
        "before:pointer-events-none before:absolute before:left-0 before:top-0 before:h-full before:w-[3px] before:rounded-l-[inherit]",
        accent === "red"
          ? "before:bg-red-500"
          : "before:bg-amber-500/70",
        className,
      )}
    >
      {children}
    </div>
  )
}

function StepBadge({ step }: { step: Step }) {
  const labels: Record<Step, string> = {
    preview: "01 / PREVIEW",
    confirm: "02 / CONFIRM",
    running: "EXECUTING",
    done: "COMPLETE",
  }
  const colors: Record<Step, string> = {
    preview: "text-amber-500 border-amber-500/40 bg-amber-500/8",
    confirm: "text-red-400 border-red-500/40 bg-red-500/8",
    running: "text-amber-400 border-amber-500/40 bg-amber-500/8",
    done: "text-emerald-400 border-emerald-500/40 bg-emerald-500/8",
  }
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-sm border px-1.5 py-0.5 font-mono text-[10px] tracking-widest",
        colors[step],
      )}
    >
      {labels[step]}
    </span>
  )
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <p className="mb-2 font-mono text-[10px] tracking-widest text-muted-foreground/60 uppercase">
      {children}
    </p>
  )
}

function PreviewStat({
  label,
  value,
}: {
  label: string
  value: React.ReactNode
}) {
  return (
    <div className="flex items-baseline justify-between gap-4">
      <span className="font-mono text-xs text-muted-foreground">{label}</span>
      <span className="font-mono text-xs font-semibold tabular-nums text-foreground">
        {value}
      </span>
    </div>
  )
}

// ──────────────────────────────────────────────────────────────────────────────
// Main component
// ──────────────────────────────────────────────────────────────────────────────

export function NukeDialog({
  open,
  onOpenChange,
  nodeId,
  nodeName,
  plannedPhases,
}: NukeDialogProps) {
  const [step, setStep] = useState<Step>("preview")
  const [shredRoot, setShredRoot] = useState(false)
  const [keepRecord, setKeepRecord] = useState(false)
  const [typed, setTyped] = useState("")
  const [received, setReceived] = useState<NukePhaseResult[]>([])
  const [currentPhase, setCurrentPhase] = useState<string | undefined>()
  const [finalReport, setFinalReport] = useState<NukeReport | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [previewReport, setPreviewReport] = useState<NukeReport | null>(null)

  const abortRef = useRef<AbortController | null>(null)

  // Reset when dialog closes
  useEffect(() => {
    if (!open) {
      abortRef.current?.abort()
      abortRef.current = null
      setStep("preview")
      setShredRoot(false)
      setKeepRecord(false)
      setTyped("")
      setReceived([])
      setCurrentPhase(undefined)
      setFinalReport(null)
      setError(null)
      setPreviewing(false)
      setPreviewReport(null)
    }
  }, [open])

  const runPreview = useCallback(async () => {
    setPreviewing(true)
    setPreviewReport(null)
    setError(null)
    const results: NukePhaseResult[] = []

    const ac = new AbortController()
    abortRef.current = ac

    try {
      await nukeNode(
        nodeId,
        { dryRun: true, shredRoot, keepHubRecord: keepRecord },
        {
          onPhase: (p) => {
            results.push(p)
            setReceived([...results])
            setCurrentPhase(p.phase)
          },
          onDone: (r) => {
            setPreviewReport(r)
            setCurrentPhase(undefined)
          },
          onError: (e) => {
            if (!ac.signal.aborted) setError(e.message)
          },
        },
        ac.signal,
      )
    } catch (e) {
      if (!ac.signal.aborted) {
        setError(e instanceof Error ? e.message : String(e))
      }
    } finally {
      setPreviewing(false)
    }
  }, [nodeId, shredRoot, keepRecord])

  const runNuke = useCallback(async () => {
    setStep("running")
    setReceived([])
    setCurrentPhase(undefined)
    setFinalReport(null)
    setError(null)

    const ac = new AbortController()
    abortRef.current = ac
    const results: NukePhaseResult[] = []

    try {
      await nukeNode(
        nodeId,
        { dryRun: false, shredRoot, keepHubRecord: keepRecord },
        {
          onPhase: (p) => {
            results.push(p)
            setReceived([...results])
            setCurrentPhase(p.phase)
          },
          onDone: (r) => {
            setFinalReport(r)
            setCurrentPhase(undefined)
            setStep("done")
          },
          onError: (e) => {
            setError(e.message)
            setCurrentPhase(undefined)
            setStep("done")
          },
        },
        ac.signal,
      )
    } catch (e) {
      if (!ac.signal.aborted) {
        setError(e instanceof Error ? e.message : String(e))
        setCurrentPhase(undefined)
        setStep("done")
      }
    }
  }, [nodeId, shredRoot, keepRecord])

  const canExecute = typed === `NUKE ${nodeName}`

  // Derived preview stats
  const previewTotalFiles = previewReport?.phases.reduce(
    (s, p) => s + p.files_removed,
    0,
  ) ?? 0
  const previewTotalBytes = previewReport?.phases.reduce(
    (s, p) => s + p.bytes_removed,
    0,
  ) ?? 0
  const previewFailedPhases =
    previewReport?.phases.filter((p) => !p.ok).length ?? 0

  function handleClose() {
    if (step === "running") return // block close while running
    onOpenChange(false)
  }

  // ────────────────────────────────────────────────────────────────────────────
  // Render step content
  // ────────────────────────────────────────────────────────────────────────────

  const stepContent = {
    // ── Step 1: Preview ───────────────────────────────────────────────────────
    preview: (
      <ModalShell accent="neutral" className="gap-5 p-6 pl-8">
        {/* Header */}
        <div className="flex items-start justify-between gap-3">
          <div className="space-y-1.5">
            <div className="flex items-center gap-2.5">
              <ZapIcon className="size-4 text-amber-500" />
              <DialogTitle className="font-mono text-sm font-semibold tracking-tight text-foreground">
                Node Nuke
              </DialogTitle>
              <StepBadge step="preview" />
            </div>
            <DialogDescription className="font-mono text-xs text-muted-foreground">
              Reviewing destruction plan for{" "}
              <span className="font-semibold text-foreground">{nodeName}</span>
            </DialogDescription>
          </div>
        </div>

        {/* Phase list */}
        <div>
          <SectionLabel>Execution phases</SectionLabel>
          <div className="rounded-md border border-border/50 bg-muted/30 p-3">
            <NukeProgress
              plannedPhases={plannedPhases}
              received={received}
              currentPhase={previewing ? currentPhase : undefined}
              finished={!!previewReport && !previewing}
            />
          </div>
        </div>

        {/* Preview stats — shown after dry-run */}
        {previewReport && !previewing && (
          <div>
            <SectionLabel>Projected impact</SectionLabel>
            <div className="space-y-1 rounded-md border border-border/50 bg-muted/30 px-3 py-2.5">
              <PreviewStat
                label="Files to remove"
                value={previewTotalFiles.toLocaleString()}
              />
              <PreviewStat
                label="Space to reclaim"
                value={
                  previewTotalBytes < 1024 * 1024
                    ? `${(previewTotalBytes / 1024).toFixed(1)} KB`
                    : `${(previewTotalBytes / (1024 * 1024)).toFixed(2)} MB`
                }
              />
              <PreviewStat
                label="Phases at risk"
                value={
                  previewFailedPhases > 0 ? (
                    <span className="text-red-400">{previewFailedPhases}</span>
                  ) : (
                    <span className="text-emerald-400">none</span>
                  )
                }
              />
            </div>
          </div>
        )}

        {/* Error */}
        {error && (
          <p className="rounded-md border border-red-500/30 bg-red-500/8 px-3 py-2 font-mono text-[11px] text-red-400">
            {error}
          </p>
        )}

        {/* Options */}
        <div>
          <SectionLabel>Options</SectionLabel>
          <div className="space-y-3">
            <label className="flex cursor-pointer items-start gap-3">
              <Checkbox
                checked={shredRoot}
                onCheckedChange={(v) => setShredRoot(!!v)}
                className="mt-0.5"
              />
              <div className="space-y-0.5">
                <p className="text-sm font-medium leading-none">
                  Also shred /root/*
                </p>
                <p className="text-xs text-muted-foreground">
                  Overwrites home directory contents before removal
                </p>
              </div>
            </label>
            <label className="flex cursor-pointer items-start gap-3">
              <Checkbox
                checked={keepRecord}
                onCheckedChange={(v) => setKeepRecord(!!v)}
                className="mt-0.5"
              />
              <div className="space-y-0.5">
                <p className="text-sm font-medium leading-none">
                  Keep hub record for audit
                </p>
                <p className="text-xs text-muted-foreground">
                  Preserves the node entry in the hub database
                </p>
              </div>
            </label>
          </div>
        </div>

        {/* Actions */}
        <div className="flex items-center justify-between gap-3 border-t border-border/40 pt-4">
          <Button
            variant="outline"
            size="sm"
            onClick={runPreview}
            disabled={previewing}
            className="gap-1.5 font-mono text-xs"
          >
            {previewing ? (
              <Loader2Icon className="size-3 animate-spin" />
            ) : (
              <EyeIcon className="size-3" />
            )}
            {previewing ? "Running…" : "Preview (dry-run)"}
          </Button>
          <Button
            size="sm"
            onClick={() => setStep("confirm")}
            className="gap-1.5 bg-amber-500 font-mono text-xs text-black hover:bg-amber-400"
          >
            Continue
            <ArrowRightIcon className="size-3" />
          </Button>
        </div>
      </ModalShell>
    ),

    // ── Step 2: Confirm ───────────────────────────────────────────────────────
    confirm: (
      <ModalShell accent="red" className="gap-5 p-6 pl-8">
        {/* Header — elevated threat signal */}
        <div className="space-y-3">
          <div className="flex items-center gap-2.5">
            <AlertTriangleIcon className="size-4 text-red-500" />
            <DialogTitle className="font-mono text-sm font-semibold tracking-tight text-foreground">
              Final Confirmation
            </DialogTitle>
            <StepBadge step="confirm" />
          </div>

          {/* Warning banner */}
          <div className="rounded-md border border-red-500/30 bg-red-500/6 px-4 py-3">
            <p className="font-mono text-xs font-semibold uppercase tracking-widest text-red-400">
              Irreversible operation
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              All data on{" "}
              <span className="font-semibold text-foreground">{nodeName}</span>{" "}
              will be permanently destroyed. This cannot be undone.
            </p>
          </div>
        </div>

        {/* Summary of what will happen */}
        <div>
          <SectionLabel>What will be nuked</SectionLabel>
          <ul className="space-y-1.5">
            {plannedPhases.map((phase) => (
              <li
                key={phase}
                className="flex items-center gap-2 font-mono text-xs text-muted-foreground"
              >
                <span className="size-1.5 rounded-full bg-red-500/60 shrink-0" />
                {phase}
              </li>
            ))}
            {shredRoot && (
              <li className="flex items-center gap-2 font-mono text-xs text-red-400">
                <span className="size-1.5 rounded-full bg-red-500 shrink-0" />
                /root/* (shred)
              </li>
            )}
          </ul>
        </div>

        {/* Type-to-confirm */}
        <div className="space-y-2">
          <SectionLabel>Type to confirm</SectionLabel>
          <label className="block text-xs text-muted-foreground">
            Enter{" "}
            <span className="rounded border border-border/60 bg-muted px-1.5 py-0.5 font-mono text-[11px] font-semibold text-foreground">
              NUKE {nodeName}
            </span>{" "}
            exactly
          </label>
          <Input
            autoFocus
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            placeholder={`NUKE ${nodeName}`}
            className={cn(
              "font-mono text-sm tracking-wide",
              "border-red-500/30 bg-red-500/4 focus-visible:border-red-500/60 focus-visible:ring-red-500/20",
              canExecute && "border-emerald-500/50 bg-emerald-500/4 text-emerald-400",
            )}
            spellCheck={false}
            autoComplete="off"
          />
          {typed.length > 0 && !canExecute && (
            <p className="font-mono text-[10px] text-red-400/70">
              ✗ Does not match — keep typing
            </p>
          )}
          {canExecute && (
            <p className="font-mono text-[10px] text-emerald-400">
              ✓ Confirmed — ready to execute
            </p>
          )}
        </div>

        {/* Actions */}
        <div className="flex items-center justify-between gap-3 border-t border-border/40 pt-4">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setStep("preview")
              setTyped("")
            }}
            className="gap-1.5 font-mono text-xs"
          >
            <ArrowLeftIcon className="size-3" />
            Back
          </Button>
          <Button
            size="sm"
            disabled={!canExecute}
            onClick={runNuke}
            className={cn(
              "gap-1.5 font-mono text-xs font-semibold transition-all",
              canExecute
                ? "bg-red-600 text-white hover:bg-red-500 shadow-[0_0_12px_rgba(239,68,68,0.35)]"
                : "bg-red-600/40 text-red-300/50",
            )}
          >
            <ZapIcon className="size-3" />
            Execute Nuke
          </Button>
        </div>
      </ModalShell>
    ),

    // ── Running ───────────────────────────────────────────────────────────────
    running: (
      <ModalShell accent="red" className="gap-5 p-6 pl-8">
        <div className="space-y-1.5">
          <div className="flex items-center gap-2.5">
            <Loader2Icon className="size-4 animate-spin text-amber-400" />
            <DialogTitle className="font-mono text-sm font-semibold tracking-tight">
              Nuking {nodeName}
            </DialogTitle>
            <StepBadge step="running" />
          </div>
          <DialogDescription className="font-mono text-xs text-muted-foreground">
            Do not close this window. Streaming live phase data…
          </DialogDescription>
        </div>

        <div className="rounded-md border border-border/50 bg-muted/30 p-3">
          <NukeProgress
            plannedPhases={plannedPhases}
            received={received}
            currentPhase={currentPhase}
            finished={false}
          />
        </div>

        <div className="flex justify-end border-t border-border/40 pt-4">
          <Button
            variant="outline"
            size="sm"
            disabled
            className="cursor-not-allowed font-mono text-xs opacity-40"
          >
            Cancel (disabled)
          </Button>
        </div>
      </ModalShell>
    ),

    // ── Done ─────────────────────────────────────────────────────────────────
    done: (
      <ModalShell
        accent={
          finalReport && resultLabel(finalReport) === "SUCCESS" ? "neutral" : "red"
        }
        className="gap-5 p-6 pl-8"
      >
        {/* Result header */}
        <div className="space-y-3">
          <div className="flex items-center gap-2.5">
            {finalReport && resultLabel(finalReport) === "SUCCESS" ? (
              <CheckCircle2Icon className="size-4 text-emerald-400" />
            ) : finalReport && resultLabel(finalReport) === "PARTIAL" ? (
              <AlertCircleIcon className="size-4 text-amber-400" />
            ) : (
              <XCircleIcon className="size-4 text-red-500" />
            )}
            <DialogTitle className="font-mono text-sm font-semibold tracking-tight">
              Nuke {finalReport ? resultLabel(finalReport) : "FAILED"}
            </DialogTitle>
            <StepBadge step="done" />
          </div>

          {/* Outcome banner */}
          {finalReport && (
            <div
              className={cn(
                "rounded-md border px-4 py-3",
                resultLabel(finalReport) === "SUCCESS" &&
                  "border-emerald-500/30 bg-emerald-500/6",
                resultLabel(finalReport) === "PARTIAL" &&
                  "border-amber-500/30 bg-amber-500/6",
                resultLabel(finalReport) === "FAILED" &&
                  "border-red-500/30 bg-red-500/6",
              )}
            >
              <p
                className={cn(
                  "font-mono text-xs font-semibold uppercase tracking-widest",
                  resultLabel(finalReport) === "SUCCESS" && "text-emerald-400",
                  resultLabel(finalReport) === "PARTIAL" && "text-amber-400",
                  resultLabel(finalReport) === "FAILED" && "text-red-400",
                )}
              >
                {resultLabel(finalReport) === "SUCCESS" && "Node destroyed successfully"}
                {resultLabel(finalReport) === "PARTIAL" && "Partial completion — some phases failed"}
                {resultLabel(finalReport) === "FAILED" && "Nuke failed — manual cleanup required"}
              </p>
              <div className="mt-2 flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground">
                <ClockIcon className="size-3" />
                <span>Total duration: {formatMs(finalReport.total_duration_ms)}</span>
              </div>
            </div>
          )}

          {error && !finalReport && (
            <div className="rounded-md border border-red-500/30 bg-red-500/6 px-4 py-3">
              <p className="font-mono text-xs font-semibold uppercase tracking-widest text-red-400">
                Stream error
              </p>
              <p className="mt-1 font-mono text-[11px] text-muted-foreground">{error}</p>
            </div>
          )}
        </div>

        {/* Final phase list */}
        <div>
          <SectionLabel>Phase summary</SectionLabel>
          <div className="rounded-md border border-border/50 bg-muted/30 p-3">
            <NukeProgress
              plannedPhases={plannedPhases}
              received={received}
              finished
            />
          </div>
        </div>

        <div className="flex justify-end border-t border-border/40 pt-4">
          <Button
            size="sm"
            onClick={() => onOpenChange(false)}
            className="font-mono text-xs"
          >
            Close
          </Button>
        </div>
      </ModalShell>
    ),
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent
        className={cn(
          "overflow-hidden p-0 sm:max-w-lg",
          // Extra overlay darkness for confirm step — makes it feel weighty
          step === "confirm" && "[&+div]:bg-black/90",
        )}
      >
        {/* Scrollable interior */}
        <div className="max-h-[85vh] overflow-y-auto">
          {stepContent[step]}
        </div>
      </DialogContent>
    </Dialog>
  )
}
