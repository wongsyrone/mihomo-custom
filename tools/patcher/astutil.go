package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

var posType = reflect.TypeOf(token.Pos(0))

func patchFile(path string, fn func(*token.FileSet, *ast.File) error) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	cmap := ast.NewCommentMap(fset, file, file.Comments)
	if err := fn(fset, file); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	file.Comments = cmap.Filter(file).Comments()

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return fmt.Errorf("format AST %s: %w", path, err)
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("gofmt %s: %w", path, err)
	}
	normalized := bytes.ReplaceAll(formatted, []byte("\r\n"), []byte("\n"))
	if err := os.WriteFile(path, normalized, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func zeroPos(n any) {
	seen := make(map[uintptr]bool)
	clearPos(reflect.ValueOf(n), seen)
}

func clearPos(v reflect.Value, seen map[uintptr]bool) {
	if !v.IsValid() {
		return
	}
	if v.Type() == posType {
		if v.CanSet() {
			v.SetInt(0)
		}
		return
	}
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		ptr := v.Pointer()
		if seen[ptr] {
			return
		}
		seen[ptr] = true
		clearPos(v.Elem(), seen)
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		clearPos(v.Elem(), seen)
	case reflect.Struct:
		for i := range v.NumField() {
			clearPos(v.Field(i), seen)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			clearPos(v.Index(i), seen)
		}
	}
}

func mustParseStmt(src string) ast.Stmt {
	stmts := mustParseStmts(src)
	if len(stmts) != 1 {
		panic(fmt.Sprintf("expected 1 statement for %q, got %d", src, len(stmts)))
	}
	return stmts[0]
}

func mustParseStmts(src string) []ast.Stmt {
	fset := token.NewFileSet()
	wrapped := "package p\nfunc _() {\n" + src + "\n}\n"
	f, err := parser.ParseFile(fset, "", wrapped, 0)
	if err != nil {
		panic(fmt.Sprintf("parse stmts %q: %v", src, err))
	}
	fn := f.Decls[0].(*ast.FuncDecl)
	for _, s := range fn.Body.List {
		zeroPos(s)
	}
	return fn.Body.List
}

func mustParseExpr(src string) ast.Expr {
	expr, err := parser.ParseExpr(src)
	if err != nil {
		panic(fmt.Sprintf("parse expr %q: %v", src, err))
	}
	zeroPos(expr)
	return expr
}

func mustParseField(src string) *ast.Field {
	fset := token.NewFileSet()
	wrapped := "package p\ntype _ struct {\n" + src + "\n}\n"
	f, err := parser.ParseFile(fset, "", wrapped, 0)
	if err != nil {
		panic(fmt.Sprintf("parse struct field %q: %v", src, err))
	}
	ts := f.Decls[0].(*ast.GenDecl).Specs[0].(*ast.TypeSpec)
	st := ts.Type.(*ast.StructType)
	field := st.Fields.List[0]
	zeroPos(field)
	return field
}

func mustParseInterfaceMethod(src string) *ast.Field {
	fset := token.NewFileSet()
	wrapped := "package p\ntype _ interface {\n" + src + "\n}\n"
	f, err := parser.ParseFile(fset, "", wrapped, 0)
	if err != nil {
		panic(fmt.Sprintf("parse interface method %q: %v", src, err))
	}
	ts := f.Decls[0].(*ast.GenDecl).Specs[0].(*ast.TypeSpec)
	it := ts.Type.(*ast.InterfaceType)
	method := it.Methods.List[0]
	zeroPos(method)
	return method
}

func findStructType(file *ast.File, typeName string) (*ast.StructType, error) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != typeName {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return nil, fmt.Errorf("type %s is not a struct", typeName)
			}
			return st, nil
		}
	}
	return nil, fmt.Errorf("struct %s not found", typeName)
}

func findInterfaceType(file *ast.File, typeName string) (*ast.InterfaceType, error) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != typeName {
				continue
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok {
				return nil, fmt.Errorf("type %s is not an interface", typeName)
			}
			return it, nil
		}
	}
	return nil, fmt.Errorf("interface %s not found", typeName)
}

func findFuncDecl(file *ast.File, recvType, funcName string) (*ast.FuncDecl, error) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != funcName {
			continue
		}
		if recvType == "" {
			if fn.Recv == nil || len(fn.Recv.List) == 0 {
				return fn, nil
			}
			continue
		}
		if fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		rt := fn.Recv.List[0].Type
		if star, ok := rt.(*ast.StarExpr); ok {
			rt = star.X
		}
		if idx, ok := rt.(*ast.IndexExpr); ok {
			rt = idx.X
		}
		if ident, ok := rt.(*ast.Ident); ok && ident.Name == recvType {
			return fn, nil
		}
	}
	if recvType == "" {
		return nil, fmt.Errorf("func %s not found", funcName)
	}
	return nil, fmt.Errorf("method (%s).%s not found", recvType, funcName)
}

func receiverName(fn *ast.FuncDecl) string {
	if fn != nil && fn.Recv != nil && len(fn.Recv.List) > 0 && len(fn.Recv.List[0].Names) > 0 {
		return fn.Recv.List[0].Names[0].Name
	}
	return ""
}

func paramName(fn *ast.FuncDecl, idx int, fallback string) string {
	if fn == nil || fn.Type == nil || fn.Type.Params == nil {
		return fallback
	}
	cur := 0
	for _, f := range fn.Type.Params.List {
		if len(f.Names) == 0 {
			if cur == idx {
				return fallback
			}
			cur++
			continue
		}
		for _, n := range f.Names {
			if cur == idx {
				return n.Name
			}
			cur++
		}
	}
	return fallback
}

func fieldListParamName(fl *ast.FieldList, idx int, fallback string) string {
	if fl == nil {
		return fallback
	}
	cur := 0
	for _, f := range fl.List {
		if len(f.Names) == 0 {
			if cur == idx {
				return fallback
			}
			cur++
			continue
		}
		for _, n := range f.Names {
			if cur == idx {
				return n.Name
			}
			cur++
		}
	}
	return fallback
}

func addStructField(st *ast.StructType, fieldName string, field *ast.Field) bool {
	for _, f := range st.Fields.List {
		for _, n := range f.Names {
			if n.Name == fieldName {
				return false
			}
		}
	}
	st.Fields.List = append(st.Fields.List, field)
	return true
}

func addInterfaceMethod(it *ast.InterfaceType, methodName string, method *ast.Field) bool {
	for _, m := range it.Methods.List {
		for _, n := range m.Names {
			if n.Name == methodName {
				return false
			}
		}
	}
	it.Methods.List = append(it.Methods.List, method)
	return true
}

func ensureImport(file *ast.File, alias, importPath string) {
	quoted := strconv.Quote(importPath)
	for _, imp := range file.Imports {
		if unq, err := strconv.Unquote(imp.Path.Value); err == nil && unq == importPath {
			return
		}
	}
	spec := &ast.ImportSpec{
		Path: &ast.BasicLit{Kind: token.STRING, Value: quoted},
	}
	if alias != "" {
		spec.Name = ast.NewIdent(alias)
	}
	lastImportIdx := -1
	for i, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		lastImportIdx = i
		if gen.Lparen.IsValid() {
			gen.Specs = append(gen.Specs, spec)
			return
		}
	}
	newDecl := &ast.GenDecl{
		Tok:   token.IMPORT,
		Specs: []ast.Spec{spec},
	}
	if lastImportIdx >= 0 {
		decls := make([]ast.Decl, 0, len(file.Decls)+1)
		decls = append(decls, file.Decls[:lastImportIdx+1]...)
		decls = append(decls, newDecl)
		decls = append(decls, file.Decls[lastImportIdx+1:]...)
		file.Decls = decls
		return
	}
	file.Decls = append([]ast.Decl{newDecl}, file.Decls...)
}

func addCompositeLitKV(lit *ast.CompositeLit, key string, val ast.Expr) bool {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if id, ok := kv.Key.(*ast.Ident); ok && id.Name == key {
			return false
		}
	}
	zeroPos(val)
	lit.Elts = append(lit.Elts, &ast.KeyValueExpr{
		Key:   ast.NewIdent(key),
		Value: val,
	})
	return true
}

func typeExprMatches(expr ast.Expr, pkg, name string) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if pkg == "" {
		id, ok := expr.(*ast.Ident)
		return ok && id.Name == name
	}
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == pkg && sel.Sel.Name == name
}

func exprIsIdent(expr ast.Expr, name string) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == name
}

func exprIsSelector(expr ast.Expr, baseIdent, selName string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return exprIsIdent(sel.X, baseIdent) && sel.Sel.Name == selName
}

func insertStmts(list []ast.Stmt, idx int, extra ...ast.Stmt) []ast.Stmt {
	out := make([]ast.Stmt, 0, len(list)+len(extra))
	out = append(out, list[:idx]...)
	out = append(out, extra...)
	out = append(out, list[idx:]...)
	return out
}

func copyOverlayDir(srcRoot, dstRoot string) (int, error) {
	if _, err := os.Stat(srcRoot); err != nil {
		return 0, fmt.Errorf("stat overlay dir %s: %w", srcRoot, err)
	}
	copied := 0
	err := filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(rel, ".git") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read overlay %s: %w", path, err)
		}
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
		target := filepath.Join(dstRoot, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("write overlay %s: %w", target, err)
		}
		copied++
		return nil
	})
	return copied, err
}
