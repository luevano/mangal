// Package migrate is the TUI's "mangal migrate": moves data under old provider ids.
package migrate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/viewport"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
)

var _ base.State = (*state)(nil)

type state struct {
	migration *library.Migration
	// plan shows the migration, it can be long
	plan   *viewport.Model
	keyMap keyMap
}

type plannedMsg struct {
	migration library.Migration
}

// migratedMsg is a migration done, relink how many watches need a re-link then.
type migratedMsg struct{ relink int }

// New lists what would be migrated, enter does it.
func New() base.State {
	plan := viewport.New()
	// esc goes back
	plan.KeyMap.Back.SetEnabled(false)
	return &state{plan: plan, keyMap: keyMap{apply: util.Bind("migrate", "enter")}}
}

func (s *state) Key() string     { return "migrate" }
func (s *state) Backable() bool  { return true }
func (s *state) Inputting() bool { return false }
func (s *state) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(util.KeyMap{Short: []key.Binding{s.keyMap.apply}}, s.plan.KeyMap)
}
func (s *state) Title() base.Title             { return base.Title{Text: "Migrate"} }
func (s *state) Status() string                { return "" }
func (s *state) Resize(size base.Size) tea.Cmd { return s.plan.Resize(size) }
func (s *state) Subtitle() string {
	return "data under old provider ids moves to the current ones, the old cache is dropped"
}

func (s *state) Init(ctx context.Context) tea.Cmd {
	providerDirs := config.Download.Provider.CreateDir.Get()
	return base.LoadWithStatus(ctx, "Looking for old provider ids", func(_ context.Context, status func(string)) (plannedMsg, error) {
		root, err := path.ExistingDownloads()
		if err != nil {
			return plannedMsg{}, err
		}
		loaders, err := manager.Loaders()
		if err != nil {
			return plannedMsg{}, err
		}
		st, err := cache.Storage()
		if err != nil {
			return plannedMsg{}, err
		}
		migration, err := library.FindMigration(afs.Afero, st, root, providerDirs, loaders, func(p library.Progress) { status(p.String()) })
		if err != nil {
			return plannedMsg{}, err
		}
		return plannedMsg{migration: migration}, nil
	}, func(msg plannedMsg) tea.Msg { return msg })
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case plannedMsg:
		s.migration = &msg.migration
		s.keyMap.apply.SetEnabled(!msg.migration.Empty())
		if msg.migration.Empty() {
			s.plan.SetContent(style.Normal.Secondary.Render("Nothing to migrate."))
		} else {
			s.plan.SetContent(strings.Join(msg.migration.Lines(), "\n"))
		}
	case migratedMsg:
		if msg.relink > 0 {
			return tea.Sequence(base.Back, base.NotifyWithDuration(fmt.Sprintf(
				"Migrated. %d watches need a re-link (the site may use other ids): %s, then %s lists them, %s re-links one", msg.relink,
				base.KeyWatches.Help().Key, base.KeyNeedsRelink.Help().Key, base.KeyRelink.Help().Key), 10*time.Second))
		}
		return tea.Sequence(base.Back, base.Notify("Migrated, folders keep their names (mangal library rename renames them)"))
	case tea.KeyMsg:
		if !key.Matches(msg, s.keyMap.apply) {
			return s.plan.Update(msg)
		}
		if s.migration == nil || s.migration.Empty() || base.Busy(ctx) {
			return nil
		}
		migration := *s.migration
		modes := client.LibraryModes()
		return base.LoadWithStatus(ctx, "Migrating", func(_ context.Context, status func(string)) (migratedMsg, error) {
			migration.OnProgress = func(p library.Progress) { status(p.String()) }
			st, err := cache.Storage()
			if err != nil {
				return migratedMsg{}, err
			}
			summary := fmt.Sprintf("migrate %d old provider ids, %d markers", len(migration.OldIDs), len(migration.Markers))
			if _, err := client.Operation("migrate", summary, func(j library.Journal) error {
				return migration.ApplyTo(afs.Afero, j, st, modes)
			}); err != nil {
				return migratedMsg{}, err
			}
			watches, err := st.Watches().List()
			msg := migratedMsg{}
			for _, w := range watches {
				if w.NeedsRelink {
					msg.relink++
				}
			}
			return msg, err
		}, func(msg migratedMsg) tea.Msg { return msg })
	}
	return nil
}

func (s *state) View() string {
	if s.migration == nil {
		return ""
	}
	return s.plan.View()
}

type keyMap struct {
	apply key.Binding
}
