package svart

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"reflect"
	"testing"

	"github.com/yeti/svart-dns/internal/apigen"
	"golang.org/x/tools/go/packages"
)

func reflectWireName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Pointer:
		return reflectWireName(t.Elem())
	case reflect.Slice:
		return "[]" + reflectWireName(t.Elem())
	case reflect.Map:
		return "map[" + reflectWireName(t.Key()) + "]" + reflectWireName(t.Elem())
	}
	return t.Name()
}
func goWireName(t types.Type) string {
	switch v := t.(type) {
	case *types.Pointer:
		return goWireName(v.Elem())
	case *types.Slice:
		return "[]" + goWireName(v.Elem())
	case *types.Map:
		return "map[" + goWireName(v.Key()) + "]" + goWireName(v.Elem())
	case *types.Named:
		return v.Obj().Name()
	case *types.Basic:
		return v.Name()
	}
	return types.TypeString(t, nil)
}

func TestAPIContractCoversEveryTypedPayload(t *testing.T) {
	source, err := apigen.ReadSource("internal/svart")
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]map[string]bool{}
	requests := map[string]map[string]bool{}
	for _, op := range apiOperations() {
		seen := map[string]bool{}
		var visit func(string)
		visit = func(name string) {
			if seen[name] {
				return
			}
			seen[name] = true
			if covered[name] == nil {
				covered[name] = map[string]bool{}
			}
			if requests[name] == nil {
				requests[name] = map[string]bool{}
			}
			if op.Request != nil {
				requests[name][reflectWireName(op.Request)] = true
			}
			for _, response := range op.Response {
				covered[name][fmt.Sprintf("%d %s", op.Status, reflectWireName(response))] = true
			}
			for _, call := range source[name].Calls {
				visit(call)
			}
		}
		visit(op.Handler)
	}
	loaded, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps}, ".")
	if err != nil {
		t.Fatal(err)
	}
	if packages.PrintErrors(loaded) > 0 {
		t.Fatal("cannot type-check API source")
	}
	for _, pkg := range loaded {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					name, ok := call.Fun.(*ast.Ident)
					if !ok {
						return true
					}
					if name.Name == "decodeJSONBody" && len(call.Args) == 4 {
						wireName := goWireName(pkg.TypesInfo.TypeOf(call.Args[3]))
						if !requests[fn.Name.Name][wireName] {
							t.Errorf("%s: request type %s in %s is absent from operation contract", pkg.Fset.Position(call.Pos()), wireName, fn.Name.Name)
						}
						return true
					}
					if name.Name != "writeJSON" || len(call.Args) != 3 {
						return true
					}
					wireName := goWireName(pkg.TypesInfo.TypeOf(call.Args[2]))
					value := pkg.TypesInfo.Types[call.Args[1]].Value
					if value == nil {
						t.Errorf("%s: dynamic success status needs explicit contract analysis", pkg.Fset.Position(call.Pos()))
						return true
					}
					status, ok := constant.Int64Val(value)
					if !ok {
						t.Errorf("%s: non-integer success status", pkg.Fset.Position(call.Pos()))
						return true
					}
					if !covered[fn.Name.Name][fmt.Sprintf("%d %s", status, wireName)] {
						t.Errorf("%s: success status/type %d %s in %s is absent from operation contract", pkg.Fset.Position(call.Pos()), status, wireName, fn.Name.Name)
					}
					return true
				})
			}
		}
	}
}
