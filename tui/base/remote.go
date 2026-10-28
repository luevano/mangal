package base

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/core"
)

// Remote is the mangal serve the TUI runs on (--remote, remote.url).
type Remote struct {
	// URL is the server's address, Version its mangal's.
	URL, Version string
	// Features are the server's (its status' features: core.Features names).
	Features []string
	// Perms are the permissions the server gives the token (its status' scopes), Who
	// whose they are ("token claude").
	Perms []string
	Who   string
	// Services are the server's.
	Services core.Services
	// Events tells fn the topics (storage.Rev*) the server says changed, until ctx is
	// done: the shown State's DBChangedMsg.
	Events func(ctx context.Context, fn func(topics []string))
}

// CurrentRemote is the server the TUI runs on (set by the tui package), nil for this
// machine.
var CurrentRemote *Remote

// Has is true when the server allows the permission.
func (r *Remote) Has(perm string) bool { return slices.Contains(r.Perms, perm) }

// Host is the server's address without the scheme.
func (r *Remote) Host() string {
	_, host, _ := strings.Cut(r.URL, "://")
	return host
}

// Header is the header's line: where the TUI runs and as whom.
func (r *Remote) Header() string {
	role := core.RoleOf(r.Perms)
	if role == "custom" {
		role = strings.Join(r.Perms, ", ")
	}
	return fmt.Sprintf("remote: %s as %s (%s)", r.Host(), r.Who, role)
}

// gate is what a screen's action needs on a remote server: a permission, a feature of
// the server; or why it can't be done there yet.
type gate struct {
	perm, feature string
	unsupported   string
}

// remoteGates are the actions a remote server may refuse, by "screen:help" (the screen's
// key up to its first slash, with the slash: "library/" is a library manga's), else
// "*:help". The server enforces the permissions anyway: these hide what it would refuse.
var remoteGates = map[string]gate{
	"home:providers":    {unsupported: "browsing the server's providers"},
	"home:accounts":     {unsupported: "the accounts, users and tokens"},
	"home:health":       {perm: core.ScopeManage, feature: "health"},
	"home:migrate":      {perm: core.ScopeAdmin, feature: "migrate"},
	"home:read history": {feature: "read history"},

	"library:scan":                {perm: core.ScopeManage, feature: "library scan"},
	"library:scan and check ids":  {perm: core.ScopeManage, feature: "library scan"},
	"library:merge":               {perm: core.ScopeManage, feature: "merge"},
	"library:move to a provider":  {unsupported: "moving a library manga to a provider"},
	"library:trash":               {perm: core.ScopeManage, feature: "trash"},
	"library:delete":              {perm: core.ScopeManage, feature: "delete"},
	"library:undo":                {perm: core.ScopeManage, feature: "undo"},
	"library:duplicates only":     {perm: core.ScopeManage, feature: "merge"},
	"library:bindings":            {unsupported: "a library manga's links"},
	"library:fill backups":        {perm: core.ScopeManage, feature: "backup links"},
	"library:rename":              {perm: core.ScopeAdmin, feature: "rename"},
	"library:hold / new provider": {perm: core.ScopeManage, feature: "hold marks"},

	"library/:open":                {unsupported: "reading the server's chapter files"},
	"library/:scan":                {unsupported: "scanning one manga"},
	"library/:delete":              {perm: core.ScopeManage, feature: "delete"},
	"library/:watch":               {unsupported: "adding a watch (it looks the manga up on a provider)"},
	"library/:bindings":            {unsupported: "a library manga's links"},
	"library/:hold / new provider": {perm: core.ScopeManage, feature: "hold marks"},
	"library/:retitle chapters":    {unsupported: "retitling chapters"},

	"trash:empty": {perm: core.ScopeAdmin, feature: "empty the trash"},

	"history:delete":    {perm: core.ScopeAdmin, feature: "history clear"},
	"history:clear all": {perm: core.ScopeAdmin, feature: "history clear"},

	"health:apply fix":            {perm: core.ScopeManage, feature: "health fix"},
	"health:merge":                {perm: core.ScopeManage, feature: "merge"},
	"health:dismiss":              {perm: core.ScopeManage, feature: "health"},
	"health/:apply fix":           {perm: core.ScopeManage, feature: "health fix"},
	"health/:merge":               {perm: core.ScopeManage, feature: "merge"},
	"health/:dismiss":             {perm: core.ScopeManage, feature: "health"},
	"*:check now":                 {perm: core.ScopeManage, feature: "watch check"},
	"*:check due":                 {perm: core.ScopeManage, feature: "watch check"},
	"*:check provider":            {perm: core.ScopeManage, feature: "watch check"},
	"*:fill gaps":                 {perm: core.ScopeManage, feature: "watch check"},
	"*:stop watching":             {perm: core.ScopeManage, feature: "watches"},
	"*:re-link manga":             {unsupported: "re-linking a watch (it searches a provider)"},
	"watches:pause/resume":        {perm: core.ScopeManage, feature: "watches"},
	"watches:edit":                {perm: core.ScopeManage, feature: "watches"},
	"watch/:pause/resume":         {perm: core.ScopeManage, feature: "watches"},
	"watch/:edit":                 {perm: core.ScopeManage, feature: "watches"},
	"*:pause/resume its provider": {perm: core.ScopeManage, feature: "watches"},
	"*:hold / new provider":       {perm: core.ScopeManage, feature: "hold marks"},
	"*:delete check":              {perm: core.ScopeManage, feature: "watch history"},

	"downloads:cancel":            {perm: core.ScopeDownload, feature: "download"},
	"downloads:retry":             {perm: core.ScopeDownload, feature: "download"},
	"downloads:remove":            {perm: core.ScopeDownload, feature: "download"},
	"downloads:cancel all":        {perm: core.ScopeDownload, feature: "download"},
	"downloads:clear its queue":   {perm: core.ScopeAdmin, feature: "download"},
	"downloads:clear every queue": {perm: core.ScopeAdmin, feature: "download"},
	"download/:cancel":            {perm: core.ScopeDownload, feature: "download"},
	"download/:retry":             {perm: core.ScopeDownload, feature: "download"},
	"download/:open directory":    {unsupported: "opening the server's folders"},
	"*:jobs":                      {unsupported: "the maintenance jobs"},
}

// remoteScreens are the screens a remote server can't show yet, by their key up to its
// first slash: they need this machine's providers, files or accounts (docs/remote.md R3).
var remoteScreens = map[string]string{
	"providers":       "browsing the server's providers",
	"mangas":          "browsing the server's providers",
	"volumes":         "browsing the server's providers",
	"chapters":        "browsing the server's providers",
	"chapters-fill":   "getting chapters from another provider",
	"mirrors":         "the providers' mirrors",
	"bindings":        "the metadata bindings",
	"library-links":   "a library manga's links",
	"library-move":    "moving a library manga to a provider",
	"library-retitle": "retitling chapters",
	"watch-add":       "adding a watch",
	"watch-relink":    "re-linking a watch",
	"accounts":        "the accounts, users and tokens (the web UI's Users page has them)",
	"jobs":            "the maintenance jobs",
	"jobs-settings":   "the maintenance jobs",
	"login":           "the logins",
}

// screenClass is the key up to its first slash, with the slash.
func screenClass(key string) string {
	if i := strings.IndexByte(key, '/'); i >= 0 {
		return key[:i+1]
	}
	return key
}

// remoteGate is the gate of the screen's action with the help, false without one.
func remoteGate(screen, desc string) (gate, bool) {
	if g, ok := remoteGates[screenClass(screen)+":"+desc]; ok {
		return g, true
	}
	g, ok := remoteGates["*:"+desc]
	return g, ok
}

// refusal is why the server can't do g, "" when it can.
func (r *Remote) refusal(g gate) string {
	switch {
	case g.unsupported != "":
		return capitalize(g.unsupported) + " isn't on a remote server yet (mangal without --remote does it)"
	case g.feature != "" && !slices.Contains(r.Features, g.feature):
		return fmt.Sprintf("The server (mangal %s) doesn't have %s: update it", r.Version, g.feature)
	case g.perm != "" && !r.Has(g.perm):
		return fmt.Sprintf("That needs the %s permission on %s", g.perm, r.Host())
	}
	return ""
}

// screenRefusal is why the server can't show the screen with the key, "" when it can.
func (r *Remote) screenRefusal(key string) string {
	what, ok := remoteScreens[strings.TrimSuffix(screenClass(key), "/")]
	if !ok {
		return ""
	}
	return capitalize(what) + " isn't on a remote server yet (mangal without --remote does it)"
}

// keyRefusal is why the server refuses the pressed key of the screen's bindings, "" when
// it doesn't (or it isn't one).
func (r *Remote) keyRefusal(screen string, keys help.KeyMap, msg tea.KeyPressMsg) string {
	for _, b := range flatten(keys) {
		if !key.Matches(msg, b) {
			continue
		}
		if g, ok := remoteGate(screen, b.Help().Desc); ok {
			return r.refusal(g)
		}
		return ""
	}
	return ""
}

func flatten(keys help.KeyMap) []key.Binding {
	var all []key.Binding
	for _, group := range keys.FullHelp() {
		all = append(all, group...)
	}
	return append(all, keys.ShortHelp()...)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// gatedKeyMap is a screen's keys without those the server refuses.
type gatedKeyMap struct {
	keys   help.KeyMap
	screen string
	r      *Remote
}

func (g gatedKeyMap) allowed(bs []key.Binding) []key.Binding {
	return slices.DeleteFunc(slices.Clone(bs), func(b key.Binding) bool {
		gt, ok := remoteGate(g.screen, b.Help().Desc)
		return ok && g.r.refusal(gt) != ""
	})
}

func (g gatedKeyMap) ShortHelp() []key.Binding { return g.allowed(g.keys.ShortHelp()) }

func (g gatedKeyMap) FullHelp() [][]key.Binding {
	var out [][]key.Binding
	for _, group := range g.keys.FullHelp() {
		out = append(out, g.allowed(group))
	}
	return out
}
