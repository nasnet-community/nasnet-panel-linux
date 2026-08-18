import * as React from "react"
import { ChevronUp, ChevronDown } from "lucide-react"

import { cn } from "@/lib/utils"

// Shared visual chrome for the text field. For plain inputs it lives on the
// <input> itself; for number inputs it moves to the wrapper box so the custom
// stepper can sit flush against the field's right edge.
const FIELD_CHROME =
  "file:text-foreground placeholder:text-muted-foreground selection:bg-primary selection:text-primary-foreground dark:bg-input/30 border-input h-9 w-full min-w-0 rounded-md border bg-transparent px-3 py-1 text-base shadow-xs transition-[color,box-shadow] outline-none file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm"

const FIELD_FOCUS =
  "focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]"

const FIELD_INVALID =
  "aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive"

function mergeRefs<T>(...refs: Array<React.Ref<T> | undefined>) {
  return (node: T | null) => {
    for (const ref of refs) {
      if (typeof ref === "function") ref(node)
      else if (ref) (ref as React.MutableRefObject<T | null>).current = node
    }
  }
}

// decimalsOf("0.1") -> 1, decimalsOf("1") -> 0. Keeps step math free of the
// float dust that makes 0.1 + 0.2 render as 0.30000000000000004.
function decimalsOf(step: string): number {
  const dot = step.indexOf(".")
  return dot === -1 ? 0 : step.length - dot - 1
}

function Input({ className, type, ref, ...props }: React.ComponentProps<"input">) {
  if (type !== "number") {
    return (
      <input
        ref={ref}
        type={type}
        data-slot="input"
        className={cn(FIELD_CHROME, FIELD_FOCUS, FIELD_INVALID, className)}
        {...props}
      />
    )
  }

  return <NumberInput ref={ref} className={className} {...props} />
}

function NumberInput({
  className,
  ref,
  ...props
}: React.ComponentProps<"input">) {
  const innerRef = React.useRef<HTMLInputElement>(null)
  const repeatTimer = React.useRef<number | undefined>(undefined)

  const stopRepeat = React.useCallback(() => {
    if (repeatTimer.current !== undefined) {
      window.clearTimeout(repeatTimer.current)
      repeatTimer.current = undefined
    }
  }, [])

  React.useEffect(() => stopRepeat, [stopRepeat])

  // Nudge the value by one step, clamp to min/max, and route it through the
  // native value setter so React's onChange fires for controlled inputs.
  const step = React.useCallback((dir: 1 | -1) => {
    const el = innerRef.current
    if (!el || el.disabled || el.readOnly) return

    const stepStr = el.step && el.step !== "any" ? el.step : "1"
    const stepVal = Number(stepStr) || 1
    const min = el.min === "" ? Number.NEGATIVE_INFINITY : Number(el.min)
    const max = el.max === "" ? Number.POSITIVE_INFINITY : Number(el.max)
    const current = el.value === "" ? 0 : Number(el.value)
    if (!Number.isFinite(current)) return

    let next = current + dir * stepVal
    next = Math.min(max, Math.max(min, next))
    const nextStr = next.toFixed(decimalsOf(stepStr))

    const setter = Object.getOwnPropertyDescriptor(
      window.HTMLInputElement.prototype,
      "value",
    )?.set
    setter?.call(el, nextStr)
    el.dispatchEvent(new Event("input", { bubbles: true }))
  }, [])

  // Press-and-hold: fire once, then accelerate from 400ms toward 40ms.
  const startRepeat = React.useCallback(
    (dir: 1 | -1) => {
      stopRepeat()
      step(dir)
      let delay = 400
      const tick = () => {
        step(dir)
        delay = Math.max(40, delay * 0.82)
        repeatTimer.current = window.setTimeout(tick, delay)
      }
      repeatTimer.current = window.setTimeout(tick, delay)
    },
    [step, stopRepeat],
  )

  const stepperButton =
    "flex-1 grid place-items-center text-muted-foreground/70 transition-colors hover:bg-accent hover:text-foreground active:bg-accent/70 disabled:pointer-events-none disabled:opacity-40"

  return (
    <div
      data-slot="input"
      className={cn(
        FIELD_CHROME,
        "relative flex items-stretch gap-0 p-0 pr-0 overflow-hidden",
        "focus-within:border-ring focus-within:ring-ring/50 focus-within:ring-[3px]",
        "has-[input:disabled]:pointer-events-none has-[input:disabled]:opacity-50",
        "has-[input[aria-invalid=true]]:border-destructive has-[input[aria-invalid=true]]:ring-destructive/20 dark:has-[input[aria-invalid=true]]:ring-destructive/40",
        className,
      )}
    >
      <input
        ref={mergeRefs(innerRef, ref)}
        type="number"
        className="peer h-full w-full min-w-0 bg-transparent px-3 py-1 text-base outline-none placeholder:text-muted-foreground selection:bg-primary selection:text-primary-foreground disabled:cursor-not-allowed md:text-sm [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
        {...props}
      />
      <div className="flex w-7 shrink-0 flex-col divide-y divide-border border-l border-border">
        <button
          type="button"
          tabIndex={-1}
          aria-label="Increase"
          className={cn(stepperButton, "rounded-tr-[inherit]")}
          onPointerDown={(e) => {
            e.preventDefault()
            startRepeat(1)
          }}
          onPointerUp={stopRepeat}
          onPointerLeave={stopRepeat}
          onPointerCancel={stopRepeat}
        >
          <ChevronUp className="size-3" strokeWidth={2.5} />
        </button>
        <button
          type="button"
          tabIndex={-1}
          aria-label="Decrease"
          className={cn(stepperButton, "rounded-br-[inherit]")}
          onPointerDown={(e) => {
            e.preventDefault()
            startRepeat(-1)
          }}
          onPointerUp={stopRepeat}
          onPointerLeave={stopRepeat}
          onPointerCancel={stopRepeat}
        >
          <ChevronDown className="size-3" strokeWidth={2.5} />
        </button>
      </div>
    </div>
  )
}

export { Input }
