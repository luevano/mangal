package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(bindCmd)
	f := bindCmd.Flags()
	f.StringVarP(&bindArgs.Selector, "manga-selector", "m", "exact", "Manga selector of the search (first|last|exact|closest|<index>)")
	f.StringVar(&bindArgs.Anilist, "anilist", "", "Bind the AniList manga with this id")
	f.StringVar(&bindArgs.Kitsu, "kitsu", "", "Bind the Kitsu manga with this id")
	f.StringVar(&bindArgs.MyAnimeList, "mal", "", "Bind the MyAnimeList manga with this id")
	f.StringVar(&bindArgs.Default, "default", "", "")
	_ = f.MarkHidden("default")
	f.StringSliceVar(&bindArgs.Remove, "remove", nil, "Remove the binding of these sources (anilist|kitsu|mal)")
	f.BoolVarP(&bindArgs.JSON, "json", "j", false, "Print JSON")

	bindCmd.AddCommand(bindLsCmd)
	bindLsCmd.Flags().BoolVarP(&bindArgs.JSON, "json", "j", false, "Print JSON")
}

var bindArgs = struct {
	Selector                    string
	Anilist, Kitsu, MyAnimeList string
	Default                     string
	Remove                      []string
	JSON                        bool
}{}

var bindCmd = &cobra.Command{
	Use:   "bind <provider> <query|mid:id>",
	Short: "Bind a manga to its AniList, Kitsu or MyAnimeList manga",
	Long: `Bind a provider's manga (the one the selector picks from a search of the query, or the
manga with the id, "mid:<id>") to metadata sources: the default binding's metadata is what
its downloads use (ComicInfo.xml, folder names), every bound id is used to sync the reading
progress. The default is the first bound source in the order AniList, Kitsu, MyAnimeList;
the others are its backups, used in that order when the default's metadata can't be fetched.
Without flags it prints the manga's bindings. Watches, the library and the TUI (A on a
manga) share the bindings.`,
	Example: `  mangal bind mango-mangapill "One Piece" --anilist 30013 --kitsu 38
  mangal bind mango-mangaplus mid:100171 --remove kitsu
  mangal bind ls mango-mangapill`,
	Args:              cobra.MinimumNArgs(2),
	ValidArgsFunction: completionFirstProvider,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		type set struct {
			src client.MetadataSource
			id  string
		}
		var sets []set
		for _, s := range []struct {
			key, id string
		}{{"anilist", bindArgs.Anilist}, {"kitsu", bindArgs.Kitsu}, {"mal", bindArgs.MyAnimeList}} {
			if s.id == "" {
				continue
			}
			src, _ := client.ParseMetadataSource(s.key)
			sets = append(sets, set{src: src, id: strings.TrimSpace(s.id)})
		}
		if cmd.Flags().Changed("default") {
			return errors.New("--default is gone: the default is the first bound source of AniList, Kitsu, MyAnimeList (remove the ones before the one you want with --remove)")
		}
		var remove []metadata.IDCode
		for _, r := range bindArgs.Remove {
			src, err := client.ParseMetadataSource(r)
			if err != nil {
				return err
			}
			remove = append(remove, src.Code)
		}

		c, warnings, err := client.AcquireByID(ctx, args[0])
		if err != nil {
			return err
		}
		for _, w := range warnings {
			cmd.PrintErrf("warning: %s\n", w)
		}
		provider := c.Info().ID
		query := strings.Join(args[1:], " ")
		mangaID, title := "", ""
		if id, ok := selector.IDQuery(query); ok {
			// the id is all a binding needs, no request
			mangaID = id
		} else {
			mangas, err := c.SearchMangasContext(ctx, query)
			if err != nil {
				return fmt.Errorf("search %q: %w", query, err)
			}
			indexes, err := selector.Mangas(bindArgs.Selector, query, mangas)
			if err != nil {
				return err
			}
			if len(indexes) != 1 {
				return fmt.Errorf("manga selector %q selects %d mangas, it needs to select 1", bindArgs.Selector, len(indexes))
			}
			mangaID, title = mangas[indexes[0]].Info().ID, mangas[indexes[0]].Info().Title
		}

		bindings, err := cache.Bindings()
		if err != nil {
			return err
		}
		for _, s := range sets {
			meta, err := client.Bind(ctx, c, provider, mangaID, nil, s.src.Code, s.id)
			if err != nil {
				return err
			}
			successf(cmd, "bound to %s %s: %s", s.src.Name, s.id, meta)
		}
		for _, code := range remove {
			removed, err := client.Unbind(provider, mangaID, code)
			if err != nil {
				return err
			}
			if removed {
				successf(cmd, "removed the %s binding", client.SourceName(code))
			}
		}

		list, err := bindings.Get(provider, mangaID)
		if err != nil {
			return err
		}
		if bindArgs.JSON {
			if list == nil {
				list = []storage.Binding{}
			}
			return printJSON(cmd.OutOrStdout(), list)
		}
		name := mangaID
		if title != "" {
			name = fmt.Sprintf("%q (%s)", title, mangaID)
		}
		if len(list) == 0 {
			cmd.Printf("%s %s: no bindings\n", provider, name)
			return nil
		}
		cmd.Printf("%s %s: %s\n", provider, name, formatBindings(list))
		return nil
	},
}

// formatBindings is "AniList 30013 (default), Kitsu 11 (backup)".
func formatBindings(bindings []storage.Binding) string {
	parts := make([]string, len(bindings))
	for i, b := range bindings {
		parts[i] = client.SourceName(b.Source) + " " + b.ID
		if b.Default {
			parts[i] += " (default)"
		} else {
			parts[i] += " (backup)"
		}
	}
	return strings.Join(parts, ", ")
}

var bindLsCmd = &cobra.Command{
	Use:               "ls [provider [manga-id]]",
	Aliases:           []string{"list"},
	Short:             "List the metadata bindings",
	Args:              cobra.MaximumNArgs(2),
	ValidArgsFunction: completionFirstProvider,
	RunE: func(cmd *cobra.Command, args []string) error {
		bindings, err := cache.Bindings()
		if err != nil {
			return err
		}
		provider, mangaID := providerManga(args)
		list, err := bindings.List(provider, mangaID)
		if err != nil {
			return err
		}
		if bindArgs.JSON {
			if list == nil {
				list = []storage.Binding{}
			}
			return printJSON(cmd.OutOrStdout(), list)
		}
		if len(list) == 0 {
			cmd.Println("no bindings")
			return nil
		}
		titles := knownTitles()
		for start := 0; start < len(list); {
			end := start + 1
			for end < len(list) && list[end].Provider == list[start].Provider && list[end].MangaID == list[start].MangaID {
				end++
			}
			b := list[start]
			name := b.MangaID
			if t := titles[[2]string{b.Provider, b.MangaID}]; t != "" {
				name = fmt.Sprintf("%s %q", b.MangaID, t)
			}
			cmd.Printf("%s %s: %s\n", b.Provider, name, formatBindings(list[start:end]))
			start = end
		}
		return nil
	},
}

// knownTitles are the titles of the watched and downloaded mangas by provider and id,
// best effort.
func knownTitles() map[[2]string]string {
	titles := map[[2]string]string{}
	if lib, err := cache.Library(); err == nil {
		if mangas, err := lib.Mangas(); err == nil {
			for _, m := range mangas {
				for _, s := range m.Sources {
					titles[[2]string{s.Provider, s.MangaID}] = s.Title
				}
			}
		}
	}
	if watches, err := cache.Watches(); err == nil {
		if list, err := watches.List(); err == nil {
			for _, w := range list {
				titles[[2]string{w.Provider, w.MangaID}] = w.Title
			}
		}
	}
	return titles
}
