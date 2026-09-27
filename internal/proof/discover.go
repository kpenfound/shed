// Package proof finds the proofs of spec clauses and runs them. A proof is a
// Go test function whose doc comment carries a //shed:proves directive naming
// the spec clauses it proves.
package proof

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/docs"
)

// Directive is the comment prefix that marks a proof.
const Directive = "//shed:proves"

// Proof is one test function and the spec clauses it proves.
type Proof struct {
	// Dir is the package directory, slash-separated and relative to the
	// repository root.
	Dir     string
	Test    string
	File    string
	Line    int
	Clauses []clause.ID
}

// Name identifies the proof as package directory and test, such as
// internal/docs.TestGap, or by test alone in the root package.
func (p Proof) Name() string {
	if p.Dir == "." {
		return p.Test
	}
	return p.Dir + "." + p.Test
}

// Discover finds every proof in the Go module at root. Directories the go
// tool ignores, and nested modules, are skipped.
func Discover(root string) ([]Proof, []clause.Problem, error) {
	var proofs []Proof
	var problems []clause.Problem
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDir(path, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			problems = append(problems, clause.Problem{File: rel, Msg: err.Error()})
			return nil
		}
		p, pp := inspect(fset, f, rel)
		proofs = append(proofs, p...)
		problems = append(problems, pp...)
		return nil
	})
	return proofs, problems, err
}

func skipDir(path, name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || name == "vendor" {
		return true
	}
	_, err := os.Stat(filepath.Join(path, "go.mod"))
	return err == nil
}

func inspect(fset *token.FileSet, f *ast.File, rel string) ([]Proof, []clause.Problem) {
	var proofs []Proof
	var problems []clause.Problem
	attached := map[*ast.CommentGroup]bool{}
	dir := filepath.ToSlash(filepath.Dir(rel))

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Doc == nil {
			continue
		}
		var ids []clause.ID
		var found bool
		for _, c := range fn.Doc.List {
			rest, ok := strings.CutPrefix(c.Text, Directive)
			if !ok {
				continue
			}
			found = true
			line := fset.Position(c.Pos()).Line
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				problems = append(problems, clause.Problem{File: rel, Line: line, Msg: "shed:proves names no clause"})
			}
			for _, field := range fields {
				id, err := clause.ParseID(field)
				switch {
				case err != nil:
					problems = append(problems, clause.Problem{File: rel, Line: line, Msg: err.Error()})
				case id.Kind != clause.Spec:
					problems = append(problems, clause.Problem{File: rel, Line: line, Msg: fmt.Sprintf("%s is not a spec clause; proofs prove spec clauses", id)})
				default:
					ids = append(ids, id)
				}
			}
		}
		if !found {
			continue
		}
		attached[fn.Doc] = true
		line := fset.Position(fn.Pos()).Line
		if !isTest(fn) {
			problems = append(problems, clause.Problem{File: rel, Line: line, Msg: fmt.Sprintf("%s is not a test function; only tests can be proofs", fn.Name.Name)})
			continue
		}
		proofs = append(proofs, Proof{Dir: dir, Test: fn.Name.Name, File: rel, Line: line, Clauses: ids})
	}

	for _, g := range f.Comments {
		if attached[g] {
			continue
		}
		for _, c := range g.List {
			if strings.HasPrefix(c.Text, Directive) {
				problems = append(problems, clause.Problem{File: rel, Line: fset.Position(c.Pos()).Line,
					Msg: "shed:proves must be in the doc comment of a test function"})
			}
		}
	}
	return proofs, problems
}

// isTest reports whether fn is a test the go tool runs: TestXxx(t *testing.T).
func isTest(fn *ast.FuncDecl) bool {
	name := fn.Name.Name
	if fn.Recv != nil || !strings.HasPrefix(name, "Test") {
		return false
	}
	if rest := name[len("Test"):]; rest != "" {
		r, _ := utf8.DecodeRuneInString(rest)
		if unicode.IsLower(r) {
			return false
		}
	}
	params := fn.Type.Params.List
	if len(params) != 1 || len(params[0].Names) > 1 {
		return false
	}
	star, ok := params[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "testing" && sel.Sel.Name == "T"
}

// Check refuses spec clauses without a proof and proofs that name clauses
// missing from the spec.
func Check(proofs []Proof, s *docs.Set) []clause.Problem {
	var problems []clause.Problem
	proven := map[clause.ID]bool{}
	for _, p := range proofs {
		for _, id := range p.Clauses {
			proven[id] = true
			if _, ok := s.Lookup(id); !ok {
				problems = append(problems, clause.Problem{File: p.File, Line: p.Line,
					Msg: fmt.Sprintf("%s proves %s, which is not in the spec", p.Test, id)})
			}
		}
	}
	for _, c := range s.Clauses(clause.Spec) {
		if !proven[c.ID] {
			problems = append(problems, clause.Problem{File: c.File, Line: c.Line,
				Msg: fmt.Sprintf("%s has no proof", c.ID)})
		}
	}
	return problems
}

// For returns the proofs of a clause.
func For(proofs []Proof, id clause.ID) []Proof {
	var out []Proof
	for _, p := range proofs {
		if slices.Contains(p.Clauses, id) {
			out = append(out, p)
		}
	}
	return out
}
