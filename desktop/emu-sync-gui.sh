#!/bin/sh
# Wrapper script for running emu-sync with a kdialog progress notification.
# KDE/SteamOS uses kdialog for simple GUI notifications.

SYNC_OUTPUT=$("$HOME/.local/bin/emu-sync" sync 2>&1)
EXIT_CODE=$?

if command -v kdialog >/dev/null 2>&1; then
    if [ $EXIT_CODE -eq 0 ]; then
        kdialog --passivepopup "$SYNC_OUTPUT" 10 --title "emu-sync"
    else
        kdialog --error "$SYNC_OUTPUT" --title "emu-sync"
    fi
else
    if [ $EXIT_CODE -ne 0 ]; then
        echo "emu-sync failed:" >&2
        echo "$SYNC_OUTPUT" >&2
    fi
fi

exit $EXIT_CODE
