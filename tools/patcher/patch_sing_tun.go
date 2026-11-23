// Semantic AST rules for MetaCubeX/sing-tun.
//
// Problem:
// On multi-homed Windows hosts using Automatic Metric, active adapters often share
// equal or close default-route combined metrics (row.Metric + iface.Metric). Minor
// link-speed updates or Hyper-V VmSwitch events cause DefaultInterfaceMonitor to
// flip repeatedly between interfaces, and equal-metric ties depend on OS table
// iteration order instead of keeping the active interface.
//
// Solution:
// 1. Add PreferredInterfaces []string to DefaultInterfaceMonitorOptions (monitor.go).
// 2. Store filtered preferredInterfaces on defaultInterfaceMonitor and wire it in
//    NewDefaultInterfaceMonitor (monitor_shared.go).
// 3. Replace checkUpdate() lowestMetric tracking in monitor_windows.go with the
//    overlay defaultRouteSelector helper, honoring PreferredInterfaces order first
//    and breaking equal-metric ties in favor of the active interface.

package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
)

func patchSingTun(root string) error {
	if err := patchSingTunMonitorGo(filepath.Join(root, "monitor.go")); err != nil {
		return err
	}
	if err := patchSingTunMonitorSharedGo(filepath.Join(root, "monitor_shared.go")); err != nil {
		return err
	}
	if err := patchSingTunMonitorWindowsGo(filepath.Join(root, "monitor_windows.go")); err != nil {
		return err
	}
	return nil
}

func patchSingTunMonitorGo(path string) error {
	return patchFile(path, func(fset *token.FileSet, file *ast.File) error {
		st, err := findStructType(file, "DefaultInterfaceMonitorOptions")
		if err != nil {
			return err
		}
		addStructField(st, "PreferredInterfaces", mustParseField("PreferredInterfaces []string"))
		return nil
	})
}

func patchSingTunMonitorSharedGo(path string) error {
	return patchFile(path, func(fset *token.FileSet, file *ast.File) error {
		st, err := findStructType(file, "defaultInterfaceMonitor")
		if err != nil {
			return err
		}
		addStructField(st, "preferredInterfaces", mustParseField("preferredInterfaces []string"))

		fn, err := findFuncDecl(file, "", "NewDefaultInterfaceMonitor")
		if err != nil {
			return err
		}
		optParam := paramName(fn, 2, "options")

		var targetLit *ast.CompositeLit
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if targetLit != nil {
				return false
			}
			lit, ok := n.(*ast.CompositeLit)
			if ok && typeExprMatches(lit.Type, "", "defaultInterfaceMonitor") {
				targetLit = lit
				return false
			}
			return true
		})
		if targetLit == nil {
			return fmt.Errorf("composite literal &defaultInterfaceMonitor{...} not found in NewDefaultInterfaceMonitor")
		}
		addCompositeLitKV(targetLit, "preferredInterfaces", mustParseExpr(fmt.Sprintf("filterPreferredInterfaces(%s.PreferredInterfaces)", optParam)))
		return nil
	})
}

func patchSingTunMonitorWindowsGo(path string) error {
	return patchFile(path, func(fset *token.FileSet, file *ast.File) error {
		fn, err := findFuncDecl(file, "defaultInterfaceMonitor", "checkUpdate")
		if err != nil {
			return err
		}
		recv := receiverName(fn)
		if recv == "" {
			return fmt.Errorf("checkUpdate has unnamed receiver")
		}

		// Drop lowestMetric initialization only if present and track that we found it.
		foundLowestInit := false
		filtered := make([]ast.Stmt, 0, len(fn.Body.List))
		for _, stmt := range fn.Body.List {
			if isAssignToIdent(stmt, "lowestMetric") {
				foundLowestInit = true
				continue
			}
			filtered = append(filtered, stmt)
		}
		if !foundLowestInit {
			return fmt.Errorf("lowestMetric initialization not found in checkUpdate")
		}
		fn.Body.List = filtered

		rangeIdx := -1
		var rangeStmt *ast.RangeStmt
		for i, stmt := range fn.Body.List {
			rs, ok := stmt.(*ast.RangeStmt)
			if ok && exprIsIdent(rs.X, "rows") {
				rangeIdx = i
				rangeStmt = rs
				break
			}
		}
		if rangeStmt == nil {
			return fmt.Errorf("range over rows not found in checkUpdate")
		}

		replacedMetricIf := false
		for i, stmt := range rangeStmt.Body.List {
			ifStmt, ok := stmt.(*ast.IfStmt)
			if !ok || ifStmt.Init != nil {
				continue
			}
			bin, ok := ifStmt.Cond.(*ast.BinaryExpr)
			if !ok || bin.Op != token.LSS {
				continue
			}
			if exprIsIdent(bin.X, "metric") && exprIsIdent(bin.Y, "lowestMetric") {
				rangeStmt.Body.List[i] = mustParseStmt("_routeSelector.add(ifrow.Alias(), int(ifrow.InterfaceIndex), metric)")
				replacedMetricIf = true
				break
			}
		}
		if !replacedMetricIf {
			return fmt.Errorf("if metric < lowestMetric statement not found in checkUpdate rows loop")
		}

		preStmt := mustParseStmt("var _routeSelector defaultRouteSelector")
		postStmt := mustParseStmt(fmt.Sprintf("if selectedAlias, selectedIndex, ok := _routeSelector.selectFor(%s); ok { alias = selectedAlias; index = selectedIndex }", recv))

		out := make([]ast.Stmt, 0, len(fn.Body.List)+2)
		out = append(out, fn.Body.List[:rangeIdx]...)
		out = append(out, preStmt, rangeStmt, postStmt)
		out = append(out, fn.Body.List[rangeIdx+1:]...)
		fn.Body.List = out
		return nil
	})
}

func isAssignToIdent(stmt ast.Stmt, name string) bool {
	as, ok := stmt.(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 1 {
		return false
	}
	return exprIsIdent(as.Lhs[0], name)
}
