package cmd

import (
	"fmt"
	"github.com/luevano/mangal/v5/util"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/inline"
	"github.com/spf13/cobra"
)

var inlineArgs = inline.Args{}

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

	f.Bool("search-metadata", config.Download.Metadata.Search.Get(), "Search metadata and replace the provider metadata")

	// Loader options, these are reused for script cmd
	f.Uint8("parallelism", config.Providers.Parallelism.Get(), "Provider parallelism to use (when supported)")
	f.Bool("nsfw", config.Providers.Filter.NSFW.Get(), "Include NSFW content (when supported)")
	f.String("language", config.Providers.Filter.Language.Get(), "Manga/Chapter language")
	f.Bool("avoid-duplicate-chapters", config.Providers.Filter.AvoidDuplicateChapters.Get(), "No duplicate chapters")
	f.Bool("show-unavailable-chapters", config.Providers.Filter.ShowUnavailableChapters.Get(), "Show undownloadable chapters")
	f.Bool("headless-use-flaresolverr", config.Providers.Headless.UseFlaresolverr.Get(), "Use Flaresolverr for headlessproviders")
	f.String("headless-flaresolverr-url", config.Providers.Headless.FlaresolverrURL.Get(), "Flaresolverr service URL")

	inlineCmd.MarkPersistentFlagRequired("provider")
	inlineCmd.MarkPersistentFlagRequired("query")
	inlineCmd.RegisterFlagCompletionFunc("provider", completionProviderIDs)

	// when anilist-id is provided, then use that exclusively
	inlineCmd.MarkFlagsMutuallyExclusive("anilist-id", "search-metadata")
	inlineCmd.MarkFlagsMutuallyExclusive("anilist-id", "prefer-provider-metadata")

	// config(viper) flag binds
	config.BindPFlag(config.Download.Metadata.Search.Key, f.Lookup("search-metadata"))
	config.BindPFlag(config.Providers.Parallelism.Key, f.Lookup("parallelism"))
	config.BindPFlag(config.Providers.Filter.NSFW.Key, f.Lookup("nsfw"))
	config.BindPFlag(config.Providers.Filter.Language.Key, f.Lookup("language"))
	config.BindPFlag(config.Providers.Filter.AvoidDuplicateChapters.Key, f.Lookup("avoid-duplicate-chapters"))
	config.BindPFlag(config.Providers.Filter.ShowUnavailableChapters.Key, f.Lookup("show-unavailable-chapters"))
	config.BindPFlag(config.Providers.Headless.UseFlaresolverr.Key, f.Lookup("headless-use-flaresolverr"))
	config.BindPFlag(config.Providers.Headless.FlaresolverrURL.Key, f.Lookup("headless-flaresolverr-url"))
}

var inlineCmd = &cobra.Command{
	Use:     config.ModeInline.String(),
	Short:   "Useful for automation",
	Long:    fmt.Sprintf("%s, useful for automation", config.ModeInline),
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
	Run: func(cmd *cobra.Command, _ []string) {
		if err := inline.RunJSON(cmd.Context(), inlineArgs); err != nil {
			errorf(cmd, "%s", util.ErrorTree(err))
		}
	},
}

func init() {
	inlineCmd.AddCommand(inlineDownloadCmd)

	f := inlineDownloadCmd.Flags()
	fmtDesc := fmt.Sprintf("Download format (%s)", strings.Join(libmangal.FormatStrings(), "|"))
	f.BoolVar(&inlineArgs.JSONOutput, "json-output", false, "JSON format for individual chapter download output")
	f.StringP("format", "f", config.Download.Format.Get().String(), fmtDesc)
	f.StringP("directory", "d", config.Download.Path.Get(), "Download directory")

	inlineDownloadCmd.MarkFlagDirname("directory")

	config.BindPFlag(config.Download.Format.Key, f.Lookup("format"))
	config.BindPFlag(config.Download.Path.Key, f.Lookup("directory"))
}

var inlineDownloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Download manga",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		if err := inline.RunDownload(cmd.Context(), inlineArgs); err != nil {
			errorf(cmd, "%s", util.ErrorTree(err))
		}
	},
}
