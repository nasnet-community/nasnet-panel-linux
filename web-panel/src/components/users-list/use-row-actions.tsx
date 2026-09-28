import { useState, type ReactNode } from "react"
import { toast } from "@/lib/toast"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { NumberDialog } from "@/components/user/user-dialogs"
import { useBanUser, useToggleAdmin, useUpdateTelegramID } from "@/lib/queries"
import { copyToClipboard } from "@/lib/utils"
import { useSubscriptionsStore } from "@/store/subscriptions-store"
import type { UserListItem } from "@/lib/types"
import { listName } from "./format"
import type { UserRowActions } from "./row-actions"

export const BAN_COPY = "The bot ignores them, notices stop, and their active subscriptions are paused until you unban them."
export const UNBAN_COPY = "Resumes the subscriptions their ban paused."

export function useUserRowActions(): { actions: UserRowActions; dialogs: ReactNode } {
    const confirm = useConfirm()
    const ban = useBanUser()
    const toggleAdmin = useToggleAdmin()
    const updateTelegramId = useUpdateTelegramID()
    const openCreateManualDialog = useSubscriptionsStore((s) => s.openCreateManualDialog)
    const [telegramUser, setTelegramUser] = useState<UserListItem | null>(null)

    const actions: UserRowActions = {
        busy: ban.isPending || toggleAdmin.isPending || updateTelegramId.isPending,
        copyTelegramId: async (u) => {
            if (await copyToClipboard(String(u.telegram_id))) toast.success("Telegram ID copied")
        },
        copyLink: async (s) => {
            if (s.subscription_url && (await copyToClipboard(s.subscription_url))) toast.success("Subscription link copied")
        },
        newSubscription: (u) => openCreateManualDialog(u.id, u),
        linkTelegram: (u) => setTelegramUser(u),
        toggleAdmin: async (u) => {
            const name = listName(u)
            const ok = await confirm({
                title: u.is_admin ? `Remove bot admin from ${name}?` : `Make ${name} a bot admin?`,
                description: u.is_admin
                    ? "Takes the bot's admin commands away from this Telegram account. Panel access is unchanged."
                    : "Gives this Telegram account the bot's admin commands. Panel access is unchanged.",
                confirmLabel: u.is_admin ? "Remove bot admin" : "Make bot admin",
                variant: u.is_admin ? "default" : "warning",
            })
            if (ok) toggleAdmin.mutate({ userId: u.id, isAdmin: u.is_admin })
        },
        toggleBan: async (u) => {
            const name = listName(u)
            const ok = await confirm(
                u.is_banned
                    ? { title: `Unban ${name}?`, description: UNBAN_COPY, confirmLabel: "Unban" }
                    : { title: `Ban ${name}?`, description: BAN_COPY, confirmLabel: "Ban", variant: "destructive" },
            )
            if (ok) ban.mutate({ userId: u.id, isBanned: u.is_banned })
        },
    }

    const dialogs = (
        <>
            <NumberDialog
                open={telegramUser !== null}
                onOpenChange={(open) => !open && setTelegramUser(null)}
                title="Link Telegram ID"
                description="The chat ID the bot uses to reach this customer."
                label="Telegram ID"
                initial=""
                min={1}
                pending={updateTelegramId.isPending}
                confirmLabel={() => "Save"}
                onSubmit={(telegramId) => telegramUser && updateTelegramId.mutate({ userId: telegramUser.id, telegramId }, { onSuccess: () => setTelegramUser(null) })}
            />
        </>
    )

    return { actions, dialogs }
}
