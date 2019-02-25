module shjs

go 1.11

replace github.com/gopherjs/gopherjs => github.com/myitcv/gopherjs v0.0.0-20181206184521-f5b96be2a04c

replace mvdan.cc/sh/v3 => ../

require (
	github.com/gopherjs/gopherjs v0.0.0-20181103185306-d547d1d9531e
	golang.org/x/sync v0.0.0-20181221193216-37e7f081c4d4 // indirect
	golang.org/x/tools v0.0.0-20190221204921-83362c3779f5 // indirect
	mvdan.cc/sh/v3 v3.0.0-00010101000000-000000000000
)
