module shjs

go 1.17

replace github.com/gopherjs/gopherjs => ./gopherjs

replace mvdan.cc/sh/v3 => ../

require (
	github.com/gopherjs/gopherjs v0.0.0-20220221023154-0b2280d3ff96
	mvdan.cc/sh/v3 v3.0.0-00010101000000-000000000000
)

require (
	github.com/fsnotify/fsnotify v1.4.9 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/neelance/astrewrite v0.0.0-20160511093645-99348263ae86 // indirect
	github.com/neelance/sourcemap v0.0.0-20200213170602-2833bce08e4c // indirect
	github.com/shurcooL/httpfs v0.0.0-20190707220628-8d4bc4ba7749 // indirect
	github.com/sirupsen/logrus v1.8.1 // indirect
	github.com/spf13/cobra v1.2.1 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	golang.org/x/crypto v0.0.0-20210711020723-a769d52b0f97 // indirect
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c // indirect
	golang.org/x/sys v0.0.0-20211216021012-1d35b9e2eb4e // indirect
	golang.org/x/term v0.0.0-20210927222741-03fcf44c2211 // indirect
	golang.org/x/tools v0.1.5 // indirect
)
