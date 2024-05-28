//go:build !(tinygo && wasm)

package tinygostub

import "os"

// SameFile calls [os.SameFile].
func SameFile(fi1, fi2 os.FileInfo) bool {
	return os.SameFile(fi1, fi2)
}
