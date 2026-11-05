package main

import (
	"fmt"
	"strconv"
	"strings"
)

// PlainLine is the data behind the 40-cell plain progress line (#45 §7.1).
type PlainLine struct {
	Ratio       float64
	Message     string
	Position    uint64
	Total       uint64
	Rate        uint64
	HasTransfer bool

	Item    string
	HasItem bool

	ItemsDone, ItemsTotal int
	HasItems              bool

	MirrorsDone, MirrorsTotal int
	HasMirrors                bool
}

// renderPlainLine reproduces nala's PlainProgress grammar byte-for-byte:
//
//	"[" + "="×filled + " "×(40−filled) + "]" + " " + %3d + "% " + message
//	    [+ " " + position + "/" + total + " " + rate + "/s"]
//	    [+ " | Package: " + item] [+ " | Packages: cur/tot"]
//	    [+ " | Mirrors: cur/tot"]
//
// The bar is fixed at 40 cells and filled rounds (#45 §7.1).
func renderPlainLine(p PlainLine) string {
	const width = 40
	filled := fillCount(width, p.Ratio)
	var b strings.Builder
	b.WriteByte('[')
	b.WriteString(repeat("=", filled))
	b.WriteString(repeat(" ", width-filled))
	b.WriteString("] ")
	b.WriteString(fmt.Sprintf("%3d", percent(p.Ratio)))
	b.WriteString("% ")
	b.WriteString(p.Message)
	if p.HasTransfer {
		b.WriteString(" " + widgetSize(p.Position) + "/" + widgetSize(p.Total))
		b.WriteString(" " + widgetSize(p.Rate) + "/s")
	}
	if p.HasItem {
		b.WriteString(" | Package: " + p.Item)
	}
	if p.HasItems {
		b.WriteString(" | Packages: " + strconv.Itoa(p.ItemsDone) + "/" + strconv.Itoa(p.ItemsTotal))
	}
	if p.HasMirrors {
		b.WriteString(" | Mirrors: " + strconv.Itoa(p.MirrorsDone) + "/" + strconv.Itoa(p.MirrorsTotal))
	}
	return b.String()
}
