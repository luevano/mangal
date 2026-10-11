// Package migrate is the TUI's "mangal migrate": moves data under old provider ids.
package migrate

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
)

var _ base.State = (*state)(nil)

type state struct {
	migration *library.Migration
	keyMap    keyMap
}

type plannedMsg struct {
	migration library.Migration
}

// New lists what would be migrated, enter does it.
func New() base.State {
	return &state{keyMap: keyMap{apply: util.Bind("migrate", "enter")}}
}

func (s *state) Backable() bool                { return true }
func (s *state) Inputting() bool               { return false }
func (s *state) KeyMap() help.KeyMap           { return s.keyMap }
func (s *state) Title() base.Title             { return base.Title{Text: "Migrate"} }
func (s *state) Status() string                { return "" }
func (s *state) Resize(size base.Size) tea.Cmd { return nil }
func (s *state) Subtitle() string {
	return "data under old provider ids moves to the current ones, the old cache is dropped"
}

func (s *state) Init(ctx context.Context) tea.Cmd {
	return tea.Sequence(
		base.Loading("Looking for old provider ids"),
		func() tea.Msg {
			loaders, err := manager.Loaders()
			if err != nil {
				return err
			}
			cacheDB, dataDB, err := cache.DBs()
			if err != nil {
				return err
			}
			migration, err := library.PlanMigration(afs.Afero, cacheDB, dataDB, path.DownloadsDir(), config.Download.Provider.CreateDir.Get(), loaders)
			if err != nil {
				return err
			}
			return plannedMsg{migration: migration}
		},
		base.Loaded,
	)
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case plannedMsg:
		s.migration = &msg.migration
		s.keyMap.apply.SetEnabled(!msg.migration.Empty())
	case tea.KeyMsg:
		if s.migration == nil || s.migration.Empty() || !key.Matches(msg, s.keyMap.apply) {
			return nil
		}
		migration := *s.migration
		return tea.Sequence(
			base.Loading("Migrating"),
			func() tea.Msg {
				cacheDB, dataDB, err := cache.DBs()
				if err != nil {
					return err
				}
				if err := migration.Apply(afs.Afero, cacheDB, dataDB); err != nil {
					return err
				}
				return base.Back()
			},
			base.Loaded,
			base.Notify("Migrated, folders keep their names (mangal library rename renames them)"),
		)
	}
	return nil
}

func (s *state) View() string {
	switch {
	case s.migration == nil:
		return ""
	case s.migration.Empty():
		return style.Normal.Secondary.Render("Nothing to migrate.")
	}
	return strings.Join(s.migration.Lines(), "\n")
}

type keyMap struct {
	apply key.Binding
}

func (k keyMap) ShortHelp() []key.Binding  { return []key.Binding{k.apply} }
func (k keyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }
