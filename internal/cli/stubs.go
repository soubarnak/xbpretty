package cli

import (
	"fmt"
	"io"
)

// runMoo implements the hidden easter egg (FR-002, FR-075). It never loads
// config and never runs a child.
func runMoo(out io.Writer, opts *options) int {
	fmt.Fprintln(out, `        (__)`)
	fmt.Fprintln(out, `        (oo)`)
	fmt.Fprintln(out, `  /-----\/ `)
	fmt.Fprintln(out, ` / |    ||  `)
	fmt.Fprintln(out, `*  /\---/\ `)
	fmt.Fprintln(out, `   ~~   ~~`)
	switch {
	case opts.help:
		fmt.Fprintln(out, `moo: there is no help; the cow is the help.`)
	case opts.explicit["update"]:
		fmt.Fprintln(out, `moo: the cow is already up to date.`)
	case opts.explicit["no-update"]:
		fmt.Fprintln(out, `moo: the cow refuses to update; it is content.`)
	default:
		fmt.Fprintln(out, `..."Have you mooed today?"...`)
	}
	return exitOK
}
