/** The Project X (Xray-core) mark, from xtls.github.io. Filled with currentColor. */
export function XrayLogo({ className }: { className?: string }) {
    return (
        <svg viewBox="0 0 1000 1000" className={className} fill="currentColor" role="img" aria-label="Xray-core">
            <polygon points="530,530 900,530 650,650 530,1000" />
            <polygon points="470,530 470,900 350,650 0,530" />
            <polygon points="530,470 530,100 650,350 1000,470" />
            <polygon points="470,470 100,470 350,350 470,0" />
        </svg>
    )
}
