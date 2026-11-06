package cli

import (
	"fmt"
	"io"
)

// diagf writes one stderr diagnostic with its leading prefix themed. Colour is
// resolved per stream (st.colorErr), so --color=always themes a piped stderr
// and Auto leaves the prefix plain when stderr is redirected (FR-057/FR-061).
func (st *state) diagf(role progressRole, prefix, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	_, _ = io.WriteString(st.stderr, paintRole(st.theme, st.colorErr, role, prefix)+msg)
}

// errorf writes xbpretty's Error-role diagnostic line.
func (st *state) errorf(format string, args ...any) {
	st.diagf(roleError, "xbpretty: ", format, args...)
}

// warnf writes xbpretty's Warning-role diagnostic line.
func (st *state) warnf(format string, args ...any) {
	st.diagf(roleWarning, "xbpretty: warning: ", format, args...)
}

// hintf writes a Notice-role hint line.
func (st *state) hintf(format string, args ...any) {
	st.diagf(roleNotice, "hint: ", format, args...)
}
