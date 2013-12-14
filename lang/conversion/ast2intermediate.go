package conversion

import (
	"fmt"
	. "github.com/draftcode/sandal/lang/data"
	"strconv"
	"strings"
)

func convertASTToIntModule(defs []Definition) (error, []intModule) {
	converter := newIntModConverter()
	for _, def := range defs {
		switch def := def.(type) {
		case DataDefinition:
			for _, elem := range def.Elems {
				converter.env.add(elem, intInternalLiteral{
					Lit: elem,
					Type: NamedType{def.Name},
				})
			}
			converter.env.add(def.Name, intInternalDataTypeDef{
				Elems: def.Elems,
			})
		case ModuleDefinition:
			// TODO
		case ConstantDefinition:
			converter.env.add(def.Name, intInternalConstantDef{
				Type: def.Type,
				Expr: def.Expr,
			})
		case ProcDefinition:
			converter.env.add(def.Name, intInternalProcDef{
				Def: def,
			})
		case InitBlock:
			// Do nothing
		}
	}
	for _, def := range defs {
		switch def := def.(type) {
		case InitBlock:
			converter.convertInitBlock(def)
		}
	}
	if err := converter.buildMainModule(); err != nil {
		return err, nil
	}
	return nil, converter.modules
}

// ========================================

type intModConverter struct {
	env      *varEnv
	channels []intInternalObj
	procs    []intInternalProcVar
	modules  []intModule
	pid      int
}

func newIntModConverter() (converter *intModConverter) {
	converter = new(intModConverter)
	converter.env = newVarEnv()
	return
}

func (x *intModConverter) pushEnv() {
	x.env = newVarEnvFromUpper(x.env)
}

func (x *intModConverter) popEnv() {
	x.env = x.env.upper
}

func (x *intModConverter) convertInitBlock(def InitBlock) error {
	x.pushEnv()
	defer x.popEnv()
	for _, initVar := range def.Vars {
		switch initVar := initVar.(type) {
		case InstanceVar:
			// Do nothing
		case ChannelVar:
			err, chVar := x.buildChannelVar(initVar.Name, initVar.Type)
			if err != nil {
				return err
			}
			x.env.add(initVar.Name, chVar)
		default:
			panic("Unknown InitVar")
		}
	}
	for _, initVar := range def.Vars {
		switch initVar := initVar.(type) {
		case InstanceVar:
			err := x.buildProcVar(initVar)
			if err != nil {
				return err
			}
		case ChannelVar:
			// Do nothing
		}
	}
	return nil
}

func (x *intModConverter) buildMainModule() error {
	if len(x.procs) == 0 {
		return fmt.Errorf("No running procs")
	}
	pids := make([]int, len(x.procs))
	pidStrs := make([]string, len(x.procs))
	for i, proc := range x.procs {
		pids[i] = proc.Pid
		pidStrs[i] = strconv.Itoa(proc.Pid)
	}

	module := intMainModule{}
	// Vars
	for _, chVar := range x.channels {
		switch chVar := chVar.(type) {
		case intInternalHandshakeChannelVar:
			args := []string{"running_pid", chVar.RealName + "_filled", chVar.RealName + "_received"}
			for i := 0; i < len(chVar.Type.Elems); i++ {
				args = append(args, fmt.Sprintf("%s_value_%d", chVar.RealName, i))
			}
			module.Vars = append(module.Vars, intVar{
				Name: chVar.RealName,
				Type: fmt.Sprintf("%s(%s)", chVar.ModuleName, argJoin(args)),
			})
			// TODO: each proxy should set default
			for pid, _ := range chVar.Pids {
				module.Vars = append(module.Vars, intVar{
					Name: fmt.Sprintf("__pid%d_%s", pid, chVar.RealName),
					Type: fmt.Sprintf("%sProxy(%s)", chVar.ModuleName, chVar.RealName),
				})
			}
		case intInternalBufferedChannelVar:
			// TODO
		default:
			panic("Unknown channel value")
		}
	}
	for _, procVal := range x.procs {
		args := []string{"running_pid", strconv.Itoa(procVal.Pid)}
		for _, arg := range procVal.Args {
			if arrayArg, isArrayLit := arg.(intInternalArrayLiteral); isArrayLit {
				args = append(args, arrayArg.ArgString()...)
			} else {
				args = append(args, arg.String())
			}
		}
		module.Vars = append(module.Vars, intVar{
			Name: procVal.Name,
			Type: fmt.Sprintf("%s(%s)", procVal.ModuleName, argJoin(args)),
		})
	}
	module.Vars = append(module.Vars, intVar{"running_pid", "{" + argJoin(pidStrs) + "}"})

	// Assigns
	module.Assigns = append(module.Assigns, intAssign{"running_pid", "{" + argJoin(pidStrs) + "}"})

	// Defs
	for _, chVar := range x.channels {
		switch chVar := chVar.(type) {
		case intInternalHandshakeChannelVar:
			nextFilled := []string{}
			nextReceived := []string{}
			nextValues := make([][]string, len(chVar.Type.Elems))
			for _, pid := range pids {
				if chVar.Pids[pid] {
					nextFilled = append(nextFilled, fmt.Sprintf("__pid%d_%s.next_filled", pid, chVar.RealName))
					nextReceived = append(nextReceived, fmt.Sprintf("__pid%d_%s.next_received", pid, chVar.RealName))
					for i := 0; i < len(chVar.Type.Elems); i++ {
						nextValues[i] = append(nextValues[i], fmt.Sprintf("__pid%d_%s.next_value_%d", pid, chVar.RealName, i))
					}
				} else {
					nextFilled = append(nextFilled, fmt.Sprintf("%s.filled", chVar.RealName))
					nextReceived = append(nextReceived, fmt.Sprintf("%s.received", chVar.RealName))
					for i := 0; i < len(chVar.Type.Elems); i++ {
						nextValues[i] = append(nextValues[i], fmt.Sprintf("%s.value_%d", chVar.RealName, i))
					}
				}
			}
			module.Defs = append(module.Defs, intAssign{chVar.RealName + "_filled", "[" + argJoin(nextFilled) + "]"})
			module.Defs = append(module.Defs, intAssign{chVar.RealName + "_received", "[" + argJoin(nextReceived) + "]"})
			for i := 0; i < len(chVar.Type.Elems); i++ {
				module.Defs = append(module.Defs, intAssign{
					LHS: fmt.Sprintf("%s_value_%d", chVar.RealName, i),
					RHS: "[" + argJoin(nextValues[i]) + "]",
				})
			}
		case intInternalBufferedChannelVar:
			// TODO
		}
	}

	x.modules = append(x.modules, module)
	return nil
}

func (x *intModConverter) buildChannelVar(name string, ty Type) (error, intInternalObj) {
	chNumber := len(x.channels)
	var mod intModule
	var chVar intInternalObj
	switch ty := ty.(type) {
	case HandshakeChannelType:
		types := []string{}
		for _, elem := range ty.Elems {
			types = append(types, convertTypeToString(elem, x.env))
		}
		moduleName := fmt.Sprintf("HandshakeChannel%d", chNumber)
		mod = intHandshakeChannel{
			Name:      moduleName,
			ValueType: types,
		}
		chVar = intInternalHandshakeChannelVar{
			ModuleName: moduleName,
			RealName:   name,
			Type:       ty,
			Pids:       make(map[int]bool),
		}
	case BufferedChannelType:
		types := []string{}
		for _, elem := range ty.Elems {
			types = append(types, convertTypeToString(elem, x.env))
		}
		moduleName := fmt.Sprintf("BufferedChannel%d", chNumber)
		mod = intBufferedChannel{
			Name:      moduleName,
			Length:    x.calculateConstExpression(ty.BufferSize),
			ValueType: types,
		}
		chVar = intInternalBufferedChannelVar{
			ModuleName: moduleName,
			RealName:   name,
			Type:       ty,
			Pids:       make(map[int]bool),
		}
	default:
		panic("Unknown channel type")
	}
	x.modules = append(x.modules, mod)
	x.channels = append(x.channels, chVar)
	return nil, chVar
}

func (x *intModConverter) buildProcVar(initVar InstanceVar) error {
	// Find intInternalProcDef from ProcDefName
	intVal := x.env.lookup(initVar.ProcDefName)
	if intVal == nil {
		panic(initVar.ProcDefName + " should be found in env")
	}
	var intProcDef intInternalProcDef
	if def, ok := intVal.(intInternalProcDef); ok {
		intProcDef = def
	} else {
		panic(initVar.ProcDefName + " should be a intInternalProcDef")
	}

	x.pid = len(x.procs)
	args := []intInternalExpressionObj{}
	for _, arg := range initVar.Args {
		args = append(args, changeToProxy(expressionToInternalObj(arg, x.env), x.pid))
	}
	moduleName := fmt.Sprintf("__pid%d_%s", x.pid, initVar.ProcDefName)
	x.instantiateProcDef(intProcDef, moduleName, args)
	procvar := intInternalProcVar{
		Name:       initVar.Name,
		ModuleName: moduleName,
		Def:        intProcDef,
		Args:       args,
		Pid:        x.pid,
	}
	x.procs = append(x.procs, procvar)
	return nil
}

func (x *intModConverter) instantiateProcDef(def intInternalProcDef, moduleName string, args []intInternalExpressionObj) {
	x.pushEnv()
	defer x.popEnv()

	addHandshakeChannelDefaults := func(paramName string, numElems int, defaults map[string]string) {
		defaults[paramName+".next_filled"] = paramName + ".filled"
		defaults[paramName+".next_received"] = paramName + ".received"
		for i := 0; i < numElems; i++ {
			defaults[fmt.Sprintf("%s.next_value_%d", paramName, i)] = fmt.Sprintf("%s.value_%d", paramName, i)
		}
	}

	params := []string{"running_pid", "pid"}
	defaults := make(map[string]string)
	for idx, arg := range args {
		param := def.Def.Parameters[idx]
		switch arg := arg.(type) {
		case intInternalArrayLiteral:
			for i := 0; i < len(arg.Elems); i++ {
				paramName := fmt.Sprintf("__elem%d_%s", i, param.Name)
				params = append(params, paramName)
				switch elem := arg.Elems[i].(type) {
				case intInternalHandshakeChannelProxyVar:
					addHandshakeChannelDefaults(paramName, len(elem.ChannelVar.Type.Elems), defaults)
				case intInternalBufferedChannelProxyVar:
					panic("not implemented")
				}
			}
			x.env.add(param.Name, intInternalArrayVar{param.Name, arg})
		case intInternalHandshakeChannelProxyVar:
			params = append(params, param.Name)
			addHandshakeChannelDefaults(param.Name, len(arg.ChannelVar.Type.Elems), defaults)
			x.env.add(param.Name, intInternalPrimitiveVar{param.Name, param.Type})
		case intInternalBufferedChannelProxyVar:
			panic("not implemented")
		case intInternalLiteral, intInternalNot, intInternalUnarySub, intInternalParen, intInternalBinOp:
			params = append(params, param.Name)
			x.env.add(param.Name, intInternalPrimitiveVar{param.Name, param.Type})
		default:
			panic("unexpected")
		}
	}
	vars, initState, trans := x.convertStatements(def.Def.Statements, defaults)

	x.modules = append(x.modules, intProcModule{
		Name:      moduleName,
		Args:      params,
		Vars:      vars,
		InitState: initState,
		Trans:     trans,
		Defaults:  defaults,
	})
}

func convertTypeToString(ty Type, env *varEnv) string {
	// TODO
	switch ty := ty.(type) {
	case NamedType:
		switch ty.Name {
		case "bool":
			return "boolean"
		case "int":
			return "0..8"
		default:
			switch intObj := env.lookup(ty.Name).(type) {
			case intInternalDataTypeDef:
				return "{" + argJoin(intObj.Elems) + "}"
			default:
				_ = intObj
				panic("unknown type")
			}
		}
	default:
		return ty.String()
	}
}

func (x *intModConverter) calculateConstExpression(expr Expression) int {
	// TODO
	return 0
}

// ========================================

func argJoin(args []string) string {
	return strings.Join(args, ", ")
}

