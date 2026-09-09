import { act, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { listCertificates } from "@/lib/api/certificates"
import { listSNIs } from "@/lib/api/domains"
import type { AgentCertificate, SNI } from "@/lib/types"
import { useCertificatesStore } from "@/store/certificates-store"
import DomainsPage from "../domains"
import CertificatesPage from "../certificates"

const { mobile } = vi.hoisted(() => ({ mobile: { value: false } }))

vi.mock("@/lib/api/domains", async (original) => ({
    ...await original<typeof import("@/lib/api/domains")>(), listSNIs: vi.fn(),
}))
vi.mock("@/lib/api/certificates", async (original) => ({
    ...await original<typeof import("@/lib/api/certificates")>(), listCertificates: vi.fn(),
}))
vi.mock("@/hooks/use-is-mobile", () => ({ useIsMobile: () => mobile.value }))
vi.mock("@/components/domains/add-domain-dialog", () => ({ AddDomainDialog: () => null }))
vi.mock("@/components/domains/domain-details-dialog", () => ({ DomainDetailsDialog: () => null }))
vi.mock("@/components/domains/edit-domain-dialog", () => ({ EditDomainDialog: () => null }))
vi.mock("@/components/certificates/issue-cert-dialog", () => ({ IssueCertDialog: () => null }))
vi.mock("@/components/certificates/cert-details-dialog", () => ({ CertDetailsDialog: () => null }))
vi.mock("@/components/certificates/bulk-actions-bar", () => ({ BulkActionsBar: () => null }))

// Keep the query lifecycle and the page's state handling real; row actions are
// unrelated to load recovery and can be represented by the retrieved names.
vi.mock("@/components/domains/domains-table", () => ({
    DomainsTable: ({ domains, isLoading }: { domains: SNI[]; isLoading: boolean }) => (
        isLoading ? <p>Loading domains...</p> : <div>{domains.map(domain => <p key={domain.id}>{domain.domain}</p>)}</div>
    ),
}))
vi.mock("@/components/certificates/certificates-table", () => ({
    CertificatesTable: ({ certificates, isLoading }: { certificates: AgentCertificate[]; isLoading: boolean }) => (
        isLoading ? <p>Loading certificates...</p> : certificates.length === 0 ? <p>No certificates found</p> :
            <div>{certificates.map(cert => <p key={cert.id}>{cert.common_name}</p>)}</div>
    ),
}))

const domain = { id: 1, name: "Edge", domain: "edge.example.com", is_auto_issued: false } as SNI
const certificate = {
    id: 1,
    type: "ca",
    common_name: "Proxy CA",
    serial_number: "CA-001",
    is_valid: true,
    is_revoked: false,
    days_until_expiry: 90,
    not_after: "2027-01-01T00:00:00Z",
} as AgentCertificate

function renderPage(page: React.ReactNode) {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={qc}>{page}</QueryClientProvider>)
    return qc
}

beforeEach(() => {
    vi.clearAllMocks()
    mobile.value = false
    useCertificatesStore.setState({
        activeTab: "internal",
        activeFilter: "all",
        viewMode: "list",
        searchQuery: "",
        selectedIds: new Set(),
        caBannerExpanded: false,
    })
    vi.mocked(listSNIs).mockResolvedValue({ success: true, data: [domain] })
    vi.mocked(listCertificates).mockResolvedValue({ success: true, data: { certificates: [certificate] } })
})

describe("domain query recovery", () => {
    it("shows a retryable error instead of an empty list, then recovers", async () => {
        vi.mocked(listSNIs).mockResolvedValueOnce({ success: false, error: "Offline" })
        renderPage(<DomainsPage />)

        expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't load domains")
        expect(screen.queryByText("No domains found")).not.toBeInTheDocument()

        await userEvent.click(screen.getByRole("button", { name: "Try again" }))

        expect(await screen.findByText("edge.example.com")).toBeInTheDocument()
        expect(screen.queryByRole("alert")).not.toBeInTheDocument()
    })

    it("keeps loaded domains visible and warns after a failed refresh", async () => {
        const qc = renderPage(<DomainsPage />)
        await screen.findByText("edge.example.com")
        vi.mocked(listSNIs).mockRejectedValueOnce(new Error("Offline"))

        await act(async () => { await qc.invalidateQueries({ queryKey: ["sni"] }) })

        expect(await screen.findByRole("alert")).toHaveTextContent("Showing the last loaded domains")
        expect(screen.getByText("edge.example.com")).toBeInTheDocument()
        await userEvent.click(screen.getByRole("button", { name: "Try again" }))
        await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument())
    })

    it("still shows an empty state for a successful empty response", async () => {
        vi.mocked(listSNIs).mockResolvedValueOnce({ success: true, data: [] })
        renderPage(<DomainsPage />)

        expect(await screen.findByText("No domains found")).toBeInTheDocument()
        expect(screen.queryByRole("alert")).not.toBeInTheDocument()
    })
})

describe("certificate query recovery", () => {
    it.each(["desktop", "mobile", "timeline"])("shows an initial error without inventing a missing CA in %s view", async (view) => {
        mobile.value = view === "mobile"
        useCertificatesStore.setState({ viewMode: view === "timeline" ? "timeline" : "list" })
        vi.mocked(listCertificates).mockResolvedValueOnce({ success: false, error: "Offline" })
        renderPage(<CertificatesPage />)

        expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't load certificates")
        expect(screen.queryByText("Not Initialized")).not.toBeInTheDocument()
        expect(screen.queryByText("No certificates found")).not.toBeInTheDocument()
        expect(screen.queryByText("No certificate expirations in the next 12 months")).not.toBeInTheDocument()
        expect(screen.queryByRole("button", { name: /^All/ })).not.toBeInTheDocument()
    })

    it("retries an initial certificate failure and loads the actual CA status", async () => {
        vi.mocked(listCertificates).mockRejectedValueOnce(new Error("Offline"))
        renderPage(<CertificatesPage />)
        await screen.findByRole("alert")

        await userEvent.click(screen.getByRole("button", { name: "Try again" }))

        expect(await screen.findByText("Proxy CA")).toBeInTheDocument()
        expect(screen.getByText("Active")).toBeInTheDocument()
        expect(screen.queryByRole("alert")).not.toBeInTheDocument()
    })

    it("keeps the last certificates and CA status on a failed refresh", async () => {
        const qc = renderPage(<CertificatesPage />)
        await screen.findByText("Proxy CA")
        vi.mocked(listCertificates).mockRejectedValueOnce(new Error("Offline"))

        await act(async () => { await qc.invalidateQueries({ queryKey: ["certificates"] }) })

        expect(await screen.findByRole("alert")).toHaveTextContent("These may be out of date")
        expect(screen.getByText("Proxy CA")).toBeInTheDocument()
        expect(screen.getByText("Active")).toBeInTheDocument()
        await userEvent.click(screen.getByRole("button", { name: "Try again" }))
        await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument())
    })

    it("shows loading in timeline view without declaring the CA uninitialized", async () => {
        useCertificatesStore.setState({ viewMode: "timeline" })
        vi.mocked(listCertificates).mockImplementationOnce(() => new Promise(() => {}))
        renderPage(<CertificatesPage />)

        expect(screen.getByRole("status")).toHaveTextContent("Loading certificates")
        expect(screen.queryByText("Not Initialized")).not.toBeInTheDocument()
        expect(screen.queryByText("No certificate expirations in the next 12 months")).not.toBeInTheDocument()
    })

    it("still reports an uninitialized CA after a successful empty response", async () => {
        vi.mocked(listCertificates).mockResolvedValueOnce({ success: true, data: { certificates: [] } })
        renderPage(<CertificatesPage />)

        expect(await screen.findByText("Not Initialized")).toBeInTheDocument()
        expect(screen.getByText("No certificates found")).toBeInTheDocument()
        expect(screen.queryByRole("alert")).not.toBeInTheDocument()
    })
})
