import { Badge } from "@/components/ui/badge"
import { SettingField } from "./setting-field"
import type { Setting, SettingsGrouped } from "@/lib/domain/setting"

interface SearchResult {
    category: string
    setting: Setting
    serverValue: string | undefined
}

interface SettingsSearchResultsProps {
    results: SearchResult[]
    onSettingChange: (category: string, key: string, value: string | boolean) => void
    onNavigateToCategory: (category: string, key: string) => void
    highlightedKey: string | null
}

export function SettingsSearchResults({
    results,
    onSettingChange,
    onNavigateToCategory,
    highlightedKey,
}: SettingsSearchResultsProps) {
    if (results.length === 0) {
        return (
            <div className="flex items-center justify-center py-16 text-muted-foreground">
                No settings match your search
            </div>
        )
    }

    return (
        <div className="space-y-3">
            {results.map(({ category, setting, serverValue }) => (
                <div key={setting.key}>
                    <button
                        onClick={() => onNavigateToCategory(category, setting.key)}
                        className="mb-1"
                    >
                        <Badge variant="secondary" className="text-xs capitalize cursor-pointer hover:bg-secondary/80">
                            {category}
                        </Badge>
                    </button>
                    <SettingField
                        setting={setting}
                        isModified={serverValue !== setting.value}
                        isHighlighted={highlightedKey === setting.key}
                        onChange={(key, value) => onSettingChange(category, key, value)}
                    />
                </div>
            ))}
        </div>
    )
}

export function buildSearchResults(
    editedSettings: SettingsGrouped,
    serverSettings: SettingsGrouped | undefined,
    search: string
): SearchResult[] {
    if (!search) return []
    const term = search.toLowerCase()
    const results: SearchResult[] = []

    for (const category of Object.keys(editedSettings)) {
        for (const setting of editedSettings[category]) {
            const matches =
                setting.label?.toLowerCase().includes(term) ||
                setting.key.toLowerCase().includes(term) ||
                setting.description?.toLowerCase().includes(term) ||
                category.toLowerCase().includes(term)

            if (matches) {
                results.push({
                    category,
                    setting,
                    serverValue: serverSettings?.[category]?.find(s => s.key === setting.key)?.value,
                })
            }
        }
    }

    return results
}
