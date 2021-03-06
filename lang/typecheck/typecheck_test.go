package typecheck

import (
	. "github.com/k0kubun/sandal/lang/data"
	"testing"
)

func universalTypeCheck(x interface{}, env *typeEnv) error {
	switch x := x.(type) {
	case Def:
		return typeCheckDef(x, env)
	case Stmt:
		return typeCheckStmt(x, env)
	case Expr:
		return typeCheckExpr(x, env)
	}
	panic("Unknown value")
}

func expectValid(t *testing.T, x interface{}, env *typeEnv) {
	if err := universalTypeCheck(x, env); err != nil {
		t.Errorf("Expect %q to be valid, but got an error %q", x, err)
	}
}

func expectInvalid(t *testing.T, x interface{}, env *typeEnv) {
	if err := universalTypeCheck(x, env); err == nil {
		t.Errorf("Expect %q to be invalid", x)
	}
}
