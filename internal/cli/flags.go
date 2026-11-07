package cli

import (
	"flag"
	"io"
	"strings"
)

// newFlagSet returns a silent FlagSet: errors and help are reported by the
// commands, styled, instead of by the flag package.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// stringFlag registers a string flag under every comma-separated name, so
// "-q" and "--quality" set the same value.
func stringFlag(fs *flag.FlagSet, p *string, names string) {
	for n := range strings.SplitSeq(names, ",") {
		fs.StringVar(p, n, "", "")
	}
}

// boolFlag is stringFlag for booleans.
func boolFlag(fs *flag.FlagSet, p *bool, names string) {
	for n := range strings.SplitSeq(names, ",") {
		fs.BoolVar(p, n, false, "")
	}
}

// parseInterspersed parses flags in any position. The standard flag package
// stops at the first non-flag argument, so "dotdl video URL -o dir" would
// treat "-o" as a URL. After "--", everything is positional, by convention.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		// Parse consumes "--" without returning it: if it was the last
		// argument consumed, the rest is all positional.
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}
