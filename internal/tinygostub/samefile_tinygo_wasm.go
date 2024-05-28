//go:build tinygo && wasm

package tinygostub

import "os"

// SameFile returns false.
func SameFile(fi1, fi2 os.FileInfo) bool {
	return false
}
