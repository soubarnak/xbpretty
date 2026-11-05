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
	machine     bool
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
type snapshot struct {
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

func newSnapshot() *snapshot {
	return &snapshot{
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
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
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
func (st *state) loadSnapshot(needs discoveryNeeds) (*snapshot, int, bool) {
	s := newSnapshot()

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

	// The repository pass is required: it is the half of the universe that is
	// not in the pkgdb.
	res, ok := st.runQuery("-R", "-s", "")
	if !ok {
		return nil, res.exitCode, false
	}
	prop, ok := st.runQuery("-R", "-s", "", "-p", "repository")
	if !ok {
		return nil, prop.exitCode, false
	}
	repoByPkgver := parseRepositoryProperty(prop.stdout)
	archByPkgver, archRepo := map[string]string{}, map[string]string{}
	// xbps 0.60.7 returns no rows for `-R -s '' -p repository`; the property
	// search only carries the repository on the parenthesised suffix of other
	// properties. Fall back to the architecture pass only when the repository
	// pass yielded nothing, so the normal fixture shape keeps its exact pass
	// set and a host without repository rows still attributes packages.
	if needs.machine || len(repoByPkgver) == 0 {
		arch, ok := st.runQuery("-R", "-s", "", "-p", "architecture")
		if !ok {
			return nil, arch.exitCode, false
		}
		archByPkgver, archRepo = parseArchitectureProperty(arch.stdout)
	}
	for pkgver, repo := range archRepo {
		if repoByPkgver[pkgver] == "" {
			repoByPkgver[pkgver] = repo
		}
	}
	s.parseRepository(res.stdout, repoByPkgver, archByPkgver)

	if needs.description {
		res, ok := st.runQuery("-R", "-s", "", "-p", "long_desc")
		if !ok {
			return nil, res.exitCode, false
		}
		s.longDesc = parseDescriptionProperty(res.stdout)
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
func (s *snapshot) loadPlan(st *state) {
	res, err := st.r.run(st.r.cfg.Xbps.InstallPath, "-n", "-u")
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
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

func (s *snapshot) parseInstalled(stdout string) {
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

func (s *snapshot) parseNames(stdout string, dst map[string]bool) {
	for _, line := range strings.Split(stdout, "\n") {
		if name, _ := discovery.SplitPkgverVersion(strings.TrimSpace(line)); name != "" {
			dst[name] = true
		}
	}
}

func (s *snapshot) parseRepository(stdout string, repoByPkgver, archByPkgver map[string]string) {
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
		pkgver := fields[0]
		name, version := discovery.SplitPkgverVersion(pkgver)
		if name == "" {
			continue
		}
		repo := repoByPkgver[pkgver]
		if repo == "" {
			repo = archByPkgver[pkgver]
		}
		if _, seen := s.repoRank[repo]; !seen {
			s.repoRank[repo] = s.repoRankSeq
			s.repoRankSeq++
		}
		desc := strings.Join(fields[1:], " ")
		if desc != "" {
			s.shortDesc[pkgver] = desc
		}
		upstream, revision := discovery.SplitRevision(version)
		row := discovery.Row{
			Name: name, Pkgver: pkgver, Version: upstream, Revision: revision,
			Architecture: archByPkgver[pkgver], Repository: repo, ShortDesc: desc,
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
func parsePropertyRows(stdout string) [][3]string {
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
		value, repo := splitRepositorySuffix(right)
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
	if !strings.HasPrefix(repo, "http://") && !strings.HasPrefix(repo, "https://") && !strings.HasPrefix(repo, "/") {
		return value, ""
	}
	return strings.TrimSpace(value[:open]), repo
}

func parseRepositoryProperty(stdout string) map[string]string {
	out := map[string]string{}
	for _, row := range parsePropertyRows(stdout) {
		repo := row[2]
		if repo == "" {
			repo = row[1]
		}
		if repo != "" {
			out[row[0]] = repo
		}
	}
	return out
}

func parseArchitectureProperty(stdout string) (map[string]string, map[string]string) {
	arch, repo := map[string]string{}, map[string]string{}
	for _, row := range parsePropertyRows(stdout) {
		arch[row[0]] = row[1]
		repo[row[0]] = row[2]
	}
	return arch, repo
}

func parseDescriptionProperty(stdout string) map[string]string {
	out := map[string]string{}
	for _, row := range parsePropertyRows(stdout) {
		out[row[0]] = row[1]
	}
	return out
}

func parseProvidesProperty(stdout string) map[string][]string {
	out := map[string][]string{}
	for _, row := range parsePropertyRows(stdout) {
		out[row[0]] = append(out[row[0]], row[1])
	}
	return out
}

// resolveMultiVariant runs `-R -S` for repository-only names that have more
// than one variant. The calls go through the same eight-worker pool as the
// transaction lookups (ADR-0001).
func (st *state) resolveMultiVariant(names []string) map[string]string {
	out := map[string]string{}
	if len(names) == 0 {
		return out
	}
	type answer struct {
		name   string
		pkgver string
		stderr string
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
				results <- answer{name: name, pkgver: pkgver, stderr: res.stderr}
			}
		}()
	}
	go func() {
		for _, name := range names {
			jobs <- name
		}
		close(jobs)
	}()
	for i := 0; i < len(names); i++ {
		a := <-results
		if a.stderr != "" {
			fmt.Fprint(st.stderr, a.stderr)
		}
		out[a.name] = a.pkgver
	}
	return out
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
		machine: opts.machine, plan: !opts.virtual, description: opts.description, virtual: opts.virtual,
	}
	s, code, ok := st.loadSnapshot(needs)
	if !ok {
		return code
	}

	namesNeedingResolution := s.namesNeedingResolution(opts)
	if len(namesNeedingResolution) > 0 {
		s.resolved = st.resolveMultiVariant(namesNeedingResolution)
	}

	rows := s.rows(opts)
	if search {
		filtered, badPattern, err := filterSearchRows(rows, opts.args, opts.namesOnly)
		if err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: invalid search pattern %q: %v\n", badPattern, err)
			return exitRuntime
		}
		rows = filtered
	} else {
		filtered, notices, badPattern, err := filterGlobRows(rows, opts.args)
		if err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: invalid glob %q: %v\n", badPattern, err)
			return exitRuntime
		}
		for _, notice := range notices {
			fmt.Fprintf(st.stderr, "Notice: '%s' was not found\n", notice)
		}
		rows = filtered
	}
	return st.renderDiscoveryRows(rows, opts, s)
}

func (s *snapshot) namesNeedingResolution(opts *options) []string {
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

func (s *snapshot) rows(opts *options) []discovery.Row {
	if opts.virtual {
		return s.virtualRows()
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
		if opts.allVersions {
			rows = append(rows, s.variantRows(name, opts)...)
			continue
		}
		if opts.upgradable {
			if plan, ok := s.plan[name]; ok {
				rows = append(rows, s.planRow(plan, opts.upgradable))
			}
			continue
		}
		rows = append(rows, s.defaultRow(name))
	}
	return rows
}

func (s *snapshot) defaultRow(name string) discovery.Row {
	if plan, ok := s.plan[name]; ok && s.planOK {
		return s.planRow(plan, false)
	}
	if info, ok := s.installed[name]; ok {
		return s.installedRow(name, info)
	}
	variants := s.repo[name]
	if len(variants) == 0 {
		return discovery.Row{Name: name}
	}
	if len(variants) > 1 {
		if pkgver := s.resolved[name]; pkgver != "" {
			for _, variant := range variants {
				if variant.Pkgver == pkgver {
					return variant
				}
			}
		}
	}
	return variants[0]
}

func (s *snapshot) installedRow(name string, info installedInfo) discovery.Row {
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

func (s *snapshot) attachInstalled(row discovery.Row, info installedInfo) discovery.Row {
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

func (s *snapshot) variantRows(name string, opts *options) []discovery.Row {
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
			variant.InstalledVersion = info.Version
			if info.Version != "" {
				variant.UpgradableFrom = info.Version
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

func (s *snapshot) planRow(plan xbps.Row, withSizes bool) discovery.Row {
	name, version := discovery.SplitPkgverVersion(plan.Pkgver())
	upstream, revision := discovery.SplitRevision(version)
	row := discovery.Row{
		Name: name, Pkgver: plan.Pkgver(), Version: upstream, Revision: revision,
		Architecture: plan.Arch, Repository: plan.Repo,
		ShortDesc: s.shortDesc[plan.Pkgver()],
	}
	if info, ok := s.installed[name]; ok {
		row.InstalledVersion = info.Version
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

func (s *snapshot) virtualRows() []discovery.Row {
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
		for provider := range providers[name] {
			list = append(list, provider)
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

func (st *state) renderDiscoveryRows(rows []discovery.Row, opts *options, s *snapshot) int {
	for _, row := range rows {
		if opts.machine {
			if err := st.writeMachineRow(row, nil); err != nil {
				fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
				return exitRuntime
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
	s, code, ok := st.loadSnapshot(discoveryNeeds{orphans: true})
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

		var perName []block
		installedPrinted := false
		if found {
			row := s.recordRow(installedRec, true)
			perName = append(perName, block{row: row, props: installedRec})
			installedPrinted = true
		}
		if !found && len(variants) == 0 {
			missing = append(missing, name)
			continue
		}

		if opts.allVersions {
			for _, variant := range variants {
				rec, variantFound, code := st.record("-R", "-S", variant.Pkgver)
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
			seen := map[string]bool{}
			for _, variant := range variants {
				if variant.Pkgver == row.Pkgver || seen[variant.Pkgver] {
					continue
				}
				seen[variant.Pkgver] = true
				additional++
			}
		} else {
			printedPkgver := ""
			if len(perName) > 0 {
				printedPkgver = perName[0].row.Pkgver
			}
			seen := map[string]bool{}
			for _, variant := range variants {
				if variant.Pkgver == printedPkgver || seen[variant.Pkgver] {
					continue
				}
				seen[variant.Pkgver] = true
				additional++
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
				fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
				return exitRuntime
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
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
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

func (s *snapshot) recordRow(rec *xbpsRecord, installed bool) discovery.Row {
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

// xbpsRecord is one `xbps-query -S` answer: scalar properties plus the
// tab-indented array properties.
type xbpsRecord struct {
	scalar     map[string]string
	list       map[string][]string
	properties map[string]any
}

func parseRecord(stdout string) *xbpsRecord {
	rec := &xbpsRecord{scalar: map[string]string{}, list: map[string][]string{}, properties: map[string]any{}}
	var current string
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ") {
			if current != "" {
				value := strings.TrimSpace(line)
				if value != "" {
					rec.list[current] = append(rec.list[current], value)
					rec.properties[current] = rec.list[current]
				}
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || key == "" {
			current = ""
			continue
		}
		current = key
		value = strings.TrimSpace(value)
		if value == "" {
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
		field{"Alternatives", rec.list["alternatives"]},
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
		if len(f.values) == 0 {
			continue
		}
		if len(f.values) <= 3 {
			fmt.Fprintf(out, "%s: %s\n", f.label, strings.Join(f.values, ", "))
			continue
		}
		fmt.Fprintf(out, "%s: %s\n", f.label, f.values[0])
		for _, value := range f.values[1:] {
			fmt.Fprintf(out, "    %s\n", value)
		}
	}
}
