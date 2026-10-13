package cmd

import (
	"fmt"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/inline"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var inlineArgs = inline.Args{}

// addLoaderFlags adds the provider options flags, they set the config keys when given.
func addLoaderFlags(f *pflag.FlagSet) {
	f.Uint8("parallelism", 0, "Provider parallelism to use (when supported)")
	configKeyFlag(f, "parallelism", config.Providers.Parallelism.Key)
	f.Bool("nsfw", false, "Include NSFW content (when supported)")
	configKeyFlag(f, "nsfw", config.Providers.Filter.NSFW.Key)
	f.String("language", "", "Manga/Chapter language")
	configKeyFlag(f, "language", config.Providers.Filter.Language.Key)
	f.Bool("avoid-duplicate-chapters", false, "No duplicate chapters")
	configKeyFlag(f, "avoid-duplicate-chapters", config.Providers.Filter.AvoidDuplicateChapters.Key)
	f.Bool("show-unavailable-chapters", false, "Show undownloadable chapters")
	configKeyFlag(f, "show-unavailable-chapters", config.Providers.Filter.ShowUnavailableChapters.Key)
	f.Bool("headless-use-flaresolverr", false, "Use Flaresolverr for headless providers")
	configKeyFlag(f, "headless-use-flaresolverr", config.Providers.Headless.UseFlaresolverr.Key)
	f.String("headless-flaresolverr-url", "", "Flaresolverr service URL")
	configKeyFlag(f, "headless-flaresolverr-url", config.Providers.Headless.FlaresolverrURL.Key)
}

func init() {
	rootCmd.AddCommand(inlineCmd)
	// To shorten the statements a bit
	f := inlineCmd.PersistentFlags()

	f.StringVarP(&inlineArgs.Query, "query", "q", "", "Query to search")
	f.StringVarP(&inlineArgs.Provider, "provider", "p", "", "Provider id to use")
	f.StringVarP(&inlineArgs.MangaSelector, "manga-selector", "m", "all", "Manga selector (all|first|last|id|exact|closest|<index>)")
	f.StringVarP(&inlineArgs.ChapterSelector, "chapter-selector", "c", "all", "Chapter selector (all|first|last|<num>|[from]-[to])")
	f.IntVarP(&inlineArgs.AnilistID, "anilist-id", "a", 0, "Anilist ID to search for metadata")
	f.BoolVar(&inlineArgs.PreferProviderMetadata, "prefer-provider-metadata", false, "Prefer provider metadata if valid (skips --search-metadata)")

	f.Bool("search-metadata", false, "Search metadata and replace the provider metadata")
	configKeyFlag(f, "search-metadata", config.Download.Metadata.Search.Key)

	// also used by the script command
	addLoaderFlags(f)

	inlineCmd.MarkPersistentFlagRequired("provider")
	inlineCmd.MarkPersistentFlagRequired("query")
	inlineCmd.RegisterFlagCompletionFunc("provider", completionProviderIDs)

	// when anilist-id is provided, then use that exclusively
	inlineCmd.MarkFlagsMutuallyExclusive("anilist-id", "search-metadata")
	inlineCmd.MarkFlagsMutuallyExclusive("anilist-id", "prefer-provider-metadata")
}

var inlineCmd = &cobra.Command{
	Use:     "inline",
	Short:   "Useful for automation",
	Long:    "inline, useful for automation",
	GroupID: groupMode,
	Args:    cobra.NoArgs,
}

func init() {
	inlineCmd.AddCommand(inlineJSONCmd)

	f := inlineJSONCmd.Flags()
	f.BoolVar(&inlineArgs.ChapterPopulate, "chapter-populate", false, "Populate chapter metadata")
	f.BoolVar(&inlineArgs.AnilistDisable, "anilist-disable", false, "Disable anilist search")

	inlineJSONCmd.MarkFlagsMutuallyExclusive("anilist-id", "anilist-disable")
}

var inlineJSONCmd = &cobra.Command{
	Use:   "json",
	Short: "Output search results",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		st := newStatus(cmd)
		defer st.Done()
		args := inlineArgs
		args.Status = st
		return inline.RunJSON(st.Context(cmd.Context()), args)
	},
}

func init() {
	inlineCmd.AddCommand(inlineDownloadCmd)

	f := inlineDownloadCmd.Flags()
	f.BoolVar(&inlineArgs.JSONOutput, "json-output", false, "JSON format for individual chapter download output")
	f.StringP("format", "f", "", fmt.Sprintf("Download format (%s)", strings.Join(libmangal.FormatStrings(), "|")))
	configKeyFlag(f, "format", config.Download.Format.Key)
	f.StringP("directory", "d", "", "Download directory")
	configKeyFlag(f, "directory", config.Download.Path.Key)

	inlineDownloadCmd.MarkFlagDirname("directory")
}

var inlineDownloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Download manga",
	Long: `Download the chapters (--chapter-selector) of a manga. The manga selector must select a
single manga: the default "all" only works when the search finds one, use e.g. -m first.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		st := newStatus(cmd)
		defer st.Done()
		args := inlineArgs
		args.Status = st
		return inline.RunDownload(st.Context(cmd.Context()), args)
	},
}
