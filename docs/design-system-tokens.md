# NasNet Panel — Design Tokens

Single source of truth for color, motion, spacing, radius, elevation across the admin panel (`web-panel`) and the sub-panel surface (`web-panel/src/pages/sub`).

- **CSS source**: `web-panel/src/styles/tokens.css`
- **JS palette** (Recharts / canvas only): `web-panel/src/lib/design/palette.ts`
- **Imported by**: `web-panel/src/globals.css` → `src/main.tsx`

## Architecture — three layers

```
PRIMITIVES   →   SEMANTIC   →   BRIDGE (Tailwind v4 @theme)
oklch ramps     intent vars     utility classes
--gray-500      --text-tertiary  text-text-tertiary
```

- **Primitives** — `--gray-50..950`, `--success/warning/danger/info-{soft,base,strong}`, `--chart-{emerald,red,blue,amber,teal,violet}`. Never reference these from components.
- **Semantic** — intent (`--status-success`, `--surface-2`, `--text-secondary`, `--border-subtle`, `--dur-base`, `--ease-out`). Use these directly.
- **Bridge** — `@theme inline` maps semantic vars to Tailwind utilities (`bg-status-success`, `text-text-primary`, `border-border-subtle`).

## Color — semantic intent

| Intent | Tailwind utility | CSS var | When |
|---|---|---|---|
| Page background | `bg-background` | `--background` | Body, page shell |
| Card surface | `bg-card` / `bg-surface-2` | `--surface-2` | Card, panel, dialog body |
| Elevated hover / muted | `bg-surface-3` / `bg-muted` | `--surface-3` | Hover, table-row alt, muted block |
| Primary text | `text-text-primary` | `--text-primary` | Headings, key values |
| Secondary text | `text-text-secondary` | `--text-secondary` | Labels, body copy |
| Tertiary text | `text-text-tertiary` / `text-muted-foreground` | `--text-tertiary` | Captions, helper |
| Disabled text | `text-text-disabled` | `--text-disabled` | Inactive controls |
| Brand action | `bg-primary` / `text-primary` | `--primary` | Primary CTA |

### Status (use instead of raw `text-red-500`, `text-amber-500`, etc.)

| Intent | Solid | Soft fill | Foreground |
|---|---|---|---|
| Success | `bg-status-success` | `bg-status-success-soft` | `text-status-success-foreground` |
| Warning | `bg-status-warning` | `bg-status-warning-soft` | `text-status-warning-foreground` |
| Danger | `bg-status-danger` | `bg-status-danger-soft` | `text-status-danger-foreground` |
| Info | `bg-status-info` | `bg-status-info-soft` | `text-status-info-foreground` |
| Neutral | `bg-status-neutral` | `bg-status-neutral-soft` | `text-status-neutral-foreground` |

### Subscription lifecycle (domain aliases)

`bg-sub-active`, `bg-sub-expiring`, `bg-sub-exhausted`, `bg-sub-expired`, `bg-sub-paused`, `bg-sub-cancelled`.

Replaces hardcoded `#22c55e/#fbbf24/#ef4444/...` in `sidebar-context-panel.tsx`.

## Motion

Four-step duration ladder + four easings.

| Token | Value | When |
|---|---|---|
| `--dur-fast` | 150ms | Tabs, hover, focus, micro-fade |
| `--dur-base` | 200ms | Default: dialog/menu enter, scale |
| `--dur-slow` | 300ms | Drawer, sidebar collapse, composite |
| `--dur-ambient` | 500ms | Hero, splash, page-level |
| `--ease-out` | `cubic-bezier(0.16, 1, 0.3, 1)` | Enter, expand |
| `--ease-in` | `cubic-bezier(0.7, 0, 0.84, 0)` | Exit, collapse |
| `--ease-in-out` | `cubic-bezier(0.65, 0, 0.35, 1)` | Bidirectional |
| `--ease-spring` | `cubic-bezier(0.34, 1.56, 0.64, 1)` | Playful, attention |

### Framer Motion

```tsx
transition={{ duration: 0.2, ease: [0.16, 1, 0.3, 1] }}  // base + ease-out
```

Or pull from CSS via a JS helper if added later. For now, mirror the numeric values above.

### Tailwind / inline CSS

```tsx
className="transition-colors duration-[var(--dur-fast)] ease-[var(--ease-out)]"
```

## Charts (Recharts + canvas)

CSS vars do not traverse SVG `stroke=`/`fill=` props or `ctx.fillStyle` cleanly. Import from `lib/design/palette.ts` instead:

```tsx
import { chart, status, network, starlink, axis } from "@/lib/design/palette"

<Area stroke={network.rx} fill={`url(#grad-rx)`} />
<stop stopColor={chart.emerald} stopOpacity={0.4} />
<XAxis tick={{ fontSize: 10, fill: axis.tick }} />
```

Migration map (hardcoded → palette):

| Old | New |
|---|---|
| `"#10b981"` | `chart.emerald` / `network.rx` / `status.success` |
| `"#ef4444"` | `chart.red` / `status.danger` |
| `"#3b82f6"` | `chart.blue` / `network.tx` / `status.info` |
| `"#f59e0b"` | `chart.amber` / `status.warning` / `starlink.latency` |
| `"#14b8a6"` | `chart.teal` |
| `"#a855f7"` | `chart.violet` |
| `"#6b7280"` | `axis.tick` |
| `"#9ca3af"` | `axis.label` |

Recharts series order: `--chart-1..6` maps to emerald, blue, amber, red, teal, violet.

## Radius

Tailwind v4 utilities derived from `--radius: 0.625rem`:

- `rounded-sm` (6px), `rounded-md` (8px), `rounded-lg` (10px, default), `rounded-xl` (14px), `rounded-2xl` (18px), `rounded-3xl` (22px), `rounded-4xl` (26px)
- `rounded-pill` (9999px) — new alias replacing `rounded-full` when intent is "pill button" vs "circle avatar"

## Elevation (shadow)

| Token | When |
|---|---|
| `shadow-sm` | Resting card lift (rare; admin is mostly flat) |
| `shadow-md` | Popover, dropdown |
| `shadow-lg` | Modal, sheet |
| `shadow-glow` | Focus accent, hero brand element |

## Z-index

`--z-base 0`, `--z-sticky 10`, `--z-dropdown 20`, `--z-overlay 30`, `--z-modal 50`, `--z-popover 60`, `--z-toast 80`, `--z-tooltip 90`.

Use inline `style={{ zIndex: "var(--z-modal)" }}` or extend Tailwind via `z-[var(--z-modal)]`.

## Migration — drift hotspots

Found in audit (2026-05-19). Replace incrementally; old utilities keep working.

| File | Old | New |
|---|---|---|
| `components/sidebar/sidebar-context-panel.tsx` | `"#22c55e"`, `"#fbbf24"`, `"#ef4444"`, `"#991b1b"`, `"#4b5563"`, `"#374151"` | `subscription.{active,expiring,exhausted,expired,paused,cancelled}` from `lib/design/palette.ts` — or use `bg-sub-*` Tailwind utilities |
| `components/network-chart.tsx` | `"#3b82f6"`, `"#10b981"`, `"#6b7280"`, `"#9ca3af"` | `network.tx`, `network.rx`, `axis.tick`, `axis.label` |
| `components/access-history/result-overview.tsx` | `"#10b981"`, `"#ef4444"`, `"#6b7280"` | `status.success`, `status.danger`, `axis.tick` |
| `components/node/starlink/*` | `"#f59e0b"`, `"#ef4444"`, `"#10b981"`, `"#3b82f6"`, `"#14b8a6"`, `"#0a0a0f"`, `"#1a1a2e"` | `starlink.{latency,dropRate,download,upload,good,background,cellEmpty}` |
| `components/dashboard/peak-hours-widget.tsx`, `blocked-domains-widget.tsx` | `var(--color-emerald-500, #10b981)` | `chart.emerald` |
| `pages/login/index.tsx` | `bg-[#0a0a0f]` | `bg-surface-1 dark:bg-[var(--surface-1)]` or `bg-background` (re-eval brand intent) |
| Text colors: `text-red-500/400/600`, `text-amber-500/400/600`, `text-emerald-500`, `text-green-500/600/700`, `text-yellow-*`, `text-blue-500/400/600` | mixed for same intent | `text-status-{success,warning,danger,info}` |
| Status soft backgrounds: `bg-red-500/10`, `bg-amber-500/10`, `bg-emerald-500/10` | mixed | `bg-status-{danger,warning,success}-soft` |

`pages/login/proxy-globe.tsx` canvas effects — keep raw `rgba(...)`. Hard to tokenize and effect-local.

## Adding a new token

1. Decide layer: primitive (raw color), semantic (intent), or domain alias (sub lifecycle, starlink, etc.).
2. Add to `src/styles/tokens.css` in the matching section. Add dark-mode pair if surface-coupled.
3. If Tailwind utility needed → add bridge entry under `@theme inline`.
4. If SVG/canvas needed → mirror in `src/lib/design/palette.ts`.
5. Update this doc's table.

## Rules

- **Never** reference primitives (`--gray-500`, `--success-base`) from components. Go through semantic vars.
- **Never** add new hex literals in `.tsx`. Use `lib/design/palette.ts` or Tailwind utilities.
- **Never** invent a duration outside the 4-step ladder unless ambient/decorative (one-off animations OK).
- Dark mode parity is mandatory for any new surface/text/border token.
