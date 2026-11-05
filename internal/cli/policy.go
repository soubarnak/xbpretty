package cli

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/soubarnak/xbpretty/internal/discovery"
)

// repositoryLine matches one `xbps-query -L` / `-L -v` entry. The leading
// number is the repository's package count; the precedence is its position in
// this list.
var repositoryLine = regexp.MustCompile(`^\s*[0-9]+\s+(\S+)\s+\(RSA (signed|unsigned)\)\s*$`)

// runPolicy implements FR-026: the global repository/pin view without names
// and the package resolution view with them.
func (st *state) runPolicy(opts *options) int {
	if len(opts.args) > 0 {
		return st.runPolicyPackages(opts)
	}
	return st.runPolicyGlobal(opts)
}

// parsePolicyRepositories reads the plain or verbose repository list. A
// verbose signed entry carries `Signed-by:` and the key length plus
// fingerprint on the two indented lines after it.
func parsePolicyRepositories(stdout string) []discovery.PolicyFile {
	var files []discovery.PolicyFile
	for _, line := range strings.Split(stdout, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if match := repositoryLine.FindStringSubmatch(trimmed); match != nil {
			files = append(files, discovery.PolicyFile{
				Precedence: len(files) + 1,
				Repository: match[1],
				Signed:     match[2] == "signed",
			})
			continue
		}
		if len(files) == 0 {
			continue
		}
		current := &files[len(files)-1]
		switch {
		case strings.HasPrefix(trimmed, "Signed-by:"):
			current.SignedBy = strings.TrimSpace(strings.TrimPrefix(trimmed, "Signed-by:"))
		case current.Signed && current.Fingerprint == "":
			current.Fingerprint = trimmed
		}
	}
	return files
}

// loadPolicyRepositories reads the repository list. -L -v carries the
// signature details policy prints; when it produced no rows (a stub or an
// xbps build without the verbose answer) the plain -L list is the fallback.
// Both are in FR-022's fixed offline set.
func (st *state) loadPolicyRepositories() ([]discovery.PolicyFile, int, bool) {
	res, ok := st.runQuery("-L", "-v")
	if !ok {
		return nil, res.exitCode, false
	}
	if files := parsePolicyRepositories(res.stdout); len(files) > 0 {
		return files, -1, true
	}
	plain, ok := st.runQuery("-L")
	if !ok {
		return nil, plain.exitCode, false
	}
	return parsePolicyRepositories(plain.stdout), -1, true
}

// parsePolicyPins turns one hold or repolock list into name → pkgver.
func parsePolicyPins(stdout string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(stdout, "\n") {
		pkgver := strings.TrimSpace(line)
		if name, _ := discovery.SplitPkgverVersion(pkgver); name != "" {
			out[name] = pkgver
		}
	}
	return out
}

// policyPinned merges the hold and repolock maps into the sorted pin list.
// A package carrying both bits prints once per reason, because the two are
// separate xbps states.
func policyPinned(holds, repolocked map[string]string) []discovery.PinnedPackage {
	var out []discovery.PinnedPackage
	add := func(pins map[string]string, reason string) {
		for name, pkgver := range pins {
			_, version := discovery.SplitPkgverVersion(pkgver)
			out = append(out, discovery.PinnedPackage{Name: name, Version: version, Reason: reason})
		}
	}
	add(holds, "hold")
	add(repolocked, "repolock")
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

// runPolicyGlobal prints FR-026's no-name view. It reads only -L -v, -H and
// the repolock list: no package database pass, no repository scan.
func (st *state) runPolicyGlobal(opts *options) int {
	files, code, ok := st.loadPolicyRepositories()
	if !ok {
		return code
	}
	holdRes, ok := st.runQuery("-H")
	if !ok {
		return holdRes.exitCode
	}
	repolockRes, ok := st.runQuery("--list-repolock-pkgs")
	if !ok {
		return repolockRes.exitCode
	}

	pinned := policyPinned(parsePolicyPins(holdRes.stdout), parsePolicyPins(repolockRes.stdout))

	if opts.machine {
		object := discovery.MachineGlobalPolicy{
			Schema:         discovery.SchemaVersion,
			PackageFiles:   make([]discovery.MachinePolicyFile, 0, len(files)),
			PinnedPackages: make([]discovery.MachinePinnedPackage, 0, len(pinned)),
		}
		for _, file := range files {
			object.PackageFiles = append(object.PackageFiles, discovery.MachinePolicyFile{
				Precedence:  file.Precedence,
				Repository:  file.Repository,
				Signed:      file.Signed,
				SignedBy:    file.SignedBy,
				Fingerprint: file.Fingerprint,
			})
		}
		for _, pin := range pinned {
			object.PinnedPackages = append(object.PinnedPackages, discovery.MachinePinnedPackage{
				Name: pin.Name, Version: pin.Version, Reason: pin.Reason,
			})
		}
		if err := discovery.WriteNDJSON(st.stdout, object); err != nil {
			// FR-074: stop writing on EPIPE and keep the successful exit.
			return exitOK
		}
		return exitOK
	}

	fmt.Fprintln(st.stdout, "Package files:")
	for _, file := range files {
		signature := "unsigned"
		if file.Signed {
			signature = "signed"
		}
		fmt.Fprintf(st.stdout, " %d %s (RSA %s)\n", file.Precedence, file.Repository, signature)
		if !file.Signed {
			continue
		}
		if file.SignedBy != "" {
			fmt.Fprintf(st.stdout, "     Signed-by: %s\n", file.SignedBy)
		}
		if file.Fingerprint != "" {
			fmt.Fprintf(st.stdout, "     %s\n", file.Fingerprint)
		}
	}
	fmt.Fprintln(st.stdout, "Pinned packages:")
	for _, pin := range pinned {
		fmt.Fprintf(st.stdout, "   %s -> %s (%s)\n", pin.Name, pin.Version, pin.Reason)
	}
	return exitOK
}

// policyPrecedence maps a repository URL to its 1-based -L precedence. When
// the repository list did not run or omitted a row, the architecture pass
// order the row model already carries is the fallback.
func policyPrecedence(files []discovery.PolicyFile, s *snapshot) map[string]int {
	out := make(map[string]int, len(files)+len(s.repoRank))
	for _, file := range files {
		out[file.Repository] = file.Precedence
	}
	fallback := len(files)
	for repository, rank := range s.repoRank {
		if _, ok := out[repository]; !ok {
			out[repository] = fallback + rank + 1
		}
	}
	return out
}

// policyVersionEntry builds one version-table row for a variant. A version
// is a variant in this view: the same version in two repositories stays two
// rows, each with its own repository line and precedence.
func policyVersionEntry(variant discovery.Row, precedence int, installed, candidate *string, candidateRepository string) discovery.MachinePolicyVersion {
	version := variant.FullVersion()
	return discovery.MachinePolicyVersion{
		Version:    version,
		Precedence: precedence,
		Installed:  installed != nil && *installed == version,
		Candidate:  candidate != nil && *candidate == version && (candidateRepository == "" || candidateRepository == variant.Repository),
		Repositories: []discovery.MachinePolicyRepository{{
			Precedence: precedence, Repository: variant.Repository,
		}},
	}
}

// runPolicyPackages implements FR-026's with-names view and FR-027's array.
func (st *state) runPolicyPackages(opts *options) int {
	s, code, ok := st.loadSnapshot(discoveryNeeds{})
	if !ok {
		return code
	}
	repoFiles, code, ok := st.loadPolicyRepositories()
	if !ok {
		return code
	}
	precedence := policyPrecedence(repoFiles, s)

	views := make([]discovery.MachinePackagePolicy, 0, len(opts.args))
	var missing []string
	for _, name := range opts.args {
		installedRec, installedFound, code := st.record("-S", name)
		if code >= 0 {
			return code
		}
		candidateRec, candidateFound, code := st.record("-R", "-S", name)
		if code >= 0 {
			return code
		}
		variants := s.repo[name]
		if !installedFound && !candidateFound && len(variants) == 0 {
			missing = append(missing, name)
			continue
		}

		var installedVersion, candidateVersion, candidateRepository *string
		if installedFound {
			value := recordVersion(installedRec)
			installedVersion = &value
		}
		if candidateFound {
			value := recordVersion(candidateRec)
			candidateVersion = &value
			repository := candidateRec.scalar["repository"]
			candidateRepository = &repository
		}

		// The version table follows -L precedence, which need not be the
		// architecture pass's first-seen order.
		variants = append([]discovery.Row(nil), variants...)
		sort.SliceStable(variants, func(i, j int) bool {
			return precedence[variants[i].Repository] < precedence[variants[j].Repository]
		})

		view := discovery.MachinePackagePolicy{
			Schema:    discovery.SchemaVersion,
			Package:   name,
			Installed: installedVersion,
			Candidate: candidateVersion,
			Versions:  make([]discovery.MachinePolicyVersion, 0, len(variants)),
		}
		for _, variant := range variants {
			view.Versions = append(view.Versions, policyVersionEntry(
				variant, precedence[variant.Repository], installedVersion, candidateVersion, deref(candidateRepository),
			))
		}
		views = append(views, view)
	}

	if opts.machine {
		if err := discovery.WriteNDJSON(st.stdout, views); err != nil {
			// FR-074: stop writing on EPIPE and keep the successful exit.
			return exitOK
		}
	} else {
		for i, view := range views {
			if i > 0 {
				fmt.Fprintln(st.stdout)
			}
			printPolicyPackage(st.stdout, view)
		}
	}
	for _, name := range missing {
		fmt.Fprintf(st.stderr, "Notice: '%s' was not found\n", name)
	}
	return exitOK
}

// recordVersion is the version part of a record's pkgver.
func recordVersion(rec *xbpsRecord) string {
	_, version := discovery.SplitPkgverVersion(rec.scalar["pkgver"])
	return version
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// printPolicyPackage writes one FR-026 package block.
func printPolicyPackage(out interface{ Write([]byte) (int, error) }, view discovery.MachinePackagePolicy) {
	fmt.Fprintf(out, "%s:\n", view.Package)
	fmt.Fprintf(out, "  Installed: %s\n", policyVersionText(view.Installed))
	fmt.Fprintf(out, "  Candidate: %s\n", policyVersionText(view.Candidate))
	fmt.Fprintln(out, "  Version table:")
	for _, version := range view.Versions {
		marker := "   "
		if version.Candidate {
			marker = "***"
		}
		fmt.Fprintf(out, "  %s %s %d\n", marker, version.Version, version.Precedence)
		for _, repository := range version.Repositories {
			fmt.Fprintf(out, "      %d %s\n", repository.Precedence, repository.Repository)
		}
	}
}

func policyVersionText(version *string) string {
	if version == nil {
		return "none"
	}
	return *version
}
