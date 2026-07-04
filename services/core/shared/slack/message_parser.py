import re
from typing import Any


class SlackMessageParser:
    def __init__(self, mention_pattern: re.Pattern[str] | None = None) -> None:
        self._mention_pattern = mention_pattern or re.compile(r"<@[A-Z0-9]+>")

    def strip_mention(self, text: str) -> str:
        return self._mention_pattern.sub("", text or "").strip()

    def extract_question(self, event: dict[str, Any]) -> str:
        return self.strip_mention(event.get("text", ""))
