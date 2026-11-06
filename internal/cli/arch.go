package cli

import (
	"os"
	"strings"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

// xbpsArch is the xbps architecture name fetch probes repodata with (BF-12). It
// resolves, in order: the architecture= line an administrator wrote, the
// fetchArch test hook, $XBPS_ARCH, `xbps-uhelper arch`, and Go's runtime
// mapping. Only xbps knows whether a system is musl, so its answer comes before
// the runtime guess.
func (st *state) xbpsArch(cfg fetch.RepositoryConfig) string {
	if cfg.Arch != "" {
		return cfg.Arch
	}
	if fetchArch != "" {
		return fetchArch
	}
	if arch := strings.TrimSpace(os.Getenv("XBPS_ARCH")); arch != "" {
		return arch
	}
	if arch := st.uhelperArch(); arch != "" {
		return arch
	}
	return runtimeXBPSArch()
}

// uhelperArch asks xbps-uhelper which architecture xbps would use. It returns
// "" when the binary is missing, exits non-zero, or prints anything other than
// one word.
func (st *state) uhelperArch() string {
	if st.r == nil {
		return ""
	}
	if err := st.r.cfg.ResolveFor("uhelper"); err != nil {
		return ""
	}
	res, err := st.r.run(st.r.cfg.Xbps.UhelperPath, "arch")
	if err != nil || res.exitCode != 0 {
		return ""
	}
	words := strings.Fields(res.stdout)
	if len(words) != 1 {
		return ""
	}
	return words[0]
}
