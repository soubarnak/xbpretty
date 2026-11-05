// Package main is a throwaway prototype of xbpretty's rev 6 inline
// rendering: nala-faithful borderless tables, ASCII '=' rules, the nine theme
// roles and the rounded progress widget fed by xbps telemetry.
//
// It exists to answer issue #51's question ("how should nala-faithful inline
// rendering look and behave on real xbps data?"). It is not production code:
// no config, no CLI wiring, no tests.
package main

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Role is one of nala v0.18.2's nine theme roles (#45 §2.1).
type Role int

const (
	RolePrimary Role = iota
	RoleSecondary
	RoleHighlight
	RoleRegular
	RoleProgressFilled
	RoleProgressUnfilled
	RoleNotice
	RoleWarning
	RoleError
)

// ansiPrefix is the exact SGR prefix nala emits for the role under the
// default theme (#45 §2.1, src/config/color.rs:683-689).
var ansiPrefix = map[Role]string{
	RolePrimary:          "\x1b[1;92m",
	RoleSecondary:        "\x1b[1;94m",
	RoleHighlight:        "\x1b[1;39m",
	RoleRegular:          "\x1b[0;39m",
	RoleProgressFilled:   "\x1b[1;92m",
	RoleProgressUnfilled: "\x1b[1;91m",
	RoleNotice:           "\x1b[1;93m",
	RoleWarning:          "\x1b[1;93m",
	RoleError:            "\x1b[1;91m",
}

const ansiReset = "\x1b[0m"

// Theme paints spans. With color off it returns the string untouched, so a
// --no-color run emits no ANSI bytes at all (#45 §2.3, D5).
type Theme struct {
	Color bool
}

// Paint wraps s in the role's SGR prefix and a reset (#45 §2.1: every painted
// span resets at its end).
func (t Theme) Paint(r Role, s string) string {
	if !t.Color || s == "" {
		return s
	}
	return ansiPrefix[r] + s + ansiReset
}

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes SGR sequences so widths are measured on visible text
// only (#45 §3.2: ANSI-aware measurement).
func stripANSI(s string) string { return ansiRegexp.ReplaceAllString(s, "") }

// visibleWidth is the display width of s in terminal columns.
func visibleWidth(s string) int { return utf8.RuneCountInString(stripANSI(s)) }

func padRight(s string, w int) string {
	if n := visibleWidth(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func padLeft(s string, w int) string {
	if n := visibleWidth(s); n < w {
		return strings.Repeat(" ", w-n) + s
	}
	return s
}

// clip truncates s to w visible columns, never splitting an ANSI sequence
// (labels are clipped, never wrapped — #45 §6.2).
func clip(s string, w int) string {
	if visibleWidth(s) <= w {
		return s
	}
	var b strings.Builder
	width, i := 0, 0
	raw := []byte(s)
	for i < len(raw) && width < w {
		if raw[i] == 0x1b && i+1 < len(raw) && raw[i+1] == '[' {
			j := i + 2
			for j < len(raw) && !((raw[j] >= 'a' && raw[j] <= 'z') || (raw[j] >= 'A' && raw[j] <= 'Z')) {
				j++
			}
			if j < len(raw) {
				j++
			}
			b.Write(raw[i:j])
			i = j
			continue
		}
		r, size := utf8.DecodeRune(raw[i:])
		b.WriteRune(r)
		width++
		i += size
	}
	return b.String()
}
