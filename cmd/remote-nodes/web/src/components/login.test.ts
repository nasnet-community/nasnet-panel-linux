import { describe, expect, it } from 'vitest'
import { emptyLogin, keyNeedsTrust, keyTrusted, loginError, toLogin, withoutSecrets } from './login'

const probe = (status: 'new' | 'known' | 'changed') => ({ fingerprint: 'SHA256:x', status })

describe('host keys', () => {
  it('trusts a first key and asks only about one that changed', () => {
    expect(keyNeedsTrust(probe('known'))).toBe(false)
    expect(keyNeedsTrust(probe('new'))).toBe(false)
    expect(keyNeedsTrust(probe('changed'))).toBe(true)
  })

  it('needs the box ticked only for a changed key', () => {
    expect(keyTrusted(probe('new'), false)).toBe(true)
    expect(keyTrusted(probe('changed'), false)).toBe(false)
    expect(keyTrusted(probe('changed'), true)).toBe(true)
  })
})

describe('login forms', () => {
  it('starts from the SSH defaults', () => {
    expect(emptyLogin()).toMatchObject({ sshPort: '22', sshUser: 'root', method: 'password' })
    expect(emptyLogin(0, '')).toMatchObject({ sshPort: '22', sshUser: 'root' })
    expect(emptyLogin(2222, 'deploy')).toMatchObject({ sshPort: '2222', sshUser: 'deploy' })
  })

  it('sends the key it was given and only the chosen secret', () => {
    const f = { ...emptyLogin(), password: 'pw', privateKey: 'k' }
    expect(toLogin(f, 'SHA256:new')).toEqual({ sshPort: 22, sshUser: 'root', hostKey: 'SHA256:new', password: 'pw' })
    const key = { ...f, method: 'key' as const, passphrase: 'pp' }
    expect(toLogin(key, 'SHA256:new')).toEqual({ sshPort: 22, sshUser: 'root', hostKey: 'SHA256:new', privateKey: 'k', passphrase: 'pp' })
    expect(toLogin({ ...key, passphrase: '' }, 'SHA256:new')).not.toHaveProperty('passphrase')
    expect(withoutSecrets(f)).toMatchObject({ password: '', privateKey: '', passphrase: '' })
  })

  it('names what is missing as a message key, in the order the form reads', () => {
    const ok = { ...emptyLogin(), password: 'pw' }
    expect(loginError(ok)).toBeNull()
    expect(loginError({ ...ok, sshPort: '0' })).toBe('login.errPort')
    expect(loginError({ ...ok, sshPort: '70000' })).toBe('login.errPort')
    expect(loginError({ ...ok, sshPort: '2.5' })).toBe('login.errPort')
    expect(loginError({ ...ok, sshUser: '  ' })).toBe('login.errUser')
    expect(loginError({ ...ok, password: '' })).toBe('login.errPassword')
    expect(loginError({ ...ok, method: 'key', privateKey: '  ' })).toBe('login.errKey')
    expect(loginError({ ...ok, method: 'key', privateKey: 'k', password: '' })).toBeNull()
  })
})
