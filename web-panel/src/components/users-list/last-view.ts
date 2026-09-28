// The users-list URL the admin last looked at this session, so "back to users"
// from a user's page lands on the same tab, filters and page.
const KEY = "users-list-view"

export function rememberUsersView(search: string) {
    try {
        sessionStorage.setItem(KEY, search)
    } catch {
        // storage blocked; back goes to the plain list
    }
}

export function lastUsersListUrl(): string {
    try {
        const search = sessionStorage.getItem(KEY)
        return search ? `/users?${search}` : "/users"
    } catch {
        return "/users"
    }
}
