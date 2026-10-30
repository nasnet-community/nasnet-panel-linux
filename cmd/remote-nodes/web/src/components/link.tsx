import type { AnchorHTMLAttributes, ReactNode } from 'react'
import { navigate, routeHash, type Target } from '../hooks/useRoute'

/**
 * A link to a page of this app. A plain click navigates without a reload (a bare "#…"
 * resolves against <base href> and can reload the page); a modified click or middle
 * click opens the address in a new tab as usual.
 */
export function RouteLink({
  to,
  children,
  ...rest
}: { to: Target; children: ReactNode } & Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'href'>) {
  return (
    <a
      {...rest}
      href={routeHash(to)}
      onClick={(e) => {
        rest.onClick?.(e)
        if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
        e.preventDefault()
        navigate(to)
      }}
    >
      {children}
    </a>
  )
}
