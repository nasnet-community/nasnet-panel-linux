import { usersExportUrl } from "@/lib/api/users"
import { toListParams, type UsersView } from "./use-users-view"

/** Downloads the view's CSV (every page), or only the given users. Cookie auth
 *  rides along because it's a same-origin GET. */
export function downloadUsersCsv(view: UsersView, ids?: number[]) {
    const params = toListParams(view, 0)
    delete params.page
    delete params.per_page
    const a = document.createElement("a")
    a.href = usersExportUrl(params, ids)
    a.download = ""
    document.body.appendChild(a)
    a.click()
    a.remove()
}
