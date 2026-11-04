import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { useLang } from '../i18n'
import { calendarFor, addDays, isDay, weekday, type Day } from '../lib/calendar'
import { endOfDay, today } from '../lib/subscriptions'
import { CalendarDays, ChevronLeft, ChevronRight } from './icons'
import { FIELD, cx } from './ui'

/**
 * A day, picked on the calendar of the page's language: the Persian one in Persian, with Saturday
 * first and Persian digits, and the Gregorian one in English. The value is always a Gregorian day
 * ("2026-12-15"), so the rest of the page doesn't care which calendar was on the screen.
 *
 * The calendar opens under its field, in the page's flow rather than over it, so a dialog can't clip
 * it and a click on it is never a click outside the dialog. It works from the keyboard as a date
 * grid should: arrows by day and week (mirrored right to left), Page Up and Down by month, Shift
 * with them by year, Home and End for the week's ends, Enter to pick, Escape to close without
 * closing the dialog around it.
 */
export function DatePicker({
  id,
  value,
  onChange,
  min,
  placeholder,
  'aria-describedby': describedBy,
  'aria-invalid': invalid,
}: {
  id?: string
  value: Day | ''
  onChange: (day: Day) => void
  /** the earliest day that can be picked */
  min?: Day
  /** what the field says while it is empty */
  placeholder: string
  'aria-describedby'?: string
  'aria-invalid'?: true
}) {
  const { t, fmt, lang } = useLang()
  const cal = calendarFor(lang)
  const rtl = lang === 'fa'
  const [open, setOpen] = useState(false)
  // the day the keyboard is on; the month shown is the one it falls in
  const start = () => (value && isDay(value) ? value : min && today() < min ? min : today())
  const [focus, setFocus] = useState<Day>(start)
  const [moved, setMoved] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const grid = useRef<HTMLTableElement>(null)

  // a value set from outside (the quick days) is where the calendar opens next
  useEffect(() => {
    if (value && isDay(value)) setFocus(value)
  }, [value])

  // the keyboard's day takes the focus once the grid shows it
  useEffect(() => {
    if (open && moved) grid.current?.querySelector<HTMLButtonElement>(`[data-day="${focus}"]`)?.focus()
  }, [open, moved, focus])

  // a click anywhere else closes it
  useEffect(() => {
    if (!open) return
    const away = (e: MouseEvent) => {
      if (!box.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', away)
    return () => document.removeEventListener('mousedown', away)
  }, [open])

  const blocked = (day: Day) => !!min && day < min
  const show = (day: Day) => {
    setFocus(day)
    setMoved(true)
  }
  const pick = (day: Day) => {
    if (blocked(day)) return
    onChange(day)
    setOpen(false)
    trigger.current?.focus()
  }

  const onKey = (e: KeyboardEvent<HTMLButtonElement>) => {
    const ahead = rtl ? -1 : 1
    const weekStart = addDays(focus, -((weekday(focus) - cal.firstDay + 7) % 7))
    const go: Record<string, () => Day> = {
      ArrowRight: () => addDays(focus, ahead),
      ArrowLeft: () => addDays(focus, -ahead),
      ArrowDown: () => addDays(focus, 7),
      ArrowUp: () => addDays(focus, -7),
      Home: () => weekStart,
      End: () => addDays(weekStart, 6),
      PageDown: () => cal.shiftMonth(focus, e.shiftKey ? 12 : 1),
      PageUp: () => cal.shiftMonth(focus, e.shiftKey ? -12 : -1),
    }
    if (go[e.key]) {
      e.preventDefault()
      show(go[e.key]())
    } else if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      pick(focus)
    } else if (e.key === 'Escape') {
      // the calendar closes; the dialog it sits in stays
      e.preventDefault()
      e.stopPropagation()
      setOpen(false)
      trigger.current?.focus()
    }
  }

  const shown = value && isDay(value) ? endOfDay(value) : null
  const now = today()
  const days = cal.weekdays()
  const Prev = rtl ? ChevronRight : ChevronLeft
  const Next = rtl ? ChevronLeft : ChevronRight

  return (
    <div ref={box} className="flex flex-col gap-1.5">
      <button
        ref={trigger}
        id={id}
        type="button"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-describedby={describedBy}
        aria-invalid={invalid}
        data-value={value}
        onClick={() => {
          if (!open) {
            setFocus(start())
            setMoved(true)
          }
          setOpen(!open)
        }}
        className={cx(FIELD, 'flex h-11 items-center justify-between gap-2 px-3 text-start sm:h-9')}
      >
        <span className={cx('truncate', !shown && 'text-faint')}>{shown ? fmt.date(shown) : placeholder}</span>
        <CalendarDays className="h-4 w-4 shrink-0 text-muted" />
      </button>

      {open && (
        <div role="dialog" aria-label={t('date.choose')} className="rounded-md border border-border-strong bg-surface p-2 shadow-sm">
          <div className="mb-1 flex items-center justify-between gap-1">
            <button type="button" aria-label={t('date.prev')} className={NAV} onClick={() => show(cal.shiftMonth(focus, -1))}>
              <Prev className="h-4 w-4" />
            </button>
            <span aria-live="polite" className="text-[13px] font-semibold">
              {cal.title(focus)}
            </span>
            <button type="button" aria-label={t('date.next')} className={NAV} onClick={() => show(cal.shiftMonth(focus, 1))}>
              <Next className="h-4 w-4" />
            </button>
          </div>
          <table ref={grid} role="grid" aria-label={cal.title(focus)} className="w-full table-fixed border-collapse">
            <thead>
              <tr>
                {days.map((d) => (
                  <th key={d.long} scope="col" abbr={d.long} className="pb-1 text-center text-[11px] font-medium text-faint">
                    {d.short}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {cal.grid(focus).map((week, i) => (
                <tr key={i}>
                  {week.map((day, j) =>
                    day === null ? (
                      <td key={j} />
                    ) : (
                      <td key={day} role="gridcell" aria-selected={day === value} className="p-0.5 text-center">
                        <button
                          type="button"
                          data-day={day}
                          tabIndex={day === focus ? 0 : -1}
                          aria-label={cal.label(day)}
                          aria-disabled={blocked(day) || undefined}
                          aria-current={day === now ? 'date' : undefined}
                          onClick={() => pick(day)}
                          onKeyDown={onKey}
                          className={cx(
                            'tabular inline-flex h-11 w-full items-center justify-center rounded-md text-[13px] sm:h-8',
                            'focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent',
                            day === value ? 'bg-accent font-semibold text-accent-fg'
                            : blocked(day) ? 'cursor-not-allowed text-faint opacity-50'
                            : 'hover:bg-card2',
                            day === now && day !== value && 'ring-1 ring-inset ring-border-strong',
                          )}
                        >
                          {cal.dayNumber(day)}
                        </button>
                      </td>
                    ),
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

const NAV = cx(
  'inline-flex h-11 w-11 items-center justify-center rounded-md text-muted sm:h-8 sm:w-8',
  'hover:bg-card2 hover:text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent',
)
