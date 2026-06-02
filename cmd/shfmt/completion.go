// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"flag"
	"fmt"
	"os"
)

var completionFlag = flagVal("", "completion", "", flag.StringVar)

func printCompletion(shell string) {
	switch shell {
	case "bash":
		fmt.Print(bashCompletion)
	case "zsh":
		fmt.Print(zshCompletion)
	case "fish":
		fmt.Print(fishCompletion)
	default:
		fmt.Fprintf(os.Stderr, "unsupported shell: %s (supported: bash, zsh, fish)\n", shell)
		os.Exit(1)
	}
}

const bashCompletion = `# bash completion for shfmt
_shfmt_completions() {
    local cur prev
    _init_completion || return

    case "$prev" in
        --language-dialect|-ln)
            COMPREPLY=($(compgen -W "bash posix mksh bats zsh auto" -- "$cur"))
            return
            ;;
        --indent|-i)
            COMPREPLY=($(compgen -W "0 2 4 8" -- "$cur"))
            return
            ;;
        --filename)
            _filedir
            return
            ;;
    esac

    if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "\
            --version \
            -l --list \
            -w --write \
            -d --diff \
            --apply-ignore \
            --filename \
            -ln --language-dialect \
            -p --posix \
            -s --simplify \
            -i --indent \
            -bn --binary-next-line \
            -ci --case-indent \
            -sr --space-redirects \
            -kp --keep-padding \
            -fn --func-next-line \
            -mn --minify \
            -f --find \
            --to-json \
            --from-json \
            --completion \
            " -- "$cur"))
    else
        _filedir 'sh'
    fi
}

complete -F _shfmt_completions shfmt
`

const zshCompletion = `#compdef shfmt

_shfmt() {
    local -a args

    args=(
        '(- *)'{-v,--version}'[show version and exit]'
        '(-l --list)'{-l,--list}'[list files whose formatting differs]'
        '(-w --write)'{-w,--write}'[write result to file instead of stdout]'
        '(-d --diff)'{-d,--diff}'[error with a diff when formatting differs]'
        '--apply-ignore[always apply EditorConfig ignore rules]'
        '--filename[provide a name for the standard input file]:filename:_files'
        '(-ln --language-dialect)'{-ln,--language-dialect}'[shell language dialect]:language:(bash posix mksh bats zsh auto)'
        '(-p --posix)'{-p,--posix}'[shorthand for -ln=posix]'
        '(-s --simplify)'{-s,--simplify}'[simplify the code]'
        '(-i --indent)'{-i,--indent}'[indent: 0 for tabs, >0 for spaces]:indent:(0 2 4 8)'
        '(-bn --binary-next-line)'{-bn,--binary-next-line}'[binary ops may start a line]'
        '(-ci --case-indent)'{-ci,--case-indent}'[switch cases will be indented]'
        '(-sr --space-redirects)'{-sr,--space-redirects}'[redirect operators followed by a space]'
        '(-kp --keep-padding)'{-kp,--keep-padding}'[keep column alignment paddings]'
        '(-fn --func-next-line)'{-fn,--func-next-line}'[function opening braces on separate line]'
        '(-mn --minify)'{-mn,--minify}'[minify the code (implies -s)]'
        '(-f --find)'{-f,--find}'[recursively find all shell files]'
        '--to-json[print syntax tree as typed JSON]'
        '--from-json[read syntax tree from stdin as typed JSON]'
        '--completion[generate shell completion script]:shell:(bash zsh fish)'
        '*:file:_files -g "*.sh"'
    )

    _arguments -s $args
}

_shfmt "$@"
`

const fishCompletion = `# Fish completion for shfmt

complete -c shfmt -l version -d 'Show version and exit'
complete -c shfmt -s l -l list -d 'List files whose formatting differs'
complete -c shfmt -s w -l write -d 'Write result to file instead of stdout'
complete -c shfmt -s d -l diff -d 'Error with a diff when formatting differs'
complete -c shfmt -l apply-ignore -d 'Always apply EditorConfig ignore rules'
complete -c shfmt -l filename -r -d 'Provide a name for the standard input file'

complete -c shfmt -s ln -l language-dialect -r -d 'Shell language dialect' -xa 'bash posix mksh bats zsh auto'
complete -c shfmt -s p -l posix -d 'Shorthand for -ln=posix'
complete -c shfmt -s s -l simplify -d 'Simplify the code'

complete -c shfmt -s i -l indent -r -d 'Indent: 0 for tabs, >0 for spaces' -xa '0 2 4 8'
complete -c shfmt -s bn -l binary-next-line -d 'Binary ops may start a line'
complete -c shfmt -s ci -l case-indent -d 'Switch cases will be indented'
complete -c shfmt -s sr -l space-redirects -d 'Redirect operators followed by a space'
complete -c shfmt -s kp -l keep-padding -d 'Keep column alignment paddings'
complete -c shfmt -s fn -l func-next-line -d 'Function opening braces on separate line'
complete -c shfmt -s mn -l minify -d 'Minify the code (implies -s)'

complete -c shfmt -s f -l find -d 'Recursively find all shell files'
complete -c shfmt -l to-json -d 'Print syntax tree as typed JSON'
complete -c shfmt -l from-json -d 'Read syntax tree from stdin as typed JSON'

complete -c shfmt -l completion -r -d 'Generate shell completion script' -xa 'bash zsh fish'

complete -c shfmt -F -d 'Shell script file'
`
