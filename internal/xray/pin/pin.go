// Package pin is the one xray-core release the panel installs on every node.
package pin

// Version is the pinned release (no "v"); bump it here with both hash tables.
const Version = "26.9.30"

// ZipSHA256 is each release zip's hash, as XTLS publishes it in the .dgst files.
var ZipSHA256 = map[string]string{
	"amd64": "f851110beaff16e78d643f0ccfd9524b4a44dfd59bae3e34bb52bba378f7690e", // Xray-linux-64.zip
	"arm64": "9886f077f9fd8e6713b84c377c1c7db4e53b9bfa8c276a5bd12561139522b473", // Xray-linux-arm64-v8a.zip
}

// BinarySHA256 is the hash of the xray binary inside each zip, what a node checks before running it.
var BinarySHA256 = map[string]string{
	"amd64": "96efb28b8b2086275598dc46e73b5dc340da948b4e1ccc4f1719f83f67b793a7",
	"arm64": "d16ca768110de6ed098dfc2e9068cbdadf886c9b9715671ee252b1339a50452d",
}
