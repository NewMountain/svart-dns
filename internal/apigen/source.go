package apigen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Function contains handler documentation and statically discovered API behavior.
type Function struct {
	Summary, Description string
	Calls, Queries       []string
	Statuses             []int
	Codes                map[int]string
}

// Source indexes named Go functions for handler call-graph traversal.
type Source map[string]Function

// ReadSource parses production Go files in a directory into handler metadata.
func ReadSource(dir string) (Source, error) {
	result := Source{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			meta := Function{Codes: map[int]string{}}
			if fn.Doc != nil {
				for _, line := range strings.Split(fn.Doc.Text(), "\n") {
					if text, ok := strings.CutPrefix(line, "@Summary "); ok {
						meta.Summary = text
					}
					if text, ok := strings.CutPrefix(line, "@Description "); ok {
						meta.Description = text
					}
				}
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if fn.Name.Name == "apiErrorCode" {
					if clause, ok := n.(*ast.CaseClause); ok {
						for _, stmt := range clause.Body {
							if ret, ok := stmt.(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
								if lit, ok := ret.Results[0].(*ast.BasicLit); ok {
									code, err := strconv.Unquote(lit.Value)
									if err != nil {
										continue
									}
									if len(clause.List) == 0 {
										meta.Codes[0] = code
									}
									for _, expr := range clause.List {
										if sel, ok := expr.(*ast.SelectorExpr); ok {
											meta.Codes[httpStatuses[sel.Sel.Name]] = code
										}
									}
								}
							}
						}
					}
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if id, ok := call.Fun.(*ast.Ident); ok {
					meta.Calls = append(meta.Calls, id.Name)
					if id.Name == "intQueryParam" && len(call.Args) >= 2 {
						if lit, ok := call.Args[1].(*ast.BasicLit); ok {
							if name, err := strconv.Unquote(lit.Value); err == nil {
								meta.Queries = append(meta.Queries, name)
							}
						}
					}
					if id.Name == "writeError" && len(call.Args) > 1 {
						if sel, ok := call.Args[1].(*ast.SelectorExpr); ok {
							if status := httpStatuses[sel.Sel.Name]; status != 0 {
								meta.Statuses = append(meta.Statuses, status)
							}
						}
						if literal, ok := call.Args[1].(*ast.BasicLit); ok {
							if status, err := strconv.Atoi(literal.Value); err == nil {
								meta.Statuses = append(meta.Statuses, status)
							}
						}
					}
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Get" && len(call.Args) == 1 {
					query := false
					if id, ok := sel.X.(*ast.Ident); ok && (id.Name == "q" || id.Name == "params") {
						query = true
					}
					if c, ok := sel.X.(*ast.CallExpr); ok {
						if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Query" {
							query = true
						}
					}
					if query {
						if lit, ok := call.Args[0].(*ast.BasicLit); ok {
							if v, err := strconv.Unquote(lit.Value); err == nil {
								meta.Queries = append(meta.Queries, v)
							}
						}
					}
				}
				return true
			})
			result[fn.Name.Name] = meta
		}
	}
	return result, nil
}

var httpStatuses = map[string]int{
	"StatusBadRequest": http.StatusBadRequest, "StatusUnauthorized": http.StatusUnauthorized, "StatusForbidden": http.StatusForbidden, "StatusNotFound": http.StatusNotFound, "StatusMethodNotAllowed": http.StatusMethodNotAllowed, "StatusRequestTimeout": http.StatusRequestTimeout, "StatusConflict": http.StatusConflict, "StatusRequestEntityTooLarge": http.StatusRequestEntityTooLarge, "StatusMisdirectedRequest": http.StatusMisdirectedRequest, "StatusUnprocessableEntity": http.StatusUnprocessableEntity, "StatusUpgradeRequired": http.StatusUpgradeRequired, "StatusTooManyRequests": http.StatusTooManyRequests, "StatusInternalServerError": http.StatusInternalServerError, "StatusBadGateway": http.StatusBadGateway, "StatusServiceUnavailable": http.StatusServiceUnavailable, "StatusGatewayTimeout": http.StatusGatewayTimeout,
}

// Reachable gathers response statuses and query arguments through the handler call graph.
func (src Source) Reachable(handler string) Function {
	result := Function{}
	visited := map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		fn := src[name]
		result.Statuses = append(result.Statuses, fn.Statuses...)
		result.Queries = append(result.Queries, fn.Queries...)
		for _, call := range fn.Calls {
			visit(call)
		}
	}
	visit(handler)
	result.Summary = src[handler].Summary
	result.Description = src[handler].Description
	return result
}
