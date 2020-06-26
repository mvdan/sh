module local.tld/fuzz

go 1.14

replace mvdan.cc/sh/v3 => ./../..

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dvyukov/go-fuzz v0.0.0-20191008232133-fdaa9b19a67d
	github.com/elazarl/go-bindata-assetfs v1.0.0 // indirect
	github.com/stephens2424/writerset v1.0.2 // indirect
	golang.org/x/tools v0.0.0-20191012152004-8de300cfc20a // indirect
	gopkg.in/yaml.v2 v2.2.4 // indirect
	mvdan.cc/sh/v3 v3.0.0-00010101000000-000000000000
)
