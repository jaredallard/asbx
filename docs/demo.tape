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
# Records the README's demo: asbx starting Claude Code in a sandbox. It
# runs the asbx on PATH in the repository, which must already be
# trusted.
#
# Usage: mise run demo

Output docs/demo.gif

Set Shell bash
Set FontSize 16
Set Width 1200
Set Height 600
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 50ms

# A plain prompt, so only asbx shows.
Hide
Type "export PS1='$ ' && clear"
Enter
Show

# Show we're not on Ubuntu
Type "bat /etc/os-release"
Sleep 200ms
Enter
Sleep 200ms

Type "asbx"
Sleep 500ms
Enter

# The banner, then Claude Code once it's ready for input. The timeout
# covers pulling the image and installing tools on a cold start.
Wait+Screen@15s /sandbox starting/
Wait+Screen@90s /auto mode on/
Sleep 200ms

# Show we're on Ubuntu now
Type "Hey Claude, what's the contents of /etc/os-release"
Enter
Wait+Screen@90s /ubuntu/
Sleep 13s

# Quit Claude Code, so the container doesn't outlive the recording.
Hide
Ctrl+C
Ctrl+C
Ctrl+C
Sleep 3s
