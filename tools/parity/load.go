package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var kinds = []string{"CMD", "OPT", "CFG", "VIS", "TUI", "BEH", "PKG"}

var expectedCounts = map[string]int{
	"CMD": 18, "OPT": 60, "CFG": 21, "VIS": 34, "TUI": 5, "BEH": 40, "PKG": 11,
}

var dispositions = map[string]bool{
	"equivalent": true, "adaptation": true, "extension": true, "n/a": true, "defect-not-copied": true,
}

type InventoryRow struct {
	ID, Kind, Surface, Aliases, Source, Notes string
}

type MatrixRow struct {
	ID, Disposition, Xbps, Behaviour, Evidence, Spec, Test, Notes string
}

type Matrix struct {
	Inventory   []InventoryRow
	Matrix      []MatrixRow
	Counts      map[string]int
	HeaderTotal int
	SpecRev     string
}

var (
	idRe      = regexp.MustCompile(`^P-(CMD|OPT|CFG|VIS|TUI|BEH|PKG)-\d{3}$`)
	countRe   = regexp.MustCompile(`([A-Za-z]+)=(\d+)`)
	refRe     = regexp.MustCompile(`^(FR|NFR|AC)-\d{3}$`)
	evRe      = regexp.MustCompile(`^(src|obs|dec|proto|fixture|spec):.+$`)
	specRevRe = regexp.MustCompile(`\(rev (\d+)\)`)
	defFRRe   = regexp.MustCompile(`^\| (FR-\d{3}) \| (M|S|C) \|`)
	defNFRRe  = regexp.MustCompile(`^\| (NFR-\d{3}) \| (M|S|C) \|`)
	acRe      = regexp.MustCompile(`\*\*(AC-\d{3})\b`)
)

func Load(root string) (*Matrix, error) {
	invPath := filepath.Join(root, "testdata/parity/nala-v0.18.2.tsv")
	matPath := filepath.Join(root, "testdata/parity/matrix.tsv")
	invLines, err := readLines(invPath)
	if err != nil {
		return nil, err
	}
	matLines, err := readLines(matPath)
	if err != nil {
		return nil, err
	}

	m := &Matrix{Counts: map[string]int{}}
	var invHeader, matHeader []string

	// inventory
	rows := 0
	for _, line := range invLines {
		if strings.HasPrefix(line, "#") {
			if strings.HasPrefix(line, "# counts:") {
				if err := parseCounts(line, m); err != nil {
					return nil, err
				}
			}
			if strings.HasPrefix(line, "# columns:") {
				invHeader = strings.Split(strings.TrimPrefix(line, "# columns: "), "\t")
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			return nil, fmt.Errorf("parity: nala-v0.18.2.tsv:%d has %d columns, want 6", rows+1, len(f))
		}
		rows++
		m.Inventory = append(m.Inventory, InventoryRow{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	if len(invHeader) != 6 {
		return nil, fmt.Errorf("parity: nala-v0.18.2.tsv is missing its column header")
	}

	// matrix
	for i, line := range matLines {
		if strings.HasPrefix(line, "#") {
			if strings.HasPrefix(line, "# spec_rev:") {
				m.SpecRev = strings.TrimSpace(strings.TrimPrefix(line, "# spec_rev:"))
			}
			if strings.HasPrefix(line, "# counts:") {
				mc := &Matrix{Counts: map[string]int{}}
				if err := parseCounts(line, mc); err != nil {
					return nil, err
				}
				for k, v := range mc.Counts {
					if m.Counts[k] != v {
						return nil, fmt.Errorf("parity: matrix counts %s=%d differ from the inventory header %s=%d", k, v, k, m.Counts[k])
					}
				}
				if mc.HeaderTotal != m.HeaderTotal {
					return nil, fmt.Errorf("parity: matrix total %d differs from the inventory total %d", mc.HeaderTotal, m.HeaderTotal)
				}
			}
			if strings.HasPrefix(line, "# columns:") {
				matHeader = strings.Split(strings.TrimPrefix(line, "# columns: "), "\t")
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			return nil, fmt.Errorf("parity: matrix.tsv:%d has %d columns, want 8", i+1, len(f))
		}
		m.Matrix = append(m.Matrix, MatrixRow{f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7]})
	}
	if len(matHeader) != 8 {
		return nil, fmt.Errorf("parity: matrix.tsv is missing its column header")
	}
	if m.SpecRev == "" {
		return nil, fmt.Errorf("parity: matrix.tsv is missing the spec_rev header")
	}
	if !strings.Contains(invPath, "v0.18.2") || len(invLines) == 0 || !strings.Contains(invLines[0], "nala v0.18.2") {
		return nil, fmt.Errorf("parity: inventory header names nala v0.18.2 but the file has %d rows; the inventory is frozen to its tag", len(m.Inventory))
	}
	return m, nil
}

func parseCounts(line string, m *Matrix) error {
	for _, pair := range countRe.FindAllStringSubmatch(line, -1) {
		n, _ := strconv.Atoi(pair[2])
		if pair[1] == "total" {
			m.HeaderTotal = n
		} else {
			m.Counts[pair[1]] = n
		}
	}
	if m.HeaderTotal == 0 {
		return fmt.Errorf("parity: counts header has no total: %s", line)
	}
	return nil
}

func (m *Matrix) NACount() int {
	n := 0
	for _, r := range m.Matrix {
		if r.Disposition == "n/a" {
			n++
		}
	}
	return n
}

func (m *Matrix) Validate(root string) error {
	// counts header vs expected and vs body.
	actual := map[string]int{}
	for _, r := range m.Inventory {
		actual[r.Kind]++
	}
	for _, k := range kinds {
		if m.Counts[k] != expectedCounts[k] {
			return fmt.Errorf("parity: header %s=%d, want %d", k, m.Counts[k], expectedCounts[k])
		}
		if actual[k] != expectedCounts[k] {
			return fmt.Errorf("parity: inventory counts CMD=%d OPT=%d CFG=%d VIS=%d TUI=%d BEH=%d PKG=%d total=%d do not match the header total=%d: one row was added or removed; update the header deliberately",
				actual["CMD"], actual["OPT"], actual["CFG"], actual["VIS"], actual["TUI"], actual["BEH"], actual["PKG"], len(m.Inventory), m.HeaderTotal)
		}
	}
	if len(m.Inventory) != 189 {
		return fmt.Errorf("parity: inventory has %d rows, want 189", len(m.Inventory))
	}

	// inventory ids, order, aliases.
	seenID := map[string]bool{}
	lastNum := map[string]int{}
	seenAlias := map[string]bool{}
	for _, r := range m.Inventory {
		if !idRe.MatchString(r.ID) {
			return fmt.Errorf("parity: inventory id %q is not P-<KIND>-<NNN>", r.ID)
		}
		if !strings.HasPrefix(r.ID, "P-"+r.Kind+"-") {
			return fmt.Errorf("parity: %s has kind %s but its id prefix says %s", r.ID, r.Kind, strings.Split(r.ID, "-")[1])
		}
		if seenID[r.ID] {
			return fmt.Errorf("parity: %s appears 2 times in nala-v0.18.2.tsv", r.ID)
		}
		seenID[r.ID] = true
		n, _ := strconv.Atoi(r.ID[len(r.ID)-3:])
		if n <= lastNum[r.Kind] {
			return fmt.Errorf("parity: %s is out of order (ids within a kind must strictly increase)", r.ID)
		}
		lastNum[r.Kind] = n
		if r.Aliases != "—" {
			for _, a := range strings.Split(r.Aliases, ",") {
				a = strings.TrimSpace(a)
				if seenAlias[a] {
					return fmt.Errorf("parity: alias %q appears on more than one inventory row", a)
				}
				seenAlias[a] = true
			}
		}
	}

	// matrix bijection and schema.
	seenMat := map[string]bool{}
	inv := map[string]InventoryRow{}
	for _, r := range m.Inventory {
		inv[r.ID] = r
	}
	citedAC := map[string]bool{}
	for _, r := range m.Matrix {
		ir, ok := inv[r.ID]
		if !ok {
			return fmt.Errorf("parity: %s has a matrix row but is not in the inventory", r.ID)
		}
		if seenMat[r.ID] {
			return fmt.Errorf("parity: %s appears 2 times in matrix.tsv", r.ID)
		}
		seenMat[r.ID] = true
		if !dispositions[r.Disposition] {
			return fmt.Errorf("parity: %s disposition %q is not one of equivalent, adaptation, extension, n/a, defect-not-copied", r.ID, r.Disposition)
		}
		for name, v := range map[string]string{"xbps": r.Xbps, "behaviour": r.Behaviour, "evidence": r.Evidence, "spec": r.Spec, "notes": r.Notes} {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("parity: %s has an empty %s column", r.ID, name)
			}
		}
		if r.Xbps == "—" && r.Disposition != "equivalent" {
			return fmt.Errorf("parity: %s is %s but its xbps column is empty", r.ID, r.Disposition)
		}
		for _, tok := range strings.Split(r.Evidence, ";") {
			tok = strings.TrimSpace(tok)
			if tok == "" || !evRe.MatchString(tok) {
				return fmt.Errorf("parity: %s evidence token %q is outside the grammar (src:, obs:, dec:, proto:, fixture:, spec:)", r.ID, tok)
			}
		}
		acCount := 0
		for _, tok := range strings.Split(r.Spec, ",") {
			tok = strings.TrimSpace(tok)
			if !refRe.MatchString(tok) {
				return fmt.Errorf("parity: %s spec token %q is not FR-/NFR-/AC-", r.ID, tok)
			}
			if strings.HasPrefix(tok, "AC-") {
				acCount++
				citedAC[tok] = true
			}
		}
		if r.Disposition == "n/a" {
			if acCount == 0 {
				return fmt.Errorf("parity: %s is n/a but cites no AC in its spec column", r.ID)
			}
			if err := checkNABehaviour(r); err != nil {
				return err
			}
			if err := checkNAProof(r, ir); err != nil {
				return err
			}
			if strings.Contains(r.Test, "release:") {
				return fmt.Errorf("parity: %s is n/a and may not use a release: marker", r.ID)
			}
		}
		if r.Test != "—" && !strings.HasPrefix(r.Test, "TestParity_") && !strings.HasPrefix(r.Test, "release:") {
			return fmt.Errorf("parity: %s test cell %q is neither — nor a TestParity_/release: reference", r.ID, r.Test)
		}
		for _, v := range []string{r.Xbps, r.Behaviour, r.Evidence, r.Spec, r.Test, r.Notes} {
			up := strings.ToUpper(v)
			for _, p := range []string{"TBD", "TODO", "FIXME"} {
				if strings.Contains(up, p) {
					return fmt.Errorf("parity: %s has a placeholder in a matrix cell (%s); the matrix must not ship placeholders", r.ID, p)
				}
			}
		}
	}
	for _, r := range m.Inventory {
		if !seenMat[r.ID] {
			return fmt.Errorf("parity: %s is in the inventory but has no matrix row", r.ID)
		}
	}
	if err := m.checkOrder(); err != nil {
		return err
	}

	// spec revision and refs.
	specBytes, err := os.ReadFile(filepath.Join(root, "xbpretty-spec.md"))
	if err != nil {
		return err
	}
	spec := string(specBytes)
	rev := ""
	if mm := specRevRe.FindStringSubmatch(spec); mm != nil {
		rev = mm[1]
	}
	if rev == "" {
		return fmt.Errorf("parity: cannot read the revision from xbpretty-spec.md's title")
	}
	if rev != m.SpecRev {
		return fmt.Errorf("parity: matrix spec_rev %s does not match xbpretty-spec.md rev %s", m.SpecRev, rev)
	}
	defined := map[string]bool{}
	parityAC := map[string]bool{}
	for _, line := range strings.Split(spec, "\n") {
		if mm := defFRRe.FindStringSubmatch(line); mm != nil {
			defined[mm[1]] = true
		}
		if mm := defNFRRe.FindStringSubmatch(line); mm != nil {
			defined[mm[1]] = true
		}
		if mm := acRe.FindStringSubmatch(line); mm != nil {
			defined[mm[1]] = true
			if strings.Contains(line, "[P]") {
				parityAC[mm[1]] = true
			}
		}
	}
	for _, r := range m.Matrix {
		for _, tok := range strings.Split(r.Spec, ",") {
			tok = strings.TrimSpace(tok)
			if !defined[tok] {
				return fmt.Errorf("parity: %s cites %s, which the spec does not define", r.ID, tok)
			}
		}
	}
	for ac := range parityAC {
		if !citedAC[ac] {
			return fmt.Errorf("parity: %s is a [P] AC but no matrix row cites it", ac)
		}
	}
	if implementationMode(root) {
		if err := m.requireTestCells(); err != nil {
			return err
		}
	}
	tests, err := declaredTests(root)
	if err != nil {
		return err
	}
	return m.checkTestCells(tests)
}

// implementationMode reports whether the packaged spec exists. ADR-0020: the
// spec phase may leave `test` cells at the em dash; once internal/spec exists,
// the release half of the contract is live and every cell must resolve.
func implementationMode(root string) bool {
	_, err := os.Stat(filepath.Join(root, "internal", "spec"))
	return err == nil
}

// requireTestCells is the release-phase rule: no row may keep the spec-phase
// placeholder. A `release:` gate is the only accepted non-test value.
func (m *Matrix) requireTestCells() error {
	for _, r := range m.Matrix {
		if strings.TrimSpace(r.Test) == "—" || strings.TrimSpace(r.Test) == "" {
			return fmt.Errorf("parity: %s has no test cell; implementation mode requires a TestParity_ name or a release: gate", r.ID)
		}
	}
	return nil
}

// checkOrder requires the matrix to carry exactly the inventory's order.
func (m *Matrix) checkOrder() error {
	if len(m.Matrix) != len(m.Inventory) {
		return fmt.Errorf("parity: matrix has %d rows, inventory has %d", len(m.Matrix), len(m.Inventory))
	}
	for i := range m.Matrix {
		if m.Matrix[i].ID != m.Inventory[i].ID {
			return fmt.Errorf("parity: matrix row %d is %s, inventory row %d is %s; the matrix must follow the inventory order", i+1, m.Matrix[i].ID, i+1, m.Inventory[i].ID)
		}
	}
	return nil
}

// checkTestCells resolves every cited Test* name against the module's declared
// tests. This is AC-063's renamed/deleted clause and its exact message is
// asserted by TestCitedTestMustExist. A TestParity_ cell must also carry its
// own row id so the mapping cannot be copied between rows.
func (m *Matrix) checkTestCells(tests map[string]bool) error {
	for _, r := range m.Matrix {
		wantPrefix := "TestParity_" + strings.ReplaceAll(r.ID, "-", "_") + "_"
		for _, tok := range strings.Split(r.Test, ",") {
			tok = strings.TrimSpace(tok)
			if !strings.HasPrefix(tok, "Test") {
				continue
			}
			if strings.HasPrefix(tok, "TestParity_") && !strings.HasPrefix(tok, wantPrefix) {
				return fmt.Errorf("parity: %s cites test %s, which does not carry its id prefix %s", r.ID, tok, wantPrefix)
			}
			if !tests[tok] {
				return fmt.Errorf("parity: %s cites test %s, which no *_test.go declares", r.ID, tok)
			}
		}
	}
	return nil
}

// declaredTests collects every Test* function declared in the module's *_test.go
// files. The spec phase has no TestParity_* names, so this is dormant; it is the
// release-phase half of the contract (AC-051).
func declaredTests(root string) (map[string]bool, error) {
	out := map[string]bool{}
	re := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, mm := range re.FindAllStringSubmatch(string(b), -1) {
			out[mm[1]] = true
		}
		return nil
	})
	return out, err
}

func checkNAProof(r MatrixRow, ir InventoryRow) error {
	beh := strings.ToLower(r.Behaviour)
	refusal := strings.Contains(beh, "exit 2") && strings.Contains(beh, "xbpretty:")
	absence := strings.Contains(beh, "never") || strings.Contains(beh, "silent no-op")
	if !refusal && !absence {
		switch ir.Kind {
		case "CMD", "OPT", "CFG":
			return fmt.Errorf("parity: %s is n/a but its behaviour names no exit code and no stderr string", r.ID)
		default:
			return fmt.Errorf("parity: %s is n/a but its behaviour names no backticked literal and does not say never", r.ID)
		}
	}
	return nil
}

// checkNABehaviour rejects the vague prose AC-062 forbids on an n/a row.
func checkNABehaviour(r MatrixRow) error {
	low := strings.ToLower(r.Behaviour)
	for _, generic := range []string{"not applicable", "unsupported", "no equivalent"} {
		if strings.Contains(low, generic) {
			return fmt.Errorf("parity: %s is n/a but its behaviour is the generic phrase %q; name the exact refusal or absence", r.ID, generic)
		}
	}
	return nil
}
