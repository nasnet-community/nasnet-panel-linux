import { CheckIcon, CircleIcon, Loader2Icon, XIcon, MinusIcon } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { NukePhaseResult } from '@/lib/api/nuke'

type PhaseStatus = 'pending' | 'running' | 'ok' | 'skipped' | 'failed'

export interface NukeProgressProps {
  plannedPhases: string[]
  received: NukePhaseResult[]
  currentPhase?: string
  finished?: boolean
}

function deriveStatus(
  name: string,
  result: NukePhaseResult | undefined,
  currentPhase: string | undefined,
  finished: boolean | undefined,
): PhaseStatus {
  if (result == null) {
    if (currentPhase === name && !finished) return 'running'
    return 'pending'
  }
  if (!result.ok) return 'failed'
  if (result.skipped) return 'skipped'
  return 'ok'
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
}

export function NukeProgress({
  plannedPhases,
  received,
  currentPhase,
  finished,
}: NukeProgressProps) {
  const byPhase = new Map(received.map((r) => [r.phase, r]))

  const doneCount = received.length
  const totalCount = plannedPhases.length
  const progressPct = totalCount > 0 ? Math.round((doneCount / totalCount) * 100) : 0

  return (
    <div className="space-y-3">
      {/* Compact progress header */}
      <div className="flex items-center gap-3">
        <div
          className="relative h-1 flex-1 overflow-hidden rounded-full bg-muted"
          role="progressbar"
          aria-valuenow={progressPct}
          aria-valuemin={0}
          aria-valuemax={100}
        >
          <div
            className={cn(
              'absolute left-0 top-0 h-full rounded-full transition-all duration-500',
              finished
                ? received.some((r) => !r.ok)
                  ? 'bg-red-500'
                  : 'bg-emerald-500'
                : 'bg-amber-500',
            )}
            style={{ width: `${progressPct}%` }}
          />
        </div>
        <span className="shrink-0 font-mono text-[10px] tabular-nums text-muted-foreground">
          {doneCount}/{totalCount}
        </span>
      </div>

      {/* Phase list */}
      <ol className="space-y-px">
        {plannedPhases.map((name) => {
          const r = byPhase.get(name)
          const status = deriveStatus(name, r, currentPhase, finished)

          return (
            <li
              key={name}
              className={cn(
                'flex flex-col gap-0.5 border-l-2 py-1.5 pl-3 pr-2 transition-colors duration-300',
                status === 'running' && 'nuke-phase-running border-amber-400',
                status === 'ok' && 'border-emerald-500/70',
                status === 'skipped' && 'border-muted-foreground/20',
                status === 'failed' && 'border-red-500',
                status === 'pending' && 'border-muted/40',
              )}
            >
              <div className="flex items-center gap-2.5">
                <StatusIcon status={status} />
                <span
                  className={cn(
                    'flex-1 truncate font-mono text-xs tracking-tight',
                    status === 'pending' && 'text-muted-foreground/50',
                    status === 'running' && 'text-amber-400',
                    status === 'ok' && 'text-foreground',
                    status === 'skipped' && 'text-muted-foreground/60',
                    status === 'failed' && 'text-red-400',
                  )}
                >
                  {name}
                </span>

                {r && (status === 'ok' || status === 'skipped') && (
                  <span className="shrink-0 font-mono text-[10px] tabular-nums text-muted-foreground/60">
                    {r.files_removed}f&nbsp;/&nbsp;{formatBytes(r.bytes_removed)}&nbsp;·&nbsp;{r.duration_ms}ms
                  </span>
                )}

                {r && status === 'failed' && (
                  <span className="shrink-0 font-mono text-[10px] tabular-nums text-red-500/80">
                    {r.duration_ms}ms
                  </span>
                )}
              </div>

              {/* Inline error — only when failed and error text is present */}
              {r && !r.ok && r.error && (
                <p className="pl-6 font-mono text-[10px] leading-tight text-red-400/80">
                  {r.error}
                </p>
              )}
            </li>
          )
        })}
      </ol>
    </div>
  )
}

function StatusIcon({ status }: { status: PhaseStatus }) {
  switch (status) {
    case 'running':
      return (
        <Loader2Icon className="h-3.5 w-3.5 shrink-0 animate-spin text-amber-400" />
      )
    case 'ok':
      return (
        <CheckIcon className="h-3.5 w-3.5 shrink-0 text-emerald-500" />
      )
    case 'skipped':
      return (
        <MinusIcon className="h-3.5 w-3.5 shrink-0 text-muted-foreground/40" />
      )
    case 'failed':
      return (
        <XIcon className="h-3.5 w-3.5 shrink-0 text-red-500" />
      )
    default:
      return (
        <CircleIcon className="h-3.5 w-3.5 shrink-0 text-muted-foreground/20" />
      )
  }
}
