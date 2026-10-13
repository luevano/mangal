package watches

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// fromAll is the from value that downloads every chapter the library misses.
const fromAll = "all"

// parseFrom is the from field: nil for all, else the number.
func parseFrom(s string) (*float32, error) {
	if s == "" || strings.EqualFold(s, fromAll) {
		return nil, nil
	}
	n, err := strconv.ParseFloat(s, 32)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("%q isn't a chapter number, or %q", s, fromAll)
	}
	f := float32(n)
	return &f, nil
}

func validateFrom(s string) error {
	_, err := parseFrom(s)
	return err
}

// anilistID is the AniList id of the manga's metadata (bound or found), 0 if it isn't
// AniList's.
func anilistID(manga mangadata.Manga) int {
	meta := manga.Metadata()
	if meta == nil || meta.ID().Source != lmmeta.IDSourceAnilist {
		return 0
	}
	id, _ := strconv.Atoi(meta.ID().Raw)
	return id
}

// highestNumber is the highest number of the available chapters, nil if none.
func highestNumber(chapters []mangadata.Chapter) *float32 {
	var n *float32
	for _, ch := range chapters {
		info := ch.Info()
		if info.Unavailable != "" {
			continue
		}
		if n == nil || info.Number > *n {
			num := info.Number
			n = &num
		}
	}
	return n
}

// defaultFrom is what the from field starts with: the last chapter the library has
// (the checks start from it anyway), else the latest one (only new chapters), else all.
func defaultFrom(libraryLast, latest *float32) string {
	switch {
	case libraryLast != nil:
		return stringutil.FormatFloa32(*libraryLast)
	case latest != nil:
		return stringutil.FormatFloa32(*latest)
	}
	return fromAll
}

// fromHint says where the from default comes from.
func fromHint(libraryLast, latest *float32) string {
	var known []string
	if libraryLast != nil {
		known = append(known, "the library has up to "+stringutil.FormatFloa32(*libraryLast))
	}
	if latest != nil {
		known = append(known, "the latest is "+stringutil.FormatFloa32(*latest))
	}
	hint := `chapters up to it count as done, "all" downloads every missing one`
	if len(known) != 0 {
		hint += " (" + strings.Join(known, ", ") + ")"
	}
	return hint
}

var _ base.State = (*addState)(nil)

// addState watches a manga: from which chapter, its AniList binding, gap filling.
type addState struct {
	client   *libmangal.Client
	manga    mangadata.Manga
	chapters []mangadata.Chapter
	connect  connector

	// set once loaded
	manager     watch.Manager
	status      watch.Status
	watched     bool
	libraryLast *float32

	form          *form
	from, anilist *field
	gaps          *field
}

// infoMsg is what the add screen needs to know first.
type infoMsg struct {
	manager     watch.Manager
	status      watch.Status
	watched     bool
	libraryLast *float32
	latest      *float32
}

type addedMsg struct{ w watch.Watch }

// NewAdd watches the client's manga. chapters are its chapters when known (nil loads
// them), for the latest one.
func NewAdd(c *libmangal.Client, manga mangadata.Manga, chapters []mangadata.Chapter) base.State {
	return newAddState(Connect, c, manga, chapters)
}

func newAddState(connect connector, c *libmangal.Client, manga mangadata.Manga, chapters []mangadata.Chapter) *addState {
	s := &addState{
		client:   c,
		manga:    manga,
		chapters: chapters,
		connect:  connect,
		anilist:  textField("AniList id", formatAnilistID(anilistID(manga)), "the metadata of its downloads, empty to search it as usual", validateAnilistID),
		gaps:     toggleField("Gaps", config.Watch.Gaps.Get(), "also download the missing chapters below the last one"),
	}
	s.from = textField("From chapter", fromAll, fromHint(nil, nil), validateFrom)
	s.form = newForm(s.from, s.anilist, s.gaps)
	return s
}

// setFrom sets the default from once the library and the chapters are known, unless it
// was changed already.
func (s *addState) setFrom(libraryLast, latest *float32) {
	s.libraryLast = libraryLast
	s.from.hint = fromHint(libraryLast, latest)
	if s.from.value() == fromAll {
		s.from.input.SetValue(defaultFrom(libraryLast, latest))
		s.from.input.CursorEnd()
	}
}

func (s *addState) Backable() bool      { return true }
func (s *addState) Inputting() bool     { return true }
func (s *addState) KeyMap() help.KeyMap { return util.KeyMap{Short: s.form.keys()} }
func (s *addState) Title() base.Title   { return base.Title{Text: "Watch " + s.manga.String()} }
func (s *addState) Key() string {
	return "watch-add/" + s.client.Info().ID + "/" + s.manga.Info().ID
}
func (s *addState) Status() string { return "" }

func (s *addState) Subtitle() string {
	sub := s.client.Info().Name
	if s.manager != nil {
		sub += " · " + managerName(s.status)
	}
	if s.watched {
		sub += " · already watched"
	}
	return sub
}

func (s *addState) Resize(size base.Size) tea.Cmd { return nil }

func (s *addState) Init(ctx context.Context) tea.Cmd {
	c, manga, chapters, connect := s.client, s.manga, s.chapters, s.connect
	providerDirs := config.Download.Provider.CreateDir.Get()
	return base.Load(ctx, "Looking up "+manga.String(), func(ctx context.Context) (infoMsg, error) {
		var msg infoMsg
		m, err := connect(ctx)
		if err != nil {
			return msg, err
		}
		msg.manager = m
		if msg.status, err = m.Status(ctx); err != nil {
			return msg, err
		}
		list, err := m.List(ctx)
		if err != nil {
			return msg, err
		}
		msg.watched = NewSet(list).Has(c.Info().ID, manga.Info().ID)
		if msg.libraryLast, err = libraryLast(c.Info(), manga, providerDirs); err != nil {
			return msg, err
		}
		if chapters == nil {
			if chapters, err = client.MangaChapters(ctx, c, manga); err != nil {
				return msg, err
			}
		}
		msg.latest = highestNumber(chapters)
		return msg, nil
	}, func(msg infoMsg) tea.Msg { return msg })
}

// libraryLast is the highest chapter number the library has of the manga, nil if none.
func libraryLast(provider libmangal.ProviderInfo, manga mangadata.Manga, providerDirs bool) (*float32, error) {
	st, err := cache.Storage()
	if err != nil {
		return nil, err
	}
	m, found, err := st.Library().MangaFor(provider, manga, providerDirs)
	if err != nil || !found {
		return nil, err
	}
	numbers, err := st.Watches().LibraryNumbers(m.ID)
	if err != nil {
		return nil, err
	}
	var last *float32
	for n := range numbers {
		if last == nil || n > *last {
			n := n
			last = &n
		}
	}
	return last, nil
}

// options are what to add, valid once the form is.
func (s *addState) options() watch.AddOptions {
	opts := watch.AddOptions{
		Provider: s.client.Info().ID,
		MangaID:  s.manga.Info().ID,
		// providers without a lookup by id find it by its title
		Query: s.manga.Info().Title,
	}
	opts.AnilistID, _ = parseAnilistID(s.anilist.value())
	from, _ := parseFrom(s.from.value())
	// the checks start from the library's last chapter anyway, a from would stop gap
	// filling below it
	if from != nil && (s.libraryLast == nil || *from != *s.libraryLast) {
		opts.From = from
	}
	gaps := s.gaps.on
	opts.Gaps = &gaps
	return opts
}

func (s *addState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case infoMsg:
		s.manager, s.status, s.watched = msg.manager, msg.status, msg.watched
		s.setFrom(msg.libraryLast, msg.latest)
		return nil
	case addedMsg:
		return tea.Sequence(base.Back, base.Notify("Watching "+msg.w.Title))
	case tea.KeyMsg:
		if base.Busy(ctx) {
			return nil
		}
	}
	submitted, cmd := s.form.Update(msg)
	switch {
	case !submitted:
		return cmd
	case s.manager == nil:
		return base.Notify("Still looking it up")
	case s.watched:
		return base.Notify("Already watched, see the watches (" + base.KeyWatches.Help().Key + " on the home screen)")
	}
	m, opts := s.manager, s.options()
	return base.Load(ctx, "Watching "+s.manga.String(), func(ctx context.Context) (addedMsg, error) {
		w, err := m.Add(ctx, opts)
		if errors.Is(err, watch.ErrExists) {
			return addedMsg{}, fmt.Errorf("already watched: %w", err)
		}
		return addedMsg{w: w}, err
	}, func(msg addedMsg) tea.Msg { return msg })
}

func (s *addState) View() string { return s.form.View() }
