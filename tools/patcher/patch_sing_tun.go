// Semantic AST rules for MetaCubeX/sing-tun.
//
// Problem:
// On multi-homed hosts across Windows, Linux, and macOS, default-route selection
// previously relied solely on raw OS metric (Automatic Metric / netlink Priority)
// or BSD RIB iteration order, causing checkUpdate() oscillations or ignoring the
// user's preferred interface order.
//
// Solution:
// 1. Add PreferredInterfaces []string to DefaultInterfaceMonitorOptions (monitor.go).
// 2. Store filtered preferredInterfaces and routeTable on defaultInterfaceMonitor,
//    wire PreferredInterfaces in NewDefaultInterfaceMonitor, and refresh the OS
//    non-default route table in postCheckUpdate (monitor_shared.go).
// 3. Replace checkUpdate() selection in monitor_windows.go, monitor_linux_default.go,
//    and monitor_darwin.go with the overlay defaultRouteSelector helper, honoring
//    PreferredInterfaces order first and breaking equal-metric ties in favor of the
//    active interface.

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
	if err := patchSingTunMonitorLinuxDefaultGo(filepath.Join(root, "monitor_linux_default.go")); err != nil {
		return err
	}
	if err := patchSingTunMonitorDarwinGo(filepath.Join(root, "monitor_darwin.go")); err != nil {
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
		addStructField(st, "routeTable", mustParseField("routeTable atomic.Pointer[routeTable]"))

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

		postFn, err := findFuncDecl(file, "defaultInterfaceMonitor", "postCheckUpdate")
		if err != nil {
			return err
		}
		postRecv := receiverName(postFn)
		if postRecv == "" {
			return fmt.Errorf("postCheckUpdate has unnamed receiver")
		}
		checkIdx := -1
		for i, stmt := range postFn.Body.List {
			as, ok := stmt.(*ast.AssignStmt)
			if !ok || len(as.Rhs) != 1 {
				continue
			}
			call, ok := as.Rhs[0].(*ast.CallExpr)
			if !ok || len(call.Args) != 0 {
				continue
			}
			if exprIsSelector(call.Fun, postRecv, "checkUpdate") {
				checkIdx = i
				break
			}
		}
		if checkIdx < 0 {
			return fmt.Errorf("%s.checkUpdate() call not found in postCheckUpdate", postRecv)
		}
		postFn.Body.List = insertStmts(postFn.Body.List, checkIdx, mustParseStmt(fmt.Sprintf("%s.refreshRouteTable()", postRecv)))
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

func patchSingTunMonitorLinuxDefaultGo(path string) error {
	return patchFile(path, func(fset *token.FileSet, file *ast.File) error {
		fn, err := findFuncDecl(file, "defaultInterfaceMonitor", "checkUpdate")
		if err != nil {
			return err
		}
		recv := receiverName(fn)
		if recv == "" {
			return fmt.Errorf("checkUpdate has unnamed receiver")
		}

		rangeIdx := -1
		var rangeStmt *ast.RangeStmt
		for i, stmt := range fn.Body.List {
			rs, ok := stmt.(*ast.RangeStmt)
			if ok && exprIsIdent(rs.X, "routes") {
				rangeIdx = i
				rangeStmt = rs
				break
			}
		}
		if rangeStmt == nil {
			return fmt.Errorf("range over routes not found in checkUpdate")
		}
		routeIdent, ok := rangeStmt.Value.(*ast.Ident)
		if !ok || routeIdent.Name == "" {
			return fmt.Errorf("routes range value identifier not found in checkUpdate")
		}
		routeVar := routeIdent.Name

		linkAssignIdx := -1
		linkVar := ""
		for i, stmt := range rangeStmt.Body.List {
			as, ok := stmt.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != 2 || len(as.Rhs) != 1 {
				continue
			}
			call, ok := as.Rhs[0].(*ast.CallExpr)
			if !ok || !exprIsSelector(call.Fun, "netlink", "LinkByIndex") {
				continue
			}
			if id, ok := as.Lhs[0].(*ast.Ident); ok {
				linkAssignIdx = i
				linkVar = id.Name
				break
			}
		}
		if linkAssignIdx < 0 || linkVar == "" {
			return fmt.Errorf("netlink.LinkByIndex assignment not found in checkUpdate")
		}
		if linkAssignIdx+1 >= len(rangeStmt.Body.List) {
			return fmt.Errorf("expected if err != nil check after netlink.LinkByIndex")
		}
		ifStmt, ok := rangeStmt.Body.List[linkAssignIdx+1].(*ast.IfStmt)
		if !ok {
			return fmt.Errorf("expected if err != nil statement after netlink.LinkByIndex")
		}

		foundSwap := false
		for _, stmt := range rangeStmt.Body.List[linkAssignIdx+2:] {
			ast.Inspect(stmt, func(n ast.Node) bool {
				if foundSwap {
					return false
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Swap" {
					foundSwap = true
					return false
				}
				return true
			})
			if foundSwap {
				break
			}
		}
		if !foundSwap {
			return fmt.Errorf("defaultInterface.Swap anchor not found in routes loop")
		}

		n := len(fn.Body.List)
		if n == 0 {
			return fmt.Errorf("checkUpdate body is empty")
		}
		retStmt, ok := fn.Body.List[n-1].(*ast.ReturnStmt)
		if !ok || len(retStmt.Results) != 1 || !exprIsIdent(retStmt.Results[0], "ErrNoRoute") {
			return fmt.Errorf("checkUpdate does not end with return ErrNoRoute")
		}

		ifStmt.Body.List = mustParseStmts("continue")
		loopAddStmts := mustParseStmts(fmt.Sprintf(
			"var _routeMetric uint32\nif %s.Priority > 0 {\n_routeMetric = uint32(%s.Priority)\n}\n_routeSelector.add(%s.Attrs().Name, %s.Attrs().Index, _routeMetric)",
			routeVar, routeVar, linkVar, linkVar,
		))
		newLoopBody := make([]ast.Stmt, 0, linkAssignIdx+2+len(loopAddStmts))
		newLoopBody = append(newLoopBody, rangeStmt.Body.List[:linkAssignIdx+2]...)
		newLoopBody = append(newLoopBody, loopAddStmts...)
		rangeStmt.Body.List = newLoopBody

		preStmt := mustParseStmt("var _routeSelector defaultRouteSelector")
		postStmts := mustParseStmts(fmt.Sprintf(
			"selectedAlias, selectedIndex, ok := _routeSelector.selectFor(%s)\nif !ok {\nreturn ErrNoRoute\n}\nnewInterface, err := %s.interfaceFinder.ByIndex(selectedIndex)\nif err != nil {\nreturn E.Cause(err, \"find updated interface: \", selectedAlias)\n}\noldInterface := %s.defaultInterface.Swap(newInterface)\nif oldInterface != nil && oldInterface.Equals(*newInterface) {\nreturn nil\n}\n%s.emit(newInterface, 0)\nreturn nil",
			recv, recv, recv, recv,
		))

		out := make([]ast.Stmt, 0, len(fn.Body.List)+len(postStmts))
		out = append(out, fn.Body.List[:rangeIdx]...)
		out = append(out, preStmt, rangeStmt)
		out = append(out, fn.Body.List[rangeIdx+1:n-1]...)
		out = append(out, postStmts...)
		fn.Body.List = out
		return nil
	})
}

func patchSingTunMonitorDarwinGo(path string) error {
	return patchFile(path, func(fset *token.FileSet, file *ast.File) error {
		fn, err := findFuncDecl(file, "defaultInterfaceMonitor", "checkUpdate")
		if err != nil {
			return err
		}
		recv := receiverName(fn)
		if recv == "" {
			return fmt.Errorf("checkUpdate has unnamed receiver")
		}

		var parentBlock *ast.BlockStmt
		rangeIdx := -1
		var rangeStmt *ast.RangeStmt
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if rangeStmt != nil {
				return false
			}
			block, ok := n.(*ast.BlockStmt)
			if !ok {
				return true
			}
			for i, stmt := range block.List {
				rs, ok := stmt.(*ast.RangeStmt)
				if ok && exprIsIdent(rs.X, "routeMessages") {
					parentBlock = block
					rangeIdx = i
					rangeStmt = rs
					return false
				}
			}
			return true
		})
		if rangeStmt == nil || parentBlock == nil {
			return fmt.Errorf("range over routeMessages not found in checkUpdate")
		}

		routeMsgVar := ""
		byIndexIdx := -1
		routeIfVar := ""
		assignIdx := -1
		for i, stmt := range rangeStmt.Body.List {
			if routeMsgVar == "" {
				if as, ok := stmt.(*ast.AssignStmt); ok && len(as.Lhs) == 1 && len(as.Rhs) == 1 {
					if _, ok := as.Rhs[0].(*ast.TypeAssertExpr); ok {
						if id, ok := as.Lhs[0].(*ast.Ident); ok {
							routeMsgVar = id.Name
						}
					}
				}
			}
			if byIndexIdx < 0 {
				if as, ok := stmt.(*ast.AssignStmt); ok && len(as.Lhs) == 2 && len(as.Rhs) == 1 {
					if call, ok := as.Rhs[0].(*ast.CallExpr); ok {
						if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "ByIndex" {
							if id, ok := as.Lhs[0].(*ast.Ident); ok {
								byIndexIdx = i
								routeIfVar = id.Name
							}
						}
					}
				}
			}
			if assignIdx < 0 && routeIfVar != "" && i+1 < len(rangeStmt.Body.List) {
				as, ok := stmt.(*ast.AssignStmt)
				br, ok2 := rangeStmt.Body.List[i+1].(*ast.BranchStmt)
				if ok && ok2 && as.Tok == token.ASSIGN && len(as.Lhs) == 1 && len(as.Rhs) == 1 &&
					exprIsIdent(as.Lhs[0], "defaultInterface") && exprIsIdent(as.Rhs[0], routeIfVar) &&
					br.Tok == token.BREAK {
					assignIdx = i
				}
			}
		}
		if routeMsgVar == "" {
			return fmt.Errorf("routeMessage type assertion not found in routeMessages loop")
		}
		if byIndexIdx < 0 || routeIfVar == "" {
			return fmt.Errorf("ByIndex assignment not found in routeMessages loop")
		}
		if assignIdx < 0 {
			return fmt.Errorf("defaultInterface = %s; break anchor not found in routeMessages loop", routeIfVar)
		}

		if byIndexIdx+1 < len(rangeStmt.Body.List) {
			if ifStmt, ok := rangeStmt.Body.List[byIndexIdx+1].(*ast.IfStmt); ok {
				ifStmt.Body.List = mustParseStmts("continue")
			}
		}

		collectStmts := mustParseStmts(fmt.Sprintf(
			"_routeMetric := _routeOrder\n_routeOrder++\nif %s.Flags&unix.RTF_IFSCOPE != 0 {\n_routeMetric += 1 << 30\n}\n_routeSelector.add(%s.Name, %s.Index, _routeMetric)",
			routeMsgVar, routeIfVar, routeIfVar,
		))
		newLoopBody := make([]ast.Stmt, 0, len(rangeStmt.Body.List)-2+len(collectStmts))
		newLoopBody = append(newLoopBody, rangeStmt.Body.List[:assignIdx]...)
		newLoopBody = append(newLoopBody, collectStmts...)
		newLoopBody = append(newLoopBody, rangeStmt.Body.List[assignIdx+2:]...)
		rangeStmt.Body.List = newLoopBody

		preStmts := mustParseStmts("var _routeSelector defaultRouteSelector\nvar _routeOrder uint32")
		postStmt := mustParseStmt(fmt.Sprintf(
			"if _, selectedIndex, ok := _routeSelector.selectFor(%s); ok {\nselectedInterface, findErr := %s.interfaceFinder.ByIndex(selectedIndex)\nif findErr != nil {\nreturn findErr\n}\ndefaultInterface = selectedInterface\n}",
			recv, recv,
		))

		parentBlock.List = insertStmts(parentBlock.List, rangeIdx+1, postStmt)
		parentBlock.List = insertStmts(parentBlock.List, rangeIdx, preStmts...)
		return nil
	})
}
