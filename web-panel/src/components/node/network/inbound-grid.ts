// One grid shared by the column header and every inbound row, so a column keeps
// its position all the way down the list. The list's own width decides the
// density — not the viewport — because the two side navs eat most of a tablet.
export const INBOUND_GRID =
    "grid items-center gap-x-3 @3xl:gap-x-4 " +
    "grid-cols-[28px_16px_10px_minmax(130px,1fr)_120px_72px_72px_32px] " +
    "@3xl:grid-cols-[28px_16px_10px_minmax(180px,1fr)_150px_84px_80px_56px_32px]"

// The narrow density drops Expired; hiding the cell also drops its column.
export const EXPIRED_CELL = "hidden @3xl:block"
