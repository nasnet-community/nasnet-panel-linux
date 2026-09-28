export const IS_MAC = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.userAgent)
export const MOD_KEY = IS_MAC ? "⌘" : "Ctrl "
