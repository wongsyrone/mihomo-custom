// Semantic AST rules for MetaCubeX/mihomo TUN configuration and switch handling.
//
// Problem:
// 1. Mihomo's tun configuration has no declarative primary/backup interface priority.
// 2. Official compatibility requires omitting the field when unused while official
//    builds ignore unknown YAML keys via yaml.Unmarshal without KnownFields.
// 3. Switching default interface in listener/sing_tun/server.go only flushes caches
//    and DNS resolver connections without resetting active connections or proxy session pools.
//
// Solution:
// 1. Add DefaultInterfacePriority []string (yaml+json omitempty) to config.RawTun
//    and wire it in parseTun (config/config.go).
// 2. Add DefaultInterfacePriority []string to listener/config.Tun and check
//    slices.Equal inside (*Tun).Equal (listener/config/tun.go).
// 3. Pass options.DefaultInterfacePriority to tun.DefaultInterfaceMonitorOptions and
//    hook interfaceSwitchTracker.onInterfaceUpdate inside DefaultInterfaceMonitor's
//    callback in listener/sing_tun/server.go.

package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
)

func patchMihomoTun(root string) error {
	if err := patchMihomoConfigGo(filepath.Join(root, "config/config.go")); err != nil {
		return err
	}
	if err := patchMihomoListenerConfigTunGo(filepath.Join(root, "listener/config/tun.go")); err != nil {
		return err
	}
	if err := patchMihomoSingTunServerGo(filepath.Join(root, "listener/sing_tun/server.go")); err != nil {
		return err
	}
	return nil
}

func patchMihomoConfigGo(path string) error {
	return patchFile(path, func(fset *token.FileSet, file *ast.File) error {
		st, err := findStructType(file, "RawTun")
		if err != nil {
			return err
		}
		field := mustParseField("DefaultInterfacePriority []string `yaml:\"default-interface-priority,omitempty\" json:\"default-interface-priority,omitempty\"`")
		addStructField(st, "DefaultInterfacePriority", field)

		fn, err := findFuncDecl(file, "", "parseTun")
		if err != nil {
			return err
		}
		rawTunParam := paramName(fn, 0, "rawTun")

		var tunLit *ast.CompositeLit
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if tunLit != nil {
				return false
			}
			lit, ok := n.(*ast.CompositeLit)
			if ok && typeExprMatches(lit.Type, "LC", "Tun") {
				tunLit = lit
				return false
			}
			return true
		})
		if tunLit == nil {
			return fmt.Errorf("LC.Tun composite literal not found in parseTun")
		}
		addCompositeLitKV(tunLit, "DefaultInterfacePriority", mustParseExpr(fmt.Sprintf("%s.DefaultInterfacePriority", rawTunParam)))
		return nil
	})
}

func patchMihomoListenerConfigTunGo(path string) error {
	return patchFile(path, func(fset *token.FileSet, file *ast.File) error {
		st, err := findStructType(file, "Tun")
		if err != nil {
			return err
		}
		field := mustParseField("DefaultInterfacePriority []string `yaml:\"default-interface-priority,omitempty\" json:\"default-interface-priority,omitempty\"`")
		addStructField(st, "DefaultInterfacePriority", field)

		fn, err := findFuncDecl(file, "Tun", "Equal")
		if err != nil {
			return err
		}
		recv := receiverName(fn)
		otherParam := paramName(fn, 0, "other")
		if recv == "" {
			return fmt.Errorf("Tun.Equal has unnamed receiver")
		}

		n := len(fn.Body.List)
		if n == 0 {
			return fmt.Errorf("Tun.Equal body is empty")
		}
		retStmt, ok := fn.Body.List[n-1].(*ast.ReturnStmt)
		if !ok || len(retStmt.Results) != 1 || !exprIsIdent(retStmt.Results[0], "true") {
			return fmt.Errorf("Tun.Equal does not end with return true")
		}

		checkStmt := mustParseStmt(fmt.Sprintf("if !slices.Equal(%s.DefaultInterfacePriority, %s.DefaultInterfacePriority) { return false }", recv, otherParam))
		fn.Body.List = insertStmts(fn.Body.List, n-1, checkStmt)
		return nil
	})
}

func patchMihomoSingTunServerGo(path string) error {
	return patchFile(path, func(fset *token.FileSet, file *ast.File) error {
		fn, err := findFuncDecl(file, "", "New")
		if err != nil {
			return err
		}
		optionsParam := paramName(fn, 0, "options")
		tunnelParam := paramName(fn, 1, "tunnel")

		var optsLit *ast.CompositeLit
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if optsLit != nil {
				return false
			}
			lit, ok := n.(*ast.CompositeLit)
			if ok && typeExprMatches(lit.Type, "tun", "DefaultInterfaceMonitorOptions") {
				optsLit = lit
				return false
			}
			return true
		})
		if optsLit == nil {
			return fmt.Errorf("tun.DefaultInterfaceMonitorOptions composite literal not found in New")
		}
		addCompositeLitKV(optsLit, "PreferredInterfaces", mustParseExpr(fmt.Sprintf("%s.DefaultInterfacePriority", optionsParam)))

		patchedCallback := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if patchedCallback {
				return false
			}
			block, ok := n.(*ast.BlockStmt)
			if !ok {
				return true
			}
			for i, stmt := range block.List {
				es, ok := stmt.(*ast.ExprStmt)
				if !ok {
					continue
				}
				call, ok := es.X.(*ast.CallExpr)
				if !ok || len(call.Args) != 1 {
					continue
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "RegisterCallback" || !exprIsIdent(sel.X, "defaultInterfaceMonitor") {
					continue
				}
				fl, ok := call.Args[0].(*ast.FuncLit)
				if !ok || fl.Body == nil {
					return false
				}
				ifaceArg := fieldListParamName(fl.Type.Params, 0, "defaultInterface")
				fl.Body.List = append(fl.Body.List, mustParseStmt(fmt.Sprintf("_ifSwitchTracker.onInterfaceUpdate(%s, %s)", ifaceArg, tunnelParam)))
				block.List = insertStmts(block.List, i, mustParseStmt("_ifSwitchTracker := interfaceSwitchTracker{tunName: tunName, monitor: defaultInterfaceMonitor}"))
				patchedCallback = true
				return false
			}
			return true
		})
		if !patchedCallback {
			return fmt.Errorf("defaultInterfaceMonitor.RegisterCallback(...) not found in New")
		}

		finderFn, err := findFuncDecl(file, "cDialerInterfaceFinder", "DefaultInterfaceName")
		if err != nil {
			return err
		}
		finderRecv := receiverName(finderFn)
		if finderRecv == "" {
			return fmt.Errorf("cDialerInterfaceFinder.DefaultInterfaceName has unnamed receiver")
		}
		destParam := paramName(finderFn, 0, "destination")
		routeGuard := mustParseStmt(fmt.Sprintf("if routeIfName := %s.routeInterfaceName(%s); routeIfName != \"\" { return routeIfName }", finderRecv, destParam))
		finderFn.Body.List = insertStmts(finderFn.Body.List, 0, routeGuard)
		return nil
	})
}
