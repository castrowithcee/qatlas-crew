#!/usr/bin/env python3
"""Record allowlisted Codex hook or notify signals for a disposable session."""

import json
import os
import re
import sys
from datetime import datetime, timezone

EVENTS = {
    "SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest",
    "PostToolUse", "PreCompact", "PostCompact", "SubagentStart",
    "SubagentStop", "Stop", "Interrupt", "SessionEnd",
}
TOKEN = re.compile(r"[A-Za-z0-9_.:-]{1,128}\Z")


def token(value):
    return value if isinstance(value, str) and TOKEN.fullmatch(value) else None


def main():
    try:
        source = json.loads(sys.argv[1] if len(sys.argv) == 2 else sys.stdin.read())
        if not isinstance(source, dict):
            return 0
        notify = len(sys.argv) == 2
        name = source.get("type") if notify else source.get("hook_event_name")
        if notify and name != "agent-turn-complete" or not notify and name not in EVENTS:
            return 0
        record = {
            "at": datetime.now(timezone.utc).isoformat(),
            "event": name,
            "crew_session_id": token(os.environ.get("CREW_SESSION_ID")),
            "host_session_id": token(source.get("thread-id" if notify else "session_id")),
        }
        if name in {"PreToolUse", "PermissionRequest", "PostToolUse"}:
            record["tool"] = token(source.get("tool_name"))
        if name == "SessionStart":
            record["source"] = token(source.get("source"))
        if name == "SessionEnd":
            record["reason"] = token(source.get("reason"))
        if name in {"SubagentStart", "SubagentStop"}:
            record["agent_id"] = token(source.get("agent_id"))
            record["agent_type"] = token(source.get("agent_type"))
        with open(os.environ["CREW_EVENTS_FILE"], "a", encoding="utf-8") as out:
            out.write(json.dumps(record, separators=(",", ":")) + "\n")
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"crew hook: {type(error).__name__}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
