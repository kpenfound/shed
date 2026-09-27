// Package clause parses the clause documents: the charter, the spec and the
// horizon. A clause is a Markdown list item that starts with its ID in bold.
package clause

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Kind is the namespace an ID belongs to.
type Kind int

const (
	Charter Kind = iota + 1
	Spec
	Horizon
	Milestone
)

func (k Kind) String() string {
	switch k {
	case Charter:
		return "charter"
	case Spec:
		return "spec"
	case Horizon:
		return "horizon"
	case Milestone:
		return "milestone"
	}
	return "unknown"
}

// ID is a parsed clause ID such as C3, S.doc.2, H.greet.7 or M4.
type ID struct {
	Kind Kind
	// Area is empty for charter and milestone IDs.
	Area string
	N    int
}

func (id ID) String() string {
	switch id.Kind {
	case Charter:
		return "C" + strconv.Itoa(id.N)
	case Milestone:
		return "M" + strconv.Itoa(id.N)
	case Spec:
		return "S." + id.Area + "." + strconv.Itoa(id.N)
	case Horizon:
		return "H." + id.Area + "." + strconv.Itoa(id.N)
	}
	return ""
}

// Compare orders IDs by kind, then area, then number.
func Compare(a, b ID) int {
	if a.Kind != b.Kind {
		return int(a.Kind) - int(b.Kind)
	}
	if c := strings.Compare(a.Area, b.Area); c != 0 {
		return c
	}
	return a.N - b.N
}

// SameSeries reports whether two IDs share a kind and area, so that a range
// between them is meaningful.
func SameSeries(a, b ID) bool {
	return a.Kind == b.Kind && a.Area == b.Area
}

const (
	numberPattern = `[1-9][0-9]*`
	areaPattern   = `[a-z][a-z0-9-]*`
)

var (
	idPattern = regexp.MustCompile(`^(?:C(` + numberPattern + `)|M(` + numberPattern + `)|([SH])\.(` + areaPattern + `)\.(` + numberPattern + `))$`)

	// candidatePattern matches tokens that look like an attempt at an ID: a
	// capital letter followed by a digit or a dot.
	candidatePattern = regexp.MustCompile(`^[A-Z][0-9.]`)

	// mentionPattern finds ID-shaped tokens in prose, with an optional
	// @revision suffix. Malformed tokens are matched too so that they can be
	// reported instead of silently ignored.
	mentionPattern = regexp.MustCompile(`\b(?:C[0-9]+|M[0-9]+|[SH]\.[a-z][a-z0-9-]*\.[0-9]+)(?:@[^\s,;()]+)?`)
)

// ParseID parses a clause ID. Numbers start at 1 and have no leading zeros.
func ParseID(s string) (ID, error) {
	m := idPattern.FindStringSubmatch(s)
	if m == nil {
		return ID{}, fmt.Errorf("malformed clause ID %q", s)
	}
	switch {
	case m[1] != "":
		n, _ := strconv.Atoi(m[1])
		return ID{Kind: Charter, N: n}, nil
	case m[2] != "":
		n, _ := strconv.Atoi(m[2])
		return ID{Kind: Milestone, N: n}, nil
	default:
		n, _ := strconv.Atoi(m[5])
		kind := Spec
		if m[3] == "H" {
			kind = Horizon
		}
		return ID{Kind: kind, Area: m[4], N: n}, nil
	}
}

// MustParseID is ParseID for IDs known to be valid.
func MustParseID(s string) ID {
	id, err := ParseID(s)
	if err != nil {
		panic(err)
	}
	return id
}

// IsCandidate reports whether a bold token at the start of a list item is
// meant as an ID: a capital letter followed by a digit or a dot.
func IsCandidate(token string) bool {
	return candidatePattern.MatchString(token)
}
