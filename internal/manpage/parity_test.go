package manpage

import "testing"

// parityManpageFreshness proves P-PKG-008: the committed pages are current and
// pass mandoc lint when the check-time tool is available.
func parityManpageFreshness(t *testing.T) {
	TestGeneratedManpagesAreCurrent(t)
	TestManpagesPassMandocLint(t)
}
