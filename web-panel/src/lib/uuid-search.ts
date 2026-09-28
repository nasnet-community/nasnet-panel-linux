// Operators get handed identifiers in whatever form the customer had them:
// a bare UUID, or the whole subscription link. These helpers let one search
// field accept both.

/** Canonical hyphenated UUID — the shape both link_key and config_id are minted in. */
const CANONICAL_UUID = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/

/**
 * The key segment of a panel subscription link. Host-agnostic on purpose —
 * a deployment can be reached by domain, by IP, or through a tunnel, and the
 * operator pastes whichever one the customer saw. The character class stops
 * at `?`, `#` and `/`, so query strings, fragments and trailing slashes fall
 * away without extra parsing.
 */
const SUB_LINK = /\/sub\/([0-9a-fA-F-]{8,})/

export interface SearchInput {
    /** What to put in the field and send to the API. */
    value: string
    /** True when `value` was pulled out of a pasted subscription link. */
    fromLink: boolean
}

/**
 * Reads whatever landed in the search field. A subscription link collapses to
 * the key it carries; everything else passes through untouched, including
 * mid-typing whitespace.
 */
export function readSearchInput(raw: string): SearchInput {
    const link = SUB_LINK.exec(raw.trim())
    if (link) return { value: link[1], fromLink: true }
    return { value: raw, fromLink: false }
}

/**
 * A full UUID names exactly one subscription, so a search on one that returns a
 * single row has already found it. Prefixes stay ambiguous and never qualify.
 */
export function isFullUUID(value: string): boolean {
    return CANONICAL_UUID.test(value.trim())
}
