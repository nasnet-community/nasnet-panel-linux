// Package pin is the one xray-core release the panel installs on every node.
package pin

// Version is the pinned release (no "v"); bump it here with both hash tables.
const Version = "26.10.10"

// ZipSHA256 is each release zip's hash, as XTLS publishes it in the .dgst files.
var ZipSHA256 = map[string]string{
	"amd64": "72a5fde819713a103c6fc26a87aa025c1518c918a34eba4bc38d64dc0728e5d9", // Xray-linux-64.zip
	"arm64": "58e339e67827220175b3a1c6f66f48ccd086b79209f30ba1c0bcb47885d4c22a", // Xray-linux-arm64-v8a.zip
}

// BinarySHA256 is the hash of the xray binary inside each zip, what a node checks before running it.
var BinarySHA256 = map[string]string{
	"amd64": "4cca4de3ab8c16b3803a70e9ebe0570d2a72480c716d89aa91603ec568bf0f44",
	"arm64": "fadf8d792889536a9410686f3d5c4df76ec7865b7aa2297495365759ffeb424c",
}
