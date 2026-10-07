package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type permissionSourceRoute struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Source string `json:"source"`
	Line   int    `json:"line"`
}

var permissionSourceFuncs = map[string]*ast.FuncDecl{}
var permissionSourceFiles = token.NewFileSet()
var permissionSourceFound = map[string]permissionSourceRoute{}
var permissionSourceSeen = map[string]bool{}

// nonAdminPageRoutes are the only routes under /api/v1/pages (registered by
// handler.RegisterPageRoutes) that are not administrative: page content is
// JWT-only and page images are public. Every other /api/v1/pages route must be
// mapped, so a new admin route there fails this gate.
var nonAdminPageRoutes = map[string]bool{
	"GET /api/v1/pages/:slug":                  true,
	"GET /api/v1/pages/:slug/images/*filename": true,
}

func permissionSourceLiteral(e ast.Expr) string {
	if b, ok := e.(*ast.BasicLit); ok && b.Kind == token.STRING {
		s, _ := strconv.Unquote(b.Value)
		return s
	}
	return ""
}
func permissionSourceValue(e ast.Expr, env map[string]string) string {
	switch x := e.(type) {
	case *ast.Ident:
		return env[x.Name]
	case *ast.CallExpr:
		if s, ok := x.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Group" && len(x.Args) > 0 {
			base := permissionSourceValue(s.X, env)
			if base != "" {
				return path.Join(base, permissionSourceLiteral(x.Args[0]))
			}
		}
	}
	return ""
}
func walkPermissionSource(name string, args []string) {
	f := permissionSourceFuncs[name]
	if f == nil {
		return
	}
	key := name + strings.Join(args, "|")
	if permissionSourceSeen[key] {
		return
	}
	permissionSourceSeen[key] = true
	env := map[string]string{}
	i := 0
	for _, p := range f.Type.Params.List {
		for _, n := range p.Names {
			if i < len(args) {
				env[n.Name] = args[i]
			}
			i++
		}
	}
	ast.Inspect(f.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				if id, ok := l.(*ast.Ident); ok && i < len(x.Rhs) {
					if v := permissionSourceValue(x.Rhs[i], env); v != "" {
						env[id.Name] = v
					}
				}
			}
		case *ast.CallExpr:
			s, ok := x.Fun.(*ast.SelectorExpr)
			if ok {
				method := s.Sel.Name
				base := permissionSourceValue(s.X, env)
				if base != "" && len(x.Args) > 0 && strings.Contains(" GET POST PUT PATCH DELETE HEAD OPTIONS Any ", " "+method+" ") {
					p := path.Join(base, permissionSourceLiteral(x.Args[0]))
					loc := permissionSourceFiles.Position(x.Pos())
					permissionSourceFound[method+" "+p] = permissionSourceRoute{method, p, loc.Filename, loc.Line}
				}
				if method == "RegisterRoutes" && len(x.Args) > 0 {
					if base := permissionSourceValue(x.Args[0], env); strings.HasSuffix(base, "/groups") {
						walkPermissionSource("DynamicRateHandler.RegisterRoutes", []string{base})
					}
				}
			} else if id, ok := x.Fun.(*ast.Ident); ok && permissionSourceFuncs[id.Name] != nil {
				a := make([]string, len(x.Args))
				for i, e := range x.Args {
					a[i] = permissionSourceValue(e, env)
				}
				walkPermissionSource(id.Name, a)
			}
		}
		return true
	})
}

// Static route registration traversal: no application, database, or Redis is
// initialized. Adding an unclassified administrative route fails this gate.
func TestAdminPermissionInventoryCoversAllRegisteredRoutes(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	permissionSourceFuncs = map[string]*ast.FuncDecl{}
	permissionSourceFiles = token.NewFileSet()
	permissionSourceFound = map[string]permissionSourceRoute{}
	permissionSourceSeen = map[string]bool{}
	for _, dir := range []string{"internal/server/routes", "internal/handler"} {
		files, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			parsed, err := parser.ParseFile(permissionSourceFiles, file, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, decl := range parsed.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				if fn.Recv == nil {
					permissionSourceFuncs[fn.Name.Name] = fn
					continue
				}
				if fn.Name.Name != "RegisterRoutes" {
					continue
				}
				if ptr, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
					if id, ok := ptr.X.(*ast.Ident); ok {
						permissionSourceFuncs[id.Name+"."+fn.Name.Name] = fn
					}
				}
			}
		}
	}
	walkPermissionSource("RegisterAdminRoutes", []string{"/api/v1"})
	walkPermissionSource("RegisterPaymentRoutes", []string{"/api/v1"})
	walkPermissionSource("RegisterPageRoutes", []string{"/api/v1"})
	if _, ok := permissionSourceFound["GET /api/v1/pages"]; !ok {
		t.Fatal("RegisterPageRoutes traversal did not find GET /api/v1/pages")
	}
	for route := range nonAdminPageRoutes {
		if _, ok := permissionSourceFound[route]; !ok {
			t.Errorf("whitelisted non-admin page route %s is no longer registered", route)
		}
	}
	count := 0
	for route, entry := range permissionSourceFound {
		adminRoute := strings.HasPrefix(entry.Path, "/api/v1/admin")
		pageRoute := strings.HasPrefix(entry.Path, "/api/v1/pages") && !nonAdminPageRoutes[route]
		if !adminRoute && !pageRoute {
			continue
		}
		count++
		permission, ok := middleware.LookupAdminRoutePermission(entry.Method, entry.Path)
		if !ok {
			t.Errorf("unmapped route %s %s (%s:%d)", entry.Method, entry.Path, entry.Source, entry.Line)
			continue
		}
		if !service.IsKnownAdminPermission(permission) {
			t.Errorf("unknown permission %s on %s", permission, entry.Path)
		}
	}
	if count < 470 {
		t.Fatalf("route inventory unexpectedly incomplete: %d", count)
	}
	if _, ok := middleware.LookupAdminRoutePermission("POST", "/api/v1/admin/future-dangerous-route"); ok {
		t.Fatal("unknown routes must remain denied")
	}
	t.Logf("mapped %d administrative route patterns", count)
}

// Write routes must not be satisfiable with read/export permissions (F-8).
func TestAdminWriteRoutesMapToWritePermissions(t *testing.T) {
	want := map[string]string{
		"PUT /api/v1/admin/api-keys/:id":      "users.credentials.write",
		"POST /api/v1/admin/audit-logs/clear": "audit.manage",
	}
	for route, permission := range want {
		method, path := splitRoute(route)
		got, ok := middleware.LookupAdminRoutePermission(method, path)
		if !ok || got != permission {
			t.Errorf("route %s = %q/%v, want %q", route, got, ok, permission)
		}
	}
}
