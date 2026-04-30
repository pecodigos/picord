# DeepSeek V4 Pro Next Iteration Plan

> **For DeepSeek V4 Pro:** Implement this plan task-by-task. Commit after each task. Do not re-add the removed browser Web UI.

**Goal:** Make Picord reliably configurable after boot through the system tray/GTK path, then harden the remaining local API/CLI surface that supports tray and command-line workflows.

**Architecture:** Picord is now tray/CLI first. The daemon may still expose a localhost JSON API for CLI and tray actions, but it must not serve or link a browser Web UI. Startup should prefer an explicit tray session (`picord run --tray`) and the GTK Settings dialog must be available from the tray after login.

**Tech Stack:** Go 1.21+, gotk3 GTK dialog, energye/systray, Linux desktop autostart/systemd user units, local HTTP API for CLI commands.

---

## Current repository state

Base before this handoff: `origin/master` / `562caa1 feat: refine app detection and settings`.

New local commits from this iteration:

1. `bdc40e4 fix: make tray autostart explicit and remove web UI`
   - `picord run` now accepts `--tray` and `--no-tray`.
   - `resources/picord.desktop` now uses `Exec=picord run` and includes `X-GNOME-Autostart-Delay=5`.
   - Tray no longer has `Open Web GUI`.
   - Embedded `internal/server/web/**` files were deleted.
   - Root HTTP path now returns JSON 404 instead of serving HTML.
   - Server startup log now says `Picord API`, not Web GUI.

2. `686999d docs: remove web UI references`
   - README no longer advertises the browser Web UI.
   - README points users to tray Settings, CLI, and YAML config.

Validation already run after the changes:

```bash
go test -count=1 ./...
go vet ./...
make build
```

All passed. `make build` produced ignored `bin/picord`.

Repo status at plan time: `master` is ahead of `origin/master` by 2 commits. Push these commits after your next successful task batch unless the user says otherwise.

---

## Constraints / do not regress

- Do not bring back `internal/server/web/**`, browser HTML/CSS/JS assets, `Open Web GUI`, or README claims about a Web UI.
- Keep the localhost API only as an implementation detail for CLI/tray commands unless you intentionally replace those flows.
- Do not expose secrets. SteamGridDB API key must remain masked in API responses and must not be written into docs/logs.
- Preserve the public Picord Discord app ID: `1499058229571752148`.
- Commit every completed change.
- Prefer user-facing wording like “Settings”, “Local controls”, or “Command line”; avoid backend/web jargon in UI copy.

---

## P0 findings to address first

### P0.1: Systemd user service likely still starts without a tray

**Impact:** The user reported no system-tray access after boot. The desktop autostart file now explicitly starts `picord run`, but `resources/picord.service` still runs `%h/.local/bin/picord` and may start outside the fully initialized tray/session environment. The Settings dialog's “Launch on login (systemd service)” checkbox currently enables that service, which may recreate the no-tray-after-boot problem.

**Files:**
- Modify: `resources/picord.service`
- Modify: `internal/settings/dialog.go`
- Test: add/extend tests if a helper is extracted; otherwise document manual validation in README/HANDOFF.

**Required fix:**
1. Decide whether login startup should use desktop autostart, systemd, or both.
2. Recommended: make the GTK Settings checkbox manage `~/.config/autostart/picord.desktop` instead of only `systemctl --user enable picord.service`, because a tray icon needs a graphical desktop session.
3. If keeping systemd, update `resources/picord.service` to call `%h/.local/bin/picord run --tray`, remove brittle `DISPLAY=:0` / `WAYLAND_DISPLAY=wayland-0` assumptions where possible, and document that desktop autostart is preferred for tray access.

**Acceptance checks:**
- Fresh copy of `resources/picord.desktop` starts `picord run --tray` or `picord run` with tray enabled.
- The Settings dialog login toggle enables the same startup mechanism documented in README.
- On a real desktop session: reboot or log out/in, then confirm Picord tray icon appears and Settings opens.

### P0.2: Tray setting can still disable the only graphical configuration path

**Impact:** With the browser Web UI removed, `show_tray_icon: false` removes the user's graphical configuration access after restart. This was acceptable when a browser UI existed, but it is now dangerous.

**Files:**
- Modify: `internal/settings/dialog.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Possibly modify: `internal/server/server.go` settings DTOs if keeping `show_tray_icon` in API.

**Required fix:**
1. Either remove the “Show icon in system tray” setting entirely, or rename it to an advanced/headless option with a warning.
2. Recommended: keep config backward-compatible but make default/fresh installs always tray-enabled unless the user starts `picord run --no-tray` explicitly.
3. Add config tests for legacy `show_tray_icon: false` behavior after the chosen policy.

**Acceptance checks:**
- Fresh config after `config.Load(missingPath)` has tray enabled.
- User can still deliberately run headless via `picord run --no-tray`.
- The GTK Settings dialog cannot accidentally strand a non-technical user without a tray.

---

## P1 hardening tasks

### Task 1: Add explicit CLI coverage for tray flags

**Objective:** Lock in the `picord run --tray` / `--no-tray` behavior so startup regressions are caught.

**Files:**
- Modify: `cmd/picord/main.go` if needed to make daemon options testable.
- Modify: `cmd/picord/cli.go`
- Test: `cmd/picord/main_test.go` or new `cmd/picord/cli_test.go`

**Steps:**
1. Extract a small pure helper if needed, e.g. `runOptionsFromFlags(args []string) (daemonOptions, error)`.
2. Test:
   - default `run` means tray override true or no override with config default true (depending on final design)
   - `run --tray` enables tray
   - `run --no-tray` disables tray
   - conflicting flags have deterministic behavior or return a usage error
3. Run `go test -count=1 ./cmd/picord`.
4. Commit.

### Task 2: Remove stale “web” naming where it is no longer user-facing

**Objective:** Keep API internals clear without advertising a Web UI.

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `cmd/picord/cli.go`
- Modify: `cmd/picord/main.go`
- Modify: `README.md`

**Steps:**
1. Decide if `web_port` remains for backward compatibility. If yes, document as “local API port”.
2. Consider adding a new `api_port` field while loading old `web_port` as a fallback.
3. Update tests so old configs with `web_port` still load correctly.
4. Do not break existing users' config files.
5. Run full tests and commit.

### Task 3: Replace Web-origin security assumptions with local API assumptions

**Objective:** Since no browser UI exists, mutating API calls should still be safe for CLI but not optimized around browser origins.

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`
- Modify: `cmd/picord/cli.go` if token/header behavior changes

**Steps:**
1. Keep `X-Picord-Token` for mutating API calls.
2. Revisit `isLocalOrigin()` tests; localhost origin tests can remain, but comments should say local API/CLI safety rather than browser UI.
3. Add a test that `GET /` returns JSON 404 and does not contain HTML.
4. Run `go test -count=1 ./internal/server` and commit.

---

## P2 polish

### Task 4: Improve README autostart instructions

**Objective:** Users should install startup files in the correct places without guessing.

**Files:**
- Modify: `README.md`
- Modify: `HANDOFF.md`

**Steps:**
1. Add exact commands for desktop autostart:
   - install binary to `~/.local/bin/picord`
   - copy `resources/picord.desktop` to `~/.config/autostart/picord.desktop`
   - copy icon if desired
2. Note that systemd is optional/headless or advanced if the final decision keeps it.
3. Add manual verification: `pgrep -a picord`, right-click tray icon, open Settings.
4. Commit.

### Task 5: Audit CLI parity for removed Web UI features

**Objective:** Ensure removed browser UI features remain possible through tray/CLI/YAML.

**Files:**
- Inspect: `internal/server/web/js/app.js` from pre-removal commit if needed via git history.
- Modify: `cmd/picord/cli.go`
- Modify: `README.md`

**Steps:**
1. List operations the Web UI used to provide: status, auto-detect toggle, manual override, clear override, profile from catalog, catalog search/refresh/enrich, settings edit.
2. Confirm each has CLI, tray, or YAML route.
3. Add small CLI commands for any missing critical route, especially auto-detect enable/disable if not already easy.
4. Commit.

---

## Verification before final handoff

Run:

```bash
go test -count=1 ./...
go vet ./...
go test -race ./...
make build
git status --short --branch
```

Manual desktop validation required for this iteration:

```bash
mkdir -p ~/.config/autostart ~/.local/share/icons/hicolor/128x128/apps
cp resources/picord.desktop ~/.config/autostart/picord.desktop
cp icons/picord_128.png ~/.local/share/icons/hicolor/128x128/apps/picord.png
# log out/in or reboot
pgrep -a picord
```

Expected: a Picord tray icon is visible; right-click shows Settings, Auto-Detect, Manual Override, Reload Config, Quit; no Open Web GUI item exists.

---

## Suggested commit sequence

1. `fix: use desktop autostart for tray startup`
2. `fix: prevent settings from disabling tray access`
3. `test: cover tray startup flags`
4. `refactor: rename web port to local api port`
5. `test: assert removed web ui stays removed`
6. `docs: document tray-first startup workflow`

Keep each commit buildable.
