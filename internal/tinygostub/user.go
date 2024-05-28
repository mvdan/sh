//go:build !tinygo

package tinygostub

import "os/user"

// LookupUserHomeDir returns the path to the user's home directory given the
// user's name. It wraps around package os/user's Lookup function.
func LookupUserHomeDir(name string) (string, bool) {
	u, err := user.Lookup(name)
	if err != nil {
		return "", false
	}
	return u.HomeDir, true
}
