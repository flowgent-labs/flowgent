package main

import consolepkg "github.com/flowgent-labs/flowgent/console/pkg"

// StartConsole runs the interactive management console REPL.
func StartConsole(cfgPath string, args []string, verbose bool) {
	consolepkg.RunREPL(cfgPath, args, verbose)
}
