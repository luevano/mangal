# Mangal

> Work in progress...

Fork of [metafates/mangal@v5](https://github.com/metafates/mangal/tree/v5).

Version 5 is now based on metafates' [mangalorg](https://github.com/mangalorg) suite, which I've forked to work on them myself:

- [libmangal](https://github.com/luevano/libmangal): the engine (providers, downloads, metadata)
- [mangoprovider](https://github.com/luevano/mangoprovider): native Go providers, using [mangodex](https://github.com/luevano/mangodex) and [mangoplus](https://github.com/luevano/mangoplus)
- [luaprovider](https://github.com/luevano/luaprovider): Lua script providers, using [gopher-luadoc](https://github.com/luevano/gopher-luadoc)
- [saturno](https://github.com/luevano/saturno): Lua providers

## Features

- Search one provider or several at once, by title or by id; download as CBZ, PDF, ZIP, TAR, images, with ComicInfo.xml/series.json metadata from AniList, Kitsu or MyAnimeList.
- Four ways to use it: the [TUI](#usage), the CLI ([inline](#inline), most commands), Lua [scripts](#script) and the [web UI](#web) (with a reader), all on the same data.
- A [library](#library) of what's downloaded, from any provider: scan, merge, move to another provider, rename, link metadata ids, hold marks, trash and undo.
- [Unavailable and retired chapters](#unavailable-and-retired-chapters): list them, get them from another provider, give the files the right titles.
- [Watches](#headless): mangas kept up to date by `mangal serve` on a schedule, with Discord notifications.
- [Jobs](#jobs): the maintenance runs (watch checks, source checks, source addresses, scan, health) with their schedules and per-provider settings.
- [Health](#health): problems of the library and the watches, each with its fix.
- `mangal serve`: the web UI, a JSON API and OPDS, with [users](#users) (roles, their own history and tracker accounts) and API tokens.

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

In the TUI `?` shows the keys of each screen and `,` opens the config window from any of them. A key means one thing on every screen, `d` deletes (and confirms the delete). On the home screen: `L` library, `W` watches, `K` accounts, `X` migrate, `!` health, `alt+h` read history, `alt+j` jobs; anywhere: `Q` downloads, ctrl+l logs; on the providers: `space` ticks several to search at once, `alt+i` searches by id; on a manga: `A` its metadata bindings, `w` watch it; on its chapters: `alt+g` gets the unavailable ones from another provider. In the watches: `u` check, `p` pause, `e` edit (`b` re-links the manga), `d` stop watching, `F` fill gaps, `O` live log, `V` every check, ctrl+r refresh; lists keep `g`/`G` (start/end), `h`/`l` (pages) and `/` (filter).

**Note:** A v4 config (usually `~/.config/mangal/mangal.toml`) doesn't work with v5: its keys are ignored with a warning, remove it.

### Config

Mangal comes with sensible defaults. What drives mangal on this machine (paths, logging, `mangal serve`'s address and security, the TUI's look, the reader) is set in the config file; what mangal does with the library, providers, metadata, watches, jobs and notifications is stored in mangal's data database, so every mangal sharing it (TUI, CLI, `mangal serve`) does the same; the providers' own settings (MangaDex's groups, MangaPlus' quality, a Lua provider's `[[settings]]`) are stored there too, as `providers.<id>.<key>`. `mangal config` reads and changes both. Any key can also come from an environment variable (`MANGAL_` and the key in upper case with `_` for `.` and `-`, e.g. `MANGAL_DOWNLOAD_PATH`, `MANGAL_PROVIDERS_MANGO_MANGAPLUS_QUALITY`; a secret also from a file, `MANGAL_<KEY>_FILE`, the safer way) or from some command flags: a flag wins over the variable, which wins over the stored value. `MANGAL_CONFIG`, `MANGAL_DEBUG` and `MANGAL_VERBOSE` are the `--config`, `--debug` and `--verbose` flags. By default no config is written to disk, to write the default config to disk run:

```sh
mangal config write
```

It will be written to `$XDG_CONFIG_HOME/mangal/mangal.toml`, every key the file holds and doesn't set listed as a commented default (`# port = 6977`): still the default, it follows mangal's when that changes. Keys mangal doesn't know (removed ones, typos) or no longer uses get a warning; `mangal config clean` lists them and the values that are the default, `--apply` drops the stale keys and comments the defaults out (a copy of the file is kept).

An older config file's keys that are stored in the database now are copied there the first time mangal runs (`mangal library undo` reverts it); after that their entries in the file aren't used (a notice says so) and `mangal config clean --apply` drops them.

Then, the config can be set by modifying the file itself, from the TUI's config window (`,` on any screen, changes apply right away), or by running:

```sh
mangal config set <key> <value>
```

For example:

```sh
mangal config set download.path $HOME/downloads_test
```

Secrets aren't config: the Anilist, Kitsu and MyAnimeList logins, their API clients (the MyAnimeList one with `mangal myanimelist client`, the Anilist one is kept from the last login), the MangaPlus secret and the Discord webhook (`mangal notify webhook`) are saved in mangal's data (or the OS keyring with `auth.keyring`), as are the `mangal serve` users and API tokens. The `notification.discord.webhook_url` and `providers.mango-mangaplus.secret_file` keys are only used when none is saved; a `metadata.myanimelist.client_id` left in an old config is moved to mangal's data once.

### Providers

These are either the native Go implementations already included (from [mangoprovider](https://github.com/luevano/mangoprovider)) or Lua scripts that handle the site scrape logic (search mangas, list mangas/chapters/images, etc). List them with:

```sh
mangal providers ls
```

When a provider is renamed (e.g. MangaSee is now Weeb Central, `mango-weebcentral`), its old id still works with a warning. Data saved under the old id can be moved to the new one with `mangal migrate` (lists the changes, `--apply` does them), or with `X` on the TUI home. Moved watches need a re-link (the new site may use other manga ids): the scheduler skips them until a check finds the manga or `mangal watch move <id>` (`b` in the TUI watches, `N` lists them) points them to it.

#### Search

Several providers at once, each one's results shown as it answers (every installed provider without `-p`):

```sh
mangal search dark gathering -p mango-mangadex,mango-mangapill
```

In the TUI, `space` ticks providers on the providers screen and enter searches the ticked ones; the web UI has "Search several" on Providers. A manga can also be picked by its id instead of a title: `--id <id>` on `watch add`, `watch move`, `library move`, `bind` and `inline`, `alt+i` in the TUI, "By id" in the web UI.

#### Unavailable and retired chapters

`providers.filter.show_unavailable_chapters` lists the chapters a provider can't download (external, no pages), `providers.filter.show_retired_chapters` the ones it took down (MangaDex): shown as unavailable "retired" with their titles, never downloaded. `mangal inline` has `--show-retired-chapters` too.

```sh
mangal config set providers.filter.show_retired_chapters true
```

To get them anyway, see [Getting unavailable chapters](#getting-unavailable-chapters).

#### Lua providers

Some Lua providers are available at [saturno](https://github.com/luevano/saturno), mostly as examples since `mangoprovider` already covers those sites.

Each Lua provider is a directory with the `main.lua` (scraper) and `mangal.toml` (metadata) files inside. By default, they need to be placed in `$XDG_CONFIG_HOME/mangal/providers`. Old ids of a renamed provider go in `mangal.toml` as `[[history]]` entries. Request rate limits go there too: `rate_limits = [{ rate = "1/2s", match = "site.com", burst = 1 }]` (`match`, a host with an optional path prefix, and `burst` are optional). `icon = "https://..."` sets the site icon the web UI shows, when the website's `/favicon.ico` isn't it (`mangal serve` fetches and caches the icons, none are shipped).

A new provider can be created by running:

```sh
mangal providers new --id mysite --website https://example.com
```

It is placed in the providers path.

### Downloads

Downloaded chapters are named with the `download.*.name_template` config templates (see [Templates](#templates)). Each manga directory gets a `.libmangal.json` so it's found again when its name changes (title updates, renamed providers): new chapters keep going into the same directory. To rename the existing directories to what the current templates give:

```sh
mangal library rename             # list the renames
mangal library rename --apply     # do them
mangal library rename --refresh   # with fresh AniList metadata (titles changed there)
mangal library rename --chapters  # also rename the chapter files to the chapter template
mangal library rename --fallback  # also folders whose metadata isn't found (drops year and ids)
```

The folder names come from each library manga's links (`mangal library bind`, below), which win over the ids in `.libmangal.json` and the folder name.

The cache and mangal's data (logins, metadata and title bindings, search and read history, the library, watches) are kept in SQLite databases, see `mangal path`. The cache can be cleared with `mangal cache clear [provider]`.

Only one download per provider runs at a time, across every mangal on the same data (TUI, inline, scripts, watch checks): another one waits and says why.

### Library

mangal keeps track of what's downloaded, whichever provider got it: a chapter already in a manga's directory (same number and format) isn't downloaded again, and the reason is shown.

```sh
mangal library ls [--refresh]              # the mangas, with their publication status (finished, ongoing, hiatus...; --refresh asks AniList)
mangal library scan                        # check against the files: moved, removed, added by hand, older downloads; lists mangas sharing a metadata id
mangal library scan --check                # also check the guessed metadata ids of folders without a .libmangal.json
mangal library scan --mark [--apply]       # also list (--apply: write) a .libmangal.json for folders without one (from the library source, else the provider directory and ids)
mangal library mark <id>... [--apply]      # (re)write the .libmangal.json of these mangas: also replaces a broken one or one of another provider manga
mangal library downloads [provider]        # the downloaded chapters
mangal library merge <keep> <other> --apply  # one manga in two directories, duplicates go to the trash
mangal library move <id> <provider> [title|--id <id>] --apply  # switch a manga to another provider (folder, watch and bindings follow; names stay)
mangal library move <id> <provider> --full --apply          # also adopt the new provider's title and the folder name it gives
mangal library retitle <id> [--from <provider>] --apply      # the chapters get a provider's titles (below)
mangal library fill <id> --from <provider> --apply           # get unavailable chapters from another provider (below)
mangal library urls [--all]                # look the sources' web pages up (on the provider, by manga id); a source the provider lost is marked gone
mangal library hold <id> hiatus|ended|provider  # pause its watches (after a last check), or mark it as needing another provider; --clear
mangal library rm <id> [chapter] --apply   # delete a library manga (or one of its chapters) to the trash
mangal library bind <id> [--anilist N] [--kitsu N] [--mal N] [--rename] [--watches] --apply  # link a library manga (also without a provider source)
mangal library bind <id>                   # its links, where each comes from, and a Kitsu id to pick
mangal library bind [id] --backups --apply # fill the Kitsu and MyAnimeList links from AniList
mangal library bind <id> --dismiss         # link neither of the Kitsu ids to pick from
mangal library trash [empty]               # see or empty the trash
mangal library undo [--list] [--apply]     # undo the last operation: files moved back, database restored from its snapshot
```

When a MangaDex source links one Kitsu id and Kitsu maps the AniList id to another, `--backups` links neither and keeps both to pick from: `mangal library bind <id>`, `mangal health` and the TUI links screen (enter on the Kitsu row) show them until one is linked or dismissed.

The publication status comes from the AniList metadata mangal already has; `library ls --refresh`, `health --network` and the TUI's `S` ask AniList (batched).

A library manga's links replace the old ids everywhere: its sources' bindings, its own ids and its `.libmangal.json`, so a wrong id stops matching. What a link implies (the folder's new name, a watch bound to another id, another library manga with the id) is shown and only done when asked (`--rename`, `--watches`, merge).

Every operation that changes files (rename, merge, move, retitle, link, `scan --mark --apply`, `mark --apply`, `migrate`, a delete) first snapshots the data database into `<data dir>/backups/` (the last `data.backups`, 10 by default, are kept) and journals its file changes, so `mangal library undo` can revert it. Nothing is overwritten, and an undo that would lose later changes is refused unless `--force`.

The TUI has the same under `L` on the home screen (`u` scan, `S` scan and check ids, `J` merge, `D` duplicates only, `X` move to another provider, `A` links, `B` fill backup links, `t` trash, `U` undo the last operation, `alt+r` rename, `alt+p` hold), where `d` deletes a manga or a chapter (to the trash; a manga whose folder is gone is forgotten). On a library manga, `alt+t` retitles its chapters and `o` opens its source's web page (looked up when unknown; a manga its provider lost says so).

#### Getting unavailable chapters

When a source lists chapters it can't download (retired on MangaDex, external), the same chapters can come from another provider. Three ways:

**Fill directly**: the unavailable numbers the folder doesn't have are downloaded from another provider's manga into the manga's folder, with the source's titles and metadata. The folder's source doesn't change.

```sh
mangal library fill 12 --from mango-mangapill                     # plan: which chapters, from which manga
mangal library fill 12 --from mango-mangapill,mango-weebcentral   # search several, list the candidates
mangal library fill 12 --from mango-mangapill --id 2085/dark-gathering --chapters 1-67 --apply
mangal library fill --provider mango-mangadex --manga-id <id> --from mango-mangapill   # a provider manga, in the library or not
```

In the TUI: `alt+g` on a manga's chapters screen (when it has unavailable chapters) ticks the providers to search, then pick the manga, review and confirm; it runs as a download. In the web UI: "Get unavailable chapters from another provider" on the provider manga page.

**Download, merge, retitle**: download the chapters from the other provider as usual; when they land in another library manga, merge it into the first, then give the files the first one's titles:

```sh
mangal library merge 12 40 --apply   # 40's chapters move into 12's folder
mangal library retitle 12 --apply    # titles from 12's current source
```

**Retitle alone**: chapters already downloaded with poor titles ("Chapter 3") get a provider's titles for the same numbers, every chapter listed (retired ones too). TUI `alt+t` on the library manga, web "Retitle" on the library manga page, or:

```sh
mangal library retitle 12 --from mango-mangadex --apply
```

A CBZ's ComicInfo.xml records where the file and its title came from, as a line in its Notes (scans read it back):

```
mangal: provider=mango-mangapill manga_id=2085/dark-gathering chapter_url=https://... title_provider=mango-mangadex title_manga_id=7c145eb7-...
```

### Health

```sh
mangal health [--only watches,W10,...] [--json]  # problems of the library and the watches, with the command that fixes each
mangal health --only W10                         # the library mangas no watch keeps up to date, one by one
mangal health --network                          # also look for new chapters of the unwatched ongoing mangas on their providers
mangal health --network --only L11               # is each library manga still on its provider? (gone: `library move` it or `library hold <id> provider`)
mangal health --dismiss L6/library/trash         # hide a finding that's fine as it is (its key starts its line); --undismiss brings it back
mangal health --all                              # also the dismissed findings
```

Only reads (the library, its folders and their `.libmangal.json`); it makes no requests unless `--network` (the publication statuses and the folder names' metadata from AniList, and the chapter lists of the unwatched mangas that may still get chapters, one provider at a time like a watch check). Besides the watches and links it finds folders that are gone, folders without a `.libmangal.json` or with a broken one, provider directories under an old name, folder names off the template, what's in the trash, and mangas without chapters. In the TUI, `!` on the home screen (which shows "health: N problems"): enter opens what a finding is about (the watch, the library manga, its links, migrate, the trash), `x` applies its fix after asking (link the id the folder name says, pick a Kitsu id, resume a watch, watch a manga with new chapters, mark a folder, scan or forget a missing manga, rename, empty the trash), `J` merges duplicates, `z` dismisses a finding (`Z` shows the dismissed), `tab` shows one category, alt+n runs the network checks, `i` shows the whole finding, ctrl+r checks again. Its file changes are journaled like the library's (`U` in the library undoes them).

### Accounts

```sh
mangal accounts
```

Shows every account mangal can use, logged in or not: Anilist, Kitsu, MyAnimeList and the provider accounts (e.g. the MangaPlus subscription), `K` on the TUI home. The Anilist, Kitsu and MyAnimeList logins are this machine's [user](#users)'s: each user has their own (on the web UI: Accounts, "Your trackers"). From there you can log into Anilist, Kitsu or MyAnimeList, set the MangaPlus secret or the Discord webhook. mangal tells you when the Anilist login expires. Without a browser on the same machine, `mangal anilist login` and `mangal myanimelist login` print the URL to open and take the redirected URL pasted back. `mangal anilist login --pin` (or `metadata.anilist.redirect = "pin"`) needs no callback server at all: AniList shows the code to paste, for an Anilist API client registered with the redirect URL `https://anilist.co/api/v2/oauth/pin`.

Metadata comes from [Anilist](https://anilist.co), then [Kitsu](https://kitsu.app) when Anilist finds nothing (`metadata.kitsu.enabled`), then MyAnimeList when configured (below), or the provider, and can be written as `ComicInfo.xml` (in CBZ files), `series.json`, cover and banner, see the `download.metadata.*` config (AniList tags less relevant than `metadata.anilist.tag_rank_min`, 60 by default, are left out). Log in with `mangal anilist` to also sync the reading progress when reading from the TUI; `mangal kitsu` and `read.history.kitsu = true` do the same for Kitsu. MyAnimeList needs an API client id of your own: register an app at https://myanimelist.net/apiconfig (App Type "other", redirect URL `http://localhost:6969/oauth/mal/callback`), save its client id with `mangal myanimelist client` (or from `mangal myanimelist`, which asks for it), log in with `mangal myanimelist` and set `read.history.myanimelist = true`. Chapters read from the TUI are kept in a local history (`mangal history`, `read.history.local`).

A provider's manga can be bound to its Anilist, Kitsu and MyAnimeList ids (`A` on a manga in the TUI, or `mangal bind`; watches and the library share them). Downloads use the metadata of the first bound source in the order Anilist, Kitsu, MyAnimeList, the others are backups used when it can't be fetched; every bound id gets the reading progress.

```sh
mangal bind mango-mangapill "One-Punch Man" --anilist 74347 --kitsu 24147
mangal bind mango-mangaplus --id 100171 --remove kitsu
mangal bind ls
```

### Users

Everything a person records belongs to a mangal user: their read history, reading positions,
read marks, search history and their own Anilist, Kitsu and MyAnimeList logins (their reads sync
there). The library, watches, jobs, settings and the provider accounts are shared.

There's always a user. The first start makes `mangal`, an admin without a password: mangal on
this machine (the TUI, the CLI, scripts) is that user, nothing else to do on a desktop. A user
without a password can't log in from the web UI or another machine; give it one to log in, rename
it to yours:

```sh
mangal user rename mangal david       # their history, tokens and logins follow
mangal user passwd david              # now david can log into the web UI
mangal user add kid                   # a reader (--role downloader|manager|admin, --perms tokens)
mangal user roles --all               # what each role and permission allows
mangal user use kid                   # mangal on this machine is kid now (auth.user)
mangal user ls                        # roles, who has no password, this machine's user
mangal token add phone --perms read   # an API token (a program's, not a login)
mangal token perms phone --role reader
```

Roles: **reader** (reads the library, keeps their place), **downloader** (also searches the
sites, reads online and downloads with the server's settings), **manager** (also the watches,
library fixes, health, running a job now) and **admin** (everything: users, settings, the log,
library-wide operations). Any of them can also get `tokens` (make their own API tokens, never
with more than they have), or pick permissions one by one: read, progress, browse, download,
manage, tokens, admin. Readers and downloaders see paths relative to the library, never the
server's.

Which user mangal on this machine is: `auth.user` in mangal.toml (empty: the first admin);
`mangal user use`, `alt+u` on a user in the TUI's accounts screen (`K`), or "Server's user" on
the web UI's Users page. The same places rename (`alt+r`, Rename) and set roles and passwords.
Each user logs into their own trackers: the TUI/CLI as this machine's user (`mangal anilist`,
`mangal kitsu`, `mangal myanimelist`), anyone on the web UI's Accounts page (Anilist: open the
link, paste the code; MyAnimeList: paste the address it ends on; Kitsu: name and password). The
Anilist and MyAnimeList logins need the API client saved by a first login from mangal on the
server.

### Headless

A watch is a manga mangal keeps up to date: each check downloads the chapters from the last one
the library has up (and, with gap filling, the missing ones below it).

```sh
mangal watch add mango-mangapill "One-Punch Man" --from 240   # chapters up to 240 count as read
mangal watch add mango-mangaplus --id 100171 --anilist-id 132029
mangal watch ls                  # last check, next check, errors
mangal watch check [--dry-run]   # check now (--provider <id>, --due)
mangal watch log --since 7d      # what each check got
mangal watch log --rm 42         # delete run 42 from the history (library undo brings it back)
mangal watch set 12 --skip 189.5 # never download chapter 189.5 (a fake one); --unskip 189.5, --unskip all
mangal watch move 12 mango-weebcentral   # another provider (or the same one's new id), keeping the watch
mangal watch hold 12 hiatus      # check once more, then pause it (hiatus, ended; provider: needs a new one); --clear
mangal notify webhook            # save the Discord webhook: a message per manga with new chapters (watch.notify)
```

`mangal serve` keeps running and checks the watches on a cron schedule in local time
(`watch.schedule`, `0 */4 * * *` = every 4 hours on the hour by default; also `@hourly`, `@daily`,
`@weekly`, `@every 90m`; a watch can have its own: `watch add/set --schedule`). The ticks don't
depend on when it started: checks only start on ticks, nothing runs at start (a new watch, or one
whose tick passed while mangal was down, waits for the next tick; `mangal watch check` runs one
now), failing watches skip ticks (`watch.backoff`), gap filling
has its own schedule (`watch.gap_schedule`, off by default). Each provider has its own run
(`watch.parallel_providers` at once, a provider never twice, also against a `mangal watch check`
or a download elsewhere); the `watch.*` settings are re-read from the config file
every minute, and logins or secrets saved meanwhile (e.g. the MangaPlus secret from a TUI on the
same config) apply from the next run. It also runs the source checks and address lookups (see
[Jobs](#jobs)). On `127.0.0.1:6977` (`serve.host`, `serve.port`) it serves the
web UI (`serve.web`), a JSON API (that the TUI and the `mangal watch` commands use while it runs) and
OPDS (`/opds`). Every request logs in, wherever it comes from: people as users (`mangal user add
<name>`, then the web UI's login page, or basic auth for OPDS readers), programs with an API token
(`mangal token add <what> --perms read`, sent as `Authorization: Bearer <token>`).
mangal on the same config (the TUI, `mangal watch`, `mangal api`) uses the server's own key. Behind a
reverse proxy nothing needs setting (`serve.trusted_proxies` puts the clients' addresses in the logs);
[`nginx.conf.example`](nginx.conf.example) is an nginx site for it.

```sh
mangal user add david
mangal api status
curl -u david 127.0.0.1:6977/api/v1/watches
curl -H "Authorization: Bearer $TOKEN" -X POST 127.0.0.1:6977/api/check -d '{"ids": [1]}'   # 202 {"run": N}
curl -N -H "Authorization: Bearer $TOKEN" 127.0.0.1:6977/api/events                         # live progress
```

As a systemd service: [`mangal.service`](mangal.service) (copy it to `/etc/systemd/system/`, set
`User` and the mangal path, `systemctl enable --now mangal`; logs in `journalctl -u mangal`).

One server per config: a second instance (e.g. other mangas into another directory) uses another
config file with its own `data.path`, `download.path` (and `cache.path`) and `serve.port`, and
everything is run with `--config`: `ExecStart=/usr/local/bin/mangal --config /etc/mangal/b.toml
serve`, `mangal --config /etc/mangal/b.toml watch add ...`.

### Jobs

Downloads are jobs per provider (TUI `Q`, web Downloads). The maintenance jobs are apart, by kind: `check` (the watch checks), `sources` (is each library source still on its provider, health L11) and `urls` (the sources' web pages), which `mangal serve` runs on their schedules, and `scan` (the library scan) and `health` (the network health checks), run when asked. Each can be run now (all, or only the missing items: the due watches, the sources not checked lately, those without an address), paused, turned off, and set per provider (on, off, its own interval or schedule); requests to a provider pause `jobs.delay_min` to `jobs.delay_max` (1-3 s) between them. MangaPlus is off for sources, urls and health by default.

```sh
mangal jobs                              # every kind: on/paused, schedule, last and next run, missing items
mangal jobs urls                         # one kind, with its per-provider settings and counts
mangal jobs run sources [--all]          # run it now, here
mangal jobs pause check                  # stop its schedule (resume to restart)
mangal jobs set urls --schedule 336h --provider mango-weebcentral=off --delay 2s-5s
```

The settings are `jobs.*` in mangal.toml (the schedules stay `watch.schedule` and `health.source_check_interval`). In the TUI: `alt+j` (home, Downloads): enter a kind for its providers (`space` on/off), `C` run missing, `u` run all, `p` pause, `e` settings. In the web UI: Jobs (`/jobs`).

### Templates

Special functions are available and can be shown by running:

```sh
mangal templates funcs
```

Run a template with custom functions available:

```sh
mangal templates exec <template> -V <values>
```

Where `<template>` is in the form of a go [text/template](https://pkg.go.dev/text/template) and `<values>` a JSON. For example:

```sh
mangal templates exec "{{sanitizeWindows .Test}}" -V "{\"Test\":\"asdfg<>:?\"}"
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
mangal inline json -p mango-mangapill -q "Tengoku Daimakyou" -m exact -c 10-15 --chapter-populate
```

Each result has the manga's `metadata` and its `metadata_source` (`anilist`, `kitsu`, `mal` or
`provider`; absent when `metadata` is `null`): the provider's own with `--prefer-provider-metadata`
when it's valid, the AniList manga of `-a`, the manga's binding (`mangal bind`), else the closest
match on AniList, then Kitsu and MyAnimeList when configured. `anilist` (and `anilist_bad_match`,
the closest AniList manga when it's too unlike) are kept for older scripts: AniList only.
`--no-metadata` skips the metadata search (`--anilist-disable` still works).

For more, use the `-h` flag.

##### Download

Works exactly like the `json` option but needs to select only one manga and has couple different flags:

```sh
mangal inline download -p mango-mangapill -q "Tengoku Daimakyou" -m exact -c 10-15 -f CBZ -d "/custom/manga/root/path"
```

For more, use the `-h` flag.

#### Web

`mangal serve` serves the web UI (`serve.web`, on by default) on `serve.host`:`serve.port`
(`127.0.0.1:6977`). `mangal web` opens that one, or without a server running serves it the same
way, without the scheduled checks. It asks to log in as a user with a password (see
[Users](#users)):

```sh
mangal user rename mangal david    # the user made at the first start, an admin
mangal user passwd david
mangal serve                       # or: mangal web --open
```

What it has:
- **Library**: filters, covers, a manga's sources, watches, links and chapters; scan, merge,
  move, rename, mark, links, retitle, hold, delete, each shown as a plan before it's applied;
  duplicates, trash, undo (Operations), read history. A source's link opens its web page
  (looked up when unknown).
- **Search**: one provider (by title or "By id"), or "Search several" at once; a provider
  manga's chapters (downloaded, read and unavailable marked), download, read, watch,
  bind, "Get unavailable chapters from another provider". Site icons on the providers and metadata
  sources.
- **Reader**: downloaded and online chapters, paged (left to right, right to left) or webtoon,
  double pages, zoom, keys and tap zones, the position kept per chapter; each user has their own
  read history and positions, synced to their own AniList, Kitsu and MyAnimeList.
- **Watches**: add, check, pause, edit, re-link, hold, live log, check runs.
- **Jobs** and **Downloads**: the maintenance kinds (run, pause, settings) and the download queue.
- **Health**: the findings with their fixes, dismiss, network checks.
- **Accounts**: your own Anilist, Kitsu and MyAnimeList logins (everyone who keeps their
  place), the server's accounts for admins.
- **Logs**, **Config**, and **Users** (users and API tokens: roles, passwords, rename, which
  user the server's machine is) for admins; users with the tokens permission manage their own
  tokens there.

Users have a role (reader, downloader, manager, admin) or their own set of permissions: read,
progress, browse (the server contacts the sites: search, read online), download, manage (watches,
library fixes, health), tokens (their own API tokens), admin. They only see the actions their
permissions allow (`serve.hide_unavailable = false` shows them all and lets the server refuse);
readers and downloaders see library-relative paths, never the server's.

#### Script

Similar to [mangalorg/mangalcli](https://github.com/mangalorg/mangalcli) where a `run.lua` and a provider are required:

```sh
mangal script -f run.lua -p mango-mangapill -V "title=tengoku"
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
