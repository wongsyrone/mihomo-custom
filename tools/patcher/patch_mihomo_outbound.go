// Semantic AST rules hooking ResetSessions() prerequisites inside existing
// MetaCubeX/mihomo outbound and transport files.
//
// Problem:
// Pure overlay methods can implement ResetSessions() on most protocols, but a few
// existing structs/interfaces/methods require minimal semantic hooks so resets do
// not leak, race, or omit sub-transports:
// - WireGuard / AmneziaWG need BindUpdate() on wireguardGoDevice.
// - TUIC v4/v5 need ForceClose() on types.Client and closed checks in getQuicConn.
// - Snell Pool needs atomic generation tracking to reject stale post-reset re-pooling.
// - Hysteria2 needs to retain its Realm HTTP transport reference.
// - MASQUE needs mutex/generation tracking around L3 startLocked cancellation.
//
// Solution:
// Apply symbol-anchored AST edits to those 7 files, leaving autonomous mesh VPN
// daemons (Tailscale, EasyTier, ZeroTier) untouched.

package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
)

func patchMihomoOutbound(root string) error {
	if err := patchWireGuardGo(filepath.Join(root, "adapter/outbound/wireguard.go")); err != nil {
		return err
	}
	if err := patchTuicTypesGo(filepath.Join(root, "transport/tuic/types/type.go")); err != nil {
		return err
	}
	for _, rel := range []string{"transport/tuic/v4/client.go", "transport/tuic/v5/client.go"} {
		if err := patchTuicClientGo(filepath.Join(root, rel)); err != nil {
			return err
		}
	}
	if err := patchSnellPoolGo(filepath.Join(root, "transport/snell/pool.go")); err != nil {
		return err
	}
	if err := patchHysteria2Go(filepath.Join(root, "adapter/outbound/hysteria2.go")); err != nil {
		return err
	}
	if err := patchMasqueGo(filepath.Join(root, "adapter/outbound/masque.go")); err != nil {
		return err
	}
	return nil
}

func patchWireGuardGo(path string) error {
	return patchFile(path, func(fset *token.FileSet, file *ast.File) error {
		it, err := findInterfaceType(file, "wireguardGoDevice")
		if err != nil {
			return err
		}
		addInterfaceMethod(it, "BindUpdate", mustParseInterfaceMethod("BindUpdate() error"))
		return nil
	})
}

func patchTuicTypesGo(path string) error {
	return patchFile(path, func(fset *token.FileSet, file *ast.File) error {
		it, err := findInterfaceType(file, "Client")
		if err != nil {
			return err
		}
		addInterfaceMethod(it, "ForceClose", mustParseInterfaceMethod("ForceClose()"))
		return nil
	})
}

func patchTuicClientGo(path string) error {
	return patchFile(path, func(fset *token.FileSet, file *ast.File) error {
		fn, err := findFuncDecl(file, "clientImpl", "getQuicConn")
		if err != nil {
			return err
		}
		recv := receiverName(fn)
		if recv == "" {
			return fmt.Errorf("clientImpl.getQuicConn has unnamed receiver")
		}

		// Find `defer <recv>.connMutex.Unlock()` and insert closed check right after it.
		deferIdx := -1
		dialStmtIdx := -1
		dialVar := ""
		for i, stmt := range fn.Body.List {
			if deferIdx < 0 {
				if ds, ok := stmt.(*ast.DeferStmt); ok {
					if sel, ok := ds.Call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Unlock" {
						deferIdx = i
						continue
					}
				}
			}
			if as, ok := stmt.(*ast.AssignStmt); ok && len(as.Lhs) == 2 && len(as.Rhs) == 1 {
				if call, ok := as.Rhs[0].(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "dialFn" {
						if id, ok := as.Lhs[0].(*ast.Ident); ok {
							dialStmtIdx = i
							dialVar = id.Name
						}
					}
				}
			}
		}
		if deferIdx < 0 {
			return fmt.Errorf("defer Unlock not found in clientImpl.getQuicConn")
		}
		if dialStmtIdx < 0 || dialVar == "" {
			return fmt.Errorf("dialFn assignment not found in clientImpl.getQuicConn")
		}
		if dialStmtIdx+1 >= len(fn.Body.List) {
			return fmt.Errorf("expected error check immediately after dialFn assignment")
		}
		if _, ok := fn.Body.List[dialStmtIdx+1].(*ast.IfStmt); !ok {
			return fmt.Errorf("expected if err != nil statement after dialFn assignment")
		}

		preCheck := mustParseStmt(fmt.Sprintf("if %s.closed.Load() { return nil, types.ClientClosed }", recv))
		postDialCheck := mustParseStmt(fmt.Sprintf("if %s.closed.Load() { _ = %s.CloseWithError(ProtocolError, types.ClientClosed.Error()); return nil, types.ClientClosed }", recv, dialVar))

		// Insert postDialCheck first (higher index) so deferIdx+1 stays valid.
		fn.Body.List = insertStmts(fn.Body.List, dialStmtIdx+2, postDialCheck)
		fn.Body.List = insertStmts(fn.Body.List, deferIdx+1, preCheck)
		return nil
	})
}

func patchSnellPoolGo(path string) error {
	return patchFile(path, func(fset *token.FileSet, file *ast.File) error {
		poolSt, err := findStructType(file, "Pool")
		if err != nil {
			return err
		}
		addStructField(poolSt, "gen", mustParseField("gen atomic.Uint64"))

		pcSt, err := findStructType(file, "PoolConn")
		if err != nil {
			return err
		}
		addStructField(pcSt, "gen", mustParseField("gen uint64"))

		getFn, err := findFuncDecl(file, "Pool", "GetContext")
		if err != nil {
			return err
		}
		poolRecv := receiverName(getFn)
		if poolRecv == "" {
			return fmt.Errorf("Pool.GetContext has unnamed receiver")
		}
		var pcLit *ast.CompositeLit
		ast.Inspect(getFn.Body, func(n ast.Node) bool {
			if pcLit != nil {
				return false
			}
			lit, ok := n.(*ast.CompositeLit)
			if ok && typeExprMatches(lit.Type, "", "PoolConn") {
				pcLit = lit
				return false
			}
			return true
		})
		if pcLit == nil {
			return fmt.Errorf("PoolConn composite literal not found in Pool.GetContext")
		}
		addCompositeLitKV(pcLit, "gen", mustParseExpr("gen"))
		getFn.Body.List = insertStmts(getFn.Body.List, 0, mustParseStmt(fmt.Sprintf("gen := %s.gen.Load()", poolRecv)))

		closeFn, err := findFuncDecl(file, "PoolConn", "Close")
		if err != nil {
			return err
		}
		pcRecv := receiverName(closeFn)
		if pcRecv == "" {
			return fmt.Errorf("PoolConn.Close has unnamed receiver")
		}

		updatedCond := false
		ast.Inspect(closeFn.Body, func(n ast.Node) bool {
			if updatedCond {
				return false
			}
			ifStmt, ok := n.(*ast.IfStmt)
			if !ok {
				return true
			}
			un, ok := ifStmt.Cond.(*ast.UnaryExpr)
			if !ok || un.Op != token.NOT {
				return true
			}
			call, ok := un.X.(*ast.CallExpr)
			if !ok || len(call.Args) != 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Load" || !exprIsSelector(sel.X, pcRecv, "peerClosed") {
				return true
			}
			ifStmt.Cond = mustParseExpr(fmt.Sprintf("!%s.peerClosed.Load() || (%s.pool != nil && %s.gen != %s.pool.gen.Load())", pcRecv, pcRecv, pcRecv, pcRecv))
			updatedCond = true
			return false
		})
		if !updatedCond {
			return fmt.Errorf("if !peerClosed.Load() statement not found in PoolConn.Close")
		}
		return nil
	})
}

func patchHysteria2Go(path string) error {
	return patchFile(path, func(fset *token.FileSet, file *ast.File) error {
		st, err := findStructType(file, "Hysteria2")
		if err != nil {
			return err
		}
		addStructField(st, "realmHTTPTransport", mustParseField("realmHTTPTransport *http.Transport"))

		fn, err := findFuncDecl(file, "", "NewHysteria2")
		if err != nil {
			return err
		}

		patched := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if patched {
				return false
			}
			block, ok := n.(*ast.BlockStmt)
			if !ok {
				return true
			}
			for i, stmt := range block.List {
				as, ok := stmt.(*ast.AssignStmt)
				if !ok || len(as.Lhs) != 1 {
					continue
				}
				if exprIsSelector(as.Lhs[0], "clientOptions", "RealmOptions") {
					capture := mustParseStmt("if clientOptions.RealmOptions != nil && clientOptions.RealmOptions.HTTPClient != nil { if tr, ok := clientOptions.RealmOptions.HTTPClient.Transport.(*http.Transport); ok { outbound.realmHTTPTransport = tr } }")
					block.List = insertStmts(block.List, i+1, capture)
					patched = true
					return false
				}
			}
			return true
		})
		if !patched {
			return fmt.Errorf("clientOptions.RealmOptions assignment not found in NewHysteria2")
		}
		return nil
	})
}

func patchMasqueGo(path string) error {
	return patchFile(path, func(fset *token.FileSet, file *ast.File) error {
		ensureImport(file, "", "sync")

		st, err := findStructType(file, "Masque")
		if err != nil {
			return err
		}
		addStructField(st, "tunnelMu", mustParseField("tunnelMu sync.Mutex"))
		addStructField(st, "tunnelCancel", mustParseField("tunnelCancel context.CancelFunc"))
		addStructField(st, "tunnelGen", mustParseField("tunnelGen uint64"))

		fn, err := findFuncDecl(file, "Masque", "startLocked")
		if err != nil {
			return err
		}
		recv := receiverName(fn)
		if recv == "" {
			return fmt.Errorf("Masque.startLocked has unnamed receiver")
		}

		// Remove standalone `<recv>.running.Store(true)` right before context.WithCancel.
		filtered := make([]ast.Stmt, 0, len(fn.Body.List))
		removedRunningTrue := false
		for _, stmt := range fn.Body.List {
			if isStoreRunningBoolStmt(stmt, recv, "true") {
				removedRunningTrue = true
				continue
			}
			filtered = append(filtered, stmt)
		}
		if !removedRunningTrue {
			return fmt.Errorf("%s.running.Store(true) not found in Masque.startLocked", recv)
		}
		fn.Body.List = filtered

		withCancelIdx := -1
		cancelVar := ""
		afterFuncIdx := -1
		var afterFuncLit *ast.FuncLit
		for i, stmt := range fn.Body.List {
			if as, ok := stmt.(*ast.AssignStmt); ok && len(as.Lhs) == 2 && len(as.Rhs) == 1 {
				if call, ok := as.Rhs[0].(*ast.CallExpr); ok {
					if exprIsSelector(call.Fun, "context", "WithCancel") {
						if id, ok := as.Lhs[1].(*ast.Ident); ok {
							withCancelIdx = i
							cancelVar = id.Name
						}
					}
				}
			}
			if es, ok := stmt.(*ast.ExprStmt); ok {
				if call, ok := es.X.(*ast.CallExpr); ok && len(call.Args) == 2 {
					if exprIsSelector(call.Fun, "contextutils", "AfterFunc") {
						if fl, ok := call.Args[1].(*ast.FuncLit); ok {
							afterFuncIdx = i
							afterFuncLit = fl
						}
					}
				}
			}
		}
		if withCancelIdx < 0 || cancelVar == "" {
			return fmt.Errorf("context.WithCancel assignment not found in Masque.startLocked")
		}
		if afterFuncIdx < 0 || afterFuncLit == nil {
			return fmt.Errorf("contextutils.AfterFunc call not found in Masque.startLocked")
		}

		replacedRunningFalse := false
		for i, stmt := range afterFuncLit.Body.List {
			if isStoreRunningBoolStmt(stmt, recv, "false") {
				afterFuncLit.Body.List[i] = mustParseStmt(fmt.Sprintf("%s.clearTunnelIfCurrent(tunnelGen)", recv))
				replacedRunningFalse = true
				break
			}
		}
		if !replacedRunningFalse {
			return fmt.Errorf("%s.running.Store(false) not found inside contextutils.AfterFunc", recv)
		}

		regStmt := mustParseStmt(fmt.Sprintf("tunnelGen := %s.registerTunnelCancel(%s)", recv, cancelVar))
		fn.Body.List = insertStmts(fn.Body.List, withCancelIdx+1, regStmt)
		return nil
	})
}

func isStoreRunningBoolStmt(stmt ast.Stmt, recv, boolLit string) bool {
	es, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !exprIsIdent(call.Args[0], boolLit) {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Store" {
		return false
	}
	return exprIsSelector(sel.X, recv, "running")
}
