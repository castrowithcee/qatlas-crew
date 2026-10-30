#!/usr/bin/env python3
"""Record a minimal Claude Code hook signal for a disposable session."""

import json
import os
import re
import sys
from datetime import datetime, timezone


EVENTS = {
    "SessionStart",
    "UserPromptSubmit",
    "PreToolUse",
    "PermissionRequest",
    "PostToolUse",
    "Notification",
    "Stop",
    "SubagentStop",
    "SessionEnd",
}
TOKEN = re.compile(r"[A-Za-z0-9_.:-]{1,128}\Z")


def token(value):
    return value if isinstance(value, str) and TOKEN.fullmatch(value) else None


def main():
    try:
        event = json.load(sys.stdin)
        name = event.get("hook_event_name")
        if name not in EVENTS:
            return 0

        record = {
            "at": datetime.now(timezone.utc).isoformat(),
            "event": name,
            "crew_session_id": token(os.environ.get("CREW_SESSION_ID")),
            "host_session_id": token(event.get("session_id")),
        }
        if name in {"PreToolUse", "PermissionRequest", "PostToolUse"}:
            record["tool"] = token(event.get("tool_name"))
        if name == "Notification":
            record["notification_type"] = token(event.get("notification_type"))
        if name == "SessionStart":
            record["source"] = token(event.get("source"))
        if name == "SessionEnd":
            record["reason"] = token(event.get("reason"))
        if name == "SubagentStop":
            record["agent_id"] = token(event.get("agent_id"))
            record["agent_type"] = token(event.get("agent_type"))

        with open(os.environ["CREW_EVENTS_FILE"], "a", encoding="utf-8") as out:
            out.write(json.dumps(record, separators=(",", ":")) + "\n")
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"crew hook: {type(error).__name__}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
