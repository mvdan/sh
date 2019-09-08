module local.tld/fuzz

go 1.13

replace mvdan.cc/sh/v3 => ./../..

require (
	github.com/dvyukov/go-fuzz v0.0.0-20190808141544-193030f1cb16
	github.com/elazarl/go-bindata-assetfs v1.0.0 // indirect
	github.com/fuzzitdev/fuzzit v1.2.8-0.20190908103145-1132165d521b
	github.com/stephens2424/writerset v1.0.2 // indirect
	mvdan.cc/sh/v3 v3.0.0-00010101000000-000000000000
)
