package cli

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/soubarnak/xbpretty/internal/discovery"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// discoveryNeeds is the fixed offline pass set one discovery command needs.
// Every pass is a bulk query except the per-record -S/-R -S calls and the
// worker-pool resolution of multi-variant repository names.
type discoveryNeeds struct {
	installed   bool
	manual      bool
	holds       bool
	orphans     bool
	repolocked  bool
	plan        bool
	description bool
	virtual     bool
}

// installedInfo is one row of xbps-query -l.
type installedInfo struct {
	Pkgver     string
	Version    string
	ShortDesc  string
	State      string
	Unpacked   bool
	HalfRemove bool
}

// snapshot is the in-memory result of the offline pass set.
type discoverySnapshot struct {
	installed   map[string]installedInfo
	manual      map[string]bool
	holds       map[string]bool
	orphans     map[string]bool
	repolocked  map[string]bool
	repo        map[string][]discovery.Row
	repoRank    map[string]int
	repoRankSeq int
	shortDesc   map[string]string
	longDesc    map[string]string
	provides    map[string][]string
	plan        map[string]xbps.Row
	planOK      bool
	resolved    map[string]string
}

func newDiscoverySnapshot() *discoverySnapshot {
	return &discoverySnapshot{
		installed:  map[string]installedInfo{},
		manual:     map[string]bool{},
		holds:      map[string]bool{},
		orphans:    map[string]bool{},
		repolocked: map[string]bool{},
		repo:       map[string][]discovery.Row{},
		repoRank:   map[string]int{},
		shortDesc:  map[string]string{},
		longDesc:   map[string]string{},
		provides:   map[string][]string{},
		plan:       map[string]xbps.Row{},
		resolved:   map[string]string{},
	}
}

// runQuery is one child call. It forwards the child's stderr verbatim and
// returns its exit code when it failed.
func (st *state) runQuery(args ...string) (result, bool) {
	res, err := st.r.run(st.r.cfg.Xbps.QueryPath, args...)
	if err != nil {
		st.errorf("%v\n", err)
		res.exitCode = exitRuntime
		return res, false
	}
	if res.stderr != "" {
		fmt.Fprint(st.stderr, res.stderr)
	}
	if res.exitCode != 0 {
		return res, false
	}
	return res, true
}

// loadSnapshot runs the fixed offline passes and parses their answers.
func (st *state) loadDiscoverySnapshot(needs discoveryNeeds) (*discoverySnapshot, int, bool) {
	s := newDiscoverySnapshot()

	if needs.installed {
		res, ok := st.runQuery("-l")
		if !ok {
			return nil, res.exitCode, false
		}
		s.parseInstalled(res.stdout)
	}
	if needs.manual {
		res, ok := st.runQuery("-m")
		if !ok {
			return nil, res.exitCode, false
		}
		s.parseNames(res.stdout, s.manual)
	}
	if needs.holds {
		res, ok := st.runQuery("-H")
		if !ok {
			return nil, res.exitCode, false
		}
		s.parseNames(res.stdout, s.holds)
	}
	if needs.orphans {
		res, ok := st.runQuery("-O")
		if !ok {
			return nil, res.exitCode, false
		}
		s.parseNames(res.stdout, s.orphans)
	}
	if needs.repolocked {
		res, ok := st.runQuery("--list-repolock-pkgs")
		if !ok {
			return nil, res.exitCode, false
		}
		s.parseNames(res.stdout, s.repolocked)
	}

	// The repository pass carries the human descriptions; the architecture
	// pass carries every (repository, pkgver) variant. Measured on xbps
	// 0.60.7: `-p repository` returns no rows in repository mode, while
	// `-p architecture` returns `pkgver: arch (repository)` for every row.
	res, ok := st.runQuery("-R", "-s", "")
	if !ok {
		return nil, res.exitCode, false
	}
	repoDesc := parseRepositoryDescriptions(res.stdout)
	archRes, ok := st.runQuery("-R", "-s", "", "-p", "architecture")
	if !ok {
		return nil, archRes.exitCode, false
	}
	s.parseRepository(parseArchitectureRows(archRes.stdout), repoDesc)

	if needs.description {
		installedDesc, ok := st.runQuery("-s", "", "-p", "long_desc")
		if !ok {
			return nil, installedDesc.exitCode, false
		}
		repoLong, ok := st.runQuery("-R", "-s", "", "-p", "long_desc")
		if !ok {
			return nil, repoLong.exitCode, false
		}
		s.longDesc = parseDescriptionProperty(repoLong.stdout)
		for pkgver, desc := range parseDescriptionProperty(installedDesc.stdout) {
			s.longDesc[pkgver] = desc
		}
	}
	if needs.virtual {
		res, ok := st.runQuery("-R", "-s", "", "-p", "provides")
		if !ok {
			return nil, res.exitCode, false
		}
		s.provides = parseProvidesProperty(res.stdout)
	}
	if needs.plan {
		s.loadPlan(st)
	}
	return s, -1, true
}

// loadPlan runs the one root-free `-n -u` plan. A failure is deliberately not
// fatal: FR-023 says to forward stderr, omit Upgradable from:, and exit 0.
func (s *discoverySnapshot) loadPlan(st *state) {
	res, err := st.r.run(st.r.cfg.Xbps.InstallPath, "-n", "-u")
	if err != nil {
		st.errorf("%v\n", err)
		return
	}
	if res.stderr != "" {
		fmt.Fprint(st.stderr, res.stderr)
	}
	if res.exitCode != 0 {
		return
	}
	rows, err := xbps.ParsePlan(strings.NewReader(res.stdout))
	if err != nil {
		return
	}
	for _, row := range rows {
		if row.Action == xbps.ActionUpdate {
			s.plan[row.Name] = row
		}
	}
	s.planOK = true
}

func (s *discoverySnapshot) parseInstalled(stdout string) {
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		info := installedInfo{State: fields[0], Pkgver: fields[1]}
		info.Unpacked = fields[0] == "uu"
		info.HalfRemove = fields[0] == "hr"
		name, version := discovery.SplitPkgverVersion(fields[1])
		if name == "" {
			continue
		}
		info.Version = version
		if len(fields) > 2 {
			info.ShortDesc = strings.Join(fields[2:], " ")
		}
		s.installed[name] = info
		s.shortDesc[fields[1]] = info.ShortDesc
	}
}

func (s *discoverySnapshot) parseNames(stdout string, dst map[string]bool) {
	for _, line := range strings.Split(stdout, "\n") {
		if name, _ := discovery.SplitPkgverVersion(strings.TrimSpace(line)); name != "" {
			dst[name] = true
		}
	}
}

type architectureRow struct {
	Pkgver string
	Arch   string
	Repo   string
}

func parseArchitectureRows(stdout string) []architectureRow {
	var out []architectureRow
	for _, row := range parsePropertyRows(stdout, true) {
		out = append(out, architectureRow{Pkgver: row[0], Arch: row[1], Repo: row[2]})
	}
	return out
}

func parseRepositoryDescriptions(stdout string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "[*] "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "[*] "))
		case strings.HasPrefix(line, "[-] "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "[-] "))
		}
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		if len(fields) > 1 {
			out[fields[0]] = strings.Join(fields[1:], " ")
		}
	}
	return out
}

// parseRepository builds the variant universe from the ordered architecture
// rows. Two repositories offering the same pkgver stay two variants because
// the key is (repository, pkgver), and the first repository seen sets the
// precedence the rest of the model sorts by.
func (s *discoverySnapshot) parseRepository(rows []architectureRow, desc map[string]string) {
	for _, ar := range rows {
		name, version := discovery.SplitPkgverVersion(ar.Pkgver)
		if name == "" {
			continue
		}
		if _, seen := s.repoRank[ar.Repo]; !seen {
			s.repoRank[ar.Repo] = s.repoRankSeq
			s.repoRankSeq++
		}
		upstream, revision := discovery.SplitRevision(version)
		row := discovery.Row{
			Name: name, Pkgver: ar.Pkgver, Version: upstream, Revision: revision,
			Architecture: ar.Arch, Repository: ar.Repo, ShortDesc: desc[ar.Pkgver],
		}
		if row.ShortDesc != "" {
			s.shortDesc[ar.Pkgver] = row.ShortDesc
		}
		duplicate := false
		for _, existing := range s.repo[name] {
			if existing.Pkgver == row.Pkgver && existing.Repository == row.Repository {
				duplicate = true
				break
			}
		}
		if !duplicate {
			s.repo[name] = append(s.repo[name], row)
		}
	}
	for name := range s.repo {
		sort.SliceStable(s.repo[name], func(i, j int) bool {
			ri, rj := s.repoRank[s.repo[name][i].Repository], s.repoRank[s.repo[name][j].Repository]
			if ri != rj {
				return ri < rj
			}
			return s.repo[name][i].Pkgver < s.repo[name][j].Pkgver
		})
	}
}

// parsePropertyRows reads the `pkgver: value (repository)` shape xbps uses
// for -p searches. A repository property may also be a bare value, so the
// parser accepts both.
func parsePropertyRows(stdout string, stripRepo bool) [][3]string {
	var rows [][3]string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "[*] "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "[*] "))
		case strings.HasPrefix(line, "[-] "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "[-] "))
		}
		if line == "" {
			continue
		}
		left, right, ok := strings.Cut(line, ": ")
		if !ok {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			left, right = fields[0], strings.Join(fields[1:], " ")
		}
		value, repo := right, ""
		if stripRepo {
			value, repo = splitRepositorySuffix(right)
		}
		rows = append(rows, [3]string{left, value, repo})
	}
	return rows
}

// splitRepositorySuffix removes the trailing `(URL)` xbps appends to -p
// search values and returns it separately.
func splitRepositorySuffix(value string) (string, string) {
	value = strings.TrimSpace(value)
	if !strings.HasSuffix(value, ")") {
		return value, ""
	}
	open := strings.LastIndex(value, " (")
	if open < 0 {
		return value, ""
	}
	repo := strings.TrimSuffix(value[open+2:], ")")
	if !isRepositoryURI(repo) {
		return value, ""
	}
	return strings.TrimSpace(value[:open]), repo
}

func isRepositoryURI(value string) bool {
	switch {
	case strings.HasPrefix(value, "http://"), strings.HasPrefix(value, "https://"),
		strings.HasPrefix(value, "file://"), strings.HasPrefix(value, "ftp://"):
		return true
	case strings.HasPrefix(value, "/"), strings.HasPrefix(value, "./"), strings.HasPrefix(value, "../"):
		return true
	}
	return false
}

func parseDescriptionProperty(stdout string) map[string]string {
	out := map[string]string{}
	for _, row := range parsePropertyRows(stdout, false) {
		out[row[0]] = row[1]
	}
	return out
}

func parseProvidesProperty(stdout string) map[string][]string {
	out := map[string][]string{}
	for _, row := range parsePropertyRows(stdout, true) {
		out[row[0]] = append(out[row[0]], row[1])
	}
	return out
}

// resolveMultiVariant runs `-R -S` for repository-only names that have more
// than one variant. The calls go through the same eight-worker pool as the
// transaction lookups (ADR-0001).
func (st *state) resolveMultiVariant(names []string) (map[string]string, error) {
	out := map[string]string{}
	if len(names) == 0 {
		return out, nil
	}
	type answer struct {
		name   string
		pkgver string
		stderr string
		err    error
	}
	jobs := make(chan string)
	results := make(chan answer)
	workers := 8
	if len(names) < workers {
		workers = len(names)
	}
	for i := 0; i < workers; i++ {
		go func() {
			for name := range jobs {
				res, err := st.r.run(st.r.cfg.Xbps.QueryPath, "-R", "-S", name)
				pkgver := ""
				if err == nil && res.exitCode == 0 {
					rec := parseRecord(res.stdout)
					pkgver = rec.scalar["pkgver"]
				}
				results <- answer{name: name, pkgver: pkgver, stderr: res.stderr, err: err}
			}
		}()
	}
	go func() {
		for _, name := range names {
			jobs <- name
		}
		close(jobs)
	}()
	var firstErr error
	for i := 0; i < len(names); i++ {
		a := <-results
		if a.stderr != "" {
			fmt.Fprint(st.stderr, a.stderr)
		}
		if a.err != nil && firstErr == nil {
			firstErr = a.err
		}
		out[a.name] = a.pkgver
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// runList implements FR-023/FR-024 for list.
func (st *state) runList(opts *options) int {
	return st.runDiscoveryRows(opts, false)
}

// runSearch implements FR-024's RE2 search over names and short_desc.
func (st *state) runSearch(opts *options) int {
	return st.runDiscoveryRows(opts, true)
}

func (st *state) runDiscoveryRows(opts *options, search bool) int {
	needs := discoveryNeeds{
		installed: true, manual: true, holds: true, orphans: true, repolocked: true,
		plan: !opts.virtual, description: opts.description, virtual: opts.virtual,
	}
	s, code, ok := st.loadDiscoverySnapshot(needs)
	if !ok {
		return code
	}

	namesNeedingResolution := s.namesNeedingResolution(opts)
	if len(namesNeedingResolution) > 0 {
		resolved, err := st.resolveMultiVariant(namesNeedingResolution)
		if err != nil {
			st.errorf("%v\n", err)
			return exitRuntime
		}
		s.resolved = resolved
	}

	rows := s.rows(opts)
	if search {
		filtered, badPattern, err := filterSearchRows(rows, opts.args, opts.namesOnly)
		if err != nil {
			st.errorf("invalid pattern '%s': %v\n", badPattern, err)
			return exitRuntime
		}
		rows = filtered
	} else {
		filtered, notices, badPattern, err := filterGlobRows(rows, opts.args)
		if err != nil {
			st.errorf("invalid pattern '%s': %v\n", badPattern, err)
			return exitRuntime
		}
		for _, notice := range notices {
			fmt.Fprintf(st.stderr, "Notice: '%s' was not found\n", notice)
		}
		rows = filtered
	}
	return st.renderDiscoveryRows(rows, opts, s)
}

func (s *discoverySnapshot) namesNeedingResolution(opts *options) []string {
	if opts.virtual || opts.installed || opts.upgradable || opts.allVersions {
		return nil
	}
	var names []string
	for name, variants := range s.repo {
		if len(variants) < 2 {
			continue
		}
		if _, installed := s.installed[name]; installed {
			continue
		}
		if _, planned := s.plan[name]; planned {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *discoverySnapshot) rows(opts *options) []discovery.Row {
	if opts.virtual {
		if opts.upgradable {
			return nil
		}
		return s.virtualRows(opts.installed)
	}
	names := map[string]bool{}
	for name := range s.installed {
		names[name] = true
	}
	for name := range s.repo {
		names[name] = true
	}
	if opts.upgradable && s.planOK {
		// The plan is authoritative: a plan update can name a package the
		// bulk lists missed, and the row still carries exact sizes.
		for name := range s.plan {
			names[name] = true
		}
		for name := range names {
			if _, ok := s.plan[name]; !ok {
				delete(names, name)
			}
		}
	}
	if opts.installed {
		for name := range names {
			if _, ok := s.installed[name]; !ok {
				delete(names, name)
			}
		}
	}

	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)

	var rows []discovery.Row
	for _, name := range sorted {
		if opts.upgradable {
			if plan, ok := s.plan[name]; ok {
				rows = append(rows, s.planRow(plan, true))
			}
			continue
		}
		if opts.allVersions {
			rows = append(rows, s.variantRows(name, opts)...)
			continue
		}
		rows = append(rows, s.defaultRow(name))
	}
	return rows
}

func (s *discoverySnapshot) defaultRow(name string) discovery.Row {
	if plan, ok := s.plan[name]; ok && s.planOK {
		return s.planRow(plan, false)
	}
	variants := s.repo[name]
	if len(variants) == 0 {
		if info, ok := s.installed[name]; ok {
			return s.installedRow(name, info)
		}
		return discovery.Row{Name: name}
	}
	chosen := variants[0]
	if len(variants) > 1 {
		if pkgver := s.resolved[name]; pkgver != "" {
			for _, variant := range variants {
				if variant.Pkgver == pkgver {
					chosen = variant
					break
				}
			}
		}
	}
	if info, ok := s.installed[name]; ok && chosen.Pkgver == info.Pkgver {
		return s.attachInstalled(chosen, info)
	}
	return chosen
}

func (s *discoverySnapshot) installedRow(name string, info installedInfo) discovery.Row {
	if variants := s.repo[name]; len(variants) > 0 {
		for _, variant := range variants {
			if variant.Pkgver == info.Pkgver {
				return s.attachInstalled(variant, info)
			}
		}
	}
	row := discovery.Row{
		Name: name, Pkgver: info.Pkgver, Version: info.Version,
		ShortDesc: info.ShortDesc,
	}
	version, revision := discovery.SplitRevision(info.Version)
	row.Version, row.Revision = version, revision
	return s.attachInstalled(row, info)
}

func (s *discoverySnapshot) attachInstalled(row discovery.Row, info installedInfo) discovery.Row {
	row.Installed = true
	row.InstalledVersion = info.Version
	row.Unpacked = info.Unpacked
	row.HalfRemoved = info.HalfRemove
	row.Automatic = !s.manual[row.Name]
	row.Held = s.holds[row.Name]
	row.Repolocked = s.repolocked[row.Name]
	row.AutoRemovable = row.Automatic && s.orphans[row.Name]
	return row
}

func (s *discoverySnapshot) variantRows(name string, opts *options) []discovery.Row {
	var rows []discovery.Row
	info, installed := s.installed[name]
	installedAttached := false
	installedRepo := ""
	if installed {
		for _, variant := range s.repo[name] {
			if variant.Pkgver == info.Pkgver {
				rows = append(rows, s.attachInstalled(variant, info))
				installedAttached = true
				installedRepo = variant.Repository
				break
			}
		}
		if !installedAttached {
			rows = append(rows, s.installedRow(name, info))
		}
	}
	for _, variant := range s.repo[name] {
		if installed && installedAttached && variant.Pkgver == info.Pkgver && variant.Repository == installedRepo {
			continue
		}
		if plan, ok := s.plan[name]; ok && s.planOK && variant.Pkgver == plan.Pkgver() {
			if info, installed := s.installed[name]; installed {
				variant = s.attachInstalled(variant, info)
				if info.Version != "" {
					variant.UpgradableFrom = info.Version
				}
			}
			if opts.upgradable {
				setPlanSizes(&variant, plan)
			}
		}
		rows = append(rows, variant)
	}
	if plan, ok := s.plan[name]; ok && s.planOK {
		found := false
		for _, row := range rows {
			if row.Pkgver == plan.Pkgver() {
				found = true
				break
			}
		}
		if !found {
			rows = append(rows, s.planRow(plan, opts.upgradable))
		}
	}
	return rows
}

func (s *discoverySnapshot) planRow(plan xbps.Row, withSizes bool) discovery.Row {
	name, version := discovery.SplitPkgverVersion(plan.Pkgver())
	upstream, revision := discovery.SplitRevision(version)
	row := discovery.Row{
		Name: name, Pkgver: plan.Pkgver(), Version: upstream, Revision: revision,
		Architecture: plan.Arch, Repository: plan.Repo,
		ShortDesc: s.shortDesc[plan.Pkgver()],
	}
	if info, ok := s.installed[name]; ok {
		row = s.attachInstalled(row, info)
		if info.Version != "" {
			row.UpgradableFrom = info.Version
		}
	}
	if withSizes {
		setPlanSizes(&row, plan)
	}
	return row
}

func setPlanSizes(row *discovery.Row, plan xbps.Row) {
	if plan.HasInstalled {
		n := plan.InstalledSize
		row.InstalledSize = &n
	}
	if plan.HasDownload {
		n := plan.DownloadSize
		row.DownloadSize = &n
	}
}

func (s *discoverySnapshot) virtualRows(installedOnly bool) []discovery.Row {
	providers := map[string]map[string]bool{}
	for pkgver, values := range s.provides {
		name, _ := discovery.SplitPkgverVersion(pkgver)
		if name == "" {
			continue
		}
		for _, value := range values {
			virtual := normalizeVirtual(value)
			if virtual == "" {
				continue
			}
			if providers[virtual] == nil {
				providers[virtual] = map[string]bool{}
			}
			providers[virtual][name] = true
		}
	}
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := make([]discovery.Row, 0, len(names))
	for _, name := range names {
		var list []string
		hasInstalledProvider := false
		for provider := range providers[name] {
			list = append(list, provider)
			if _, ok := s.installed[provider]; ok {
				hasInstalledProvider = true
			}
		}
		if installedOnly && !hasInstalledProvider {
			continue
		}
		sort.Strings(list)
		rows = append(rows, discovery.Row{Name: name, Virtual: true, Providers: list})
	}
	return rows
}

func normalizeVirtual(value string) string {
	value = strings.TrimSpace(value)
	// Every provides value carries the provider's version suffix
	// (`awk-0_1`, `pc:foo-1.0_1`, `cmd:foo-1.0_1`); the virtual name is the
	// part before it. A shlib has no such suffix (`shlib:libz.so.1`).
	name, version := discovery.SplitPkgverVersion(value)
	if version != "" && strings.Contains(version, "_") && version[0] >= '0' && version[0] <= '9' {
		return name
	}
	return value
}

// filterGlobRows applies FR-024's case-insensitive globs. Each token is
// OR-ed; a token that selects nothing produces one notice.
func filterGlobRows(rows []discovery.Row, tokens []string) ([]discovery.Row, []string, string, error) {
	if len(tokens) == 0 {
		return rows, nil, "", nil
	}
	type matcher struct {
		token   string
		pattern string
		version string
		arch    string
	}
	matchers := make([]matcher, 0, len(tokens))
	for _, token := range tokens {
		m := matcher{token: token, pattern: token}
		if name, arch, ok := strings.Cut(m.pattern, ":"); ok && arch != "" && !strings.HasPrefix(name, "cmd") && !strings.HasPrefix(name, "shlib") {
			m.pattern, m.arch = name, arch
		}
		if name, version, ok := strings.Cut(m.pattern, "="); ok {
			m.pattern, m.version = name, version
		}
		expanded, err := expandBraces(m.pattern)
		if err != nil {
			return nil, nil, token, err
		}
		m.pattern = expanded[0]
		if len(expanded) > 1 {
			// A brace token is OR-ed internally; keep it as a synthetic
			// pattern set by testing each alternative below.
			m.pattern = strings.Join(expanded, "\x00")
		}
		// Validate the glob before running it, so a bad token is a runtime
		// error even when another token matches.
		for _, glob := range expanded {
			if _, err := path.Match(strings.ToLower(glob), "x"); err != nil {
				return nil, nil, token, err
			}
		}
		matchers = append(matchers, m)
	}

	var out []discovery.Row
	matched := make([]bool, len(matchers))
	for _, row := range rows {
		keep := false
		for i, m := range matchers {
			if globRowMatches(row, m.pattern, m.version, m.arch) {
				matched[i] = true
				keep = true
			}
		}
		if keep {
			out = append(out, row)
		}
	}
	var notices []string
	for i, m := range matchers {
		if !matched[i] {
			notices = append(notices, m.token)
		}
	}
	return out, notices, "", nil
}

func globRowMatches(row discovery.Row, pattern, version, arch string) bool {
	if version != "" && row.FullVersion() != version && row.Version != version {
		return false
	}
	if arch != "" && !strings.EqualFold(row.Architecture, arch) {
		return false
	}
	for _, glob := range strings.Split(pattern, "\x00") {
		ok, err := path.Match(strings.ToLower(glob), strings.ToLower(row.Name))
		if err == nil && ok {
			return true
		}
	}
	return false
}

// expandBraces expands nala's {a,b} glob alternative. Nested braces are not
// part of the surface; an unmatched brace is left for path.Match to reject.
func expandBraces(pattern string) ([]string, error) {
	open := strings.IndexByte(pattern, '{')
	if open < 0 {
		return []string{pattern}, nil
	}
	close := strings.IndexByte(pattern[open:], '}')
	if close < 0 {
		return []string{pattern}, nil
	}
	close += open
	body := pattern[open+1 : close]
	var out []string
	for _, alt := range strings.Split(body, ",") {
		rest, err := expandBraces(pattern[:open] + alt + pattern[close+1:])
		if err != nil {
			return nil, err
		}
		out = append(out, rest...)
	}
	return out, nil
}

func filterSearchRows(rows []discovery.Row, tokens []string, namesOnly bool) ([]discovery.Row, string, error) {
	if len(tokens) == 0 {
		return rows, "", nil
	}
	patterns := make([]*regexp.Regexp, 0, len(tokens))
	for _, token := range tokens {
		re, err := regexp.Compile("(?i)" + token)
		if err != nil {
			return nil, token, err
		}
		patterns = append(patterns, re)
	}
	var out []discovery.Row
	for _, row := range rows {
		for _, re := range patterns {
			if re.MatchString(row.Name) || (!namesOnly && re.MatchString(row.ShortDesc)) {
				out = append(out, row)
				break
			}
		}
	}
	return out, "", nil
}

func (st *state) renderDiscoveryRows(rows []discovery.Row, opts *options, s *discoverySnapshot) int {
	for _, row := range rows {
		if opts.machine {
			if err := st.writeMachineRow(row, nil); err != nil {
				// FR-074: stop writing on EPIPE and keep the successful
				// discovery exit code.
				return exitOK
			}
			continue
		}
		fmt.Fprintln(st.stdout, row.Line())
		text := ""
		if opts.description {
			text = row.LongDesc
			if text == "" {
				text = s.longDesc[row.Pkgver]
			}
			if text == "" {
				text = "No Description"
			}
		} else if opts.summaryFlag {
			text = row.ShortDesc
			if text == "" {
				text = "No Summary"
			}
		}
		if opts.description || opts.summaryFlag {
			fmt.Fprintln(st.stdout, text)
		}
	}
	return exitOK
}

func (st *state) writeMachineRow(row discovery.Row, properties map[string]any) error {
	if row.Virtual {
		return discovery.WriteNDJSON(st.stdout, row.VirtualMachine())
	}
	machine := row.Machine()
	machine.Properties = properties
	return discovery.WriteNDJSON(st.stdout, machine)
}

// runShow implements FR-025 for show and info.
func (st *state) runShow(opts *options) int {
	s, code, ok := st.loadDiscoverySnapshot(discoveryNeeds{orphans: true})
	if !ok {
		return code
	}
	type block struct {
		row   discovery.Row
		props *xbpsRecord
	}
	var missing []string
	var blocks []block
	var additional int

	for _, name := range opts.args {
		installedRec, found, code := st.record("-S", name)
		if code >= 0 {
			return code
		}
		variants := s.repo[name]
		if !found && len(variants) == 0 {
			missing = append(missing, name)
			continue
		}

		var perName []block
		installedPrinted := false
		if found {
			row := s.recordRow(installedRec, true)
			perName = append(perName, block{row: row, props: installedRec})
			installedPrinted = true
		}

		if opts.allVersions {
			for _, variant := range variants {
				var rec *xbpsRecord
				var variantFound bool
				if variant.Repository != "" {
					rec, variantFound, code = st.record("-R", "-i", "--repository", variant.Repository, "-S", variant.Name)
				} else {
					rec, variantFound, code = st.record("-R", "-S", variant.Pkgver)
				}
				if code >= 0 {
					return code
				}
				if !variantFound {
					continue
				}
				row := s.recordRow(rec, false)
				if row.Repository == "" {
					row.Repository = variant.Repository
				}
				perName = append(perName, block{row: row, props: rec})
			}
		} else if !installedPrinted {
			rec, variantFound, code := st.record("-R", "-S", name)
			if code >= 0 {
				return code
			}
			if !variantFound {
				missing = append(missing, name)
				continue
			}
			row := s.recordRow(rec, false)
			perName = append(perName, block{row: row, props: rec})
		}

		if len(perName) > 0 && !opts.allVersions {
			if installedPrinted {
				additional += len(variants)
			} else if len(variants) > 1 {
				additional += len(variants) - 1
			}
		}
		blocks = append(blocks, perName...)
	}

	for i, b := range blocks {
		if opts.machine {
			props := map[string]any{}
			for k, v := range b.props.properties {
				props[k] = v
			}
			if err := st.writeMachineRow(b.row, props); err != nil {
				// FR-074: a failed stdout write stops machine output but does
				// not turn a successful discovery into an error.
				return exitOK
			}
			continue
		}
		if i > 0 {
			fmt.Fprintln(st.stdout)
		}
		printRecord(st.stdout, b.row, b.props)
	}
	if additional > 0 {
		fmt.Fprintf(st.stderr, "Notice: There are %d additional records. Please use the '-a' switch to see them.\n", additional)
	}
	for _, name := range missing {
		fmt.Fprintf(st.stderr, "Notice: '%s' was not found\n", name)
	}
	return exitOK
}

// record runs one per-record query. A silent exit 2 is the xbps "not found"
// answer; every other failure is passed through.
func (st *state) record(args ...string) (*xbpsRecord, bool, int) {
	res, err := st.r.run(st.r.cfg.Xbps.QueryPath, args...)
	if err != nil {
		st.errorf("%v\n", err)
		return nil, false, exitRuntime
	}
	if res.exitCode != 0 {
		if res.exitCode == 2 && res.stdout == "" && res.stderr == "" {
			return nil, false, -1
		}
		if res.stderr != "" {
			fmt.Fprint(st.stderr, res.stderr)
		}
		return nil, false, res.exitCode
	}
	if res.stderr != "" {
		fmt.Fprint(st.stderr, res.stderr)
	}
	rec := parseRecord(res.stdout)
	if rec == nil || rec.scalar["pkgver"] == "" {
		return nil, false, -1
	}
	return rec, true, -1
}

func (s *discoverySnapshot) recordRow(rec *xbpsRecord, installed bool) discovery.Row {
	name, version := discovery.SplitPkgverVersion(rec.scalar["pkgver"])
	if name == "" {
		name = rec.scalar["pkgname"]
	}
	upstream, revision := discovery.SplitRevision(version)
	row := discovery.Row{
		Name: name, Pkgver: rec.scalar["pkgver"], Version: upstream, Revision: revision,
		Architecture: rec.scalar["architecture"],
		Repository:   rec.scalar["repository"],
		ShortDesc:    rec.scalar["short_desc"],
		LongDesc:     rec.scalar["long_desc"],
		Installed:    installed,
		Automatic:    installed && rec.scalar["automatic-install"] == "yes",
		Held:         installed && rec.scalar["hold"] == "yes",
		Repolocked:   installed && rec.scalar["repolock"] == "yes",
		Unpacked:     installed && rec.scalar["state"] == "unpacked",
		HalfRemoved:  installed && rec.scalar["state"] == "half-removed",
	}
	if installed {
		row.InstalledVersion = version
		row.AutoRemovable = row.Automatic && s.orphans[name]
	}
	return row
}

// xbpsRecord is one `xbps-query -S` answer: scalar properties, tab-indented
// list properties, and the two-space-group/tab-value alternatives dictionary.
type xbpsRecord struct {
	scalar           map[string]string
	list             map[string][]string
	alternatives     map[string][]string
	alternativeOrder []string
	properties       map[string]any
}

func parseRecord(stdout string) *xbpsRecord {
	rec := &xbpsRecord{
		scalar: map[string]string{}, list: map[string][]string{},
		alternatives: map[string][]string{}, properties: map[string]any{},
	}
	var current, group string
	for _, line := range strings.Split(stdout, "\n") {
		trimmed := strings.TrimSpace(line)
		if current == "alternatives" && strings.HasPrefix(line, "  ") {
			rest := strings.TrimPrefix(line, "  ")
			if !strings.HasPrefix(rest, "\t") && !strings.HasPrefix(rest, " ") && strings.HasSuffix(trimmed, ":") {
				group = strings.TrimSuffix(trimmed, ":")
				if _, ok := rec.alternatives[group]; !ok {
					rec.alternativeOrder = append(rec.alternativeOrder, group)
				}
				continue
			}
		}
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ") {
			if trimmed == "" {
				continue
			}
			if current == "alternatives" {
				if group != "" {
					rec.alternatives[group] = append(rec.alternatives[group], trimmed)
				}
				continue
			}
			if current != "" {
				rec.list[current] = append(rec.list[current], trimmed)
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) == "" {
			current, group = "", ""
			continue
		}
		key = strings.TrimSpace(key)
		current, group = key, ""
		value = strings.TrimSpace(value)
		if value == "" {
			if key == "alternatives" {
				continue
			}
			if _, exists := rec.list[key]; !exists {
				rec.list[key] = nil
			}
			continue
		}
		rec.scalar[key] = value
		rec.properties[key] = value
	}
	for key, values := range rec.list {
		if len(values) > 0 {
			rec.properties[key] = values
			delete(rec.scalar, key)
		} else {
			delete(rec.properties, key)
		}
	}
	if len(rec.alternatives) > 0 {
		rec.properties["alternatives"] = rec.alternatives
	}
	return rec
}

// printRecord writes one FR-025 block. Fields are in the spec's fixed order;
// a missing property omits its line.
func printRecord(out interface{ Write([]byte) (int, error) }, row discovery.Row, rec *xbpsRecord) {
	type field struct {
		label  string
		values []string
	}
	scalar := func(key string) []string {
		if v := rec.scalar[key]; v != "" {
			return []string{v}
		}
		return nil
	}
	packageName := scalar("pkgname")
	if len(packageName) == 0 && row.Name != "" {
		packageName = []string{row.Name}
	}
	fields := []field{
		{"Package", packageName},
		{"Version", []string{row.FullVersion()}},
		{"Architecture", scalar("architecture")},
		{"Installed-Size", scalar("installed_size")},
		{"Size", scalar("filename-size")},
		{"Maintainer", scalar("maintainer")},
		{"Homepage", scalar("homepage")},
		{"License", scalar("license")},
		{"SHA256", scalar("filename-sha256")},
		{"Repository", scalar("repository")},
	}
	if row.Installed {
		fields = append(fields,
			field{"State", scalar("state")},
			field{"Install-Date", scalar("install-date")},
		)
		if rec.scalar["automatic-install"] == "yes" {
			fields = append(fields, field{"Automatic", []string{"yes"}})
		}
	}
	if rec.scalar["hold"] == "yes" {
		fields = append(fields, field{"Held", []string{"yes"}})
	}
	if rec.scalar["repolock"] == "yes" {
		fields = append(fields, field{"Repolocked", []string{"yes"}})
	}
	fields = append(fields,
		field{"Provides", rec.list["provides"]},
		field{"Depends", rec.list["run_depends"]},
		field{"Required-Shlibs", rec.list["shlib-requires"]},
		field{"Provided-Shlibs", rec.list["shlib-provides"]},
		field{"Conflicts", rec.list["conflicts"]},
		field{"Replaces", rec.list["replaces"]},
		field{"Alternatives", nil},
	)
	description := rec.scalar["long_desc"]
	if description == "" {
		description = rec.scalar["short_desc"]
	}
	if description != "" {
		fields = append(fields, field{"Description", []string{description}})
	}
	fields = append(fields, field{"Attributes", []string{row.AttributeText()}})

	for _, f := range fields {
		if f.label == "Alternatives" {
			if len(rec.alternativeOrder) == 0 {
				continue
			}
			fmt.Fprintln(out, "Alternatives:")
			for _, group := range rec.alternativeOrder {
				fmt.Fprintf(out, "    %s:\n", group)
				for _, value := range rec.alternatives[group] {
					fmt.Fprintf(out, "        %s\n", value)
				}
			}
			continue
		}
		if len(f.values) == 0 {
			continue
		}
		if len(f.values) <= 3 {
			fmt.Fprintf(out, "%s: %s\n", f.label, strings.Join(f.values, ", "))
			continue
		}
		fmt.Fprintf(out, "%s:\n", f.label)
		for _, value := range f.values {
			fmt.Fprintf(out, "    %s\n", value)
		}
	}
}
