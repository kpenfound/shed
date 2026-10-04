package proof

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
)

// FileCoverage is one file's statement totals from a coverage profile
// (S.adopt.1).
type FileCoverage struct {
	Total, Executed int
}

// ParseProfile reads a go test -coverprofile file, returning each file's
// total and executed statement counts, keyed as SourceFile.Key does
// (S.adopt.1). A statement counts as executed when any block covering it
// ran at least once, whichever test or package produced that block.
func ParseProfile(path string) (map[string]FileCoverage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("no coverage profile: %w", err)
	}
	defer f.Close()

	out := map[string]FileCoverage{}
	scan := bufio.NewScanner(f)
	first := true
	for scan.Scan() {
		line := scan.Text()
		if first {
			first = false
			if !strings.HasPrefix(line, "mode:") {
				return nil, fmt.Errorf("coverage profile %s: missing mode line", path)
			}
			continue
		}
		if line == "" {
			continue
		}
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) != 3 {
			continue
		}
		stmts, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		count, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}
		fc := out[name]
		fc.Total += stmts
		if count > 0 {
			fc.Executed += stmts
		}
		out[name] = fc
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	if first {
		return nil, fmt.Errorf("coverage profile %s is empty", path)
	}
	return out, nil
}

// CountStatements counts the statements in a Go source file, for a file
// whose package no coverage profile ever instrumented because no proof
// reaches it (S.adopt.1). It counts every statement a function body holds,
// including those nested in blocks, but not the block itself.
func CountStatements(path string) (int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return 0, err
	}
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		stmt, ok := node.(ast.Stmt)
		if !ok {
			return true
		}
		if _, isBlock := stmt.(*ast.BlockStmt); !isBlock {
			n++
		}
		return true
	})
	return n, nil
}
