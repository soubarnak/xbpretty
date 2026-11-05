package cli

import (
	"testing"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/summary"
)

// TestSGRPrefix pins nala's Style::ansi_prefix bytes: modifiers, then fg,
// then an optional bg; an unset modifier list emits 0.
func TestSGRPrefix(t *testing.T) {
	tests := []struct {
		name  string
		style config.Style
		want  string
	}{
		{
			name:  "no modifier emits zero then foreground",
			style: config.Style{Fg: config.Color{Kind: config.ColorReset}},
			want:  "\x1b[0;39m",
		},
		{
			name:  "bold named foreground",
			style: config.Style{Fg: config.Color{Kind: config.ColorLightBlue}, Modifier: config.ModifierBold},
			want:  "\x1b[1;94m",
		},
		{
			name: "background is the third parameter",
			style: config.Style{
				Fg: config.Color{Kind: config.ColorRed},
				Bg: config.Color{Kind: config.ColorBlue}, HasBg: true,
				Modifier: config.ModifierBold,
			},
			want: "\x1b[1;31;44m",
		},
		{
			name:  "indexed foreground",
			style: config.Style{Fg: config.Color{Kind: config.ColorIndexed, Index: 202}},
			want:  "\x1b[0;38;5;202m",
		},
		{
			name:  "RGB foreground",
			style: config.Style{Fg: config.Color{Kind: config.ColorRGB, R: 1, G: 2, B: 3}},
			want:  "\x1b[0;38;2;1;2;3m",
		},
		{
			name: "indexed background",
			style: config.Style{
				Fg: config.Color{Kind: config.ColorReset},
				Bg: config.Color{Kind: config.ColorIndexed, Index: 17}, HasBg: true,
			},
			want: "\x1b[0;39;48;5;17m",
		},
		{
			name: "multiple modifiers keep nala's bit order",
			style: config.Style{
				Fg:       config.Color{Kind: config.ColorGray},
				Modifier: config.ModifierBold | config.ModifierItalic | config.ModifierUnderlined,
			},
			want: "\x1b[1;3;4;37m",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sgrPrefix(tt.style); got != tt.want {
				t.Errorf("sgrPrefix(%+v) = %q, want %q", tt.style, got, tt.want)
			}
		})
	}
}

// TestDefaultThemeConvertsByteForByte proves the resolved default palette maps
// onto nala's exact inline SGR prefixes.
func TestDefaultThemeConvertsByteForByte(t *testing.T) {
	if got, want := summaryTheme(config.DefaultTheme()), summary.DefaultTheme(); got != want {
		t.Errorf("summaryTheme(DefaultTheme()) = %+v, want %+v", got, want)
	}
}
