// Package pin is the one xray-core release the panel installs on every node.
package pin

// Version is the pinned release (no "v"); bump it here with both hash tables.
const Version = "26.10.21"

// ZipSHA256 is each release zip's hash, as XTLS publishes it in the .dgst files.
var ZipSHA256 = map[string]string{
	"amd64": "0eec3ed4163c273d8c8bf821f7d219e257921cf584aa3fb970715e458e631978", // Xray-linux-64.zip
	"arm64": "d1304946c486d98e6b886e9b931eaaaebb23835b30e217638037b7f2721d4e9c", // Xray-linux-arm64-v8a.zip
}

// BinarySHA256 is the hash of the xray binary inside each zip, what a node checks before running it.
var BinarySHA256 = map[string]string{
	"amd64": "a27e3f95ab6ce270956d0cecd32eda4686a811afc71c64a6a734a3dede14883e",
	"arm64": "dc3324f437132d70d29ddadea19b202949c2d1230b5d90dac469b03f68829c8b",
}
