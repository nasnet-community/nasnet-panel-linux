// Icons, drawn the way nasnet-panel draws them: lucide at stroke 1.75. Import
// icons from here, not from lucide-react, so the style stays consistent.
import { useId } from 'react'
import type { LucideIcon, LucideProps } from 'lucide-react'
import {
  CalendarDays as LCalendarDays,
  Check as LCheck,
  ChevronDown as LChevronDown,
  ChevronLeft as LChevronLeft,
  ChevronRight as LChevronRight,
  CircleCheck as LCircleCheck,
  Circle as LCircle,
  CircleX as LCircleX,
  Copy as LCopy,
  Download as LDownload,
  Ellipsis as LEllipsis,
  ExternalLink as LExternalLink,
  History as LHistory,
  Info as LInfo,
  KeyRound as LKeyRound,
  Loader2 as LLoader2,
  Moon as LMoon,
  Pencil as LPencil,
  Plus as LPlus,
  QrCode as LQrCode,
  RefreshCw as LRefreshCw,
  RotateCcw as LRotateCcw,
  Sun as LSun,
  Trash2 as LTrash2,
  TriangleAlert as LTriangleAlert,
  Upload as LUpload,
  Users as LUsers,
  X as LX,
} from 'lucide-react'

const panel = (Icon: LucideIcon) => {
  const Styled = (props: LucideProps) => <Icon strokeWidth={1.75} aria-hidden {...props} />
  Styled.displayName = Icon.displayName
  return Styled
}

export const CalendarDays = panel(LCalendarDays)
export const Check = panel(LCheck)
export const ChevronDown = panel(LChevronDown)
export const ChevronLeft = panel(LChevronLeft)
export const ChevronRight = panel(LChevronRight)
export const CircleCheck = panel(LCircleCheck)
export const Circle = panel(LCircle)
export const CircleX = panel(LCircleX)
export const Copy = panel(LCopy)
export const Download = panel(LDownload)
export const Ellipsis = panel(LEllipsis)
export const ExternalLink = panel(LExternalLink)
export const History = panel(LHistory)
export const Info = panel(LInfo)
export const KeyRound = panel(LKeyRound)
export const Loader = panel(LLoader2)
export const Moon = panel(LMoon)
export const Pencil = panel(LPencil)
export const Plus = panel(LPlus)
export const QrCode = panel(LQrCode)
export const RefreshCw = panel(LRefreshCw)
export const RotateCcw = panel(LRotateCcw)
export const Sun = panel(LSun)
export const Trash2 = panel(LTrash2)
export const TriangleAlert = panel(LTriangleAlert)
export const Upload = panel(LUpload)
export const Users = panel(LUsers)
export const X = panel(LX)

/** The page's logo: a hub with three spokes, in the purple the panel's plugin list uses. */
export function RemoteNodesLogo({ size = 32 }: { size?: number }) {
  // each logo on the page needs its own gradient id, or the first one's is used for all
  const grad = `rn-${useId().replace(/\W/g, '')}`
  // the Xray mark the panel and the store card use; the faint edge keeps the tile visible on a dark page
  return (
    <svg viewBox="0 0 64 64" width={size} height={size} role="img" aria-label="V2ray/Xray logo">
      <defs>
        <linearGradient id={grad} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stopColor="#3B3F51" />
          <stop offset="100%" stopColor="#14161F" />
        </linearGradient>
      </defs>
      <rect width="64" height="64" rx="14" fill={`url(#${grad})`} />
      <rect x="0.5" y="0.5" width="63" height="63" rx="13.5" fill="none" stroke="#ffffff" strokeOpacity="0.12" />
      <path d="M20 19h7.3l4.7 7.3 4.7-7.3H44L36.7 32 44 45h-7.3L32 37.7 27.3 45H20l7.3-13L20 19Z" fill="#ffffff" />
    </svg>
  )
}
