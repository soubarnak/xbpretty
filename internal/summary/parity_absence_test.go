package summary

import (
	"bytes"
	"strings"
	"testing"
)

// renderParityAbsence renders the shared golden summary in the plain,
// 80-column frame. It is the seam the explicit-absence rows assert against:
// the renderer output is byte-pinned by TestRendererGoldens, so a literal that
// never appears here never appears in a real plan either.
func renderParityAbsence(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	r := Renderer{Color: false, Width: 80, Unit: SizeBinary, Theme: DefaultTheme()}
	if err := r.Render(&b, goldenSummary()); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// parityAbsentDowngrade proves P-VIS-028: the Downgrade section never renders;
// a downgrade is planned as Install or Upgrade instead.
func parityAbsentDowngrade(t *testing.T) {
	out := renderParityAbsence(t)
	if strings.Contains(out, "Downgrade") {
		t.Errorf("the Downgrade section rendered:\n%s", out)
	}
	if !strings.Contains(out, "Upgrade") || !strings.Contains(out, "Install") {
		t.Errorf("the replacement Install/Upgrade sections are missing:\n%s", out)
	}
}

// parityAbsentExcluded proves P-VIS-029: Excluded, Kept back and Phased never
// render; Manual hold is the only reason xbps can carry.
func parityAbsentExcluded(t *testing.T) {
	out := renderParityAbsence(t)
	for _, literal := range []string{"Excluded", "Kept back", "Phased"} {
		if strings.Contains(out, literal) {
			t.Errorf("%q rendered:\n%s", literal, out)
		}
	}
	if !strings.Contains(out, "Manual hold") {
		t.Errorf("the replacement Manual hold reason is missing:\n%s", out)
	}
}

// parityAbsentEssentialWarning proves P-VIS-034: an APT essential/protected
// removal warning never renders; xbps refuses through its dependency
// resolver, so the remove summary stays a plain table.
func parityAbsentEssentialWarning(t *testing.T) {
	out := renderParityAbsence(t)
	for _, literal := range []string{"essential", "protected", "APT"} {
		if strings.Contains(out, literal) {
			t.Errorf("%q rendered:\n%s", literal, out)
		}
	}
	if !strings.Contains(out, "Remove") {
		t.Errorf("the Remove section is missing:\n%s", out)
	}
}
