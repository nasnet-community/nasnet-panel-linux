import { cn } from "@/lib/utils"

interface ChartTooltipProps {
  active?: boolean
  payload?: Array<{ name: string; value: number; color: string }>
  label?: string
  valueFormatter?: (value: number) => string
  labelFormatter?: (label: string) => string
  className?: string
}

export function ChartTooltip({
  active,
  payload,
  label,
  valueFormatter = (v) => String(v),
  labelFormatter = (l) => l,
  className,
}: ChartTooltipProps) {
  if (!active || !payload?.length) return null

  return (
    <div className={cn("rounded-lg border bg-background p-2 shadow-sm", className)}>
      <div className="text-xs text-muted-foreground mb-1">{labelFormatter(String(label))}</div>
      <div className="grid gap-1">
        {payload.map((entry, i) => (
          <div key={i} className="flex items-center gap-2 text-sm">
            <div className="h-2 w-2 rounded-full" style={{ backgroundColor: entry.color }} />
            <span className="text-muted-foreground">{entry.name}:</span>
            <span className="font-medium">{valueFormatter(entry.value)}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
