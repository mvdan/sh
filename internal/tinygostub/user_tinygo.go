//go:build tinygo

package tinygostub

// LookupUserHomeDir returns ("", false) always.
func LookupUserHomeDir(name string) (string, bool) {
	return "", false
}
