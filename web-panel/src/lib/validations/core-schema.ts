import { z } from "zod"
import type { CoreProtocol } from "@/lib/types/cores"

const coreProtocolEnum = z.enum(["ssh", "mtproto"])

/** protocol → the RHF settings-slot key. */
export const SETTINGS_SLOT: Record<CoreProtocol, string> = {
    ssh: "ssh_settings",
    mtproto: "mtproto_settings",
}

// ---------------------------------------------------------------------------
// Schema
// ---------------------------------------------------------------------------

export const coreFormSchema = z
    .object({
        node_id: z.number().int().positive(),
        protocol: coreProtocolEnum,
        listen: z.string().optional(),
        port: z.number().int().min(1, "Port must be 1–65535").max(65535, "Port must be 1–65535"),
        remark: z.string().max(100, "Remark is too long").optional(),
        is_disabled: z.boolean().optional(),
        ssh_settings: z.any().optional(),
        mtproto_settings: z.any().optional(),
    })
    .superRefine((data, ctx) => {
        const proto = data.protocol as CoreProtocol
        const slot = SETTINGS_SLOT[proto]
        const s = ((data as Record<string, unknown>)[slot] ?? {}) as Record<string, unknown>

        const str = (k: string) => (typeof s[k] === "string" ? (s[k] as string) : "")

        switch (proto) {
            case "ssh":
                if (data.port === 22) {
                    ctx.addIssue({
                        code: "custom",
                        message: "Port 22 collides with the host's own SSH daemon — pick another port.",
                        path: ["port"],
                    })
                }
                break

            case "mtproto": {
                // An ad tag only reaches Telegram through the middle proxy; without
                // it the relay ignores the setting entirely.
                if (str("adtag_default") !== "" && s.middle_proxy !== true) {
                    ctx.addIssue({
                        code: "custom",
                        message: "An ad tag needs the Telegram middle proxy enabled — it is ignored otherwise.",
                        path: [slot, "adtag_default"],
                    })
                }

                // An absent `modes` key means "offer all" (the hub applies the
                // same default), so only an explicit all-off object is an error.
                const modesGiven = s.modes != null
                const modes = (
                    modesGiven ? s.modes : { classic: true, secure: true, tls: true }
                ) as Record<string, boolean | undefined>
                const anyMode = !!(modes.classic || modes.secure || modes.tls)
                if (modesGiven && !anyMode) {
                    ctx.addIssue({
                        code: "custom",
                        message: "Enable at least one camouflage mode.",
                        path: [slot, "modes"],
                    })
                }
                // FakeTLS encodes the SNI into the client secret, so with no domain
                // the mode cannot be offered at all.
                const hasDomain =
                    str("tls_domain") !== "" ||
                    (Array.isArray(s.tls_domains_extra) &&
                        (s.tls_domains_extra as unknown[]).some(
                            (d) => typeof d === "string" && d.trim() !== "",
                        ))
                if (modesGiven && modes.tls && !hasDomain) {
                    ctx.addIssue({
                        code: "custom",
                        message:
                            !modes.classic && !modes.secure
                                ? "FakeTLS is the only enabled mode but has no SNI domain — clients would get no usable link."
                                : "FakeTLS needs an SNI domain, or it won't be offered to clients.",
                        path: [slot, "tls_domain"],
                    })
                }
                // A pinned link mode the relay does not offer produces links that
                // cannot connect.
                const lm = str("link_mode")
                if (lm && lm !== "auto" && !modes[lm]) {
                    ctx.addIssue({
                        code: "custom",
                        message: `Links are pinned to "${lm}", but that mode is not enabled.`,
                        path: [slot, "link_mode"],
                    })
                }
                break
            }

        }
    })

export type CoreFormData = z.infer<typeof coreFormSchema>
