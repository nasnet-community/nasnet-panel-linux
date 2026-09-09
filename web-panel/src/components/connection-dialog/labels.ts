// Short network names for summaries and section titles, where the select
// labels ("XHTTP (SplitHTTP)", "HTTP Upgrade") are too long to fit.
const SHORT_NETWORK: Record<string, string> = {
    tcp: "TCP",
    raw: "TCP",
    ws: "WS",
    grpc: "gRPC",
    httpupgrade: "HTTPUpgrade",
    xhttp: "XHTTP",
    splithttp: "SplitHTTP",
    kcp: "mKCP",
    quic: "QUIC",
}

export function shortNetworkLabel(network: string | undefined): string {
    if (!network) return "TCP"
    return SHORT_NETWORK[network] ?? network
}
