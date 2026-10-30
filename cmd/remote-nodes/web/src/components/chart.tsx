import { useMemo } from 'react'
import { useLang } from '../i18n'
import type { Traffic } from '../lib/api'
import { axisRate, plot } from '../lib/node'

const W = 600
const H = 140

/**
 * The last 15 minutes of a node's traffic: download in the info colour over a light fill, upload
 * in grey. Everything is placed from the left, so it stays a timeline, oldest first, in Persian too,
 * while the words read in the page's direction. Screen readers get one sentence in place of the drawing.
 */
export function TrafficChart({ traffic, now }: { traffic: Traffic; now: number }) {
  const { t, fmt } = useLang()
  const p = useMemo(() => plot(traffic, now, W, H), [traffic, now])
  const label = p.last ? t('chart.label', { down: fmt.rate(p.last.down), peak: fmt.rate(p.peak), up: fmt.rate(p.last.up) }) : t('chart.none')

  return (
    <div role="img" aria-label={label} className="relative mt-3 pl-16 rtl:pl-[7.5rem]">
      {/* the unit is written out, which Persian makes long, so its axis gets more room */}
      <div className="absolute inset-y-0 left-0 flex w-14 flex-col justify-between whitespace-nowrap pb-6 text-right text-[11px] leading-tight text-faint rtl:w-28">
        <span className="-mt-1.5">{axisRate(fmt, p.top)}</span>
        <span>{axisRate(fmt, p.top / 2)}</span>
        <span className="-mb-1.5">{fmt.num(0)}</span>
      </div>
      <div className="relative h-[140px]">
        <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="absolute inset-0 h-full w-full overflow-visible" aria-hidden="true">
          {[0, H / 2, H].map((g) => (
            <line key={g} x1={0} x2={W} y1={g} y2={g} stroke="var(--border)" strokeWidth={1} vectorEffect="non-scaling-stroke" />
          ))}
          {p.drawn && (
            <>
              <path d={p.area} fill="var(--info)" opacity={0.15} />
              <path d={p.down} fill="none" stroke="var(--info)" strokeWidth={2} vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
              <path d={p.up} fill="none" stroke="var(--muted)" strokeWidth={1.5} vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
            </>
          )}
        </svg>
        {!p.drawn && <span className="absolute inset-0 flex items-center justify-center px-4 text-center text-[13px] text-muted">{t('chart.empty')}</span>}
      </div>
      <div className="relative mt-1.5 h-4 text-[11px] text-faint">
        <span className="absolute left-0">{t('chart.minAgo', { n: 15 })}</span>
        {/* a phone has no room for the middle two, least of all in Persian, and the ends say what the chart covers */}
        <span className="absolute left-1/3 hidden -translate-x-1/2 whitespace-nowrap sm:inline">{t('chart.min', { n: 10 })}</span>
        <span className="absolute left-2/3 hidden -translate-x-1/2 whitespace-nowrap sm:inline">{t('chart.min', { n: 5 })}</span>
        <span className="absolute right-0">{t('chart.now')}</span>
      </div>
    </div>
  )
}
