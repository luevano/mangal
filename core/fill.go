package core

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/library"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// FillReq gets the unavailable chapters of a provider's manga (the id, else a search of
// Query) from another provider's manga (From, its Provider needed; the search defaults to
// the manga's title): those numbers of From's manga go into the manga's folder as its own
// chapters (client.Fill). Chapters narrows the numbers (a chapter selector: 1-2, 10-).
type FillReq struct {
	Provider, MangaID, Query string
	From                     RetitleFrom
	Chapters                 string
}

// FillChapter is a chapter of a fill: Title is the manga's for it (the file's), FromTitle
// and URL the other provider's chapter's.
type FillChapter struct {
	Number    float32 `json:"number"`
	Title     string  `json:"title"`
	FromTitle string  `json:"from_title,omitempty"`
	URL       string  `json:"url,omitempty"`
}

func (c FillChapter) line() string {
	if c.Title == "" {
		return "chapter " + stringutil.FormatFloat32(c.Number)
	}
	return fmt.Sprintf("chapter %s %q", stringutil.FormatFloat32(c.Number), c.Title)
}

// FillPlan is what a fill gets: Found from From's manga, NotFound (From's manga doesn't
// have them), Downloaded (the folder has them). Job (or Download) gets Found.
type FillPlan struct {
	Provider, From   libmangal.ProviderInfo
	Manga, FromManga mangadata.MangaInfo
	// Folder is where the chapters go (the manga's folder).
	Folder                      string
	Found, NotFound, Downloaded []FillChapter

	fill     client.Fill
	chapters []mangadata.Chapter
	options  libmangal.DownloadOptions
}

// Nothing is true when there's no chapter to get.
func (p FillPlan) Nothing() bool { return len(p.Found) == 0 }

// Lines are the plan in lines.
func (p FillPlan) Lines() []string {
	var lines []string
	if p.Folder != "" {
		lines = append(lines, "into "+p.Folder)
	}
	for _, ch := range p.Found {
		lines = append(lines, fmt.Sprintf("get %s from %s (%q)", ch.line(), p.From.ID, ch.FromTitle))
	}
	for _, ch := range p.NotFound {
		lines = append(lines, fmt.Sprintf("%s: not on %s", ch.line(), cmp.Or(p.From.ID, "the other provider")))
	}
	for _, ch := range p.Downloaded {
		lines = append(lines, ch.line()+": already downloaded")
	}
	if len(p.Found)+len(p.NotFound)+len(p.Downloaded) == 0 {
		lines = append(lines, "no unavailable chapters")
	}
	return lines
}

// Title names the fill's job: "Dark Gathering: chapters 1-2 (2) from Mangapill".
func (p FillPlan) Title() string {
	what := "nothing"
	switch n := len(p.Found); n {
	case 0:
	case 1:
		what = "chapter " + stringutil.FormatFloat32(p.Found[0].Number)
	default:
		what = fmt.Sprintf("chapters %s-%s (%d)", stringutil.FormatFloat32(p.Found[0].Number), stringutil.FormatFloat32(p.Found[n-1].Number), n)
	}
	return fmt.Sprintf("%s: %s from %s", p.Manga.Title, what, p.From.Name)
}

// Job is the fill as a download job (jobs.FillSpec) of From's provider; origin is the
// screen that starts it.
func (p FillPlan) Job(origin string) jobs.Spec {
	return jobs.FillSpec(p.fill, p.FromManga.ID, p.chapters, p.options, p.Title(), origin)
}

// PlanFill plans a fill: the manga's unavailable chapter numbers (none of its chapters
// with the number can be downloaded) its folder doesn't have, matched by number on From's
// manga (the first available chapter with the number, as From's chapter list has them).
func (c *Core) PlanFill(ctx context.Context, req FillReq) (FillPlan, error) {
	switch {
	case req.Provider == "" || req.MangaID == "" && req.Query == "":
		return FillPlan{}, invalid("give the provider's manga to get the chapters for")
	case req.From.Provider == "":
		return FillPlan{}, invalid("give the provider to get the chapters from")
	case req.From.Provider == req.Provider:
		return FillPlan{}, invalid("the chapters come from another provider than %s", req.Provider)
	}
	report(ctx, library.Progress{Phase: "listing the chapters on " + req.Provider})
	hc, manga, chapters, release, err := c.env.ProviderChapters(ctx, req.Provider, req.MangaID, req.Query, "")
	if err != nil {
		return FillPlan{}, err
	}
	defer release()
	plan := FillPlan{Provider: hc.Info(), Manga: manga.Info()}

	byNumber := map[float32][]mangadata.Chapter{}
	var unavailable []mangadata.Chapter
	for _, ch := range chapters {
		n := ch.Info().Number
		if len(byNumber[n]) == 0 {
			unavailable = append(unavailable, ch)
		}
		byNumber[n] = append(byNumber[n], ch)
	}
	unavailable = slices.DeleteFunc(unavailable, func(ch mangadata.Chapter) bool {
		return slices.ContainsFunc(byNumber[ch.Info().Number], func(ch mangadata.Chapter) bool { return ch.Info().Unavailable == "" })
	})
	slices.SortStableFunc(unavailable, func(a, b mangadata.Chapter) int { return cmp.Compare(a.Info().Number, b.Info().Number) })
	if len(unavailable) == 0 {
		return plan, nil
	}
	if req.Chapters != "" {
		if unavailable, err = selector.Chapters(req.Chapters, unavailable); err != nil {
			return FillPlan{}, invalid("chapters: %s", err)
		}
	}

	plan.options = DownloadOptions(false)
	plan.options.Directory, plan.options.CreateProviderDir = c.env.Root(), c.env.ProviderDirs()
	if plan.fill, err = client.NewFill(ctx, hc, manga, unavailable[0], libmangal.ProviderInfo{}, plan.options); err != nil {
		return FillPlan{}, err
	}
	plan.Folder = plan.fill.Folder
	downloaded, err := c.folderNumbers(plan.fill)
	if err != nil {
		return FillPlan{}, err
	}
	titles := library.ChapterTitles(chapters)
	var todo []mangadata.Chapter
	for _, ch := range unavailable {
		n := ch.Info().Number
		if downloaded[n] {
			plan.Downloaded = append(plan.Downloaded, FillChapter{Number: n, Title: titles[n]})
			continue
		}
		todo = append(todo, ch)
	}
	if len(todo) == 0 {
		return plan, nil
	}

	from := req.From
	from.Query = cmp.Or(from.Query, plan.Manga.Title)
	report(ctx, library.Progress{Phase: "finding the manga on " + from.Provider})
	fc, fromManga, fromChapters, releaseFrom, err := c.env.ProviderChapters(ctx, from.Provider, from.MangaID, from.Query, from.Selector)
	if err != nil {
		return FillPlan{}, err
	}
	// the job holds the provider's client while it runs
	releaseFrom()
	plan.From, plan.FromManga, plan.fill.From = fc.Info(), fromManga.Info(), fc.Info()
	available := map[float32]mangadata.Chapter{}
	for _, ch := range fromChapters {
		info := ch.Info()
		if _, ok := available[info.Number]; !ok && info.Unavailable == "" {
			available[info.Number] = ch
		}
	}
	for _, ch := range todo {
		n := ch.Info().Number
		src, ok := available[n]
		if !ok {
			plan.NotFound = append(plan.NotFound, FillChapter{Number: n, Title: titles[n]})
			continue
		}
		// the manga's title, else src's when it says more than the number
		title := library.ChapterTitles(append(slices.Clone(byNumber[n]), src))[n]
		info := src.Info()
		plan.Found = append(plan.Found, FillChapter{Number: n, Title: title, FromTitle: info.Title, URL: info.URL})
		plan.chapters = append(plan.chapters, plan.fill.Chapter(src, title, ch.Volume().Info()))
	}
	return plan, nil
}

// folderNumbers are the chapter numbers downloaded in the fill's folder.
func (c *Core) folderNumbers(fill client.Fill) (map[float32]bool, error) {
	parent, dir := fill.Folder, ""
	if fill.MangaDir {
		parent, dir = filepath.Dir(fill.Folder), filepath.Base(fill.Folder)
	}
	lib := c.env.Storage.Library()
	m, ok, err := lib.MangaByFolder(parent, dir)
	if err != nil || !ok {
		return nil, err
	}
	downloads, err := lib.Downloads(m.ID)
	if err != nil {
		return nil, err
	}
	numbers := map[float32]bool{}
	for _, d := range downloads {
		numbers[d.Chapter] = true
	}
	return numbers, nil
}

// providerChapters finds the provider's manga (selector.Find) with its client, held until
// release, and lists its chapters.
func providerChapters(ctx context.Context, provider, id, query, sel string) (*libmangal.Client, mangadata.Manga, []mangadata.Chapter, func(), error) {
	cl, _, release, err := client.HoldByID(ctx, provider)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	manga, err := selector.Find(ctx, client.Searcher(cl), id, query, sel)
	var chapters []mangadata.Chapter
	if err == nil {
		chapters, err = client.MangaChapters(ctx, cl, manga)
	}
	if err != nil {
		release()
		return nil, nil, nil, nil, err
	}
	return cl, manga, chapters, release, nil
}
