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

Output docs/demo.gif

Set Shell bash
Set FontSize 16
Set Width 1200
Set Height 800
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 50ms

# Set up a fresh repository with a .asbx.yaml, and forget any trust from
# an earlier recording so the prompts appear again. Claude Code's
# project volume is recreated with its theme already chosen, so its
# first-run prompts don't show. Trust records are
# named after the SHA-256 of the path they're for.
Hide
Type `export PS1='$ '`
Enter
Type `rm -rf /tmp/asbx-demo && mkdir -p /tmp/asbx-demo/docs && cd -P /tmp/asbx-demo && git init -q && D=$(pwd -P)`
Enter
Type `for k in trusted-dirs trusted-configs; do rm -f ~/.config/asbx/$k/$(printf %s "$D" | shasum -a 256 | cut -d' ' -f1)* ~/.config/asbx/$k/$(printf %s "$D/.asbx.yaml" | shasum -a 256 | cut -d' ' -f1)*; done`
Enter
Type `V=asbx-asbx-demo-$(python3 -c 'import sys,zlib;print(zlib.crc32(sys.argv[1].encode()))' "$D")-claude`
Enter
Type `docker volume rm $V >/dev/null 2>&1; docker run --rm -v $V:/c alpine sh -c 'echo "{\"theme\":\"dark\",\"hasCompletedOnboarding\":true}" > /c/.claude.json && chown -R 1000:1000 /c'`
Enter
Type `printf '%s\n' 'podman_enabled: true' 'op_allowed_secret_refs:' '  - op://asbx-demo/**' 'extra_mounts:' '  - /tmp/asbx-demo/docs' 'claude:' '  home: project' '  mirror_managed_settings: false' '  litellm_token_secret_ref: ""' > .asbx.yaml`
Enter
Type "clear"
Enter
Show

# Show we're not on Ubuntu
Type "uname -a"
Sleep 200ms
Enter
Sleep 2s

Type "asbx"
Sleep 500ms
Enter

# "y" answers a prompt on its own, without Enter.
# asbx asks to trust the directory, then shows the whole config.
Wait+Screen@15s /Trust this directory/
Sleep 2s
Type "y"
Wait+Screen@15s /Trust this configuration/
Sleep 4s
Type "y"

# The banner, then Claude Code once it's ready for input.
Wait+Screen@15s /sandbox starting/
Sleep 4s

# Installing the project's tools takes a while on a cold start.
Wait+Screen@300s /auto mode on/
Sleep 200ms

# Show we're on Ubuntu now
Type "Hey Claude, what's the contents of /etc/os-release"
Enter
Wait+Screen@90s /ubuntu/
Sleep 3s

# The 1Password bridge: only items with a field allowed in .asbx.yaml are
# listed, here the Demo vault's.
Type "What secrets can you see with op item list?"
Enter
Wait+Screen@90s /hello-world/
Sleep 3s

# Show off Docker support
Type "Run an alpine container and print out the contents of /etc/os-release"
Enter
Wait+Screen@90s /alpine/
Sleep 20s

# Quit Claude Code, so the container doesn't outlive the recording.
Hide
Ctrl+C
Ctrl+C
Ctrl+C
Sleep 3s
