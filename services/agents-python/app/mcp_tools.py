"""The mcp-rust tools, reached through ADK's McpToolset over Streamable HTTP.

The pipeline calls the tools itself (decision 5.2 in the capability report): history
before the Warm-up agent, verify_timeline_math after the Timeline agent. A tool that
cannot be reached, or that answers isError, raises ToolError, and the section that
needed it fails (D3). Nothing here computes a tool's answer locally.
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
from typing import Any, Dict, Optional, Protocol

from google.adk.tools.mcp_tool.mcp_session_manager import StreamableHTTPConnectionParams
from google.adk.tools.mcp_tool.mcp_toolset import McpToolset

logger = logging.getLogger("coachbuddy.mcp_tools")

TOOL_NAMES = ("get_recent_coaching_history", "verify_timeline_math")


class ToolError(RuntimeError):
    """An MCP tool could not be called, or answered with isError."""


class Tools(Protocol):
    async def call(self, name: str, args: Dict[str, Any]) -> Dict[str, Any]: ...


def tool_result(name: str, result: Any) -> Dict[str, Any]:
    """The structured answer of a tool result, or ToolError for isError and odd shapes."""
    if not isinstance(result, dict):
        raise ToolError(f"{name}: unexpected result {type(result).__name__}")
    if result.get("isError"):
        content = result.get("content") or [{}]
        text = content[0].get("text", "") if isinstance(content[0], dict) else ""
        raise ToolError(f"{name}: {text or 'tool reported an error'}")
    structured = result.get("structuredContent")
    if isinstance(structured, dict):
        return structured
    content = result.get("content") or []
    if content and isinstance(content[0], dict) and content[0].get("text"):
        try:
            parsed = json.loads(content[0]["text"])
        except json.JSONDecodeError as exc:
            raise ToolError(f"{name}: result is not JSON") from exc
        if isinstance(parsed, dict):
            return parsed
    raise ToolError(f"{name}: empty result")


class McpTools:
    """One McpToolset for the process; the MCP session is opened on first use."""

    def __init__(self, url: Optional[str] = None, timeout_sec: float = 10.0) -> None:
        base = (url or os.getenv("MCP_RUST_URL", "http://localhost:8082")).rstrip("/")
        self.url = base if base.endswith("/mcp") else f"{base}/mcp"
        self._toolset = McpToolset(
            connection_params=StreamableHTTPConnectionParams(url=self.url, timeout=timeout_sec),
            tool_filter=list(TOOL_NAMES),
        )
        self._tools: Dict[str, Any] = {}
        self._lock = asyncio.Lock()

    async def _tool(self, name: str) -> Any:
        async with self._lock:
            if name not in self._tools:
                try:
                    tools = await self._toolset.get_tools()
                except Exception as exc:
                    raise ToolError(f"mcp-rust unreachable at {self.url}: {exc or type(exc).__name__}") from exc
                self._tools = {t.name: t for t in tools}
            tool = self._tools.get(name)
        if tool is None:
            raise ToolError(f"mcp-rust has no tool {name}")
        return tool

    async def call(self, name: str, args: Dict[str, Any]) -> Dict[str, Any]:
        tool = await self._tool(name)
        try:
            result = await tool.run_async(args=args, tool_context=None)
        except Exception as exc:
            # A dropped session is re-opened on the next call.
            async with self._lock:
                self._tools = {}
            raise ToolError(f"{name} failed: {exc or type(exc).__name__}") from exc
        return tool_result(name, result)

    async def close(self) -> None:
        try:
            await self._toolset.close()
        except Exception as exc:  # pragma: no cover
            logger.debug("closing McpToolset: %s", exc)
