package config

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/theme/icon"
)

// memStore is a Store in memory.
type memStore struct {
	values  map[string]string
	rev     int64
	done    bool
	history []string
}

func (m *memStore) All() (map[string]string, int64, error) { return maps.Clone(m.values), m.rev, nil }
func (m *memStore) Revision() (int64, error)               { return m.rev, nil }
func (m *memStore) Imported() (bool, error)                { return m.done, nil }
func (m *memStore) Set(key string, value *string, _ bool, who string) error {
	if value == nil {
		delete(m.values, key)
	} else {
		m.values[key] = *value
	}
	m.rev++
	m.history = append(m.history, who+" "+key)
	return nil
}

func useMem(t *testing.T, values map[string]string) *memStore {
	t.Helper()
	m := &memStore{values: values, done: true}
	if m.values == nil {
		m.values = map[string]string{}
	}
	if err := UseStore(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestStoredKeys(t *testing.T) {
	reset(t)
	writeConfig(t, "icons = 'nerd'\n[download]\nformat = 'cbz'\npath = '/file/lib'\n[providers.mirrors]\nmango-x = 'https://x.example/'\n[providers.mangaplus]\nquality = 'low'\nandroid_id = 'abc'\n")
	if err := Load(Path); err != nil {
		t.Fatal(err)
	}
	if len(Unsupported()) != 0 {
		t.Errorf("unsupported %v", Unsupported())
	}
	// the moved keys are ignored, not even before the database opens
	if Download.Format.Get() != libmangal.FormatPDF || len(Providers.Mirrors.Get()) != 0 {
		t.Errorf("moved keys read from the file: %s %v", Download.Format.Get(), Providers.Mirrors.Get())
	}
	var moved []string
	for _, m := range MovedInFile() {
		moved = append(moved, m.Key+">"+m.NewKey)
	}
	if want := []string{"download.format>download.format", "download.path>download.path", "providers.mangaplus.android_id>providers.mango-mangaplus.android_id", "providers.mangaplus.quality>providers.mango-mangaplus.quality", "providers.mirrors>providers.mirrors"}; !slices.Equal(moved, want) {
		t.Errorf("moved %v", moved)
	}
	values, skipped := MovedValues()
	if len(skipped) != 0 || values["download.format"] != `"CBZ"` && values["download.format"] != `"cbz"` || values["providers.mango-mangaplus.quality"] != `"low"` || values["providers.mirrors"] != `{"mango-x":"https://x.example/"}` {
		t.Errorf("moved values %v %v", values, skipped)
	}
	if !MovedSecret("providers.mango-mangaplus.android_id") || MovedSecret("download.path") {
		t.Error("moved secrets")
	}

	if err := WriteKey(Download.Format.Key, "CBZ"); !errors.Is(err, ErrNoStore) {
		t.Errorf("write without the database: %v", err)
	}
	m := useMem(t, map[string]string{"download.format": `"CBZ"`, "watch.gaps": "true"})
	if Download.Format.Get() != libmangal.FormatCBZ || !Watch.Gaps.Get() || Icons.Get() != icon.TypeNerd {
		t.Errorf("stored: %s %v %s", Download.Format.Get(), Watch.Gaps.Get(), Icons.Get())
	}
	for key, want := range map[string]string{"download.format": "db", "download.path": "default", "icons": "file", "watch.jitter": "default"} {
		if got := SourceOf(key); got.Kind != want {
			t.Errorf("%s from %v, want %s", key, got, want)
		}
	}
	t.Setenv("MANGAL_DOWNLOAD_FORMAT", "pdf")
	if got := SourceOf("download.format"); got.Kind != "env" || got.Name != "MANGAL_DOWNLOAD_FORMAT" || Download.Format.Get() != libmangal.FormatPDF {
		t.Errorf("env over the database: %v %s", got, Download.Format.Get())
	}
	os.Unsetenv("MANGAL_DOWNLOAD_FORMAT")

	// written to the database, the file untouched; the default unsets it
	before := readConfig(t)
	Who = "test"
	if err := Download.Path.Write("/db/lib"); err != nil {
		t.Fatal(err)
	}
	if err := Watch.Gaps.Write(false); err != nil {
		t.Fatal(err)
	}
	if readConfig(t) != before || m.values["download.path"] != `"/db/lib"` || Download.Path.Get() != "/db/lib" {
		t.Errorf("written: %v\n%s", m.values, readConfig(t))
	}
	if _, set := m.values["watch.gaps"]; set || !slices.Contains(m.history, "test watch.gaps") {
		t.Errorf("default not unset: %v %v", m.values, m.history)
	}
	// another process changed it: Fresh sees it, Get keeps this process's view
	m.values["watch.schedule"], m.rev = `"@daily"`, m.rev+1
	if got := Watch.Schedule.Fresh(); got != "@daily" {
		t.Errorf("fresh %q", got)
	}
	if got, _ := FileValue("watch.schedule"); got != "@daily" {
		t.Errorf("stored value %v", got)
	}

	// config write leaves the moved entries alone and adds no database keys
	if err := Write(); err != nil {
		t.Fatal(err)
	}
	file := readConfig(t)
	if !strings.Contains(file, "format = 'cbz'") || !strings.Contains(file, "quality = 'low'") || strings.Contains(file, "[watch") || !strings.Contains(file, "# port = 6977") {
		t.Errorf("written:\n%s", file)
	}
	// config clean drops them once imported
	stale, err := StaleKeys()
	if err != nil || len(stale.Moved) != 5 || stale.MovedKept {
		t.Fatalf("stale %+v %v", stale, err)
	}
	if _, err := Clean(stale); err != nil {
		t.Fatal(err)
	}
	file = readConfig(t)
	for _, gone := range []string{"format = 'cbz'", "quality", "android_id", "mango-x", "/file/lib"} {
		if strings.Contains(file, gone) {
			t.Errorf("%q not dropped:\n%s", gone, file)
		}
	}
	if !strings.Contains(file, "icons = 'nerd'") {
		t.Errorf("file key dropped:\n%s", file)
	}
}

func TestCleanKeepsMovedBeforeImport(t *testing.T) {
	reset(t)
	writeConfig(t, "[download]\nformat = 'cbz'\n")
	if err := Load(Path); err != nil {
		t.Fatal(err)
	}
	stale, err := StaleKeys()
	if err != nil || len(stale.Moved) != 1 || !stale.MovedKept || !stale.Empty() {
		t.Fatalf("stale %+v %v", stale, err)
	}
}

func TestProviderSettingKeys(t *testing.T) {
	reset(t)
	m := useMem(t, map[string]string{"providers.mango-dex.prefer_groups": `"C, D"`, "providers.gone.x": `"kept"`})
	RegisterProviders([]libmangal.ProviderInfo{{ID: "mango-dex", Settings: []libmangal.ProviderSetting{
		{Key: "data_saver", Kind: libmangal.SettingBool, Default: "false"},
		{Key: "prefer_groups", Kind: libmangal.SettingList},
		{Key: "quality", Kind: libmangal.SettingEnum, Values: []string{"low", "high"}, Default: "high", Warning: "careful"},
		{Key: "secret_file", Kind: libmangal.SettingPath, Default: "~/s"},
		{Key: "token", Kind: libmangal.SettingSecret},
	}}})
	if !Exists("providers.mango-dex.quality") || StoredIn("providers.mango-dex.quality") != StoredDB || !IsSecret("providers.mango-dex.token") || KindOf("providers.mango-dex.data_saver") != KindBool {
		t.Error("registered keys")
	}
	if got := Values("providers.mango-dex.quality"); !slices.Equal(got, []string{"low", "high"}) {
		t.Errorf("values %v", got)
	}
	if d, _ := Description("providers.mango-dex.quality"); !strings.Contains(d, "careful") {
		t.Errorf("description %q", d)
	}
	t.Setenv("MANGAL_PROVIDERS_MANGO_DEX_DATA_SAVER", "true")
	values := ProviderValues("mango-dex")
	if values["data_saver"] != "true" || values["prefer_groups"] != "C, D" || values["quality"] != "high" || values["secret_file"] != filepath.Join(xdg.Home, "s") {
		t.Errorf("values %v", values)
	}
	if _, err := Parse("providers.mango-dex.quality", "max"); err == nil {
		t.Error("invalid enum taken")
	}
	if err := WriteKey("providers.mango-dex.quality", "low"); err != nil || m.values["providers.mango-dex.quality"] != `"low"` {
		t.Errorf("write: %v %v", err, m.values)
	}
	if err := WriteKey("providers.mango-dex.data_saver", true); err == nil {
		// the variable wins in this process, the database has it
		if m.values["providers.mango-dex.data_saver"] != "true" {
			t.Errorf("bool stored %v", m.values)
		}
	} else {
		t.Error(err)
	}
	if err := Validate(); err != nil {
		t.Error(err)
	}
	// a provider that went keeps its values in the database, its keys go
	RegisterProviders(nil)
	if Exists("providers.mango-dex.quality") || m.values["providers.mango-dex.quality"] == "" {
		t.Error("unregistered")
	}
}

func TestEnvFiles(t *testing.T) {
	reset(t)
	secret := filepath.Join(dir, "hook")
	if err := os.WriteFile(secret, []byte("https://discord.com/api/webhooks/1/abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MANGAL_NOTIFICATION_DISCORD_WEBHOOK_URL_FILE", secret)
	t.Setenv("MANGAL_DOWNLOAD_PAHT", "/x")
	t.Setenv("MANGAL_CONFIG", "/c.toml")
	writeConfig(t, "")
	if err := Load(Path); err != nil {
		t.Fatal(err)
	}
	if got := Notification.Discord.WebhookURL.Get(); got != "https://discord.com/api/webhooks/1/abc" {
		t.Errorf("from the file: %q", got)
	}
	if name, set := EnvVar("notification.discord.webhook_url"); !set || name != "MANGAL_NOTIFICATION_DISCORD_WEBHOOK_URL_FILE" {
		t.Errorf("env var %s %v", name, set)
	}
	if unknown := UnknownEnv(); len(unknown) != 1 || unknown[0].Key != "MANGAL_DOWNLOAD_PAHT" || unknown[0].Suggestion != "MANGAL_DOWNLOAD_PATH" {
		t.Errorf("unknown %v", unknown)
	}
	t.Setenv("MANGAL_NOTIFICATION_DISCORD_WEBHOOK_URL", "https://discord.com/api/webhooks/2/x")
	readEnvFiles(Keys())
	if err := Validate(); err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("both set: %v", err)
	}

	t.Setenv("MANGAL_API_TOKEN_FILE", secret)
	if _, err := EnvSecret("MANGAL_API_TOKEN"); err != nil {
		t.Error(err)
	}
}

func TestEnvClash(t *testing.T) {
	reset(t)
	t.Setenv("MANGAL_PROVIDERS_MANGO_X_Q", "a")
	RegisterProviders([]libmangal.ProviderInfo{
		{ID: "mango-x", Settings: []libmangal.ProviderSetting{{Key: "q", Kind: libmangal.SettingString}}},
		{ID: "mango_x", Settings: []libmangal.ProviderSetting{{Key: "q", Kind: libmangal.SettingString}}},
	})
	if _, set := EnvVar("providers.mango-x.q"); set {
		t.Error("clashing variable used")
	}
	if v := ProviderValues("mango-x")["q"]; v != "" {
		t.Errorf("clashing value %q", v)
	}
}
