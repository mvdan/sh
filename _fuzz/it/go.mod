module local.tld/fuzz

go 1.13

replace mvdan.cc/sh/v3 => ./../..

require (
	github.com/Azure/go-ansiterm v0.0.0-20170929234023-d6e3b3328b78 // indirect
	github.com/dvyukov/go-fuzz v0.0.0-20190808141544-193030f1cb16
	github.com/elazarl/go-bindata-assetfs v1.0.0 // indirect
	github.com/frankban/quicktest v1.4.1 // indirect
	github.com/fuzzitdev/fuzzit v1.2.8-0.20190819063729-75c132bbbc59
	github.com/gorilla/mux v1.7.3 // indirect
	github.com/morikuni/aec v0.0.0-20170113033406-39771216ff4c // indirect
	github.com/stephens2424/writerset v1.0.2 // indirect
	gotest.tools v2.2.0+incompatible // indirect
	mvdan.cc/sh/v3 v3.0.0-00010101000000-000000000000
)
