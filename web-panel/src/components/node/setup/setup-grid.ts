// One grid shared by the column header and every protocol row, so a column
// keeps its position down the list. Same skeleton as the Network lists
// (chevron, dot, name, …, actions). Density follows the list's own width via
// @container, not the viewport: the two side navs leave it far narrower than
// the viewport suggests.
//
// Columns: chevron | dot | protocol | binaries | status | actions
// Narrow keeps only protocol and actions; status moves into the subline.
export const SETUP_GRID =
    "grid items-center gap-x-3 @2xl:gap-x-4 " +
    "grid-cols-[28px_10px_minmax(0,1fr)_92px] " +
    "@md:grid-cols-[28px_10px_minmax(150px,1fr)_92px_96px] " +
    "@2xl:grid-cols-[28px_10px_minmax(180px,280px)_minmax(150px,1fr)_110px_96px]"

// Hiding a cell also drops its column, so header and rows stay in step.
export const STATUS_CELL = "hidden @md:block"
export const PACKAGES_CELL = "hidden @2xl:block"
