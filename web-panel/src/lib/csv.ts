/**
 * One CSV cell: quoted when it holds a quote, comma or newline, and with a
 * leading quote when a spreadsheet would run it as a formula (= + - @, tab or
 * CR). Domains, emails and labels in exports come from clients.
 */
export function csvCell(value: unknown): string {
    let v = value == null ? "" : String(value)
    if (/^[=+\-@\t\r]/.test(v)) v = `'${v}`
    if (/[",\n\r]/.test(v)) return `"${v.replace(/"/g, '""')}"`
    return v
}
