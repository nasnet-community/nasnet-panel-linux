import { describe, expect, it } from 'vitest'
import type { OutboundSettings } from './api'
import { cleanName, decodeBase64, isWebAddress, parseOutbound, type Parsed, type ReasonCode } from './link-parser'

const ID = 'b831381d-6324-4d53-ad4f-8cda48b30811'
const PBK = 'Hq3vT9mXb2KdR7wLpN5sYc8ZfA1eJ4uG6oQ0iVtBnMk'
// 16 and 32 bytes of base64, as 2022 ciphers take them
const KEY16 = 'AAECAwQFBgcICQoLDA0ODw=='
const KEY32 = 'AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8='

/** UTF-8 text as base64, the way clients write it; `url` makes it URL-safe, `bare` drops the padding. */
const b64 = (s: string, { url = false, bare = false } = {}) => {
  let out = btoa(String.fromCharCode(...new TextEncoder().encode(s)))
  if (url) out = out.replace(/\+/g, '-').replace(/\//g, '_')
  if (bare) out = out.replace(/=+$/, '')
  return out
}

const none: OutboundSettings = {
  protocol: 'vless',
  host: '',
  port: 0,
  id: '',
  password: '',
  username: '',
  method: '',
  flow: '',
  network: 'tcp',
  path: '',
  hostHeader: '',
  mode: '',
  security: 'none',
  serverName: '',
  fingerprint: '',
  alpn: [],
  publicKey: '',
  shortId: '',
  spiderX: '',
}

function read(text: string, httpProxy = false) {
  const r = parseOutbound(text, { httpProxy })
  if (!r.ok) throw new Error(`refused ${text}: ${r.reason.code} ${r.reason.name ?? ''}`)
  return r
}

function why(text: string, httpProxy = false): { code: ReasonCode; name?: string } {
  const r = parseOutbound(text, { httpProxy })
  if (r.ok) throw new Error(`read ${text}`)
  return r.reason
}

describe('share links', () => {
  it('reads VLESS over REALITY with Vision, its remark decoded for the name', () => {
    const r = read(`vless://${ID}@de.example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=${PBK}&sid=ab12&spx=%2F&type=tcp&headerType=none#%F0%9F%87%A9%F0%9F%87%AA%20Frankfurt`)
    expect(r.settings).toEqual({
      ...none,
      host: 'de.example.com',
      port: 443,
      id: ID,
      flow: 'xtls-rprx-vision',
      security: 'reality',
      serverName: 'www.example.com',
      fingerprint: 'chrome',
      publicKey: PBK,
      shortId: 'ab12',
      spiderX: '/',
    })
    expect(r.name).toBe('🇩🇪 Frankfurt')
    expect(r.from).toBe('link')
    expect(r.notices).toEqual([])
  })

  it('reads VLESS over WebSocket and TLS: the server name is the host, the path keeps its ?ed=, ALPN splits on %2C', () => {
    const r = read(`vless://${ID}@cdn.example.net:2053?type=ws&path=%2Fws%3Fed%3D2048&host=edge.example.net&security=tls&alpn=h2%2Chttp%2F1.1#ws`)
    expect(r.settings).toMatchObject({
      network: 'ws',
      path: '/ws?ed=2048',
      hostHeader: 'edge.example.net',
      security: 'tls',
      serverName: 'cdn.example.net',
      fingerprint: 'chrome',
      alpn: ['h2', 'http/1.1'],
    })
  })

  it('takes ALPN with plain commas, and twice-encoded ones', () => {
    expect(read(`vless://${ID}@a.example.com:443?security=tls&alpn=h2,http/1.1`).settings.alpn).toEqual(['h2', 'http/1.1'])
    expect(read(`vless://${ID}@a.example.com:443?security=tls&alpn=h2%252Chttp%252F1.1`).settings.alpn).toEqual(['h2', 'http/1.1'])
  })

  it('keeps an unencoded ?ed= in a WebSocket path, and gives a path without a slash one', () => {
    expect(read(`vless://${ID}@a.example.com:443?type=ws&path=/?ed=2048&security=tls`).settings.path).toBe('/?ed=2048')
    expect(read(`vless://${ID}@a.example.com:80?type=httpupgrade&path=up`).settings).toMatchObject({ network: 'httpupgrade', path: '/up' })
  })

  it('reads XHTTP with its mode, under its old name too', () => {
    const link = (type: string) => `vless://${ID}@x.example.com:443?type=${type}&path=%2Fx&mode=stream-one&security=reality&sni=www.example.com&pbk=${PBK}`
    for (const type of ['xhttp', 'splithttp']) expect(read(link(type)).settings).toMatchObject({ network: 'xhttp', path: '/x', mode: 'stream-one', security: 'reality' })
  })

  it('reads an IPv6 address in brackets, and gives a link with no port the usual one', () => {
    const r = read(`vless://${ID}@[2001:db8::1]:8443?security=none#v6`)
    expect(r.settings).toMatchObject({ host: '2001:db8::1', port: 8443 })
    expect(read(`vless://${ID}@[2001:db8::1]?security=none`).settings.port).toBe(443)
    expect(read(`vless://${ID}@a.example.com`).settings.port).toBe(443)
  })

  it('takes the Host it sends as the server name when the address is an IP and there is no sni', () => {
    expect(read(`vless://${ID}@203.0.113.9:443?type=ws&host=cdn.example.com&security=tls`).settings.serverName).toBe('cdn.example.com')
    expect(why(`vless://${ID}@203.0.113.9:443?security=tls`)).toEqual({ code: 'no_server_name' })
    expect(why(`vless://${ID}@203.0.113.9:443?security=reality&pbk=${PBK}`)).toEqual({ code: 'no_server_name' })
  })

  it('reads raw as TCP, and drops what security none has no use for', () => {
    const r = read(`vless://${ID}@a.example.com:443?type=raw&security=none&sni=x.example.com&fp=chrome&alpn=h2`)
    expect(r.settings).toMatchObject({ network: 'tcp', security: 'none', serverName: '', fingerprint: '', alpn: [] })
  })

  it('writes the fingerprint in lower case and drops ALPN under REALITY', () => {
    const r = read(`vless://${ID}@a.example.com:443?security=reality&sni=www.example.com&fp=Firefox&pbk=${PBK}&alpn=h2`)
    expect(r.settings).toMatchObject({ fingerprint: 'firefox', alpn: [] })
  })

  it('says certificate checks stay on when a link turns them off', () => {
    for (const q of ['allowInsecure=1', 'allowinsecure=true', 'insecure=1']) {
      const r = read(`vless://${ID}@a.example.com:443?security=tls&${q}`)
      expect(r.notices, q).toEqual([{ code: 'insecure' }])
      expect(r.settings.security).toBe('tls')
    }
  })

  it('reads Trojan as TLS when the link does not say, and as plain when it says none', () => {
    expect(read('trojan://secret@tr.example.com:443?sni=front.example.com#t').settings).toMatchObject({ protocol: 'trojan', password: 'secret', security: 'tls', serverName: 'front.example.com' })
    expect(read('trojan://secret@tr.example.com:8080?security=none').settings).toMatchObject({ security: 'none', serverName: '' })
    expect(read('trojan://secret@tr.example.com:443?peer=old.example.com').settings.serverName).toBe('old.example.com')
  })

  it('keeps a Trojan password with "@" and ":" in it whole', () => {
    expect(read('trojan://p%40ss%3Aword@tr.example.com:443').settings.password).toBe('p@ss:word')
    expect(read('trojan://p@ss:w@tr.example.com:443').settings.password).toBe('p@ss:w')
  })

  it('reads VMess from its base64 JSON, padded, unpadded, URL-safe or wrapped over lines', () => {
    const json = JSON.stringify({ v: '2', ps: 'مونیخ', add: 'vm.example.com', port: '8443', id: ID, aid: '0', scy: 'auto', net: 'ws', type: 'none', host: 'vm.example.com', path: '/v?ed=2048', tls: 'tls', sni: '', alpn: 'h2', fp: '' })
    const want = { protocol: 'vmess', host: 'vm.example.com', port: 8443, id: ID, method: 'auto', network: 'ws', path: '/v?ed=2048', hostHeader: 'vm.example.com', security: 'tls', serverName: 'vm.example.com', fingerprint: 'chrome', alpn: ['h2'] }
    for (const enc of [b64(json), b64(json, { bare: true }), b64(json, { url: true, bare: true })]) {
      const r = read(`vmess://${enc}`)
      expect(r.settings).toMatchObject(want)
      expect(r.name).toBe('مونیخ')
    }
    const wrapped = b64(json).replace(/(.{60})/g, '$1\n')
    expect(read(`vmess://${wrapped}`).settings).toMatchObject(want)
  })

  it('reads a VMess cipher of none or zero as auto, and says so', () => {
    for (const scy of ['none', 'zero']) {
      const r = read(`vmess://${b64(JSON.stringify({ add: 'vm.example.com', port: 443, id: ID, scy, net: 'tcp' }))}`)
      expect(r.settings.method).toBe('auto')
      expect(r.notices).toEqual([{ code: 'vmess_cipher', name: scy }])
    }
  })

  it('takes the XHTTP mode a VMess link keeps in "type"', () => {
    const r = read(`vmess://${b64(JSON.stringify({ add: 'vm.example.com', port: 443, id: ID, net: 'xhttp', type: 'packet-up', path: '/x', tls: 'tls', sni: 'vm.example.com' }))}`)
    expect(r.settings).toMatchObject({ network: 'xhttp', mode: 'packet-up' })
  })

  it('reads Shadowsocks in SIP002 with base64url userinfo, padding or not, and the %3D padding new URL writes', () => {
    const user = 'chacha20-ietf-poly1305:a password'
    for (const info of [b64(user, { url: true, bare: true }), b64(user, { url: true }), b64(user).replace(/=/g, '%3D')]) {
      const r = read(`ss://${info}@ss.example.com:8388/?type=tcp#SS`)
      expect(r.settings).toEqual({ ...none, protocol: 'shadowsocks', host: 'ss.example.com', port: 8388, method: 'chacha20-ietf-poly1305', password: 'a password' })
      expect(r.name).toBe('SS')
    }
  })

  it('reads a 2022 cipher with its keys whole: base64 padding, "+", and the ":" between two keys', () => {
    const plain = `ss://2022-blake3-aes-256-gcm:${encodeURIComponent(`${KEY32}:${KEY32}`)}@ss.example.com:443#multi`
    expect(read(plain).settings).toMatchObject({ method: '2022-blake3-aes-256-gcm', password: `${KEY32}:${KEY32}` })
    const plus = 'AAEC+wQFBgcICQoLDA0ODw=='
    expect(read(`ss://2022-blake3-aes-128-gcm:${encodeURIComponent(plus)}@ss.example.com:443`).settings.password).toBe(plus)
    // some clients base64 the userinfo even for 2022: the split is on the first ":" there too
    expect(read(`ss://${b64(`2022-blake3-aes-128-gcm:${KEY16}:${KEY16}`, { url: true, bare: true })}@ss.example.com:443`).settings.password).toBe(`${KEY16}:${KEY16}`)
  })

  it('reads the old Shadowsocks form, all of it base64, and an IPv6 server', () => {
    expect(read(`ss://${b64('aes-256-gcm:p@ss:w0rd@198.51.100.4:8388')}#old`).settings).toMatchObject({ method: 'aes-256-gcm', password: 'p@ss:w0rd', host: '198.51.100.4', port: 8388 })
    expect(read(`ss://${b64('aes-128-gcm:pw', { url: true, bare: true })}@[2001:db8::5]:8388`).settings.host).toBe('2001:db8::5')
    expect(read(`ss://${b64('aes-128-gcm:pw')}@ss.example.com`).settings.port).toBe(8388)
  })

  it('reads SOCKS with a plain login, a base64 one (v2rayN), or none', () => {
    expect(read('socks://user:p%40ss@198.51.100.50:1080#office').settings).toEqual({ ...none, protocol: 'socks', host: '198.51.100.50', port: 1080, username: 'user', password: 'p@ss' })
    expect(read(`socks://${b64('user:pass')}@198.51.100.50:1081`).settings).toMatchObject({ username: 'user', password: 'pass' })
    expect(read('socks5://px.example.com').settings).toMatchObject({ protocol: 'socks', port: 1080, username: '', password: '' })
  })

  it('reads http:// and https:// as a proxy only when the admin says it is one', () => {
    expect(why('http://px.example.com:3128')).toEqual({ code: 'web_address' })
    expect(why('https://example.com/page')).toEqual({ code: 'web_address' })
    expect(read('http://u:p@px.example.com:3128', true).settings).toMatchObject({ protocol: 'http', port: 3128, username: 'u', password: 'p', security: 'none' })
    expect(read('http://px.example.com', true).settings.port).toBe(80)
    expect(read('https://px.example.com', true).settings).toMatchObject({ protocol: 'http', port: 443, security: 'tls', serverName: 'px.example.com', fingerprint: 'chrome' })
    expect(why('https://203.0.113.4:8443', true)).toEqual({ code: 'no_server_name' })
    expect(isWebAddress('  HTTPS://x')).toBe(true)
    expect(isWebAddress('socks://x')).toBe(false)
  })
})

describe('what links are refused for, naming what is not carried', () => {
  const cases: [string, string, ReasonCode, string?][] = [
    ['gRPC', `vless://${ID}@a.example.com:443?type=grpc&serviceName=x&security=tls`, 'transport', 'gRPC'],
    ['mKCP', `vless://${ID}@a.example.com:443?type=kcp`, 'transport', 'mKCP'],
    ['QUIC', `vless://${ID}@a.example.com:443?type=quic`, 'transport', 'QUIC'],
    ['the old h2 transport', `vless://${ID}@a.example.com:443?type=h2&security=tls`, 'transport', 'HTTP/2 (h2)'],
    ['the old http transport', `vless://${ID}@a.example.com:443?type=http&security=tls`, 'transport', 'HTTP/2 (h2)'],
    ['an unknown transport', `vless://${ID}@a.example.com:443?type=meek`, 'transport', 'meek'],
    ['a tcp header of http', `vless://${ID}@a.example.com:80?type=tcp&headerType=http&host=x.example.com`, 'tcp_http'],
    ['an xhttp extra', `vless://${ID}@a.example.com:443?type=xhttp&extra=%7B%22xmux%22%3A%7B%7D%7D&security=tls`, 'xhttp_extra'],
    ['REALITY over ws', `vless://${ID}@a.example.com:443?type=ws&security=reality&sni=w.example.com&pbk=${PBK}`, 'reality_transport', 'WebSocket'],
    ['REALITY over httpupgrade', `vless://${ID}@a.example.com:443?type=httpupgrade&security=reality&sni=w.example.com&pbk=${PBK}`, 'reality_transport', 'HTTPUpgrade'],
    ['XTLS security', `vless://${ID}@a.example.com:443?security=xtls`, 'security', 'XTLS'],
    ['an unknown security', `vless://${ID}@a.example.com:443?security=foo`, 'security', 'foo'],
    ['a flow other than Vision', `vless://${ID}@a.example.com:443?security=tls&flow=xtls-rprx-direct`, 'flow', 'xtls-rprx-direct'],
    ['Vision over ws', `vless://${ID}@a.example.com:443?type=ws&security=tls&flow=xtls-rprx-vision`, 'vision_transport'],
    ['Vision with no security', `vless://${ID}@a.example.com:443?flow=xtls-rprx-vision`, 'vision_transport'],
    ['VLESS encryption', `vless://${ID}@a.example.com:443?encryption=mlkem768x25519plus.native.0rtt.abc`, 'vless_encryption', 'mlkem768x25519plus.native.0rtt.abc'],
    ['no id', 'vless://@a.example.com:443', 'no_id'],
    ['an id that is not a UUID', 'vless://hello@a.example.com:443', 'bad_id'],
    ['REALITY with no key', `vless://${ID}@a.example.com:443?security=reality&sni=w.example.com`, 'no_public_key'],
    ['no password', 'trojan://@a.example.com:443', 'no_password'],
    ['a Shadowsocks plugin', `ss://${b64('aes-128-gcm:pw', { url: true, bare: true })}@a.example.com:8388/?plugin=obfs-local%3Bobfs%3Dhttp`, 'ss_plugin', 'obfs-local'],
    ['an unknown Shadowsocks cipher', `ss://${b64('aes-256-cfb:pw')}@a.example.com:8388`, 'cipher', 'aes-256-cfb'],
    ['Shadowsocks over ws', `ss://${b64('aes-128-gcm:pw')}@a.example.com:8388?type=ws`, 'plain_tcp', 'Shadowsocks'],
    ['a login with no password', 'socks://user@a.example.com:1080', 'login_half'],
    ['WireGuard', 'wireguard://key@a.example.com:51820?publickey=x', 'protocol', 'WireGuard'],
    ['WireGuard, short', 'wg://key@a.example.com:51820', 'protocol', 'WireGuard'],
    ['Hysteria 2', 'hysteria2://auth@a.example.com:443?sni=a.example.com', 'protocol', 'Hysteria'],
    ['Hysteria 2, short', 'hy2://auth@a.example.com:443', 'protocol', 'Hysteria'],
    ['Hysteria 1', 'hysteria://a.example.com:443?auth=x', 'protocol', 'Hysteria'],
    ['TUIC', 'tuic://u:p@a.example.com:443', 'protocol', 'TUIC'],
    ['an unknown scheme', 'anytls://pw@a.example.com:443', 'protocol', 'anytls'],
    ['a port out of range', `vless://${ID}@a.example.com:99999`, 'bad_port'],
    ['a port that is not a number', `vless://${ID}@a.example.com:https`, 'bad_port'],
    ['no host', `vless://${ID}@:443`, 'no_host'],
    ['a host that is not one', `vless://${ID}@bad_host!:443`, 'bad_host'],
    ['an unclosed IPv6 bracket', `vless://${ID}@[2001:db8::1:443`, 'bad_host'],
    ['an unknown tcp header', `vless://${ID}@a.example.com:443?headerType=srtp`, 'value', 'headerType=srtp'],
    ['an unknown xhttp mode', `vless://${ID}@a.example.com:443?type=xhttp&mode=fast&security=tls`, 'value', 'mode=fast'],
  ]
  for (const [what, link, code, name] of cases) {
    it(`refuses ${what}`, () => {
      expect(why(link)).toEqual(name === undefined ? { code } : { code, name })
    })
  }

  const vm = (o: Record<string, unknown>) => `vmess://${b64(JSON.stringify({ add: 'vm.example.com', port: 443, id: ID, net: 'tcp', ...o }))}`
  const vmessCases: [string, Record<string, unknown>, ReasonCode, string?][] = [
    ['an alterId above 0', { aid: '64' }, 'vmess_alterid'],
    ['an alterId above 0, as a number', { aid: 4 }, 'vmess_alterid'],
    ['REALITY', { tls: 'reality', sni: 'w.example.com', pbk: PBK }, 'vmess_reality'],
    ['an old cipher', { scy: 'aes-128-cfb' }, 'cipher', 'aes-128-cfb'],
    ['gRPC', { net: 'grpc', path: 'svc', tls: 'tls' }, 'transport', 'gRPC'],
    ['the old http transport', { net: 'http', tls: 'tls' }, 'transport', 'HTTP/2 (h2)'],
    ['a tcp header of http', { type: 'http' }, 'tcp_http'],
    ['no id', { id: '' }, 'no_id'],
  ]
  for (const [what, o, code, name] of vmessCases) {
    it(`refuses VMess with ${what}`, () => {
      expect(why(vm(o))).toEqual(name === undefined ? { code } : { code, name })
    })
  }
})

describe('what is no link at all', () => {
  it('says so, in a reason, and never throws', () => {
    expect(why('')).toEqual({ code: 'empty' })
    expect(why('   \n ')).toEqual({ code: 'empty' })
    expect(why('hello there')).toEqual({ code: 'not_link' })
    expect(why('de.example.com:443')).toEqual({ code: 'not_link' })
    expect(why('vmess://not base64 at all!')).toEqual({ code: 'bad_link' })
    expect(why(`vmess://${b64('not json')}`)).toEqual({ code: 'bad_link' })
    expect(why(`vmess://${b64('[1,2]')}`)).toEqual({ code: 'bad_link' })
    expect(why('ss://%%%')).toEqual({ code: 'bad_link' })
    expect(why(`ss://${b64('no colon here')}@a.example.com:1`)).toEqual({ code: 'bad_link' })
    expect(why(`vless://${ID}@a.example.com:443\ntrojan://pw@b.example.com:443`)).toEqual({ code: 'many_links' })
  })

  it('never throws, whatever it is given', () => {
    let seed = 7
    const rand = () => ((seed = (seed * 1103515245 + 12345) % 2 ** 31) / 2 ** 31)
    const alphabet = 'ab:/@?#&=%[]{}"\\ +-_.,0123456789\n'
    for (const start of ['vless://', 'vmess://', 'trojan://', 'ss://', 'socks://', 'http://', '{', '[', '']) {
      for (let i = 0; i < 300; i++) {
        const tail = Array.from({ length: Math.floor(rand() * 60) }, () => alphabet[Math.floor(rand() * alphabet.length)]).join('')
        const r: Parsed = parseOutbound(start + tail, { httpProxy: true })
        expect(typeof r.ok).toBe('boolean')
      }
    }
  })

  it('keeps a remark with a space in it, the way some clients paste it', () => {
    expect(read(`vless://${ID}@a.example.com:443#DE Frankfurt 2`).name).toBe('DE Frankfurt 2')
  })
})

describe('Xray outbound JSON', () => {
  const reality = { network: 'tcp', security: 'reality', realitySettings: { serverName: 'www.example.com', fingerprint: 'chrome', publicKey: PBK, shortId: 'ab12', spiderX: '/' } }
  const vless = { tag: 'Frankfurt', protocol: 'vless', settings: { vnext: [{ address: 'de.example.com', port: 443, users: [{ id: ID, flow: 'xtls-rprx-vision', encryption: 'none' }] }] }, streamSettings: reality }
  const want = { ...none, host: 'de.example.com', port: 443, id: ID, flow: 'xtls-rprx-vision', security: 'reality' as const, serverName: 'www.example.com', fingerprint: 'chrome', publicKey: PBK, shortId: 'ab12', spiderX: '/' }

  it('reads one outbound in the vnext form, named by its tag', () => {
    const r = read(JSON.stringify(vless))
    expect(r.settings).toEqual(want)
    expect(r.name).toBe('Frankfurt')
    expect(r.from).toBe('json')
  })

  it('takes the first proxy outbound of a whole config or a list, named by the config remarks', () => {
    const config = { remarks: 'From the app', log: {}, outbounds: [{ protocol: 'freedom', tag: 'direct' }, vless, { protocol: 'blackhole' }] }
    const r = read(JSON.stringify(config, null, 2))
    expect(r.settings).toEqual(want)
    expect(r.name).toBe('From the app')
    expect(read(JSON.stringify([{ protocol: 'dns' }, vless])).settings).toEqual(want)
  })

  it('reads the flat settings form', () => {
    const flat = { protocol: 'vless', settings: { address: 'de.example.com', port: 443, id: ID, flow: 'xtls-rprx-vision', encryption: 'none' }, streamSettings: reality }
    expect(read(JSON.stringify(flat)).settings).toEqual(want)
    expect(read(JSON.stringify(flat)).name).toBe('de.example.com')
    const trojan = { protocol: 'trojan', settings: { address: 'tr.example.com', port: 443, password: 'pw' }, streamSettings: { security: 'tls', tlsSettings: { serverName: 'tr.example.com' } } }
    expect(read(JSON.stringify(trojan)).settings).toMatchObject({ protocol: 'trojan', password: 'pw', security: 'tls', serverName: 'tr.example.com' })
  })

  it('reads the servers form of Trojan, Shadowsocks, SOCKS and HTTP', () => {
    const tr = { protocol: 'trojan', settings: { servers: [{ address: 'tr.example.com', port: 443, password: 'pw' }] }, streamSettings: { network: 'ws', security: 'tls', wsSettings: { path: '/t', headers: { Host: 'cdn.example.com' } }, tlsSettings: { serverName: 'tr.example.com', alpn: ['http/1.1'] } } }
    expect(read(JSON.stringify(tr)).settings).toMatchObject({ protocol: 'trojan', network: 'ws', path: '/t', hostHeader: 'cdn.example.com', alpn: ['http/1.1'] })
    const ss = { protocol: 'shadowsocks', settings: { servers: [{ address: 'ss.example.com', port: 8388, method: '2022-blake3-aes-128-gcm', password: `${KEY16}:${KEY16}` }] } }
    expect(read(JSON.stringify(ss)).settings).toEqual({ ...none, protocol: 'shadowsocks', host: 'ss.example.com', port: 8388, method: '2022-blake3-aes-128-gcm', password: `${KEY16}:${KEY16}` })
    const socks = { protocol: 'socks', settings: { servers: [{ address: '198.51.100.50', port: 1080, users: [{ user: 'u', pass: 'p' }] }] } }
    expect(read(JSON.stringify(socks)).settings).toMatchObject({ protocol: 'socks', username: 'u', password: 'p' })
    const http = { protocol: 'http', settings: { servers: [{ address: 'px.example.com', port: 443 }] }, streamSettings: { security: 'tls', tlsSettings: {} } }
    expect(read(JSON.stringify(http)).settings).toMatchObject({ protocol: 'http', security: 'tls', serverName: 'px.example.com', username: '', password: '' })
  })

  it('reads the transports from their own settings, splithttp under its old name too', () => {
    const base = { protocol: 'vless', settings: { address: 'a.example.com', port: 443, id: ID } }
    const hu = { ...base, streamSettings: { network: 'httpupgrade', httpupgradeSettings: { path: '/u', host: 'h.example.com' } } }
    expect(read(JSON.stringify(hu)).settings).toMatchObject({ network: 'httpupgrade', path: '/u', hostHeader: 'h.example.com' })
    const sh = { ...base, streamSettings: { network: 'splithttp', security: 'tls', splithttpSettings: { path: '/s', mode: 'packet-up' }, tlsSettings: { serverName: 'a.example.com' } } }
    expect(read(JSON.stringify(sh)).settings).toMatchObject({ network: 'xhttp', path: '/s', mode: 'packet-up' })
    const raw = { ...base, streamSettings: { network: 'raw', rawSettings: { header: { type: 'none' } } } }
    expect(read(JSON.stringify(raw)).settings.network).toBe('tcp')
  })

  it("reads REALITY's key under its newer name, and says when certificate checks were turned off", () => {
    const r = read(JSON.stringify({ ...vless, streamSettings: { network: 'tcp', security: 'reality', realitySettings: { serverName: 'www.example.com', password: PBK } } }))
    expect(r.settings.publicKey).toBe(PBK)
    const tls = read(JSON.stringify({ protocol: 'trojan', settings: { address: 'tr.example.com', port: 443, password: 'pw' }, streamSettings: { security: 'tls', tlsSettings: { allowInsecure: true } } }))
    expect(tls.notices).toEqual([{ code: 'insecure' }])
  })

  it('reads a VMess cipher of none as auto here too', () => {
    const r = read(JSON.stringify({ protocol: 'vmess', settings: { vnext: [{ address: 'vm.example.com', port: 443, users: [{ id: ID, security: 'none' }] }] } }))
    expect(r.settings).toMatchObject({ protocol: 'vmess', method: 'auto' })
    expect(r.notices).toEqual([{ code: 'vmess_cipher', name: 'none' }])
  })

  const cases: [string, unknown, ReasonCode, string?][] = [
    ['two servers', { protocol: 'vless', settings: { vnext: [{ address: 'a.example.com', port: 1, users: [{ id: ID }] }, { address: 'b.example.com', port: 1, users: [{ id: ID }] }] } }, 'many_servers'],
    ['two users', { protocol: 'vless', settings: { vnext: [{ address: 'a.example.com', port: 1, users: [{ id: ID }, { id: ID }] }] } }, 'many_servers'],
    ['no user', { protocol: 'vmess', settings: { vnext: [{ address: 'a.example.com', port: 1, users: [] }] } }, 'many_servers'],
    ['two Shadowsocks servers', { protocol: 'shadowsocks', settings: { servers: [{}, {}] } }, 'many_servers'],
    ['two SOCKS users', { protocol: 'socks', settings: { servers: [{ address: 'a.example.com', port: 1, users: [{ user: 'a', pass: 'b' }, { user: 'c', pass: 'd' }] }] } }, 'many_servers'],
    ['WireGuard', { protocol: 'wireguard', settings: {} }, 'protocol', 'WireGuard'],
    ['Hysteria', { protocol: 'hysteria2', settings: {} }, 'protocol', 'Hysteria'],
    ['an unknown protocol', { protocol: 'tor', settings: {} }, 'protocol', 'tor'],
    ['gRPC', { protocol: 'vless', settings: { address: 'a.example.com', port: 443, id: ID }, streamSettings: { network: 'grpc' } }, 'transport', 'gRPC'],
    ['mKCP', { protocol: 'vless', settings: { address: 'a.example.com', port: 443, id: ID }, streamSettings: { network: 'kcp' } }, 'transport', 'mKCP'],
    ['a tcp header of http', { protocol: 'vless', settings: { address: 'a.example.com', port: 443, id: ID }, streamSettings: { network: 'tcp', tcpSettings: { header: { type: 'http' } } } }, 'tcp_http'],
    ['an xhttp extra', { protocol: 'vless', settings: { address: 'a.example.com', port: 443, id: ID }, streamSettings: { network: 'xhttp', security: 'tls', xhttpSettings: { path: '/', extra: { xmux: {} } }, tlsSettings: { serverName: 'a.example.com' } } }, 'xhttp_extra'],
    ['REALITY over ws', { protocol: 'vless', settings: { address: 'a.example.com', port: 443, id: ID }, streamSettings: { network: 'ws', security: 'reality', realitySettings: { serverName: 'w.example.com', publicKey: PBK } } }, 'reality_transport', 'WebSocket'],
    ['an alterId above 0', { protocol: 'vmess', settings: { vnext: [{ address: 'a.example.com', port: 1, users: [{ id: ID, alterId: 4 }] }] } }, 'vmess_alterid'],
    ['a Shadowsocks plugin', { protocol: 'shadowsocks', settings: { servers: [{ address: 'a.example.com', port: 1, method: 'aes-128-gcm', password: 'p', plugin: 'v2ray-plugin' }] } }, 'ss_plugin', 'v2ray-plugin'],
    ['SOCKS over TLS', { protocol: 'socks', settings: { address: 'a.example.com', port: 1 }, streamSettings: { security: 'tls', tlsSettings: { serverName: 'a.example.com' } } }, 'plain_tcp', 'SOCKS'],
    ['an HTTP proxy over ws', { protocol: 'http', settings: { address: 'a.example.com', port: 80 }, streamSettings: { network: 'ws' } }, 'http_transport'],
    ['an HTTP proxy over REALITY', { protocol: 'http', settings: { address: 'a.example.com', port: 443 }, streamSettings: { security: 'reality', realitySettings: { serverName: 'w.example.com', publicKey: PBK } } }, 'http_transport'],
    ['no server', { protocol: 'vless', settings: {} }, 'no_host'],
  ]
  for (const [what, o, code, name] of cases) {
    it(`refuses ${what}`, () => {
      expect(why(JSON.stringify(o))).toEqual(name === undefined ? { code } : { code, name })
    })
  }

  it('says what is wrong with JSON that holds no outbound', () => {
    expect(why('{ "protocol": ')).toEqual({ code: 'bad_json' })
    expect(why('[]')).toEqual({ code: 'no_outbound' })
    expect(why('{}')).toEqual({ code: 'no_outbound' })
    expect(why(JSON.stringify({ outbounds: [{ protocol: 'freedom' }, { protocol: 'blackhole' }] }))).toEqual({ code: 'no_outbound' })
    expect(why('[1, "a", null]')).toEqual({ code: 'no_outbound' })
  })
})

describe('cleanName', () => {
  it('takes out line breaks and control characters, and cuts at 40 characters (not UTF-16 units)', () => {
    expect(cleanName('  a\tb\u2028c\u0000 ')).toBe('a b c')
    expect([...cleanName('🇩🇪'.repeat(30))].length).toBe(40)
    expect(cleanName('x'.repeat(39) + ' yz')).toBe('x'.repeat(39))
  })
})

describe('decodeBase64', () => {
  it('reads standard and URL-safe base64, padded or not, as UTF-8', () => {
    expect(decodeBase64(b64('سلام?>'))).toBe('سلام?>')
    expect(decodeBase64(b64('سلام?>', { url: true, bare: true }))).toBe('سلام?>')
    expect(decodeBase64('a')).toBeNull()
    expect(decodeBase64('!!!!')).toBeNull()
    expect(decodeBase64('')).toBeNull()
  })
})
