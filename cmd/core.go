package cmd

import (
	"context"
	"fmt"
	"strconv"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/core"
	"github.com/spf13/cobra"
)

// newCore is the app's services (core.Default). Swapped in tests.
var newCore = core.Default

// changeCtx is the command's ctx with the progress of changes and libmangal's statuses on
// the status line.
func changeCtx(cmd *cobra.Command, st *status) context.Context {
	return core.WithProgress(st.Context(ctxOf(cmd)), st.Progress)
}

// ctxOf is the command's ctx, a background one when it's run without (tests).
func ctxOf(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// applyOpts tell the status line while a change waits for the library's lock.
func applyOpts(st *status) core.ApplyOpts {
	return core.ApplyOpts{OnWait: func(b client.LibraryBusy) { st.Set(b.Message()) }}
}

// printLines prints the lines on stdout.
func printLines(cmd *cobra.Command, lines []string) {
	for _, line := range lines {
		cmd.Println(line)
	}
}

// printResultLines prints what an apply did: warnings on stderr.
func printResultLines(cmd *cobra.Command, lines []core.Line) {
	for _, l := range lines {
		if l.Warn {
			cmd.PrintErrln(l.Text)
		} else {
			cmd.Println(l.Text)
		}
	}
}

// parseMangaID is a library manga id argument.
func parseMangaID(arg string) (int64, error) {
	id, err := strconv.ParseInt(arg, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid manga id %q", arg)
	}
	return id, nil
}
