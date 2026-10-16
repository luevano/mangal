package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func successf(cmd *cobra.Command, format string, a ...any) {
	fmt.Fprintf(styled(cmd.OutOrStderr()), "%s %s\n", icon.Check.Colored(), fmt.Sprintf(format, a...))
}

// styled is w for styled text (theme styles, colored icons). lipgloss v2 renders full
// colors: the writer brings them down to what w shows, none when it's not a terminal.
func styled(w io.Writer) io.Writer {
	return colorprofile.NewWriter(w, os.Environ())
}

var detectBackground sync.Once

// styledAdaptive is styled for text with colors that depend on the background
// (color.LightDark): a terminal's is asked once. Get it before rendering.
func styledAdaptive(w io.Writer) io.Writer {
	if f, ok := w.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		detectBackground.Do(func() {
			color.SetDark(lipgloss.HasDarkBackground(os.Stdin, f))
		})
	}
	return styled(w)
}

// completionProviderIDs completes a provider id (a flag's value).
func completionProviderIDs(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	infos, err := core.NewProviders(nil).List(context.Background())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	IDs := lo.Map(infos, func(info libmangal.ProviderInfo, _ int) string {
		return info.ID
	})

	return IDs, cobra.ShellCompDirectiveNoFileComp
}

// completionFirstProvider completes the first argument with a provider id, nothing after
// it (manga ids, queries).
func completionFirstProvider(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completionProviderIDs(cmd, args, toComplete)
}

// completionConfigKeys completes the first argument with a config key.
func completionConfigKeys(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	keys := config.Keys()

	filtered := lo.Filter(keys, func(key string, _ int) bool {
		return strings.HasPrefix(key, toComplete)
	})

	return filtered, cobra.ShellCompDirectiveNoFileComp
}
