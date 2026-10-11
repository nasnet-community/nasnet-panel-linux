import { afterEach, describe, expect, it, vi } from "vitest"
import { parseMtprotoFrame, streamMtprotoJob, type MtprotoJob, type MtprotoJobCallbacks } from "../mtproto"

const job = (state: MtprotoJob["state"], extra: Partial<MtprotoJob> = {}): MtprotoJob => ({
    id: "j1", kind: "install", state, steps: [{ name: "check", state: "ok" }], started_at: "2026-10-01T00:00:00Z", ...extra,
})

function collect() {
    const seen = { progress: [] as MtprotoJob[], done: [] as MtprotoJob[], errors: [] as string[] }
    const cbs: MtprotoJobCallbacks = {
        onProgress: (j) => seen.progress.push(j),
        onDone: (j) => seen.done.push(j),
        onError: (e) => seen.errors.push(e.message),
    }
    return { seen, cbs }
}

describe("parseMtprotoFrame", () => {
    it("reports progress, and done when the job is no longer running", () => {
        const { seen, cbs } = collect()
        parseMtprotoFrame(`event: progress\ndata: ${JSON.stringify(job("running"))}`, cbs)
        expect(seen.progress).toHaveLength(1)
        expect(seen.done).toHaveLength(0)
        parseMtprotoFrame(`event: progress\ndata: ${JSON.stringify(job("ok", { node_id: 7 }))}`, cbs)
        expect(seen.done).toHaveLength(1)
        expect(seen.done[0].node_id).toBe(7)
    })

    it("treats a failed job as done, with its error", () => {
        const { seen, cbs } = collect()
        parseMtprotoFrame(`event: progress\ndata: ${JSON.stringify(job("failed", { error: "port 443 is in use" }))}`, cbs)
        expect(seen.done[0].error).toBe("port 443 is in use")
    })

    it("surfaces an error frame and ignores keepalives and empty frames", () => {
        const { seen, cbs } = collect()
        parseMtprotoFrame("event: error\ndata: no such job", cbs)
        parseMtprotoFrame(": keepalive", cbs)
        parseMtprotoFrame("event: progress", cbs)
        expect(seen.errors).toEqual(["no such job"])
        expect(seen.progress).toHaveLength(0)
    })

    it("reports a frame that is not JSON", () => {
        const { seen, cbs } = collect()
        parseMtprotoFrame("event: progress\ndata: {not json", cbs)
        expect(seen.errors).toHaveLength(1)
    })
})

describe("streamMtprotoJob", () => {
    afterEach(() => vi.unstubAllGlobals())

    it("reassembles frames split across chunks", async () => {
        const frame1 = `event: progress\ndata: ${JSON.stringify(job("running"))}\n\n`
        const frame2 = `event: progress\ndata: ${JSON.stringify(job("ok"))}\n\n`
        const all = new TextEncoder().encode(frame1 + frame2)
        const cut = Math.floor(all.length / 3)
        const stream = new ReadableStream<Uint8Array>({
            start(c) {
                c.enqueue(all.slice(0, cut))
                c.enqueue(all.slice(cut, cut * 2))
                c.enqueue(all.slice(cut * 2))
                c.close()
            },
        })
        vi.stubGlobal("fetch", vi.fn(async () => new Response(stream, { status: 200 })))
        const { seen, cbs } = collect()
        await streamMtprotoJob("j1", cbs)
        expect(seen.progress.map((j) => j.state)).toEqual(["running", "ok"])
        expect(seen.done).toHaveLength(1)
        expect(fetch).toHaveBeenCalledWith(expect.stringContaining("/mtproto/jobs/j1/stream"), expect.objectContaining({ credentials: "include" }))
    })

    it("fails when the stream cannot be opened", async () => {
        vi.stubGlobal("fetch", vi.fn(async () => new Response("nope", { status: 404, statusText: "Not Found" })))
        await expect(streamMtprotoJob("x", collect().cbs)).rejects.toThrow(/404/)
    })
})
