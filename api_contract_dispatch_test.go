package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/yeti/svart-dns/internal/apigen"
)

type routeConstraint struct {
	method string
	parts  map[int]string
	count  int
}

func (c routeConstraint) add(d routeConstraint) routeConstraint {
	result := routeConstraint{method: c.method, count: c.count, parts: map[int]string{}}
	for k, v := range c.parts {
		result.parts[k] = v
	}
	for k, v := range d.parts {
		result.parts[k] = v
	}
	if d.method != "" {
		result.method = d.method
	}
	if d.count > 0 {
		result.count = d.count
	}
	return result
}

func routeLiteral(expression ast.Expr) string {
	if value, ok := expression.(*ast.BasicLit); ok && value.Kind == token.STRING {
		decoded := mustFixture(strconv.Unquote(value.Value))
		return decoded
	}
	if value, ok := expression.(*ast.SelectorExpr); ok && strings.HasPrefix(value.Sel.Name, "Method") {
		return strings.ToUpper(strings.TrimPrefix(value.Sel.Name, "Method"))
	}
	return ""
}
func routeSelector(expression ast.Expr) (string, int) {
	switch value := expression.(type) {
	case *ast.SelectorExpr:
		if value.Sel.Name == "Method" {
			return "method", 0
		}
	case *ast.Ident:
		if value.Name == "action" {
			return "part", 1
		}
	case *ast.IndexExpr:
		if name, ok := value.X.(*ast.Ident); ok && name.Name == "parts" {
			if index, ok := value.Index.(*ast.BasicLit); ok {
				n, err := strconv.Atoi(index.Value)
				if err == nil {
					return "part", n
				}
			}
		}
	case *ast.CallExpr:
		if name, ok := value.Fun.(*ast.Ident); ok && name.Name == "len" && len(value.Args) == 1 {
			if parts, ok := value.Args[0].(*ast.Ident); ok && parts.Name == "parts" {
				return "count", 0
			}
		}
	}
	return "", 0
}
func routeAtom(selector, value ast.Expr) routeConstraint {
	kind, index := routeSelector(selector)
	literal := routeLiteral(value)
	if kind == "method" {
		return routeConstraint{method: literal}
	}
	if kind == "part" && literal != "" {
		return routeConstraint{parts: map[int]string{index: literal}}
	}
	if kind == "count" {
		if literal, ok := value.(*ast.BasicLit); ok {
			n := mustFixture(strconv.Atoi(literal.Value))
			return routeConstraint{count: n}
		}
	}
	return routeConstraint{}
}
func routeConditions(expression ast.Expr) []routeConstraint {
	if parens, ok := expression.(*ast.ParenExpr); ok {
		return routeConditions(parens.X)
	}
	binary, ok := expression.(*ast.BinaryExpr)
	if !ok {
		return []routeConstraint{{}}
	}
	switch binary.Op {
	case token.LOR:
		return append(routeConditions(binary.X), routeConditions(binary.Y)...)
	case token.LAND:
		var result []routeConstraint
		for _, left := range routeConditions(binary.X) {
			for _, right := range routeConditions(binary.Y) {
				result = append(result, left.add(right))
			}
		}
		return result
	case token.EQL, token.NEQ:
		return []routeConstraint{routeAtom(binary.X, binary.Y).add(routeAtom(binary.Y, binary.X))}
	}
	return []routeConstraint{{}}
}

func checkRouteDispatch(function *ast.FuncDecl, operations []apigen.Operation) []string {
	prefix := ""
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
			value := mustFixture(strconv.Unquote(literal.Value))
			if strings.HasPrefix(value, "/api/") && strings.HasSuffix(value, "/") && !strings.Contains(value, " ") {
				prefix = value
			}
		}
		return true
	})
	var failures []string
	check := func(condition routeConstraint) {
		if condition.method == "" && len(condition.parts) == 0 {
			return
		}
		for _, operation := range operations {
			if condition.method != "" && condition.method != operation.Method {
				continue
			}
			parts := strings.Split(strings.TrimPrefix(operation.Path, prefix), "/")
			if condition.count > 0 && prefix != "" && len(parts) != condition.count {
				continue
			}
			matches := true
			for index, part := range condition.parts {
				if prefix != "" {
					matches = matches && index < len(parts) && parts[index] == part
				} else {
					matches = matches && strings.Contains(operation.Path, "/"+part)
				}
			}
			if matches {
				return
			}
		}
		failures = append(failures, fmt.Sprintf("%s: undocumented dispatch method=%q prefix=%q parts=%v count=%d", function.Name.Name, condition.method, prefix, condition.parts, condition.count))
	}
	var walk func(ast.Stmt, routeConstraint)
	walk = func(statement ast.Stmt, parent routeConstraint) {
		switch statement := statement.(type) {
		case *ast.BlockStmt:
			for _, child := range statement.List {
				walk(child, parent)
			}
		case *ast.IfStmt:
			for _, condition := range routeConditions(statement.Cond) {
				combined := parent.add(condition)
				check(combined)
				// A negative method guard documents the accepted method but does not
				// constrain its rejection branch to that method.
				if binary, ok := statement.Cond.(*ast.BinaryExpr); ok && binary.Op == token.NEQ {
					walk(statement.Body, parent)
				} else {
					walk(statement.Body, combined)
				}
			}
			if statement.Else != nil {
				walk(statement.Else, parent)
			}
		case *ast.SwitchStmt:
			for _, item := range statement.Body.List {
				clause, ok := item.(*ast.CaseClause)
				if !ok {
					panic("switch body contains a non-case AST node")
				}
				if len(clause.List) == 0 {
					for _, child := range clause.Body {
						walk(child, parent)
					}
				}
				for _, value := range clause.List {
					condition := parent.add(routeAtom(statement.Tag, value))
					check(condition)
					for _, child := range clause.Body {
						walk(child, condition)
					}
				}
			}
		}
	}
	walk(function.Body, routeConstraint{})
	return failures
}

func TestAPIContractMultiplexedRouteCoverage(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok {
				functions[function.Name.Name] = function
			}
		}
	}
	source, err := apigen.ReadSource(".")
	if err != nil {
		t.Fatal(err)
	}
	reachable := func(from, to string) bool {
		seen := map[string]bool{}
		var visit func(string) bool
		visit = func(name string) bool {
			if name == to {
				return true
			}
			if seen[name] {
				return false
			}
			seen[name] = true
			for _, call := range source[name].Calls {
				if visit(call) {
					return true
				}
			}
			return false
		}
		return visit(from)
	}
	for name, function := range functions {
		if !strings.HasPrefix(name, "handleAPI") {
			continue
		}
		var operations []apigen.Operation
		for _, operation := range apiOperations() {
			if reachable(name, operation.Handler) || reachable(operation.Handler, name) {
				operations = append(operations, operation)
			}
		}
		if len(operations) == 0 {
			continue
		}
		for _, failure := range checkRouteDispatch(function, operations) {
			t.Error(failure)
		}
	}
}

func TestAPIContractDispatchDriftRejectsNewActionAndMethod(t *testing.T) {
	operations := []apigen.Operation{{Method: "GET", Path: "/api/ranges/{id}"}, {Method: "POST", Path: "/api/ranges/{id}/toggle"}}
	for _, test := range []struct{ name, dispatch string }{
		{"new action", `if len(parts)==2 && parts[1]=="reset" && r.Method=="POST" { writeJSON(w,200,SuccessResponse{}) }`},
		{"new method", `if len(parts)==2 && parts[1]=="toggle" && r.Method=="PATCH" { writeJSON(w,200,SuccessResponse{}) }`},
		{"existing method on another action", `if len(parts)==2 && parts[1]=="toggle" { switch r.Method { case "POST": writeJSON(w,200,SuccessResponse{}); case "GET": writeJSON(w,200,SuccessResponse{}) } }`},
		{"existing method at another depth", `if len(parts)==1 && r.Method=="POST" { writeJSON(w,200,SuccessResponse{}) }`},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", `package main; func handleAPI(w http.ResponseWriter,r *http.Request){ path:=strings.TrimPrefix(r.URL.Path,"/api/ranges/");parts:=strings.Split(path,"/");`+test.dispatch+`}`, 0)
			if err != nil {
				t.Fatal(err)
			}
			failures := checkRouteDispatch(requireFixtureType[*ast.FuncDecl](t, file.Decls[0]), operations)
			if len(failures) == 0 {
				t.Fatal("new dispatch escaped operation coverage")
			}
		})
	}
}
