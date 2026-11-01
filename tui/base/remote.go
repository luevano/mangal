package base

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/remote"
	"github.com/luevano/mangal/v5/path"
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
	// ShowAll shows the actions the permissions don't allow, the server refuses them
	// (serve.hide_unavailable false, as the web UI does).
	ShowAll bool
	// Services are the server's.
	Services core.Services
	// Loaders are the server's providers, browsed through it.
	Loaders func(ctx context.Context) ([]libmangal.ProviderLoader, error)
	// Client is the server's API: reading fetches the chapters with it.
	Client *remote.Client
	// Events tells fn the topics (storage.Rev*) the server says changed, until ctx is
	// done: the shown State's DBChangedMsg.
	Events func(ctx context.Context, fn func(topics []string))
}

// CurrentRemote is the server the TUI runs on (set by the tui package), nil for this
// machine.
var CurrentRemote *Remote

// ReadCache is where the chapters read from the server go (remote.read_cache). It reads
// the config: not from a background goroutine.
func ReadCache() remote.ReadCache {
	dir := config.Remote.ReadCache.Get()
	if dir == "" {
		dir = filepath.Join(path.Cache(), "remote")
	}
	return remote.ReadCache{Dir: dir, Max: config.Remote.ReadCacheSize.Get()}
}

// Has is true when the server allows the permission.
func (r *Remote) Has(perm string) bool { return slices.Contains(r.Perms, perm) }

// Can is true when the permission lets this ask for what needs it: always on this machine.
func Can(perm string) bool {
	return CurrentRemote == nil || CurrentRemote.Has(perm)
}

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

// gate is what a screen's action needs on a remote server: a permission (or any of
// anyPerm), a feature of the server; or why a remote can't do it.
type gate struct {
	perm, feature string
	anyPerm       []string
	impossible    string
}

// remoteGates are the actions a remote server may refuse, by "screen:help" (the screen's
// key up to its first slash, with the slash: "library/" is a library manga's), else
// "*:help". The server enforces the permissions anyway: these hide what it would refuse.
var remoteGates = map[string]gate{
	"home:accounts":     {anyPerm: []string{core.ScopeProgress, core.ScopeTokens, core.ScopeAdmin}, feature: "accounts"},
	"home:health":       {perm: core.ScopeManage, feature: "health"},
	"home:migrate":      {perm: core.ScopeAdmin, feature: "migrate"},
	"home:read history": {feature: "read history"},

	"library:scan":                {perm: core.ScopeManage, feature: "library scan"},
	"library:scan and check ids":  {perm: core.ScopeManage, feature: "library scan"},
	"library:merge":               {perm: core.ScopeManage, feature: "merge"},
	"library:move to a provider":  {perm: core.ScopeManage, feature: "move"},
	"library:trash":               {perm: core.ScopeManage, feature: "trash"},
	"library:delete":              {perm: core.ScopeManage, feature: "delete"},
	"library:undo":                {perm: core.ScopeManage, feature: "undo"},
	"library:duplicates only":     {perm: core.ScopeManage, feature: "merge"},
	"library:bindings":            {feature: "links"},
	"library:fill backups":        {perm: core.ScopeManage, feature: "backup links"},
	"library:rename":              {perm: core.ScopeAdmin, feature: "rename"},
	"library:hold / new provider": {perm: core.ScopeManage, feature: "hold marks"},

	"library/:open": {feature: "read"},

	"providers:mirrors": {perm: core.ScopeAdmin, feature: "mirrors"},

	"chapters/:download":                  {perm: core.ScopeDownload, feature: "download"},
	"chapters/:get unavailable elsewhere": {perm: core.ScopeManage, feature: "get unavailable chapters elsewhere"},
	"library/:scan":                       {perm: core.ScopeManage, feature: "library scan"},
	"library/:delete":                     {perm: core.ScopeManage, feature: "delete"},
	"library/:watch":                      {perm: core.ScopeManage, feature: "watches"},
	"library/:bindings":                   {feature: "links"},
	"library/:hold / new provider":        {perm: core.ScopeManage, feature: "hold marks"},
	"library/:retitle chapters":           {perm: core.ScopeManage, feature: "retitle"},

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
	"*:re-link manga":             {perm: core.ScopeManage, feature: "watches"},
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
	"download/:open directory":    {impossible: "A server's folder isn't on this machine"},

	"*:jobs":                 {feature: "maintenance jobs"},
	"jobs:run all":           {perm: core.ScopeManage, feature: "maintenance jobs"},
	"jobs:run missing":       {perm: core.ScopeManage, feature: "maintenance jobs"},
	"jobs:pause/resume":      {perm: core.ScopeAdmin, feature: "maintenance jobs"},
	"jobs:edit":              {perm: core.ScopeAdmin, feature: "maintenance jobs"},
	"jobs/:edit":             {perm: core.ScopeAdmin, feature: "maintenance jobs"},
	"jobs/:on/off":           {perm: core.ScopeAdmin, feature: "maintenance jobs"},
	"jobs/:reset to default": {perm: core.ScopeAdmin, feature: "maintenance jobs"},
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
	case g.impossible != "":
		return g.impossible
	case g.feature != "" && !slices.Contains(r.Features, g.feature):
		return fmt.Sprintf("The server (mangal %s) doesn't have %s: update it", r.Version, g.feature)
	case g.perm != "" && !r.Has(g.perm) && !r.ShowAll:
		return fmt.Sprintf("That needs the %s permission on %s", g.perm, r.Host())
	case len(g.anyPerm) != 0 && !slices.ContainsFunc(g.anyPerm, r.Has) && !r.ShowAll:
		return fmt.Sprintf("That needs one of the %s permissions on %s", strings.Join(g.anyPerm, ", "), r.Host())
	}
	return ""
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
