import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, JOB_LOST, api, followJob, subscriptions, type Job } from './api'

const job = (state: Job['state'], steps: Job['steps'] = []): Job => ({
  id: 'j1',
  kind: 'install',
  state,
  steps,
  startedAt: '2026-10-01T00:00:00Z',
})

const reply = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

class FakeEventSource {
  static last: FakeEventSource | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  constructor(readonly url: string) {
    FakeEventSource.last = this
  }
  close() {
    this.closed = true
  }
  send(j: Job) {
    this.onmessage?.({ data: JSON.stringify(j) })
  }
}

let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  FakeEventSource.last = null
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('request', () => {
  it('calls relative URLs with a JSON body', async () => {
    fetchMock.mockResolvedValue(reply(202, { jobId: 'abc' }))
    await expect(api.update(3, 'agent')).resolves.toEqual({ jobId: 'abc' })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('api/nodes/3/update')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({ kind: 'agent' })
  })

  it("marks every call as the page's own, with or without a body", async () => {
    fetchMock.mockResolvedValue(reply(200, {}))
    await api.nodes()
    await api.rename(1, 'x')
    for (const [, init] of fetchMock.mock.calls) expect(init.headers['X-RemoteNodes-UI']).toBe('1')
    expect(fetchMock.mock.calls[0][1].headers['Content-Type']).toBeUndefined()
    expect(fetchMock.mock.calls[1][1].headers['Content-Type']).toBe('application/json')
  })

  it('builds the subscription paths of the contract', async () => {
    fetchMock.mockResolvedValue(reply(200, {}))
    await subscriptions.link(2, 3)
    await subscriptions.setNode(2, 3, false)
    await subscriptions.replaceLink(2, 3)
    await subscriptions.remove(2)
    await api.nodeSubscriptions(1)
    const calls = fetchMock.mock.calls.map(([url, init]) => `${init.method} ${url}`)
    expect(calls).toEqual([
      'GET api/subscriptions/2/nodes/3/link',
      'PUT api/subscriptions/2/nodes/3',
      'POST api/subscriptions/2/nodes/3/replace-link',
      'DELETE api/subscriptions/2',
      'GET api/nodes/1/subscriptions',
    ])
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ enabled: false })
  })

  it("turns an error body into an ApiError and keeps the server's code and extra fields", async () => {
    fetchMock.mockResolvedValue(reply(409, { code: 'needs_login', error: 'log in first', needLogin: true }))
    const err = await api.remove(2, { force: true }).catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.message).toBe('log in first')
    expect(err.status).toBe(409)
    expect(err.code).toBe('needs_login')
    expect(err.needLogin).toBe(true)
  })

  it('reads needs_login from the code alone', async () => {
    fetchMock.mockResolvedValue(reply(409, { code: 'needs_login', error: 'log in first' }))
    expect(((await api.remove(2, { force: true }).catch((e) => e)) as ApiError).needLogin).toBe(true)
    fetchMock.mockResolvedValue(reply(403, { code: 'forbidden', error: 'no' }))
    expect(((await api.remove(2, { force: true }).catch((e) => e)) as ApiError).needLogin).toBe(false)
  })

  it('names the status when the body has no message', async () => {
    fetchMock.mockResolvedValue(new Response('bad gateway', { status: 502 }))
    const err = await api.nodes().catch((e) => e)
    expect(err.message).toBe('Request failed (502)')
    expect(err.code).toBe('')
    expect(err.needLogin).toBe(false)
  })
})

describe('followJob', () => {
  it('follows the event stream and stops at the final state', () => {
    vi.stubGlobal('EventSource', FakeEventSource)
    const seen: Job['state'][] = []
    followJob('j 1', (j) => seen.push(j.state))
    const es = FakeEventSource.last!
    expect(es.url).toBe('api/jobs/j%201/events')
    es.send(job('running'))
    es.send(job('running', [{ key: 'check_server', name: 'Check the server', state: 'ok' }]))
    es.send(job('failed'))
    expect(seen).toEqual(['running', 'running', 'failed'])
    expect(es.closed).toBe(true)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('polls when the stream errors', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('EventSource', FakeEventSource)
    fetchMock.mockResolvedValueOnce(reply(200, job('running'))).mockResolvedValueOnce(reply(200, job('ok')))
    const seen: Job['state'][] = []
    followJob('j1', (j) => seen.push(j.state), { pollMs: 100 })
    FakeEventSource.last!.onerror?.()
    expect(FakeEventSource.last!.closed).toBe(true)
    await vi.advanceTimersByTimeAsync(250)
    expect(fetchMock.mock.calls.map((c) => c[0])).toEqual(['api/jobs/j1', 'api/jobs/j1'])
    expect(seen).toEqual(['running', 'ok'])
    await vi.advanceTimersByTimeAsync(1000)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('sends the page header when it polls, since only the stream goes without', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('EventSource', FakeEventSource)
    fetchMock.mockResolvedValue(reply(200, job('ok')))
    followJob('j1', () => {}, { pollMs: 100 })
    FakeEventSource.last!.onerror?.()
    await vi.advanceTimersByTimeAsync(50)
    expect(fetchMock.mock.calls[0][1].headers['X-RemoteNodes-UI']).toBe('1')
  })

  it('polls when the stream stays silent, as behind a buffering proxy', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('EventSource', FakeEventSource)
    fetchMock.mockResolvedValue(reply(200, job('ok')))
    const seen: Job['state'][] = []
    followJob('j1', (j) => seen.push(j.state), { silentMs: 500 })
    await vi.advanceTimersByTimeAsync(400)
    expect(fetchMock).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(200)
    expect(seen).toEqual(['ok'])
    expect(FakeEventSource.last!.closed).toBe(true)
  })

  it('polls without EventSource and stops when asked', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('EventSource', undefined)
    fetchMock.mockImplementation(async () => reply(200, job('running')))
    const stop = followJob('j1', () => {}, { pollMs: 100 })
    await vi.advanceTimersByTimeAsync(250)
    const calls = fetchMock.mock.calls.length
    expect(calls).toBeGreaterThanOrEqual(2)
    stop()
    await vi.advanceTimersByTimeAsync(1000)
    expect(fetchMock.mock.calls.length).toBe(calls)
  })

  it('fails a job the plugin no longer knows', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('EventSource', FakeEventSource)
    fetchMock.mockResolvedValue(reply(404, { code: 'not_found', error: 'no such job' }))
    const seen: Job[] = []
    followJob('j1', (j) => seen.push(j), { pollMs: 100 })
    FakeEventSource.last!.send(job('running', [{ key: 'check_server', name: 'Check the server', state: 'running' }]))
    FakeEventSource.last!.onerror?.()
    await vi.advanceTimersByTimeAsync(500)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const final = seen[seen.length - 1]
    expect(final.state).toBe('failed')
    expect(final.steps).toEqual([{ key: 'check_server', name: 'Check the server', state: 'running' }])
    expect(final.error).toBe(JOB_LOST)
  })

  it('keeps polling through a failed poll', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('EventSource', undefined)
    fetchMock.mockRejectedValueOnce(new TypeError('network down')).mockResolvedValueOnce(reply(200, job('ok')))
    const seen: Job['state'][] = []
    followJob('j1', (j) => seen.push(j.state), { pollMs: 100 })
    await vi.advanceTimersByTimeAsync(250)
    expect(seen).toEqual(['ok'])
  })
})
