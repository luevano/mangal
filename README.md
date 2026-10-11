# Mangal

> Work in progress...

Fork of [metafates/mangal@v5](https://github.com/metafates/mangal/tree/v5).

Version 5 is now based on metafates' [mangalorg](https://github.com/mangalorg) suite, which I've forked to work on them myself:

- [libmangal](https://github.com/luevano/libmangal): the engine (providers, downloads, metadata)
- [mangoprovider](https://github.com/luevano/mangoprovider): native Go providers, using [mangodex](https://github.com/luevano/mangodex) and [mangoplus](https://github.com/luevano/mangoplus)
- [luaprovider](https://github.com/luevano/luaprovider): Lua script providers, using [gopher-luadoc](https://github.com/luevano/gopher-luadoc)
- [saturno](https://github.com/luevano/saturno): Lua providers

## Install

Download a binary from the [releases](https://github.com/luevano/mangal/releases), or with Go:

```sh
go install github.com/luevano/mangal/v5@latest
```

Built with `go install`, `mangal web` has no web UI; release binaries include it (see [Development](#development), `-tags webui`).

## Development

For now, just notes for myself while I work on this.

### Requirements

The following needs to be installed before starting development:

- `go>=1.27`
  - [`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen): `go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest`
  - [`enumer`](https://github.com/dmarkham/enumer): `go install github.com/dmarkham/enumer@latest`
- [`just`](https://github.com/casey/just)
- `npm`

### Setup

Update modules:

```sh
just update
```

Install [openapi-typescript](https://www.npmjs.com/package/openapi-typescript):

```sh
cd web/ui
npm i -D openapi-typescript
npm audit fix
cd ../..
```

Generate assets and code:

```sh
just generate
```

Then it can be used without compiling for testing purposes:

```sh
go run main.go ...
```

Or compiled/installed:

```sh
just build # or full or install
./mangal
```

The web UI is only embedded when building with `-tags webui` (the `just` recipes do), after building it in `web/ui`.

Releases are built by [GoReleaser](https://goreleaser.com) when a `v*` tag is pushed (see `.goreleaser.yaml`), `meta.Version` has to match the tag.

## Usage

By default if just executing the `mangal` command, it will start the TUI. To list all options:

```sh
mangal -h
```

**Note:** If the previous mangal (v4) was used, then remove the config (usually located at `~/.config/mangal`) as otherwise these commands will fail.

### Config

Mangal comes with sensible defaults that can be changed in the config file, with environment variables (`MANGAL_` and the key in upper case with `_` for `.`, e.g. `MANGAL_DOWNLOAD_PATH`) or with some command flags. By default no config is written to disk, to write the default config to disk run:

```sh
mangal config write
```

It will be written to `$XDG_CONFIG_HOME/mangal/mangal.toml`.

Then, the config can be set by modifying the file itself, from the TUI's config window (`,` on any screen, changes apply right away), or by running:

```sh
mangal config set <key> <value>
```

For example:

```sh
mangal config set download.path $HOME/downloads_test
```

### Providers

These are either the native Go implementations already included (from [mangoprovider](https://github.com/luevano/mangoprovider)) or Lua scripts that handle the site scrape logic (search mangas, list mangas/chapters/images, etc). List them with:

```sh
mangal providers ls
```

When a provider is renamed (e.g. MangaSee is now Weeb Central, `mango-weebcentral`), its old id still works with a warning. Data saved under the old id can be moved to the new one with `mangal migrate` (lists the changes, `--apply` does them), or with `M` on the TUI home.

#### Lua providers

Some Lua providers are available at [saturno](https://github.com/luevano/saturno), mostly as examples since `mangoprovider` already covers those sites.

Each Lua provider is a directory with the `main.lua` (scraper) and `mangal.toml` (metadata) files inside. By default, they need to be placed in `$XDG_CONFIG_HOME/mangal/providers`. Old ids of a renamed provider go in `mangal.toml` as `[[history]]` entries.

A new provider can be created by running:

```sh
mangal providers new
```

It is automatically placed in the required path.

### Downloads

Downloaded chapters are named with the `download.*.name_template` config templates (see [Templates](#templates)). Each manga directory gets a `.libmangal.json` so it's found again when its name changes (title updates, renamed providers): new chapters keep going into the same directory. To rename the existing directories to what the current templates give:

```sh
mangal library rename          # list the renames
mangal library rename --apply  # do them
```

The cache and mangal's data (Anilist title bindings, logins, search history, the library) are kept in SQLite databases, see `mangal path`. The cache can be cleared with `mangal cache clear [provider]`. Logins and secrets can be kept in the OS keyring instead with `auth.keyring`.

### Library

mangal keeps track of what's downloaded, whichever provider got it: a chapter already in a manga's directory (same number and format) isn't downloaded again, and the reason is shown.

```sh
mangal library ls                          # the mangas
mangal library scan                        # check against the files: moved, removed, added by hand, older downloads
mangal library merge <keep> <other> --apply  # one manga in two directories, duplicates go to the trash
mangal library trash [empty]               # see or empty the trash
```

The TUI has the same under `l` on the home screen, where `d` deletes a manga or a chapter (to the trash).

### Accounts

```sh
mangal accounts
```

Shows every account mangal can use, logged in or not: Anilist, Kitsu, MyAnimeList and the provider accounts (e.g. the MangaPlus subscription), `a` on the TUI home. From there you can log into Anilist, Kitsu or MyAnimeList or set the MangaPlus secret. mangal tells you when the Anilist login expires. Without a browser on the same machine, `mangal anilist login` and `mangal myanimelist login` print the URL to open and take the redirected URL pasted back.

Metadata comes from [Anilist](https://anilist.co), then [Kitsu](https://kitsu.app) when Anilist finds nothing (`metadata.kitsu.enabled`), or the provider, and can be written as `ComicInfo.xml` (in CBZ files), `series.json`, cover and banner, see the `download.metadata.*` config. Log in with `mangal anilist` to also sync the reading progress when reading from the TUI; `mangal kitsu` and `read.history.kitsu = true` do the same for Kitsu. MyAnimeList needs an API client id of your own: register an app at https://myanimelist.net/apiconfig (App Type "other", redirect URL `http://localhost:6969/oauth/mal/callback`), set `metadata.myanimelist.client_id`, then `mangal myanimelist` and `read.history.myanimelist = true`; it's searched last for metadata. Chapters read from the TUI are kept in a local history (`mangal history`, `read.history.local`).

### Headless

A watch is a manga mangal keeps up to date: each check downloads the chapters from the last one
the library has up (and, with gap filling, the missing ones below it).

```sh
mangal watch add mango-mangapill "One-Punch Man" --from 240   # chapters up to 240 count as read
mangal watch add mango-mangaplus mid:100171 --anilist-id 132029
mangal watch ls                  # last check, next check, errors
mangal watch check [--dry-run]   # check now (--provider <id>, --due)
mangal watch log --since 7d      # what each check got
mangal notify webhook            # Discord message per manga with new chapters (watch.notify)
```

`mangal serve` keeps running and checks the watches on a cron schedule in local time
(`watch.schedule`, `0 */4 * * *` = every 4 hours on the hour by default; also `@hourly`, `@daily`,
`@weekly`, `@every 90m`; a watch can have its own: `watch add/set --schedule`). The ticks don't
depend on when it started: a watch is due once a tick passed since its last check (a tick missed
while it was down runs once at start), failing watches skip ticks (`watch.backoff`), gap filling
has its own schedule (`watch.gap_schedule`, off by default). Each provider has its own run
(`watch.parallel_providers` at once, a provider never twice, also against a `mangal watch check`
elsewhere); the `watch.*` settings are re-read from the config file
every minute. It serves a JSON API on `127.0.0.1:6977` (`serve.host`, `serve.port`) that the TUI
and the `mangal watch` commands use while it runs; `--web` (`serve.web`) also serves the web UI
there. On an address that isn't loopback the API needs a token: set `serve.token` (or
`MANGAL_SERVE_TOKEN`) and send `Authorization: Bearer <token>`.

```sh
curl 127.0.0.1:6977/api/status
curl 127.0.0.1:6977/api/watches
curl -X POST 127.0.0.1:6977/api/check -d '{"ids": [1]}'     # 202 {"run": N}, 409 when its provider is busy
curl 127.0.0.1:6977/api/runs/N
curl -N 127.0.0.1:6977/api/events                         # live progress (server-sent events)
```

As a systemd service: [`mangal.service`](mangal.service) (copy it to `/etc/systemd/system/`, set
`User` and the mangal path, `systemctl enable --now mangal`; logs in `journalctl -u mangal`).

One server per config: a second instance (e.g. other mangas into another directory) uses another
config file with its own `data.path`, `download.path` (and `cache.path`) and `serve.port`, and
everything is run with `--config`: `ExecStart=/usr/local/bin/mangal --config /etc/mangal/b.toml
serve`, `mangal --config /etc/mangal/b.toml watch add ...`.

### Templates

Special functions are available and can be shown by running:

```sh
mangal templates funcs
```

Run a template with custom functions available:

```sh
mangal templates exec <template> -v <values>
```

Where `<template>` is in the form of a go [text/template](https://pkg.go.dev/text/template) and `<values>` a JSON. For example:

```sh
mangal templates exec "{{sanitizeWindows .Test}}" -v "{\"Test\":\"asdfg<>:?\"}"
```

Which will output `asdfg`.

### Modes

#### Inline

Similar to mangal v4 inline mode. Two submodes available, `download` which is the expected option and `json` to fetch the metadata only useful to see what would be downloaded.

##### JSON

The minimal usage only fetches mangas found in the provider:

```sh
mangal inline json -p <provider-id> -q <query>
```

A more advanced use that fetches just some chapters with anilist output:

```sh
mangal inline json -p saturno-mangapill -q "Tengoku Daimakyou" -m exact -c 10-15 --chapter-populate
```

For more, use the `-h` flag.

##### Download

Works exactly like the `json` option but needs to select only one manga and has couple different flags:

```sh
mangal inline download -p saturno-mangapill -q "Tengoku Daimakyou" -m exact -c 10-15 -f CBZ -d "/custom/manga/root/path"
```

For more, use the `-h` flag.

#### Web

Serves a browse-only web UI (search providers, see mangas and chapters; no downloads or library),
on `127.0.0.1:6969` by default. `--host 0.0.0.0` serves it to the network, without any authentication.

```sh
mangal web --open
```

#### Script

Similar to [mangalorg/mangalcli](https://github.com/mangalorg/mangalcli) where a `run.lua` and a provider are required:

```sh
mangal script -f run.lua -p mango-mangapill -v "title=tengoku"
```

For example, with this `run.lua`:

```lua
local mangal = require('mangal')
local client = mangal.client
local fzf = mangal.prompt.fzf
local json = mangal.json

local mangas = client.search_mangas(Vars.title) -- search with the given title
local volumes = client.manga_volumes(fzf.find_one(mangas, function(manga)
  return manga:info().title
end)) -- select the first manga

local chapters = {}

-- get all chapters of the manga
for _, volume in ipairs(volumes) do
  for _, chapter in ipairs(client.volume_chapters(volume)) do
    table.insert(chapters, chapter)
  end
end

-- chapters encoded in json format for later use, e.g. pipe to jq
json.print(chapters)
```
