import { act, renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { getNodeSettingsDefaults, useNodeSettingsForm } from "../use-node-settings-form"
import { getNodeSSHStatus, getXrayConfig, updateNode, updateNodeSSHConfig, updateXrayConfig } from "@/lib/api/nodes"
import { setNodeMaintenance } from "@/lib/api/maintenance"
import { nodeSettingsSchema } from "@/lib/validations/node-settings-schema"
import type { Node } from "@/lib/types"

vi.mock("@/lib/api/nodes", () => ({ getNodeSSHStatus: vi.fn(), getXrayConfig: vi.fn(), updateNode: vi.fn(), updateNodeSSHConfig: vi.fn(), updateXrayConfig: vi.fn() }))
vi.mock("@/lib/api/maintenance", () => ({ setNodeMaintenance: vi.fn() }))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
const node = { id: 6, name: "London", ip: "192.0.2.6", is_online: true, is_active: true, updated_at: "1" } as Node
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r }); return { promise, resolve } }
beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(getXrayConfig).mockResolvedValue({ success: true, data: JSON.stringify({ log: { loglevel: "warning", access: "/tmp/access.log" } }) })
    vi.mocked(getNodeSSHStatus).mockResolvedValue({ success: true, data: { enabled: true, port: 22, is_active: true } as never })
    vi.mocked(updateNode).mockResolvedValue({ success: true })
    vi.mocked(updateXrayConfig).mockResolvedValue({ success: true })
    vi.mocked(updateNodeSSHConfig).mockResolvedValue({ success: true })
    vi.mocked(setNodeMaintenance).mockResolvedValue({ success: true })
})
async function ready() {
    const hook = renderHook(() => useNodeSettingsForm(node))
    await waitFor(() => expect(hook.result.current.sshStatus).not.toBeNull())
    return hook
}
describe("node settings saving", () => {
    it("preserves an edit made while independent remote defaults are loading", async () => {
        const xray = deferred<Awaited<ReturnType<typeof getXrayConfig>>>()
        const ssh = deferred<Awaited<ReturnType<typeof getNodeSSHStatus>>>()
        vi.mocked(getXrayConfig).mockReturnValue(xray.promise)
        vi.mocked(getNodeSSHStatus).mockReturnValue(ssh.promise)
        const { result } = renderHook(() => useNodeSettingsForm(node))
        act(() => result.current.form.setValue("name", "Edited", { shouldDirty: true }))
        await act(async () => { xray.resolve({ success: true, data: '{"log":{"loglevel":"info"}}' }); ssh.resolve({ success: true, data: { enabled: true, port: 2222 } as never }) })
        expect(result.current.form.getValues("name")).toBe("Edited")
        expect(result.current.dirtyFields).toEqual(["name"])
        expect(result.current.savedValues.loglevel).toBe("info")
        expect(result.current.savedValues.ssh_port).toBe(2222)
    })
    it("rebases background updates without erasing edits or marking new server values dirty", async () => {
        const { result, rerender } = renderHook(({ current }) => useNodeSettingsForm(current), { initialProps: { current: node } })
        await waitFor(() => expect(result.current.sshStatus).not.toBeNull())
        act(() => result.current.form.setValue("name", "Draft", { shouldDirty: true }))
        rerender({ current: { ...node, name: "Changed elsewhere", datacenter: "New location", updated_at: "2" } })
        expect(result.current.form.getValues("name")).toBe("Draft")
        expect(result.current.form.getValues("datacenter")).toBe("New location")
        expect(result.current.dirtyFields).toEqual(["name"])
        act(() => result.current.reset())
        expect(result.current.form.getValues("name")).toBe("Changed elsewhere")
        expect(result.current.isDirty).toBe(false)
    })
    it("acknowledges successful groups and retries only the failed SSH change", async () => {
        const { result } = await ready()
        act(() => { result.current.form.setValue("name", "New name", { shouldDirty: true }); result.current.form.setValue("ssh_port", 2222, { shouldDirty: true }) })
        vi.mocked(updateNodeSSHConfig).mockResolvedValueOnce({ success: false, error: "Agent unavailable" })
        await act(async () => { await result.current.save() })
        expect(updateNode).toHaveBeenCalledWith(6, { name: "New name" })
        expect(result.current.dirtyFields).toEqual(["ssh_port"])
        await act(async () => { await result.current.save() })
        expect(updateNode).toHaveBeenCalledTimes(1)
        expect(updateNodeSSHConfig).toHaveBeenCalledTimes(2)
        expect(result.current.isDirty).toBe(false)
    })
    it("merges only edited log fields into the latest agent config", async () => {
        const { result } = await ready()
        act(() => result.current.form.setValue("dnsLog", true, { shouldDirty: true }))
        vi.mocked(getXrayConfig).mockResolvedValueOnce({ success: true, data: JSON.stringify({ log: { loglevel: "error", access: "/changed.log" }, routing: { rules: ["new"] } }) })
        await act(async () => { await result.current.save() })
        const saved = JSON.parse(vi.mocked(updateXrayConfig).mock.calls[0][1])
        expect(saved).toEqual({ log: { loglevel: "error", access: "/changed.log", dnsLog: true }, routing: { rules: ["new"] } })
        expect(updateNode).not.toHaveBeenCalled()
    })
    it("saves maintenance with the shared form and keeps its draft after failure", async () => {
        const { result } = await ready()
        act(() => { result.current.form.setValue("maintenance_mode", true, { shouldDirty: true }); result.current.form.setValue("maintenance_message", "Back soon", { shouldDirty: true }) })
        vi.mocked(setNodeMaintenance).mockResolvedValueOnce({ success: false, error: "Try later" })
        await act(async () => { await result.current.save() })
        expect(setNodeMaintenance).toHaveBeenCalledWith(6, { enabled: true, message: "Back soon" })
        expect(result.current.dirtyFields).toContain("maintenance_message")
        expect(updateNode).not.toHaveBeenCalled()
    })
    it("does not probe offline nodes or SSH on stealth nodes", async () => {
        const { rerender } = renderHook(({ current }) => useNodeSettingsForm(current), { initialProps: { current: { ...node, is_online: false } } })
        expect(getXrayConfig).not.toHaveBeenCalled()
        expect(getNodeSSHStatus).not.toHaveBeenCalled()
        rerender({ current: { ...node, is_stealth: true } })
        await waitFor(() => expect(getXrayConfig).toHaveBeenCalledTimes(1))
        expect(getNodeSSHStatus).not.toHaveBeenCalled()
    })
    it("routes validation to the missing recovery command without making writes", async () => {
        const { result } = await ready()
        act(() => result.current.form.setValue("crash_recovery_enabled", true, { shouldDirty: true }))
        let invalid: unknown
        await act(async () => { invalid = await result.current.save() })
        expect(invalid).toBe("crash_recovery_command")
        expect(updateNode).not.toHaveBeenCalled()
    })
    it("normalizes whitespace without leaving a phantom unsaved change", async () => {
        const { result } = await ready()
        act(() => result.current.form.setValue("name", " London ", { shouldDirty: true }))
        await act(async () => { await result.current.save() })
        expect(updateNode).not.toHaveBeenCalled()
        expect(result.current.form.getValues("name")).toBe("London")
        expect(result.current.isDirty).toBe(false)
    })
    it("prevents duplicate writes from simultaneous save actions", async () => {
        const { result } = await ready()
        act(() => result.current.form.setValue("name", "New name", { shouldDirty: true }))
        await act(async () => { await Promise.all([result.current.save(), result.current.save()]) })
        expect(updateNode).toHaveBeenCalledTimes(1)
    })
    it("retains newer edits made during a save", async () => {
        const { result } = await ready()
        const pending = deferred<Awaited<ReturnType<typeof updateNode>>>()
        vi.mocked(updateNode).mockReturnValue(pending.promise)
        act(() => result.current.form.setValue("name", "First", { shouldDirty: true }))
        let save!: Promise<unknown>
        await act(async () => { save = result.current.save() })
        act(() => result.current.form.setValue("name", "Second", { shouldDirty: true }))
        await act(async () => { pending.resolve({ success: true }); await save })
        expect(result.current.form.getValues("name")).toBe("Second")
        expect(result.current.savedValues.name).toBe("First")
        expect(result.current.isDirty).toBe(true)
    })
})
describe("settings validation", () => {
    it("accepts IPv6 and unlimited recovery attempts, rejects malformed automation", () => {
        const values = getNodeSettingsDefaults({ ...node, ip: "2001:db8::1" })
        expect(nodeSettingsSchema.safeParse(values).success).toBe(true)
        expect(nodeSettingsSchema.safeParse({ ...values, bandwidth_total_bw: 0, crash_recovery_max_attempts: -1 }).success).toBe(true)
        expect(nodeSettingsSchema.safeParse({ ...values, crash_recovery_enabled: true, crash_recovery_command: "true", crash_recovery_max_attempts: 0 }).success).toBe(true)
        for (const invalid of [{ country_code: "1" }, { starlink_enabled: true, starlink_dish_address: "dish:99999" }]) {
            expect(nodeSettingsSchema.safeParse({ ...values, ...invalid }).success).toBe(false)
        }
    })
})
