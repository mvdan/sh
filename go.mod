module mvdan.cc/sh/v3

go 1.18

require (
	github.com/creack/pty v1.1.18
	github.com/frankban/quicktest v1.14.4
	github.com/google/go-cmp v0.5.9
	github.com/google/renameio/v2 v2.0.0
	github.com/mvdan/u-root-coreutils v0.0.0-20221215222514-20b984a9fd5c
	github.com/pkg/diff v0.0.0-20210226163009-20ebb0f2a09e
	github.com/rogpeppe/go-internal v1.9.0
	golang.org/x/sync v0.1.0
	golang.org/x/sys v0.3.0
	golang.org/x/term v0.3.0
	mvdan.cc/editorconfig v0.2.0
)

require (
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/u-root/u-root v0.10.0 // indirect
)

replace github.com/mvdan/u-root-coreutils => /home/mvdan/git/u-root
