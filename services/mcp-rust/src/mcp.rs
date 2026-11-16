use crate::domain::history::{
    query_recent_coaching_history, HistoryQueryInput, HistoryQueryOutput,
};
use crate::domain::qr::{generate_qr_svg, QrGenerateInput};
use crate::domain::timeline::{verify_timeline_math, VerifyTimelineInput};
use crate::AppState;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

#[derive(Debug, Deserialize)]
pub struct JsonRpcRequest {
    #[allow(dead_code)]
    pub jsonrpc: Option<String>,
    pub id: Option<Value>,
    pub method: String,
    #[serde(default)]
    pub params: Value,
}

#[derive(Debug, Serialize)]
pub struct JsonRpcResponse {
    pub jsonrpc: String,
    pub id: Value,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub result: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<JsonRpcError>,
}

#[derive(Debug, Serialize)]
pub struct JsonRpcError {
    pub code: i32,
    pub message: String,
}

pub async fn handle_mcp_request(state: &AppState, req: JsonRpcRequest) -> JsonRpcResponse {
    let id = req.id.unwrap_or(Value::Null);

    match req.method.as_str() {
        "initialize" => JsonRpcResponse {
            jsonrpc: "2.0".to_string(),
            id,
            result: Some(json!({
                "protocolVersion": "2024-11-05",
                "capabilities": {
                    "tools": {}
                },
                "serverInfo": {
                    "name": "coachbuddy-mcp-rust",
                    "version": "0.1.0"
                }
            })),
            error: None,
        },
        "notifications/initialized" | "ping" => JsonRpcResponse {
            jsonrpc: "2.0".to_string(),
            id,
            result: Some(json!({})),
            error: None,
        },
        "tools/list" => JsonRpcResponse {
            jsonrpc: "2.0".to_string(),
            id,
            result: Some(json!({
                "tools": get_mcp_tool_definitions()
            })),
            error: None,
        },
        "tools/call" => {
            let tool_name = req
                .params
                .get("name")
                .and_then(|v| v.as_str())
                .unwrap_or("");
            let args = req
                .params
                .get("arguments")
                .cloned()
                .unwrap_or_else(|| json!({}));

            match execute_named_tool(state, tool_name, args).await {
                Ok(structured) => {
                    let serialized =
                        serde_json::to_string(&structured).unwrap_or_else(|_| "{}".to_string());
                    JsonRpcResponse {
                        jsonrpc: "2.0".to_string(),
                        id,
                        result: Some(json!({
                            "content": [
                                {
                                    "type": "text",
                                    "text": serialized
                                }
                            ],
                            "structuredContent": structured,
                            "isError": false
                        })),
                        error: None,
                    }
                }
                Err(err_msg) => JsonRpcResponse {
                    jsonrpc: "2.0".to_string(),
                    id,
                    result: None,
                    error: Some(JsonRpcError {
                        code: -32602,
                        message: err_msg,
                    }),
                },
            }
        }
        other => JsonRpcResponse {
            jsonrpc: "2.0".to_string(),
            id,
            result: None,
            error: Some(JsonRpcError {
                code: -32601,
                message: format!("Unsupported MCP method: {}", other),
            }),
        },
    }
}

pub async fn execute_named_tool(
    state: &AppState,
    tool_name: &str,
    args: Value,
) -> Result<Value, String> {
    match tool_name {
        "get_recent_coaching_history" => {
            let parsed: HistoryQueryInput = serde_json::from_value(args)
                .map_err(|e| format!("Invalid arguments for get_recent_coaching_history: {}", e))?;
            if let Some(db) = &state.db {
                let out = query_recent_coaching_history(db, parsed).await?;
                serde_json::to_value(out).map_err(|e| e.to_string())
            } else {
                let fallback = HistoryQueryOutput {
                    coach_id: parsed.coach_id,
                    recent_warmups: vec![],
                    count: 0,
                    summary: "Database not configured or currently unreachable; proceeding with cold-start warmup recommendations.".to_string(),
                };
                serde_json::to_value(fallback).map_err(|e| e.to_string())
            }
        }
        "verify_timeline_math" => {
            let parsed: VerifyTimelineInput = serde_json::from_value(args)
                .map_err(|e| format!("Invalid arguments for verify_timeline_math: {}", e))?;
            let out = verify_timeline_math(parsed);
            serde_json::to_value(out).map_err(|e| e.to_string())
        }
        "generate_qr_svg" => {
            let parsed: QrGenerateInput = serde_json::from_value(args)
                .map_err(|e| format!("Invalid arguments for generate_qr_svg: {}", e))?;
            let out = generate_qr_svg(parsed);
            serde_json::to_value(out).map_err(|e| e.to_string())
        }
        unknown => Err(format!("Unknown MCP tool '{}'", unknown)),
    }
}

fn get_mcp_tool_definitions() -> Vec<Value> {
    vec![
        json!({
            "name": "get_recent_coaching_history",
            "description": "Queries MongoDB wod_sessions for the coach's recent completed warm-up games, styles, and drills to prevent repetitive programming across consecutive days.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "coachId": {
                        "type": "string",
                        "description": "The unique coach UUID."
                    },
                    "limit": {
                        "type": "integer",
                        "default": 5,
                        "description": "Maximum number of past sessions to inspect (1..20)."
                    }
                },
                "required": ["coachId"]
            }
        }),
        json!({
            "name": "verify_timeline_math",
            "description": "Deterministic verifier for LLM-generated minute-by-minute class timelines. Verifies 0..totalMinutes contiguity, positive block durations, and exact duration sum without modifying block cues or notes.",
            "inputSchema": {
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
            }
        }),
        json!({
            "name": "generate_qr_svg",
            "description": "Generates a crisp, high-contrast dark-on-light SVG QR code inside a signature Volt accent frame for instant camera scanning and URL sharing.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "data": {
                        "type": "string",
                        "description": "URL or text payload to encode into the QR code."
                    },
                    "size": {
                        "type": "integer",
                        "default": 256,
                        "description": "SVG width/height in pixels."
                    }
                },
                "required": ["data"]
            }
        }),
    ]
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_mcp_initialize() {
        let state = AppState { db: None };
        let req = JsonRpcRequest {
            jsonrpc: Some("2.0".to_string()),
            id: Some(json!(1)),
            method: "initialize".to_string(),
            params: json!({}),
        };
        let resp = handle_mcp_request(&state, req).await;
        assert_eq!(resp.id, json!(1));
        let res = resp.result.unwrap();
        assert_eq!(res["serverInfo"]["name"], "coachbuddy-mcp-rust");
    }

    #[tokio::test]
    async fn test_mcp_tools_list() {
        let state = AppState { db: None };
        let req = JsonRpcRequest {
            jsonrpc: Some("2.0".to_string()),
            id: Some(json!(2)),
            method: "tools/list".to_string(),
            params: json!({}),
        };
        let resp = handle_mcp_request(&state, req).await;
        let tools = resp.result.unwrap()["tools"].as_array().unwrap().clone();
        assert_eq!(tools.len(), 3);
        let names: Vec<&str> = tools.iter().map(|t| t["name"].as_str().unwrap()).collect();
        assert!(names.contains(&"get_recent_coaching_history"));
        assert!(names.contains(&"verify_timeline_math"));
        assert!(names.contains(&"generate_qr_svg"));
    }

    #[tokio::test]
    async fn test_mcp_tools_call_timeline() {
        let state = AppState { db: None };
        let req = JsonRpcRequest {
            jsonrpc: Some("2.0".to_string()),
            id: Some(json!(3)),
            method: "tools/call".to_string(),
            params: json!({
                "name": "verify_timeline_math",
                "arguments": {
                    "totalMinutes": 60,
                    "blocks": [
                        { "startMin": 0, "endMin": 30, "label": "Part A" },
                        { "startMin": 30, "endMin": 60, "label": "Part B" }
                    ]
                }
            }),
        };
        let resp = handle_mcp_request(&state, req).await;
        assert!(resp.error.is_none());
        let res = resp.result.unwrap();
        let structured = &res["structuredContent"];
        assert_eq!(structured["isValid"], true);
        assert_eq!(structured["feasibilityStatus"], "OPTIMAL");
    }
}
