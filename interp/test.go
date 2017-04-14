// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"fmt"

	"github.com/mvdan/sh/syntax"
)

// non-empty string is true, empty string is false
func (r *Runner) bashTest(expr syntax.TestExpr) string {
	switch x := expr.(type) {
	case *syntax.Word:
		return r.loneWord(x)
	case *syntax.ParenTest:
		return r.bashTest(x.X)
	case *syntax.BinaryTest:
		if r.binTest(x.Op, r.bashTest(x.X), r.bashTest(x.Y)) {
			return "1"
		}
		return ""
	case *syntax.UnaryTest:
		if r.unTest(x.Op, r.bashTest(x.X)) {
			return "1"
		}
		return ""
	}
	return ""
}

func (r *Runner) binTest(op syntax.BinTestOperator, x, y string) bool {
	switch op {
	//case syntax.TsReMatch:
	//case syntax.TsNewer:
	//case syntax.TsOlder:
	//case syntax.TsDevIno:
	case syntax.TsEql:
		return atoi(x) == atoi(y)
	case syntax.TsNeq:
		return atoi(x) != atoi(y)
	case syntax.TsLeq:
		return atoi(x) <= atoi(y)
	case syntax.TsGeq:
		return atoi(x) >= atoi(y)
	case syntax.TsLss:
		return atoi(x) < atoi(y)
	case syntax.TsGtr:
		return atoi(x) > atoi(y)
	case syntax.AndTest:
		return x != "" && y != ""
	case syntax.OrTest:
		return x != "" || y != ""
	case syntax.TsEqual:
		return x == y
	case syntax.TsNequal:
		return x != y
	case syntax.TsBefore:
		return x < y
	case syntax.TsAfter:
		return x > y
	default:
		panic(fmt.Sprintf("unhandled binary test op: %v", op))
	}
}

func (r *Runner) unTest(op syntax.UnTestOperator, x string) bool {
	switch op {
	//case syntax.TsExists:
	//case syntax.TsRegFile:
	//case syntax.TsDirect:
	//case syntax.TsCharSp:
	//case syntax.TsBlckSp:
	//case syntax.TsNmPipe:
	//case syntax.TsSocket:
	//case syntax.TsSmbLink:
	//case syntax.TsSticky:
	//case syntax.TsGIDSet:
	//case syntax.TsUIDSet:
	//case syntax.TsGrpOwn:
	//case syntax.TsUsrOwn:
	//case syntax.TsModif:
	//case syntax.TsRead:
	//case syntax.TsWrite:
	//case syntax.TsExec:
	//case syntax.TsNoEmpty:
	//case syntax.TsFdTerm:
	//case syntax.TsEmpStr:
	//case syntax.TsNempStr:
	//case syntax.TsOptSet:
	//case syntax.TsVarSet:
	//case syntax.TsRefVar:
	case syntax.TsNot:
		return x == ""
	default:
		panic(fmt.Sprintf("unhandled unary test op: %v", op))
	}
}
