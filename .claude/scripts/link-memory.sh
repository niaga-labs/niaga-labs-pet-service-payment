#!/usr/bin/env bash
# Link Claude Code's auto-memory directory for THIS repo to .claude/memory, so the
# memory files live in the repo (visible in VS Code, git-tracked) instead of hidden
# under ~/.claude/projects/<slug>/memory. Same pattern as claude-config-MHUB.
#
# Windows (Git Bash): creates a directory junction (no admin needed).
# macOS/Linux:        creates a symlink.
#
# Usage: bash .claude/scripts/link-memory.sh          # link (idempotent)
#        bash .claude/scripts/link-memory.sh --check  # report only
set -u

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGET="$REPO/.claude/memory"
CHECK=0; [ "${1:-}" = "--check" ] && CHECK=1

# Claude Code's project slug = absolute path with every / \ : replaced by '-'.
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    WIN_REPO="$(cd "$REPO" && pwd -W)"                 # C:/Users/kamek/Documents/trading-bot
    SLUG="$(printf '%s' "$WIN_REPO" | sed -e 's#[/\\:]#-#g')"   # C--Users-kamek-Documents-trading-bot
    IS_WIN=1 ;;
  *)
    SLUG="$(printf '%s' "$REPO" | sed -e 's#/#-#g')"   # -Users-x-Documents-trading-bot
    IS_WIN=0 ;;
esac

PROJ_DIR="$HOME/.claude/projects/$SLUG"
LINK="$PROJ_DIR/memory"

echo "repo:    $REPO"
echo "slug:    $SLUG"
echo "link:    $LINK"
echo "target:  $TARGET"

is_link() {
  if [ "$IS_WIN" = 1 ]; then
    # A junction is a reparse point; PowerShell reports LinkType=Junction.
    [ -d "$LINK" ] && powershell.exe -NoProfile -Command \
      "if ((Get-Item -LiteralPath '$(cygpath -w "$LINK")' -Force).LinkType -eq 'Junction') { exit 0 } else { exit 1 }" >/dev/null 2>&1
  else
    [ -L "$LINK" ]
  fi
}

if is_link; then
  echo "status:  already linked ✓"
  exit 0
fi

if [ "$CHECK" = 1 ]; then
  echo "status:  NOT linked (run without --check to create)"
  exit 1
fi

mkdir -p "$PROJ_DIR" "$TARGET"

if [ -d "$LINK" ]; then
  # A real directory exists where the link should go. Preserve anything in it.
  if [ -n "$(ls -A "$LINK" 2>/dev/null)" ]; then
    echo "moving existing memory files into the repo (no overwrite):"
    for f in "$LINK"/*; do
      [ -e "$f" ] || continue
      base="$(basename "$f")"
      if [ -e "$TARGET/$base" ]; then
        echo "  keep repo copy, leaving $base at $LINK.bak/"
        mkdir -p "$LINK.bak" && mv "$f" "$LINK.bak/"
      else
        echo "  $base"
        mv "$f" "$TARGET/"
      fi
    done
  fi
  rmdir "$LINK" 2>/dev/null || { echo "could not remove $LINK (not empty?)"; exit 1; }
fi

if [ "$IS_WIN" = 1 ]; then
  # New-Item junction needs no admin rights and no Developer Mode (unlike a symlink).
  powershell.exe -NoProfile -Command \
    "New-Item -ItemType Junction -Path '$(cygpath -w "$LINK")' -Target '$(cygpath -w "$TARGET")' | Out-Null" \
    && echo "status:  junction created ✓"
else
  ln -s "$TARGET" "$LINK" && echo "status:  symlink created ✓"
fi

ls "$LINK" | head -5
