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
# Stands in for podman and docker when podman_enabled is off, telling
# the agent how to enable it rather than letting Podman fail.
#
# Usage: podman|docker [args...]

set -euo pipefail

cat >&2 <<EOF
${0##*/}: containers aren't enabled in this asbx sandbox.

To run containers, ask the user to set \`podman_enabled: true\` in
~/.config/asbx/config.yaml (or the project's .asbx.yaml) and restart
asbx. See "Podman" in the asbx README.
EOF

exit 1
