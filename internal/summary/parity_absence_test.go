package summary

import (
	"bytes"
	"strings"
	"testing"
)

// renderParityRows renders rows in the plain, 80-column frame. It is the seam
// the explicit-absence rows assert against: the renderer is byte-pinned by
// TestRendererGoldens, and these cases construct the inputs apt would have
// turned into a banned label.
func renderParityRows(t *testing.T, rows []Row) string {
	t.Helper()
	var b bytes.Buffer
	r := Renderer{Color: false, Width: 80, Unit: SizeBinary, Theme: DefaultTheme()}
	if err := r.Render(&b, Build(rows, nil, nil)); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// parityAbsentDowngrade proves P-VIS-028: a lower version is a normal install
// or update, so the Downgrade section never renders.
func parityAbsentDowngrade(t *testing.T) {
	out := renderParityRows(t, []Row{
		{Name: "older", Version: "1.0_1", Action: actionInstall, DownloadSize: 1024, HasDownload: true},
		{Name: "older-update", Version: "1.0_1", Action: actionUpdate, DownloadSize: 1024, HasDownload: true},
	})
	if strings.Contains(out, "Downgrade") {
		t.Errorf("the Downgrade section rendered:\n%s", out)
	}
	for _, want := range []string{"Install", "Upgrade", "older", "older-update"} {
		if !strings.Contains(out, want) {
			t.Errorf("the replacement %q section is missing:\n%s", want, out)
		}
	}
}

// parityAbsentExcluded proves P-VIS-029: apt's Excluded, Kept back and Phased
// reasons are coerced to the only reason xbps can carry, Manual hold.
func parityAbsentExcluded(t *testing.T) {
	var rows []Row
	for _, reason := range []string{"Excluded", "Kept back", "Phased"} {
		rows = append(rows, Row{Name: "held-" + strings.ToLower(strings.ReplaceAll(reason, " ", "-")), Version: "1.0_1", Action: actionHold, Reason: reason})
	}
	out := renderParityRows(t, rows)
	for _, literal := range []string{"Excluded", "Kept back", "Phased"} {
		if strings.Contains(out, literal) {
			t.Errorf("%q rendered:\n%s", literal, out)
		}
	}
	if got := strings.Count(out, "Manual hold"); got != len(rows) {
		t.Errorf("Manual hold appeared %d times, want %d:\n%s", got, len(rows), out)
	}
}

// parityAbsentEssentialWarning proves P-VIS-034: an APT essential/protected
// removal warning never renders; xbps refuses through its dependency resolver.
// The fixture deliberately removes system-looking packages to give the warning
// every chance to appear.
func parityAbsentEssentialWarning(t *testing.T) {
	out := renderParityRows(t, []Row{
		{Name: "base-system", Version: "0.1_1", Action: actionRemove, InstalledSize: 1024, HasInstalled: true},
		{Name: "dracut", Version: "1.0_1", Action: actionRemove, InstalledSize: 1024, HasInstalled: true},
	})
	for _, literal := range []string{"essential", "protected", "APT"} {
		if strings.Contains(out, literal) {
			t.Errorf("%q rendered:\n%s", literal, out)
		}
	}
	if !strings.Contains(out, "Remove") || !strings.Contains(out, "base-system") {
		t.Errorf("the Remove section is missing:\n%s", out)
	}
}
