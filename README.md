# asbx

A small utility to isolate your coding agents, Claude Code and Codex,
from your local machine with sane defaults and low setup cost.

![asbx starting Claude Code in a sandbox](docs/demo.gif)

## Requirements

- [Orbstack](https://orbstack.dev)
  - You can technically use Docker for Mac, but your container will not
    have access to services running your mac (which may or may not be
    acceptable!)
    - On Linux standard Docker is supported (Docker Desktop works w/o host
      network access)
- [1Password CLI](https://developer.1password.com/docs/cli/get-started/):
  Used to source tokens passed through to the agent, and by `op` in the
  container.
- [mise](https://mise.jdx.dev): While not **hard** required, it's
  strongly recommended to help ensure your project's build chain + tools
  are installed. It's automatically ran (and lockfile verified if it
  exists) on each enter (note: it is also cached via a volume).

## Features

- Agent has access to your current `pwd`, and only that directory.
- Git commit signing works via ssh-agent (e.g., 1Password).
- Forgejo authentication is passed through via `fj auth token`.
- GitHub authentication is passed through via `gh auth token`.
- `op read`, `op run` and `op item list` work in the container through
  your host's `op`, limited to the secrets you allow if enabled.
- Clipboard tools (`wl-copy`, `xclip`, `pbcopy`, ...) in the container
  copy to your host's clipboard, and can paste from it if enabled.
- `xdg-open` in the container opens http(s) URLs, such as logins, in
  your host's browser.
- Containers can be run in the container with rootless Podman, if
  enabled, without giving it access to your host's Docker or privileged
  access.

## Installation

Install the latest release via [mise](https://mise.jdx.dev):

```bash
mise use -g 'forgejo:jaredallard/asbx[api_url=https://git.rgst.io/api/v1,asset_pattern=asbx]'
```

Or add it to your mise config:

```toml
[tools]
"forgejo:jaredallard/asbx" = { version = "latest", api_url = "https://git.rgst.io/api/v1", asset_pattern = "asbx" }
```

Or via [Homebrew](https://brew.sh):

```bash
brew tap jaredallard/tap https://git.rgst.io/jaredallard/homebrew-tap.git
brew trust --cask jaredallard/tap/asbx
brew install --cask jaredallard/tap/asbx
```

## Quickstart

Install `asbx` via `mise`:

```bash
mise use -g 'forgejo:jaredallard/asbx[api_url=https://git.rgst.io/api/v1,asset_pattern=asbx]'
```

Or via [Homebrew](https://brew.sh):

```bash
brew tap jaredallard/tap https://git.rgst.io/jaredallard/homebrew-tap.git
brew trust --cask jaredallard/tap/asbx
brew install --cask jaredallard/tap/asbx
```

Run a sandbox: `asbx` (or, specify the agent with `asbx claude`).

The first time it runs, asbx writes the default configuration to
`~/.config/asbx/config.yaml`. If the agent's token isn't in 1Password
yet, asbx offers to create its _API Credential_ item for you and asks
you to paste the token:

- **Claude**: `claude-token` in the `Private` vault, in the
  `credential` field. Need a token? Run `claude setup-token` on your
  host. Note that it expires after a year as of the time of writing.
- **Codex**: `codex-token` in the `Private` vault, in the `credential`
  field. Create one at <https://platform.openai.com/api-keys>.

> [!NOTE]
> Want to store your 1Password item in a different location or add
> additional secrets? Or, enable other features? Read the configuration
> section below.

## Configuration

asbx reads `~/.config/asbx/config.yaml` for configuration values. When
it doesn't exist, asbx writes
[the example configuration](internal/config/embed/config.example.yaml),
which documents every option, there.

Repositories can also have their own `.asbx.yaml` file at its git root
(or the working directory outside of a git repo). It takes the same
options, is read after `~/.config/asbx/config.yaml` so it overrides it,
and is mounted read-only into the container. Each option it sets
replaces the user's, except `extra_secret_env_vars`, which is merged by
name. See [Security](#security) before running asbx in a repo you don't
trust.

The first time asbx sees a project's `.asbx.yaml`, it shows you the
whole file and asks you to trust it before using it. If it changes
after that, asbx shows you what changed since you trusted it and asks
again. asbx stops if you don't trust it, or if it can't ask because it
isn't running in a terminal. Trusted files are kept in
`~/.config/asbx/trusted-configs/`; delete that directory, or a file in
it, to be asked again.

The first time asbx runs in a directory, it asks you to trust it, then
tells the agent it's trusted, so the agent doesn't ask on every run (its
own answer wouldn't outlive the container). asbx stops if you don't
trust it, or if it can't ask because it isn't running in a terminal.
Trusted directories are recorded in `~/.config/asbx/trusted-dirs/`, one
file each containing its path; delete one to be asked again.

> [!NOTE]
> The old `KEY=value` files, `~/.config/asbx/config` and `.asbx`, are
> no longer read. asbx refuses to run while one exists, convert it to
> YAML using
> [the example configuration](internal/config/embed/config.example.yaml).

Every option is off when it isn't set, except `forgejo_enabled` and
`github_enabled`, which are on and skipped when `fj` or `gh` isn't
logged in, `agent`, which is `claude`, and `claude.oauth_token_secret_ref`
and `codex.api_key_secret_ref`, which are
`op://Private/claude-token/credential` and
`op://Private/codex-token/credential`. The `claude.` and `codex.`
options are nested under `claude:` and `codex:` maps, as in the example.

| Option                            | Type   | Description                                                                                                                                                            |
| --------------------------------- | ------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `agent`                           | string | The agent `asbx` runs when it isn't given one, `claude` or `codex`. See [Agents](#agents).                                                                             |
| `forgejo_enabled`                 | bool   | Passes `FORGEJO_TOKEN` (from `fj auth token`) and your host's fj config (without its token) into the container. On by default; skipped when not logged in.             |
| `github_enabled`                  | bool   | Passes `GH_TOKEN` (from `gh auth token`) into the container. On by default; skipped when not logged in.                                                                |
| `op_allowed_secret_refs`          | list   | `op://` refs that `op` in the container may read through your host's `op`. See [1Password CLI](#1password-cli).                                                        |
| `extra_secret_env_vars`           | map    | `NAME: op://ref` entries; each 1Password secret is passed into the container as the environment variable `NAME`.                                                       |
| `clipboard`                       | string | How the container reaches your host's clipboard: `off`, `copy` or `copy_paste`. See [Clipboard](#clipboard). Defaults to `copy`.                                       |
| `open_urls_enabled`               | bool   | Lets `xdg-open` in the container open http(s) URLs in your host's browser. See [Opening URLs](#opening-urls). On by default.                                           |
| `network_host`                    | bool   | Runs the container on the host's network, so it can reach every service listening on localhost. Otherwise host services are only reachable via `host.docker.internal`. |
| `podman_enabled`                  | bool   | Lets the agent run containers with rootless Podman inside the container. See [Podman](#podman).                                                                        |
| `extra_mounts`                    | list   | Absolute paths to mount read-only at the same path inside the container.                                                                                               |
| `claude.oauth_token_secret_ref`   | string | 1Password `op://` ref to read `CLAUDE_CODE_OAUTH_TOKEN` from. When it's the default and the item doesn't exist, asbx offers to create it. `''` passes none.            |
| `claude.litellm_token_secret_ref` | string | 1Password `op://` ref to read `LITELLM_API_KEY` from.                                                                                                                  |
| `claude.mirror_managed_settings`  | bool   | Mounts your OS specific managed settings into the container.                                                                                                           |
| `claude.mirror_projects`          | bool   | Mounts this directory's Claude sessions and memory read-write, so the host and container can resume each other's sessions. Only with `claude.home: host`.              |
| `claude.home`                     | string | Where Claude's state in the container is kept: `host`, `project` or `asbx`. See [Claude Code](#claude-code). Defaults to `host`.                                       |
| `codex.api_key_secret_ref`        | string | 1Password `op://` ref to read `OPENAI_API_KEY` from, which Codex is logged in with. When it's the default and the item doesn't exist, asbx offers to create it.        |

### 1Password CLI

The 1Password App's desktop integration is unable to be forwarded like
the SSH agent due to more strict validation. To work around this, we run
a container only proxy with a shim inside the container that allows
specific key paths to be read using the host `op`. This requires `op` to
be installed on the host.

To allow specific secret refs, use the `op://` syntax like so, with
wildcards to signify wider access:

```yaml
op_allowed_secret_refs:
  - op://Dev/github/token
  - op://Dev/aws/*
  - op://Agent/**
```

Query params such as `?attribute=otp` are supported.

`op item list` in the container only lists the items with a field you
allow, so the agent can find what it may read. Only each item's ID,
title, vault, category and when it was created and edited are shown,
not its username, websites, tags or other fields.

> [!WARNING]
> Only `op read`, `op run` and `op item list` are supported in the
> container.

### Clipboard

The container has no display, so asbx stands in for `wl-copy`,
`wl-paste`, `xclip`, `xsel`, `pbcopy` and `pbpaste` and forwards them
to your host's clipboard tool (`pbcopy`, `wl-clipboard`, `xclip` or
`xsel`). It's a bridge like the [1Password CLI](#1password-cli)'s, so
nothing else on the host can reach it. `clipboard` is one of:

- `copy` (default): the container can write to your clipboard, but not
  read it.
- `copy_paste`: it can also read it, such as for pasting an image into
  the agent. On macOS this supports text and PNG images.
- `off`: the tools fail, and say how to enable them.

With `copy` or `copy_paste`, the agent also runs on a terminal of its
own, and the OSC 52 sequences it prints to copy are set on your
clipboard through the same bridge, rather than left to your terminal to
honor. Queries for your clipboard are dropped.

If your host has none of the tools, asbx warns and doesn't forward the
clipboard, and OSC 52 copies, such as Claude Code's own, are left to
your terminal.

### Opening URLs

Only `http` and `https` URLs are opened, so the agent can't open files
or other apps on your host.

There is a small risk with enabling this since they're arbitrary URLs.

It's on by default; set `open_urls_enabled: false` to turn it off.

### Podman

With `podman_enabled`, the agent can run containers inside the
container with rootless Podman, using `podman` or `docker`, or the
Docker API at `DOCKER_HOST` or `/var/run/docker.sock` (e.g., for
testcontainers). Your host's Docker socket isn't mounted, and the
container gets no more capabilities: Podman nests its containers in
user namespaces, which needs a seccomp profile that allows the
syscalls to create them and mount in them. Images are kept in a volume
per working directory. Without it, `podman` and `docker` in the
container print how to enable it instead of running.

Containers inside the container differ from Docker's in a few ways:

- They share the container's PID namespace, since Docker's `/proc`
  masks keep a new one from being mounted.
- Resource limits, such as `--memory`, and `--sysctl` don't work.
- Only the default network works. Netfilter is blocked (see
  [Security](#security)), so `podman network create` and Compose's
  per-project networks fail; use `network_mode: host` or the default
  network instead.

## Agents

`asbx claude [args]` runs Claude Code and `asbx codex [args]` runs
Codex. Every argument after the agent's name is passed to it. Plain
`asbx [args]` runs the agent the `agent` option names, `claude` unless
it's set.

### Claude Code

Set `claude.oauth_token_secret_ref` to log in with a token from
`claude setup-token`. Where Claude Code's state in the container is
kept depends on `claude.home`:

- **`host`** (default): your `~/.claude` configuration is copied into
  the container, except other projects' sessions, memory and prompt
  history. Nothing Claude writes outlives the container, except this
  directory's sessions and memory with `claude.mirror_projects`.
- **`project`**: kept in a Docker volume for this directory,
  `asbx-<dir>-<checksum>-claude`, like mise's tools. It starts empty,
  your `~/.claude` isn't used. Remove the volume to start over.
- **`asbx`**: kept in `~/.local/share/asbx/claude`, shared by every
  project that uses `asbx`. It starts empty, your `~/.claude` isn't
  used. Remove the directory to start over.

### Codex

Only the configuration in `~/.codex` (or `$CODEX_HOME`) is copied into
the container: `config.toml`, `AGENTS.md`, `AGENTS.override.md`,
`prompts`, `rules` and `skills`. Sessions, history and logs aren't, as
Codex keeps every project's together.

Codex logs in with the OpenAI API key `codex.api_key_secret_ref` reads
from 1Password.

The container is the sandbox, so Codex's own is turned off in it
(`sandbox_mode = "danger-full-access"`); it generally can't run inside
Docker's default seccomp profile anyway. Codex still asks for approval
as configured, and `--sandbox` overrides it.

## Security

asbx limits agent access, but it's not perfect. There's still a few
limitations:

- Refuses to run in `/`, your home directory or its parents, or a
  dot-directory in your home (e.g., `~/.config`).
- Mounts some files read-only to the host (namely: git hooks and git
  config) to prevent automatic execution of commands on the host.
- Asks you to trust a directory the first time it runs in it, before
  the agent can read its files or run code from them.
- Doesn't mount other projects' sessions, memory or prompt history from
  `~/.claude`, or anything but configuration from `~/.codex`.
- Mounts the files that every agent asbx knows executes from on the
  host read-only, whichever agent is running (Claude Code's
  `.claude/settings.json`, `.claude/settings.local.json` and `.mcp.json`,
  Codex's `.codex/config.toml` and `.codex/rules`), when they exist.
- If project memory is symlinked, it will be pinned when first read to
  prevent the agent from mutating it and asbx feed it through on next
  run.
- Only runs images signed with asbx's key. A release's image is rebuilt
  daily, under the same tag, to pick up security fixes, so on every run
  asbx looks up the tag, verifies the image's signature and runs it by
  digest. If that fails, such as when offline, it warns and runs the
  last image it verified, if Docker still has it. You can verify an
  image yourself with
  `cosign verify --key internal/image/embed/cosign.pub --insecure-ignore-tlog ghcr.io/jaredallard/asbx:<version>`.

What it doesn't protect against:

- **sudo on Linux**: the agent can become root in the container, and root
  can create setuid-root files in the working directory. Running one on
  the host gives host root. Use rootless Docker or `userns-remap` to
  close this. macOS docker prevents this.
- **ssh-agent**: the agent can use every key in your agent against any
  host, not just for commit signing. Limit which keys are offered in
  1Password's `agent.toml`, or if using the Desktop integration, simply
  deny the request.
- **Tokens**: every token passed in, including `extra_secret_env_vars`,
  is readable by the agent. Mitigate this with scoped credentials. The
  agent can read every secret `op_allowed_secret_refs` allows, so keep
  it as minimal as possible.
- **Project config**: a repo's or work dir `.asbx.yaml` configuration can
  grant more permissions. asbx shows you a new or changed one, including
  one the agent created, and won't use it until you trust it, so read
  it before you do.
- **`clipboard`**: `copy` lets the agent replace what's on your
  clipboard, which you may later paste into a terminal, so check what
  you paste. `copy_paste` lets it read everything you copy, passwords
  included, while it runs.
- **`open_urls_enabled`**: the agent can open any http(s) URL in your
  browser, where you're logged in, such as one that starts a download or
  makes a request with side effects. Turn it off if that's a concern.
- **`podman_enabled`**: the container runs with a seccomp profile that
  allows creating user namespaces and mounting in them, which lets the
  agent reach kernel code, as root in a user namespace, that's been
  the source of many container escapes. Netfilter, the most common, is
  blocked. On Linux with rootful Docker and no `userns-remap`, the
  container isn't in a user namespace itself, so an escape through a
  kernel bug gives host root. Use rootless Docker or `userns-remap` to
  limit this. Containers it runs share its PID namespace and user, so
  they can kill the agent's processes and read their command lines,
  but not their environment. They can read each other's. Only run
  images you trust.
- **Codex's sandbox**: Codex's own sandbox is disabled in the container
  in favour of asbx's.
- **`claude.mirror_projects`**: when enabled, sessions and memory
  written in the container are read by Claude on the host, so they can
  carry prompt injection.
- **`claude.home: asbx`**: settings, memory and sessions written by the
  agent in one project are read by it in every other project that uses
  `asbx`, so they can carry prompt injection, or settings such as hooks,
  between them. They're never read by Claude on the host. `project`
  keeps them to one directory.

## License

GPL-3.0
