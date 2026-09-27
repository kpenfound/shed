package clause

import (
	"slices"
	"strings"
	"testing"
)

func parse(t *testing.T, src string, kinds ...Kind) (*Document, []Problem) {
	t.Helper()
	return Parse("doc.md", []byte(src), kinds...)
}

func messages(problems []Problem) string {
	var out []string
	for _, p := range problems {
		out = append(out, p.String())
	}
	return strings.Join(out, "\n")
}

//shed:proves S.doc.2
func TestClauseIsListItemOpeningWithBoldID(t *testing.T) {
	doc, problems := parse(t, `# Charter

Some prose that is not a clause.

- **C1** The first clause
  wraps onto a second line.

  It has a second paragraph.

  - and a nested item.
- **C2** The second clause.
- A plain list item.
`, Charter)
	if len(problems) > 0 {
		t.Fatalf("problems: %s", messages(problems))
	}
	if len(doc.Clauses) != 2 {
		t.Fatalf("got %d clauses, want 2", len(doc.Clauses))
	}
	c := doc.Clauses[0]
	if c.ID != MustParseID("C1") || c.Line != 5 || c.File != "doc.md" {
		t.Errorf("first clause = %s at %s:%d", c.ID, c.File, c.Line)
	}
	want := "The first clause wraps onto a second line. It has a second paragraph. - and a nested item."
	if c.Text != want {
		t.Errorf("text = %q, want %q", c.Text, want)
	}
	if doc.Clauses[1].Text != "The second clause." {
		t.Errorf("second text = %q", doc.Clauses[1].Text)
	}
}

//shed:proves S.doc.2 S.horizon.1 S.horizon.2
func TestTagsFollowSpecAndHorizonIDs(t *testing.T) {
	doc, problems := parse(t, `- **H.a.1** (soon, realised) Horizon text.
- **S.a.1** (H.a.1, H.a.2) Spec text.
- **M1** (not tags) Milestone text.
`, Horizon, Spec, Milestone)
	if len(problems) > 0 {
		t.Fatalf("problems: %s", messages(problems))
	}
	h, s, m := doc.Clauses[0], doc.Clauses[1], doc.Clauses[2]
	if !h.Tagged || !slices.Equal(h.Tags, []string{"soon", "realised"}) || h.Text != "Horizon text." {
		t.Errorf("horizon clause = %+v", h)
	}
	if !s.Tagged || !slices.Equal(s.Tags, []string{"H.a.1", "H.a.2"}) || s.Text != "Spec text." {
		t.Errorf("spec clause = %+v", s)
	}
	if m.Tagged || m.Text != "(not tags) Milestone text." {
		t.Errorf("milestone clause = %+v", m)
	}
}

//shed:proves S.doc.3
func TestIDShapedBoldTokensMustBeValid(t *testing.T) {
	doc, problems := parse(t, `- **Note.** A bold lead-in that is not an ID.
- **Two words** Also prose.
- **C01** Leading zero.
- **C** Just a letter is prose.
- **X4** Unknown namespace.
`, Charter)
	if len(doc.Clauses) != 0 {
		t.Errorf("got clauses %+v, want none", doc.Clauses)
	}
	got := messages(problems)
	for _, want := range []string{`doc.md:3: malformed clause ID "C01"`, `doc.md:5: malformed clause ID "X4"`} {
		if !strings.Contains(got, want) {
			t.Errorf("problems %q lack %q", got, want)
		}
	}
	if len(problems) != 2 {
		t.Errorf("got %d problems, want 2:\n%s", len(problems), got)
	}
}

//shed:proves S.doc.4
func TestIDGrammar(t *testing.T) {
	valid := map[string]ID{
		"C1":             {Kind: Charter, N: 1},
		"C19":            {Kind: Charter, N: 19},
		"M3":             {Kind: Milestone, N: 3},
		"S.doc.2":        {Kind: Spec, Area: "doc", N: 2},
		"H.vcs-2.10":     {Kind: Horizon, Area: "vcs-2", N: 10},
		"S.a1.1":         {Kind: Spec, Area: "a1", N: 1},
		"H.vision.4":     {Kind: Horizon, Area: "vision", N: 4},
		"S.proof-run.12": {Kind: Spec, Area: "proof-run", N: 12},
	}
	for s, want := range valid {
		got, err := ParseID(s)
		if err != nil || got != want {
			t.Errorf("ParseID(%q) = %+v, %v; want %+v", s, got, err, want)
		}
		if got.String() != s {
			t.Errorf("%+v prints as %q, want %q", got, got.String(), s)
		}
	}
	for _, s := range []string{"C0", "C01", "M0", "S.doc.0", "S.Doc.1", "S.1a.1", "S..1", "S.doc", "H.doc.1.2", "X1", "c1", "S.doc_x.1"} {
		if _, err := ParseID(s); err == nil {
			t.Errorf("ParseID(%q) succeeded, want error", s)
		}
	}
}

//shed:proves S.doc.5
func TestParserRefusesMisplacedAndNestedClauses(t *testing.T) {
	_, problems := parse(t, `- **C1** Charter clause.
- **H.a.1** (soon) Horizon clause in the charter.
- **C2** Outer clause.
  - **C3** Nested clause.
`, Charter)
	got := messages(problems)
	for _, want := range []string{
		"doc.md:2: H.a.1 is a horizon ID and does not belong in this document",
		"doc.md:4: clause C3 is nested inside clause C2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("problems %q lack %q", got, want)
		}
	}
}

//shed:proves S.cite.1
func TestParseCitation(t *testing.T) {
	c, err := ParseCitation("S.doc.2@HEAD~1")
	if err != nil || c.ID != MustParseID("S.doc.2") || c.Rev != "HEAD~1" {
		t.Errorf("got %+v, %v", c, err)
	}
	if c.String() != "S.doc.2@HEAD~1" {
		t.Errorf("prints as %q", c.String())
	}
	c, err = ParseCitation("C3")
	if err != nil || c.Rev != "" || c.String() != "C3" {
		t.Errorf("got %+v, %v", c, err)
	}
	for _, bad := range []string{"C3@", "C03", "@HEAD"} {
		if _, err := ParseCitation(bad); err == nil {
			t.Errorf("ParseCitation(%q) succeeded", bad)
		}
	}
}

//shed:proves S.cite.3 S.cite.4
func TestMentionsComeFromProseOnly(t *testing.T) {
	doc, _ := parse(t, "# About C1\n\n"+
		"Prose cites C2 and S.doc.3@charter/v1, then H.a.1 to\nH.a.4.\n\n"+
		"Code `C9` is ignored.\n\n"+
		"```\nC8 in a block\n```\n\n"+
		"<!-- C7 in a comment -->\n\n"+
		"- **M1** Covers M2 to M3.\n", Charter, Milestone)
	var got []string
	for _, m := range doc.Mentions {
		s := m.Token
		if m.To != "" {
			s += ".." + m.To
		}
		got = append(got, s)
	}
	want := []string{"C1", "C2", "S.doc.3@charter/v1", "H.a.1..H.a.4", "M2..M3"}
	if !slices.Equal(got, want) {
		t.Errorf("mentions = %v, want %v", got, want)
	}
	if doc.Mentions[3].Line != 3 {
		t.Errorf("range mention on line %d, want 3", doc.Mentions[3].Line)
	}
}
