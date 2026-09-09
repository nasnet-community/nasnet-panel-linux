import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, renderHook, screen } from '@testing-library/react'
import { useOutboundForm } from '@/components/outbound/use-outbound-form'
import { useInboundForm } from '@/components/inbound/use-inbound-form'
import { TransportTab } from '@/components/outbound/tabs/transport-tab'
import { TLSForm } from '@/components/shared/tls-form'
import { SOCKSForm } from '@/components/shared/protocol-forms/socks-form'
import { SockoptForm } from '@/components/shared/sockopt-form'
import { ReverseProxyDialog } from "@/components/reverse-proxy/reverse-proxy-dialog"
import { reverseProxySchema } from "@/lib/validations/reverse-proxy-schema"
import type { Outbound } from '@/lib/types'

vi.mock('@/components/ui/responsive-dialog', () => ({ ResponsiveDialog: ({ children }: { children: React.ReactNode }) => <div>{children}</div> }))
vi.mock('@/lib/queries', () => ({ useCertificates: () => ({ data: [] }), useSNIs: () => ({ data: [] }) }))
// Native selects make value changes accessible in jsdom; the tested callbacks
// and payloads are the production forms, not a duplicate form implementation.
vi.mock('@/components/ui/select', () => ({
 Select: ({ children, value, onValueChange }: { children: React.ReactNode; value: string; onValueChange: (value: string) => void }) => <select value={value} onChange={e => onValueChange(e.target.value)}>{children}</select>,
 SelectContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
 SelectGroup: ({ children }: { children: React.ReactNode }) => <>{children}</>,
 SelectLabel: () => null,
 SelectItem: ({ children, value }: { children: React.ReactNode; value: string }) => <option value={value}>{children}</option>,
 SelectTrigger: () => null,
 SelectValue: () => null,
}))
afterEach(cleanup)

describe('panel settings compatibility', () => {
 it('opens the TLS editor when switching to Hysteria', () => {
  const { result } = renderHook(() => useOutboundForm('create', null, true))
  act(() => result.current.form.setValue('protocol', 'hysteria2'))
  expect(result.current.security).toBe('tls')
  expect(result.current.sectionVisibility.security).toBe(true)
  expect(result.current.sectionVisibility.network).toBe(false)
  render(<TransportTab form={result.current.form} sectionVisibility={result.current.sectionVisibility} />)
  expect(screen.getByText('Server Name (SNI)')).toBeInTheDocument()
 })
 it('repairs the security state of an existing Hysteria outbound', () => {
  const outbound = { protocol: 'hysteria2', security: 'none', tag: 'hy' } as Outbound
  const { result } = renderHook(() => useOutboundForm('edit', outbound, true))
  expect(result.current.form.getValues('security')).toBe('tls')
 })
 it.each(['tls', 'reality'] as const)('exposes Shadowsocks %s settings', security => {
  const { result } = renderHook(() => useInboundForm('create', null, true))
  act(() => result.current.form.setValue('protocol', 'shadowsocks'))
  act(() => result.current.form.setValue('security', security))
  expect(result.current.sectionVisibility.security).toBe(true)
 })
 it('replaces the removed insecure toggle with certificate-pin guidance', () => {
  render(<TLSForm isOutbound settings={{ allowInsecure: true }} onChange={vi.fn()} />)
  expect(screen.queryByText('Allow Insecure', { exact: true })).not.toBeInTheDocument()
  expect(screen.getByText(/self-signed server/)).toBeInTheDocument()
 })
 it('clears stored SOCKS credentials when no authentication is selected', () => {
  const onChange = vi.fn()
  render(<SOCKSForm settings={{ auth: 'password', accounts: [{ user: 'old', pass: 'secret' }] }} onChange={onChange} />)
  fireEvent.change(screen.getByRole('combobox'), { target: { value: 'noauth' } })
  expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ auth: 'noauth', accounts: [] }))
 })
 it('persists a custom socket option value type', () => {
  const onChange = vi.fn()
  render(<SockoptForm data={{ customSockopt: [{ level: 0, optName: 1, optValue: 1 }] }} onChange={onChange} />)
  fireEvent.click(screen.getByRole('button', { name: /Expert/ }))
  const integer = screen.getByRole('option', { name: 'Integer' })
  fireEvent.change(integer.closest('select')!, { target: { value: 'str' } })
  expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ customSockopt: [{ level: 0, optName: 1, optValue: '1', type: 'str' }] }))
 })
})

it('limits reverse interconnections to VLESS and removes the obsolete domain field', () => {
 render(<ReverseProxyDialog open mode="create" reverseProxy={null} existingCount={0} onOpenChange={vi.fn()} onSave={vi.fn()}
  inboundTags={["tunnel-in", "http-in"]} outboundTags={["tunnel-out", "http-out"]}
  vlessInboundTags={["tunnel-in"]} vlessOutboundTags={["tunnel-out"]} />)
 expect(screen.queryByText('Domain', { exact: true })).not.toBeInTheDocument()
 const selects = screen.getAllByRole('combobox')
 expect(Array.from((selects[1] as HTMLSelectElement).options).map(o => o.value)).toEqual(['tunnel-out'])
 fireEvent.change(selects[0], { target: { value: 'portal' } })
 expect(document.getElementById('interconnection-tunnel-in')).not.toBeNull()
 expect(document.getElementById('interconnection-http-in')).toBeNull()
 expect(document.getElementById('inbound-http-in')).not.toBeNull()
})
it('accepts VLESS Reverse without a legacy control domain', () => {
 expect(reverseProxySchema.safeParse({ type: 'bridge', tag: 'bridge', domain: '', interconnection_tag: 'tunnel', outbound_tag: 'direct', interconnection_tags: [], inbound_tags: [] }).success).toBe(true)
})

it('reopens an outbound after a different template without discarding loaded protocol settings', () => {
 const outbound = { id: 12, node_id: 1, tag: 'saved-vless', protocol: 'vless', address: 'example.test', port: 443, network: 'tcp', security: 'reality', vless_settings: { uuid: 'saved-uuid', flow: 'xtls-rprx-vision' }, reality_settings: { publicKey: 'saved-key', serverNames: ['example.test'] } } as Outbound
 const { result, rerender } = renderHook(({ open }: { open: boolean }) => useOutboundForm('edit', outbound, open), { initialProps: { open: true } })
 act(() => result.current.applyPreset('shadowsocks'))
 expect(result.current.form.getValues('protocol')).toBe('shadowsocks')
 rerender({ open: false })
 rerender({ open: true })
 expect(result.current.form.getValues('protocol')).toBe('vless')
 expect(result.current.form.getValues('vless_settings')).toEqual(outbound.vless_settings)
 expect(result.current.form.getValues('reality_settings')).toEqual(outbound.reality_settings)
 expect(result.current.form.formState.isDirty).toBe(false)
})
