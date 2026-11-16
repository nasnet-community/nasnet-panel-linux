// Package pin is the one xray-core release the panel installs on every node.
package pin

// Version is the pinned release (no "v"); bump it here with Commit, go.mod and both hash tables.
const Version = "26.11.9"

// Commit is what Version's tag points at. The panel links xray-core from this
// commit, so its tests run against the Xray the nodes run.
const Commit = "676eaf66f8f05d4d558ee7a74b4736623d3feeb9"

// ZipSHA256 is each release zip's hash, as XTLS publishes it in the .dgst files.
var ZipSHA256 = map[string]string{
	"amd64": "b651f7e1d9966ee2c1b1ee1bf8b52c72771824d3065dce96bbc8bcc19ed95f82", // Xray-linux-64.zip
	"arm64": "64ed6858982a7f5eb85d88b811089063bd4f17561f4c8f44e0f1dc8e6f6430be", // Xray-linux-arm64-v8a.zip
}

// BinarySHA256 is the hash of the xray binary inside each zip, what a node checks before running it.
var BinarySHA256 = map[string]string{
	"amd64": "e87bce586acdb9136aa5b1f1d9c8db9f1ea1d145b6d86368c7f6f662f9f6a66d",
	"arm64": "c2d1e66a130556d32089adcf38202fcb6f9027e41fb456cc2cda9a5fabdc093a",
}
