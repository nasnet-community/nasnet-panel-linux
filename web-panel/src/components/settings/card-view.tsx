import type { CardDef } from "./catalog"
import { SettingsCard } from "./settings-card"
import { MaintenanceCard } from "./maintenance-card"
import { HttpsCard } from "./https-card"
import { AlertEventsCard } from "./alert-events-card"
import { RetentionCard } from "./retention-card"
import { PasswordCard } from "./password-card"

/** The right card for a catalog entry. match narrows a fields card to the rows a search found. */
export function CardView({ card, match }: { card: CardDef; match: "all" | Set<string> }) {
    switch (card.kind) {
        case "maintenance": return <MaintenanceCard card={card} match={match} />
        case "https": return <HttpsCard card={card} />
        case "events": return <AlertEventsCard card={card} />
        case "retention": return <RetentionCard card={card} />
        case "password": return <PasswordCard card={card} />
        default: return <SettingsCard card={card} match={match} />
    }
}
