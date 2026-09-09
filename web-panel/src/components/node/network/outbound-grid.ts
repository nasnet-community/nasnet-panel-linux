// One grid shared by the column header and every outbound row, so a column
// keeps its position down the list. Same skeleton as INBOUND_GRID (chevron,
// checkbox, dot, name, protocol, three numeric columns, actions) so the two
// subtabs line up when you flip between them. Density follows the list's own
// width via @container, not the viewport.
//
// Columns: chevron | checkbox | dot | name | protocol | used by | traffic | test | actions
// Nine columns and a 32px action cell leave less slack than the inbound list's
// eight, so the wide density stays lean: the fixed cells plus gaps must fit the
// card, or the kebab slides off the right edge.
export const OUTBOUND_GRID =
    "grid items-center gap-x-2.5 @3xl:gap-x-3 " +
    "grid-cols-[28px_16px_10px_minmax(120px,1fr)_96px_52px_72px_88px_32px] " +
    "@3xl:grid-cols-[28px_16px_10px_minmax(160px,1fr)_130px_64px_84px_104px_32px]"
