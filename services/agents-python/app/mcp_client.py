from __future__ import annotations

import json
import logging
import os
import uuid
from typing import Any, Dict, List, Optional

import httpx

logger = logging.getLogger("coachbuddy.mcp_client")


class RustMcpClient:
    """Client for the CoachBuddy Rust MCP Server (`mcp-rust`) over JSON-RPC 2.0 (`/mcp`).

    Exposes the deterministic Rust MCP tools implemented in Phase 1:
    - `get_recent_coaching_history`: Queries MongoDB `wod_sessions` for the coach's last 5 warm-ups.
    - `verify_timeline_math`: Pure contiguous minute-by-minute schedule math verifier (`0..totalMinutes`).
    """

    def __init__(self, base_url: Optional[str] = None, timeout_sec: float = 6.0) -> None:
        raw_url = base_url or os.getenv("MCP_RUST_URL", "http://localhost:8082")
        self.base_url = raw_url.rstrip("/")
        self.mcp_endpoint = f"{self.base_url}/mcp"
        self.timeout_sec = timeout_sec

    async def call_tool(self, tool_name: str, arguments: Dict[str, Any]) -> Dict[str, Any]:
        """Invokes a tool on the Rust MCP server via JSON-RPC `tools/call`."""
        rpc_payload = {
            "jsonrpc": "2.0",
            "id": f"adk-{uuid.uuid4().hex[:8]}",
            "method": "tools/call",
            "params": {
                "name": tool_name,
                "arguments": arguments,
            },
        }
        try:
            async with httpx.AsyncClient(timeout=self.timeout_sec) as client:
                resp = await client.post(self.mcp_endpoint, json=rpc_payload)
                resp.raise_for_status()
                body = resp.json()

                if "error" in body and body["error"]:
                    raise RuntimeError(f"MCP error: {body['error']}")

                result = body.get("result") or {}
                if "structuredContent" in result and isinstance(result["structuredContent"], dict):
                    return result["structuredContent"]

                content_list = result.get("content") or []
                if content_list and isinstance(content_list[0], dict):
                    text_val = content_list[0].get("text", "{}")
                    return json.loads(text_val)
        except Exception as exc:
            logger.warning(
                "Rust MCP server (%s) call to '%s' failed (%s); using local fallback.",
                self.mcp_endpoint,
                tool_name,
                exc,
            )

        return self._local_fallback(tool_name, arguments)

    async def get_recent_coaching_history(self, coach_id: str, limit: int = 5) -> Dict[str, Any]:
        """Calls Rust MCP `get_recent_coaching_history` to prevent repetitive warm-up games."""
        return await self.call_tool(
            "get_recent_coaching_history",
            {"coachId": coach_id, "limit": limit},
        )

    async def verify_timeline_math(
        self,
        total_minutes: int,
        blocks: List[Dict[str, Any]],
    ) -> Dict[str, Any]:
        """Calls Rust MCP `verify_timeline_math` to verify 0..totalMinutes contiguity & sum."""
        return await self.call_tool(
            "verify_timeline_math",
            {
                "totalMinutes": total_minutes,
                "blocks": blocks,
            },
        )

    def _local_fallback(self, tool_name: str, arguments: Dict[str, Any]) -> Dict[str, Any]:
        if tool_name == "get_recent_coaching_history":
            coach_id = str(arguments.get("coachId") or "unknown")
            return {
                "coachId": coach_id,
                "recentWarmups": [],
                "count": 0,
                "summary": f"No previous completed sessions found for coach {coach_id}. Fresh programming slate.",
            }

        if tool_name == "verify_timeline_math":
            total_min = int(arguments.get("totalMinutes") or 60)
            blocks = arguments.get("blocks") or []
            cursor = 0
            issues: List[Dict[str, Any]] = []
            has_gaps = False
            for idx, b in enumerate(blocks):
                start = int(b.get("startMin", 0))
                end = int(b.get("endMin", 0))
                label = str(b.get("label", f"Block {idx}"))
                if end <= start:
                    issues.append({"blockIndex": idx, "blockLabel": label, "issueType": "INVALID_DURATION"})
                if start != cursor:
                    has_gaps = True
                    issues.append({"blockIndex": idx, "blockLabel": label, "issueType": "GAP" if start > cursor else "OVERLAP"})
                cursor = end

            discrepancy = total_min - cursor
            if discrepancy != 0:
                issues.append({"blockIndex": max(len(blocks) - 1, 0), "blockLabel": "End of Class", "issueType": "SUM_MISMATCH"})

            is_valid = len(issues) == 0
            status = "OPTIMAL" if is_valid else ("TIGHT" if not has_gaps and abs(discrepancy) <= 5 else "INVALID")
            warnings = (
                ["Timeline verified: 100% contiguous 0..totalMinutes coverage with zero gaps or collisions."]
                if is_valid
                else [f"Schedule discrepancy of {abs(discrepancy)} minute(s) detected against {total_min}-min cap."]
            )
            return {
                "isValid": is_valid,
                "totalMinutes": total_min,
                "scheduledMinutes": cursor,
                "discrepancyMinutes": discrepancy,
                "feasibilityStatus": status,
                "coachWarnings": warnings,
                "issues": issues,
                "blocks": blocks,
            }

        raise ValueError(f"Unsupported MCP tool: {tool_name}")
