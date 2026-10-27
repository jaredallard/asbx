#!/usr/bin/env bash
#
# Copyright (C) 2026 asbx contributors
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with this program. If not, see <https://www.gnu.org/licenses/>.
#
# SPDX-License-Identifier: GPL-3.0
#
# Prepares the asbx container from the host's read-only mounts and starts
# the agent in the working directory (the repository, at its host path).
#
# Usage: entrypoint.sh <claude|codex> [args...]

set -euo pipefail

readonly host_mnt=/mnt/readonly_host_mnt

# copy_dir copies the contents of a host directory mounted read-only,
# dotfiles included, into a directory in the container.
#
# Usage: copy_dir <source> <destination>
copy_dir() {
	local src="$1"
	local dst="$2"

	mkdir -p "$dst"

	shopt -s dotglob nullglob
	local f
	for f in "$src"/*; do
		cp -r "$f" "$dst/"
	done
	shopt -u dotglob nullglob
}

# setup_common prepares what every agent uses: fj, Podman and the
# project's tools.
#
# Usage: setup_common
setup_common() {
	# Only mounted when Forgejo is enabled. The token comes from
	# FORGEJO_TOKEN.
	if [[ -f "$host_mnt/fj-config.json" ]]; then
		mkdir -p "$HOME/.config/fj"
		jq "del(.token)" "$host_mnt/fj-config.json" >"$HOME/.config/fj/config.json"
	fi

	# Podman's Docker API socket, for tools that use DOCKER_HOST rather
	# than the docker command.
	if [[ ${ASBX_PODMAN:-} == 1 ]]; then
		podman system service --time=0 >"$XDG_RUNTIME_DIR/podman-service.log" 2>&1 &
	else
		# Podman can't run without podman_enabled, tell the agent how to
		# enable it rather than letting it fail.
		export PATH="/opt/asbx/podman-disabled:$PATH"
	fi

	if [[ -e ".mise.toml" ]] || [[ -e "mise.toml" ]]; then
		local install_args=()
		if [[ -e "mise.lock" ]]; then
			install_args+=("--locked")
		fi

		# Its output is only shown when there's something to install,
		# it's noise otherwise.
		if ! mise -q install --dry-run-code "${install_args[@]}" &>/dev/null; then
			mise install "${install_args[@]}"
		fi
	fi
}

# setup_claude copies the host's Claude Code configuration into place,
# unless its state is kept by asbx (ASBX_CLAUDE_HOME is project or
# asbx), where it's only told the working directory is trusted.
#
# Usage: setup_claude
setup_claude() {
	if [[ ${ASBX_CLAUDE_HOME:-host} != "host" ]]; then
		# CLAUDE_CONFIG_DIR is set, so its global state is kept in it.
		local state="$CLAUDE_CONFIG_DIR/.claude.json"
		if [[ ! -s $state ]]; then
			echo '{}' >"$state"
		fi

		if [[ -n ${ASBX_TRUSTED_DIR:-} ]]; then
			jq --arg dir "$ASBX_TRUSTED_DIR" \
				'.projects[$dir].hasTrustDialogAccepted = true' \
				"$state" >"$state.tmp"
			mv "$state.tmp" "$state"
		fi

		return
	fi

	# asbx doesn't mount projects, the current project's sessions and
	# memory are bind mounted read-write over projects/<slug> when
	# mirrored.
	copy_dir "$host_mnt/.claude" "$HOME/.claude"

	if [[ -n ${ASBX_TRUSTED_DIR:-} ]]; then
		jq --arg dir "$ASBX_TRUSTED_DIR" \
			'.projects[$dir].hasTrustDialogAccepted = true' \
			"$host_mnt/.claude.json" >"$HOME/.claude.json"
	else
		cp "$host_mnt/.claude.json" "$HOME/"
	fi
}

# setup_codex copies the host's Codex configuration into place and logs
# Codex in with OPENAI_API_KEY, when it's set.
#
# Usage: setup_codex
setup_codex() {
	copy_dir "$host_mnt/.codex" "$HOME/.codex"

	local config="$HOME/.codex/config.toml"
	touch "$config"

	# The container is the sandbox, Codex's own can't run inside it. The
	# login is kept in a file, there's no keyring.
	local filter='.sandbox_mode = "danger-full-access"'
	filter+=' | .cli_auth_credentials_store = "file"'
	if [[ -n ${ASBX_TRUSTED_DIR:-} ]]; then
		filter+=' | .projects[strenv(ASBX_TRUSTED_DIR)].trust_level = "trusted"'
	fi

	mise exec -- yq -i -p toml -o toml "$filter" "$config"

	# Its output is only shown when it fails, it's noise otherwise.
	if [[ -n ${OPENAI_API_KEY:-} ]]; then
		local out
		if ! out=$(mise exec -- codex login --with-api-key <<<"$OPENAI_API_KEY" 2>&1); then
			echo "$out" >&2
			exit 1
		fi
	fi
}

# main sets up the container for agent and then runs it with args.
#
# Usage: main <claude|codex> [args...]
main() {
	if (($# < 1)); then
		echo "Usage: entrypoint.sh <claude|codex> [args...]" >&2
		exit 1
	fi

	local agent="$1"
	shift

	setup_common

	case "$agent" in
	claude) setup_claude ;;
	codex) setup_codex ;;
	*)
		echo "unknown agent: $agent" >&2
		exit 1
		;;
	esac

	# Every tool in the image's and the project's mise config goes on
	# PATH, not just the agent, so hooks and the agent can run them (e.g.,
	# rtk).
	exec mise exec -- "$agent" "$@"
}

main "$@"
