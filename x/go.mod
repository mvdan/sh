module mvdan.cc/sh/x

go 1.26.8

require (
	charm.land/turtle/coreutils v0.0.0-00010101000000-000000000000
	mvdan.cc/sh/v3 v3.11.0
)

require charm.land/turtle/fsx v0.0.0-00010101000000-000000000000 // indirect

require (
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/term v0.47.0 // indirect
)

// TODO: replace the absolute paths below with proper versioned requires
// once the turtle modules are published.
replace charm.land/turtle/coreutils => /Users/andrey/Developer/charm/turtle/coreutils

replace charm.land/turtle/fsx => /Users/andrey/Developer/charm/turtle/fsx

// TODO: use a released sh version with interp.WithHandlerContext.
replace mvdan.cc/sh/v3 => ../
