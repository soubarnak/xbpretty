package main

import (
	"os"

	"github.com/soubarnak/xbpretty/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args))
}
