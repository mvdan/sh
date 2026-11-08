// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"golang.org/x/term"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/internal"
	"mvdan.cc/sh/v3/syntax"
)

// non-empty string is true, empty string is false
func (r *Runner) bashTest(ctx context.Context, expr syntax.TestExpr, classic bool) string {
	// Nested test expressions are on the Go stack like nested statements.
	r.stmtDepth++
	defer func() { r.stmtDepth-- }()
	switch x := expr.(type) {
	case *syntax.Word:
		// In the classic "test" mode, the words were already expanded,
		// and are kept as-is; see [expandedWord].
		return r.literal(x)
	case *syntax.ParenTest:
		return r.bashTest(ctx, x.X, classic)
	case *syntax.BinaryTest:
		switch x.Op {
		case syntax.TsMatchShort, syntax.TsMatch, syntax.TsNoMatch:
			str := r.literal(x.X.(*syntax.Word))
			yw := x.Y.(*syntax.Word)
			if classic { // test, [
				lit := r.literal(yw)
				if (str == lit) == (x.Op != syntax.TsNoMatch) {
					return "1"
				}
			} else { // [[
				pattern := r.pattern(yw)
				if r.match(pattern, str) == (x.Op != syntax.TsNoMatch) {
					return "1"
				}
			}
			return ""
		case syntax.TsReMatch:
			str := r.literal(x.X.(*syntax.Word))
			if r.binTest(ctx, x.Op, str, r.regexpWord(x.Y.(*syntax.Word)), classic) {
				return "1"
			}
			return ""
		}
		if r.binTest(ctx, x.Op, r.bashTest(ctx, x.X, classic), r.bashTest(ctx, x.Y, classic), classic) {
			return "1"
		}
		return ""
	case *syntax.UnaryTest:
		if r.unTest(ctx, x.Op, r.bashTest(ctx, x.X, classic)) {
			return "1"
		}
		return ""
	}
	return ""
}

// regexpWord expands the right-hand side of [[ str =~ regexp ]]. Like in
// Bash, quoted parts match literally, including escaped characters.
func (r *Runner) regexpWord(word *syntax.Word) string {
	var sb strings.Builder
	for _, wp := range word.Parts {
		switch wp := wp.(type) {
		case *syntax.Lit:
			s := wp.Value
			for {
				before, escaped, after, ok := internal.CutEscape(s)
				if !ok {
					break
				}
				sb.WriteString(before)
				sb.WriteString(regexp.QuoteMeta(escaped))
				s = after
			}
			sb.WriteString(s)
		default:
			str := r.literal(&syntax.Word{Parts: []syntax.WordPart{wp}})
			switch wp.(type) {
			case *syntax.SglQuoted, *syntax.DblQuoted:
				str = regexp.QuoteMeta(str)
			}
			sb.WriteString(str)
		}
	}
	return sb.String()
}

func (r *Runner) binTest(ctx context.Context, op syntax.BinTestOperator, x, y string, classic bool) bool {
	switch op {
	case syntax.TsEql, syntax.TsNeq, syntax.TsLeq, syntax.TsGeq, syntax.TsLss, syntax.TsGtr:
		// [[ evaluates the operands of a numeric comparison as arithmetic
		// expressions, so [[ 010 -eq 8 ]] and [[ x+1 -eq 4 ]] hold like in
		// Bash. The classic test command only takes integers.
		var n1, n2 int64
		if classic {
			n1, n2 = atoi(x), atoi(y)
		} else {
			var err error
			if n1, err = r.testArithm(x); err != nil {
				return false
			}
			if n2, err = r.testArithm(y); err != nil {
				return false
			}
		}
		switch op {
		case syntax.TsEql:
			return n1 == n2
		case syntax.TsNeq:
			return n1 != n2
		case syntax.TsLeq:
			return n1 <= n2
		case syntax.TsGeq:
			return n1 >= n2
		case syntax.TsLss:
			return n1 < n2
		default:
			return n1 > n2
		}
	case syntax.TsReMatch:
		if len(y) > internal.MaxPatternSize {
			r.expandErr(internal.ErrPatternTooLarge)
			return false
		}
		re, err := regexp.Compile(y)
		if err != nil {
			r.exit.code = 2
			return false
		}
		m := re.FindStringSubmatch(x)
		if m == nil {
			return false
		}
		vr := expand.Variable{
			Set:  true,
			Kind: expand.Indexed,
			List: m,
		}
		r.setVar("BASH_REMATCH", vr)
		return true
	case syntax.TsNewer, syntax.TsOlder:
		info1, err1 := r.stat(ctx, x)
		info2, err2 := r.stat(ctx, y)
		// -ot is the mirror of -nt, so swap the operands and share the logic.
		if op == syntax.TsOlder {
			info1, info2, err1, err2 = info2, info1, err2, err1
		}
		// True if the first operand exists and the second does not,
		// or if both exist and the first is newer.
		if err1 != nil {
			return false
		}
		if err2 != nil {
			return true
		}
		return info1.ModTime().After(info2.ModTime())
	case syntax.TsDevIno:
		info1, err1 := r.stat(ctx, x)
		info2, err2 := r.stat(ctx, y)
		if err1 != nil || err2 != nil {
			return false
		}
		return os.SameFile(info1, info2)
	case syntax.AndTest:
		return x != "" && y != ""
	case syntax.OrTest:
		return x != "" || y != ""
	case syntax.TsBefore:
		return x < y
	case syntax.TsAfter:
		return x > y
	default:
		// Should only happen if we forgot a case above.
		panic(fmt.Sprintf("unexpected binary test operator: %q", op))
	}
}

// testArithm evaluates the operand of a [[ numeric comparison as an
// arithmetic expression. Like in Bash, an error is reported and fails the test
// without exiting the shell.
func (r *Runner) testArithm(s string) (int64, error) {
	expr, err := syntax.NewParser().Arithmetic(strings.NewReader(s))
	if err != nil {
		r.errf("%v\n", err)
		return 0, err
	}
	if expr == nil { // an empty operand counts as zero
		return 0, nil
	}
	n, err := expand.Arithm(r.ecfg, expr)
	if err != nil {
		r.errf("%v\n", err)
		return 0, err
	}
	return int64(n), nil
}

func (r *Runner) statMode(ctx context.Context, name string, mode os.FileMode) bool {
	info, err := r.stat(ctx, name)
	return err == nil && info.Mode()&mode != 0
}

func (r *Runner) unTest(ctx context.Context, op syntax.UnTestOperator, x string) bool {
	switch op {
	case syntax.TsExists:
		_, err := r.stat(ctx, x)
		return err == nil
	case syntax.TsRegFile:
		info, err := r.stat(ctx, x)
		return err == nil && info.Mode().IsRegular()
	case syntax.TsDirect:
		return r.statMode(ctx, x, os.ModeDir)
	case syntax.TsCharSp:
		return r.statMode(ctx, x, os.ModeCharDevice)
	case syntax.TsBlckSp:
		info, err := r.stat(ctx, x)
		return err == nil && info.Mode()&os.ModeDevice != 0 &&
			info.Mode()&os.ModeCharDevice == 0
	case syntax.TsNmPipe:
		return r.statMode(ctx, x, os.ModeNamedPipe)
	case syntax.TsSocket:
		return r.statMode(ctx, x, os.ModeSocket)
	case syntax.TsSmbLink:
		info, err := r.lstat(ctx, x)
		return err == nil && info.Mode()&os.ModeSymlink != 0
	case syntax.TsSticky:
		return r.statMode(ctx, x, os.ModeSticky)
	case syntax.TsUIDSet:
		return r.statMode(ctx, x, os.ModeSetuid)
	case syntax.TsGIDSet:
		return r.statMode(ctx, x, os.ModeSetgid)
	case syntax.TsModif:
		info, err := r.stat(ctx, x)
		if err != nil {
			return false
		}
		return info.ModTime().After(getAtime(info))
	case syntax.TsRead:
		return r.access(ctx, x, AccessRead) == nil
	case syntax.TsWrite:
		return r.access(ctx, x, AccessWrite) == nil
	case syntax.TsExec:
		return r.access(ctx, x, AccessExec) == nil
	case syntax.TsNoEmpty:
		info, err := r.stat(ctx, x)
		return err == nil && info.Size() > 0
	case syntax.TsFdTerm:
		fd := atoi(x)
		var f any
		switch fd {
		case 0:
			f = r.stdin
		case 1:
			f = r.stdout
		case 2:
			f = r.stderr
		}
		if f, ok := f.(interface{ Fd() uintptr }); ok {
			// Support [os.File.Fd] methods such as the one on [*os.File].
			return term.IsTerminal(int(f.Fd()))
		}
		// TODO: allow term.IsTerminal here too if running in the
		// "single process" mode.
		return false
	case syntax.TsEmpStr:
		return x == ""
	case syntax.TsNempStr:
		return x != ""
	case syntax.TsOptSet:
		if opt := r.posixOptByName(x); opt != nil {
			return *opt
		}
		return false
	case syntax.TsVarSet:
		return r.lookupVar(x).IsSet()
	case syntax.TsRefVar:
		return r.lookupVar(x).Kind == expand.NameRef
	case syntax.TsNot:
		return x == ""
	case syntax.TsUsrOwn, syntax.TsGrpOwn:
		return r.unTestOwnOrGrp(ctx, op, x)
	default:
		// Should only happen if we forgot a case above.
		panic(fmt.Sprintf("unexpected unary test op: %v", op))
	}
}
