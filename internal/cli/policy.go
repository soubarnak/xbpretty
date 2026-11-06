package cli

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/soubarnak/xbpretty/internal/discovery"
)

// repositoryLine matches one `xbps-query -L` / `-L -v` entry, including the
// measured variants policy must preserve: a negative package count, a URI
// containing spaces, and a staged repository. The leading number is the
// repository's package count; precedence is the row's 1-based ordinal.
var repositoryLine = regexp.MustCompile(`^\s*(-?\d+)\s+(.*?)\s+\((?:Staged )?RSA ([^)]+)\)\s*$`)

// policyFingerprintLine is the key-size plus colon-separated hex shape xbps
// prints under a signed repository's Signed-by line.
var policyFingerprintLine = regexp.MustCompile(`^\s*[0-9]+\s+[0-9A-Fa-f]{2}(?::[0-9A-Fa-f]{2})+\s*$`)

// runPolicy implements FR-026: the global repository/pin view without names
// and the package resolution view with them.
func (st *state) runPolicy(opts *options) int {
	if len(opts.args) > 0 {
		return st.runPolicyPackages(opts)
	}
	return st.runPolicyGlobal(opts)
}

// parsePolicyRepositories reads the plain or verbose repository list. Every
// RSA row is kept and numbered by its position. Only `Signed-by:` and
// fingerprint-shaped continuation lines attach to the current row, so an
// unrelated verbose line cannot steal a signature.
func parsePolicyRepositories(stdout string) []discovery.PolicyFile {
	var files []discovery.PolicyFile
	for _, line := range strings.Split(stdout, "\n") {
		if match := repositoryLine.FindStringSubmatch(line); match != nil {
			files = append(files, discovery.PolicyFile{
				Precedence: len(files) + 1,
				Repository: match[2],
				Signature:  strings.TrimSpace(match[3]),
				Signed:     match[3] == "signed",
			})
			continue
		}
		if len(files) == 0 {
			continue
		}
		current := &files[len(files)-1]
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Signed-by:"):
			current.SignedBy = strings.TrimSpace(strings.TrimPrefix(trimmed, "Signed-by:"))
		case policyFingerprintLine.MatchString(line) && current.Fingerprint == "":
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

// runPolicyGlobal prints FR-026's no-name view. It reads only -L/-L -v, -H
// and the repolock list: no package database pass, no repository scan.
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
		if err := discovery.WriteNDJSON(st.stdout, machineGlobalPolicy(files, pinned)); err != nil {
			// FR-074: stop writing on EPIPE and keep the successful exit.
			return exitOK
		}
		return exitOK
	}
	if !writePolicyGlobal(st.stdout, files, pinned) {
		// FR-074: a failed human write stops the view and keeps exit 0.
		return exitOK
	}
	return exitOK
}

func machineGlobalPolicy(files []discovery.PolicyFile, pinned []discovery.PinnedPackage) discovery.MachineGlobalPolicy {
	object := discovery.MachineGlobalPolicy{
		Schema:         discovery.SchemaVersion,
		PackageFiles:   make([]discovery.MachinePolicyFile, 0, len(files)),
		PinnedPackages: make([]discovery.MachinePinnedPackage, 0, len(pinned)),
	}
	for _, file := range files {
		entry := discovery.MachinePolicyFile{
			Precedence:  file.Precedence,
			Repository:  file.Repository,
			Signed:      file.Signed,
			Fingerprint: file.Fingerprint,
		}
		if file.SignedBy != "" {
			signedBy := file.SignedBy
			entry.SignedBy = &signedBy
		}
		object.PackageFiles = append(object.PackageFiles, entry)
	}
	for _, pin := range pinned {
		object.PinnedPackages = append(object.PinnedPackages, discovery.MachinePinnedPackage{
			Name: pin.Name, Version: pin.Version, Reason: pin.Reason,
		})
	}
	return object
}

// policyText writes human policy output and remembers the first failure, so
// a broken pipe stops the remaining lines without changing the exit code.
type policyText struct {
	out    io.Writer
	broken bool
}

func (w *policyText) printf(format string, args ...any) bool {
	if w.broken {
		return false
	}
	if _, err := fmt.Fprintf(w.out, format, args...); err != nil {
		w.broken = true
		return false
	}
	return true
}

func (w *policyText) println(args ...any) bool {
	if w.broken {
		return false
	}
	if _, err := fmt.Fprintln(w.out, args...); err != nil {
		w.broken = true
		return false
	}
	return true
}

// writePolicyGlobal writes the no-name view. It returns false when a write
// failed, which the caller turns into a clean exit 0.
func writePolicyGlobal(out io.Writer, files []discovery.PolicyFile, pinned []discovery.PinnedPackage) bool {
	w := &policyText{out: out}
	if !w.println("Package files:") {
		return false
	}
	for _, file := range files {
		signature := file.Signature
		if signature == "" {
			signature = "unsigned"
			if file.Signed {
				signature = "signed"
			}
		}
		if !w.printf(" %d %s (RSA %s)\n", file.Precedence, file.Repository, signature) {
			return false
		}
		if file.SignedBy != "" && !w.printf("     Signed-by: %s\n", file.SignedBy) {
			return false
		}
		if file.Fingerprint != "" && !w.printf("     %s\n", file.Fingerprint) {
			return false
		}
	}
	if !w.println("Pinned packages:") {
		return false
	}
	for _, pin := range pinned {
		if !w.printf("   %s -> %s (%s)\n", pin.Name, pin.Version, pin.Reason) {
			return false
		}
	}
	return true
}

// policyPrecedence maps a repository URL to its 1-based -L precedence. When
// the repository list did not run or omitted a row, the architecture pass
// order the row model already carries is the fallback.
func policyPrecedence(files []discovery.PolicyFile, s *discoverySnapshot) map[string]int {
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
// rows, each with its own repository line and precedence. Installed marks
// the first-precedence variant whose pkgver is the installed one, mirroring
// how the row model attaches installed state to one variant.
func policyVersionEntry(variant discovery.Row, precedence int, installed bool, candidate *string, candidateRepository string) discovery.MachinePolicyVersion {
	version := variant.FullVersion()
	return discovery.MachinePolicyVersion{
		Version:    version,
		Precedence: precedence,
		Installed:  installed,
		Candidate:  candidate != nil && *candidate == version && (candidateRepository == "" || candidateRepository == variant.Repository),
		Repositories: []discovery.MachinePolicyRepository{{
			Precedence: precedence, Repository: variant.Repository,
		}},
	}
}

// policyRecordName is the canonical package name a -S/-R -S record resolved
// to. xbps accepts globs, pkgspecs and virtual names on those queries, so the
// heading and the variant lookup use this name, not the raw token.
func policyRecordName(rec *xbpsRecord) string {
	if name := strings.TrimSpace(rec.scalar["pkgname"]); name != "" {
		return name
	}
	name, _ := discovery.SplitPkgverVersion(rec.scalar["pkgver"])
	return name
}

// runPolicyPackages implements FR-026's with-names view and FR-027's array.
func (st *state) runPolicyPackages(opts *options) int {
	s, code, ok := st.loadDiscoverySnapshot(discoveryNeeds{})
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
	for _, token := range opts.args {
		installedRec, installedFound, code := st.record("-S", token)
		if code >= 0 {
			return code
		}
		candidateRec, candidateFound, code := st.record("-R", "-S", token)
		if code >= 0 {
			return code
		}

		name := token
		switch {
		case installedFound:
			name = policyRecordName(installedRec)
		case candidateFound:
			name = policyRecordName(candidateRec)
		}
		if name == "" {
			name = token
		}
		// BF-41: a virtual token resolves on the repository side to its
		// provider, so the installed record is asked under the canonical
		// name. Otherwise an installed provider reads as "Installed: none".
		if name != token && !installedFound {
			installedRec, installedFound, code = st.record("-S", name)
			if code >= 0 {
				return code
			}
		}
		variants := s.repo[name]
		if !installedFound && !candidateFound && len(variants) == 0 {
			missing = append(missing, token)
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
		installedMarked := false
		for _, variant := range variants {
			variantInstalled := false
			if !installedMarked && installedVersion != nil && variant.FullVersion() == *installedVersion {
				variantInstalled = true
				installedMarked = true
			}
			view.Versions = append(view.Versions, policyVersionEntry(
				variant, precedence[variant.Repository], variantInstalled, candidateVersion, deref(candidateRepository),
			))
		}
		views = append(views, view)
	}

	if opts.machine {
		if err := discovery.WriteNDJSON(st.stdout, views); err != nil {
			// FR-074: stop writing on EPIPE and keep the successful exit.
			return exitOK
		}
	} else if !writePolicyPackages(st.stdout, views) {
		// FR-074: a failed human write stops the view and keeps exit 0.
		return exitOK
	}
	for _, token := range missing {
		fmt.Fprintf(st.stderr, "Notice: '%s' was not found\n", token)
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

// writePolicyPackages writes the with-names blocks, separating them with one
// blank line. It returns false on the first failed write.
func writePolicyPackages(out io.Writer, views []discovery.MachinePackagePolicy) bool {
	w := &policyText{out: out}
	for i, view := range views {
		if i > 0 && !w.println("") {
			return false
		}
		if !writePolicyPackage(w, view) {
			return false
		}
	}
	return true
}

// writePolicyPackage writes one FR-026 package block.
func writePolicyPackage(w *policyText, view discovery.MachinePackagePolicy) bool {
	if !w.printf("%s:\n", view.Package) {
		return false
	}
	if !w.printf("  Installed: %s\n", policyVersionText(view.Installed)) {
		return false
	}
	if !w.printf("  Candidate: %s\n", policyVersionText(view.Candidate)) {
		return false
	}
	if !w.println("  Version table:") {
		return false
	}
	for _, version := range view.Versions {
		marker := "   "
		if version.Candidate {
			marker = "***"
		}
		if !w.printf("  %s %s %d\n", marker, version.Version, version.Precedence) {
			return false
		}
		for _, repository := range version.Repositories {
			if !w.printf("      %d %s\n", repository.Precedence, repository.Repository) {
				return false
			}
		}
	}
	return true
}

func policyVersionText(version *string) string {
	if version == nil {
		return "none"
	}
	return *version
}
