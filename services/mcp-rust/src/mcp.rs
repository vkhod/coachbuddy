//! The MCP server: three tools served over Streamable HTTP by rmcp.
//!
//! rmcp handles the protocol (version negotiation, `initialize`, notifications,
//! JSON-RPC errors). This module only lists the tools and runs them.
//!
//! Errors follow the MCP spec:
//! - an unknown tool is a protocol error (JSON-RPC `-32602`);
//! - bad arguments and failures while running a tool (for example MongoDB
//!   down) are tool results with `isError: true`, so the calling agent sees
//!   the reason and the section can fail visibly.

use crate::domain::history::{query_recent_coaching_history, HistoryQueryInput};
use crate::domain::qr::{generate_qr_svg, QrGenerateInput};
use crate::domain::timeline::{verify_timeline_math, VerifyTimelineInput};
use crate::AppState;
use rmcp::model::{
    CallToolRequestParams, CallToolResponse, CallToolResult, ContentBlock, Implementation,
    JsonObject, ListToolsResult, PaginatedRequestParams, ServerCapabilities, ServerConfig, Tool,
};
use rmcp::service::RequestContext;
use rmcp::{ErrorData as McpError, RoleServer, ServerHandler};
use serde_json::{json, Value};
use std::sync::Arc;
use std::time::Duration;

/// Longest a history query may take. A paused or partitioned MongoDB holds
/// pooled sockets open, so without a limit the caller waits indefinitely.
pub const HISTORY_TIMEOUT: Duration = Duration::from_secs(8);

/// Why a tool call did not produce a result.
#[derive(Debug, PartialEq)]
pub enum ToolError {
    /// No tool has this name. Reported as a protocol error.
    Unknown(String),
    /// The arguments do not match the tool's input schema.
    InvalidArguments(String),
    /// The tool ran and failed, for example because MongoDB is unreachable.
    Failed(String),
}

impl std::fmt::Display for ToolError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ToolError::Unknown(name) => write!(f, "Unknown MCP tool '{}'", name),
            ToolError::InvalidArguments(msg) | ToolError::Failed(msg) => f.write_str(msg),
        }
    }
}

/// Runs a tool by name. Shared by the MCP handler and the REST routes.
pub async fn execute_named_tool(
    state: &AppState,
    tool_name: &str,
    args: Value,
) -> Result<Value, ToolError> {
    fn parse<T: serde::de::DeserializeOwned>(tool: &str, args: Value) -> Result<T, ToolError> {
        serde_json::from_value(args)
            .map_err(|e| ToolError::InvalidArguments(format!("Invalid arguments for {tool}: {e}")))
    }
    fn to_value<T: serde::Serialize>(out: T) -> Result<Value, ToolError> {
        serde_json::to_value(out).map_err(|e| ToolError::Failed(e.to_string()))
    }

    match tool_name {
        "get_recent_coaching_history" => {
            let input: HistoryQueryInput = parse(tool_name, args)?;
            let out = tokio::time::timeout(
                HISTORY_TIMEOUT,
                query_recent_coaching_history(&state.db, input),
            )
            .await
            .map_err(|_| {
                ToolError::Failed(format!(
                    "MongoDB query timed out after {}s",
                    HISTORY_TIMEOUT.as_secs()
                ))
            })?
            .map_err(ToolError::Failed)?;
            to_value(out)
        }
        "verify_timeline_math" => {
            let input: VerifyTimelineInput = parse(tool_name, args)?;
            to_value(verify_timeline_math(input))
        }
        "generate_qr_svg" => {
            let input: QrGenerateInput = parse(tool_name, args)?;
            to_value(generate_qr_svg(input).map_err(ToolError::InvalidArguments)?)
        }
        unknown => Err(ToolError::Unknown(unknown.to_string())),
    }
}

#[derive(Clone)]
pub struct CoachBuddyMcp {
    state: AppState,
}

impl CoachBuddyMcp {
    pub fn new(state: AppState) -> Self {
        Self { state }
    }
}

impl ServerHandler for CoachBuddyMcp {
    fn get_info(&self) -> ServerConfig {
        ServerConfig::new(ServerCapabilities::builder().enable_tools().build()).with_server_info(
            Implementation::new("coachbuddy-mcp-rust", env!("CARGO_PKG_VERSION")),
        )
    }

    async fn list_tools(
        &self,
        _request: Option<PaginatedRequestParams>,
        _context: RequestContext<RoleServer>,
    ) -> Result<ListToolsResult, McpError> {
        Ok(ListToolsResult::with_all_items(tool_definitions()))
    }

    async fn call_tool(
        &self,
        request: CallToolRequestParams,
        _context: RequestContext<RoleServer>,
    ) -> Result<CallToolResponse, McpError> {
        let args = Value::Object(request.arguments.unwrap_or_default());
        let result = match execute_named_tool(&self.state, &request.name, args).await {
            Ok(structured) => CallToolResult::structured(structured),
            Err(err @ ToolError::Unknown(_)) => {
                return Err(McpError::invalid_params(err.to_string(), None));
            }
            Err(err) => {
                tracing::warn!(tool = %request.name, error = %err, "tool call failed");
                CallToolResult::error(vec![ContentBlock::text(err.to_string())])
            }
        };
        Ok(result.into())
    }
}

fn schema(value: Value) -> Arc<JsonObject> {
    match value {
        Value::Object(map) => Arc::new(map),
        _ => unreachable!("tool input schemas are JSON objects"),
    }
}

pub fn tool_definitions() -> Vec<Tool> {
    vec![
        Tool::new(
            "get_recent_coaching_history",
            "Queries MongoDB plans for the coach's recent completed warm-up games, styles, and drills to prevent repetitive programming across consecutive days.",
            schema(json!({
                "type": "object",
                "properties": {
                    "coachId": { "type": "string", "description": "The unique coach UUID." },
                    "limit": {
                        "type": "integer",
                        "default": 5,
                        "description": "Maximum number of past sessions to inspect (1..20)."
                    }
                },
                "required": ["coachId"]
            })),
        ),
        Tool::new(
            "verify_timeline_math",
            "Deterministic verifier for LLM-generated minute-by-minute class timelines. Verifies 0..totalMinutes contiguity, positive block durations, and exact duration sum without modifying block cues or notes.",
            schema(json!({
                "type": "object",
                "properties": {
                    "totalMinutes": {
                        "type": "integer",
                        "default": 60,
                        "description": "Total class duration in minutes (e.g. 60)."
                    },
                    "blocks": {
                        "type": "array",
                        "items": {
                            "type": "object",
                            "properties": {
                                "startMin": { "type": "integer" },
                                "endMin": { "type": "integer" },
                                "label": { "type": "string" },
                                "type": { "type": "string" },
                                "notes": { "type": "string" }
                            },
                            "required": ["startMin", "endMin", "label"]
                        },
                        "description": "List of scheduled workout and transition blocks to verify."
                    }
                },
                "required": ["totalMinutes", "blocks"]
            })),
        ),
        Tool::new(
            "generate_qr_svg",
            "Generates a high-contrast SVG QR code for a share URL. Fails if the data is empty or too long for a QR code.",
            schema(json!({
                "type": "object",
                "properties": {
                    "data": {
                        "type": "string",
                        "description": "URL or text payload to encode into the QR code."
                    },
                    "size": {
                        "type": "integer",
                        "default": 256,
                        "description": "SVG width/height in pixels (128..1024)."
                    }
                },
                "required": ["data"]
            })),
        ),
    ]
}
