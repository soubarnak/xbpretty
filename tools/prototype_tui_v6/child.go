package main

import (
	"fmt"
	"os"
	"time"
)

// runFakeChild emits the telemetry stream the inline viewport consumes. It is
// a separate process with its own pipe, which is the prototype's stand-in for
// nala's dpkg pty child: the parent reads it and never lets raw child bytes
// corrupt the box.
func runFakeChild() {
	pkgs := []string{
		"libfoo-1.3_1.x86_64.xbps",
		"libbar-0.9_3.x86_64.xbps",
		"libbaz-2.0_2.x86_64.xbps",
		"glibc-2.41_1.x86_64.xbps",
		"figlet-2.2.5_6.x86_64.xbps",
		"cowsay-3.8.4_1.x86_64.xbps",
	}
	total := len(pkgs)
	emit := func(s string) {
		fmt.Fprintln(os.Stdout, s)
		time.Sleep(30 * time.Millisecond)
	}
	for i, p := range pkgs {
		emit(fmt.Sprintf("PROGRESS %s %d %d %d", p, i, total, i*5+5))
		if i == 1 {
			emit("ALT enter")
			time.Sleep(120 * time.Millisecond)
			emit("ALT leave")
		}
		emit("DONE " + p)
	}
}
