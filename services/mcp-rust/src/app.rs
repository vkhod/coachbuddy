//! HTTP routes: `/health`, the MCP endpoint `/mcp`, and the internal REST
//! routes `/tools/*` used by the gateway.

use crate::mcp::{execute_named_tool, CoachBuddyMcp, ToolError};
use crate::AppState;
use axum::{
    extract::{Json, State},
    http::StatusCode,
    response::{IntoResponse, Response},
    routing::{get, post},
    Router,
};
use mongodb::bson::doc;
use rmcp::transport::streamable_http_server::{
    session::never::NeverSessionManager, StreamableHttpServerConfig, StreamableHttpService,
};
use serde_json::{json, Value};
use std::sync::Arc;
use std::time::Duration;

/// Builds the router. `allowed_hosts` is the `Host` allow-list for `/mcp`
/// (rmcp rejects other hosts, against DNS rebinding).
pub fn router(state: AppState, allowed_hosts: Vec<String>) -> Router {
    // Stateless: every POST is answered on its own, with a JSON body. The
    // tools never send server-initiated messages, so no session is kept and a
    // restart cannot strand a client.
    let config = StreamableHttpServerConfig::default()
        .with_legacy_session_mode(false)
        .with_json_response(true)
        .with_sse_keep_alive(None)
        .with_allowed_hosts(allowed_hosts);
    let mcp_state = state.clone();
    let mcp = StreamableHttpService::new(
        move || Ok(CoachBuddyMcp::new(mcp_state.clone())),
        Arc::new(NeverSessionManager::default()),
        config,
    );

    Router::new()
        .route("/health", get(health))
        .nest_service("/mcp", mcp)
        .route("/tools/history", post(history_rest))
        .route("/tools/timeline", post(timeline_rest))
        .route("/tools/qr", post(qr_rest))
        .with_state(state)
}

/// 200 when MongoDB answers a ping, 503 otherwise. The reason is logged, not
/// returned.
async fn health(State(state): State<AppState>) -> Response {
    let ping = tokio::time::timeout(
        Duration::from_secs(2),
        state.db.run_command(doc! { "ping": 1 }, None),
    )
    .await;
    let mongo_ok = match ping {
        Ok(Ok(_)) => true,
        Ok(Err(e)) => {
            tracing::warn!(error = %e, "health: MongoDB ping failed");
            false
        }
        Err(_) => {
            tracing::warn!("health: MongoDB ping timed out");
            false
        }
    };
    let (code, status) = if mongo_ok {
        (StatusCode::OK, "ok")
    } else {
        (StatusCode::SERVICE_UNAVAILABLE, "unavailable")
    };
    (
        code,
        Json(json!({
            "status": status,
            "service": "mcp-rust",
            "version": env!("CARGO_PKG_VERSION"),
            "checks": { "mongodb": if mongo_ok { "ok" } else { "unavailable" } },
            "tools": ["get_recent_coaching_history", "verify_timeline_math", "generate_qr_svg"]
        })),
    )
        .into_response()
}

/// REST answer for a tool: 200 with the result, 400 for bad arguments,
/// 503 when the tool failed (for example MongoDB down).
async fn run_rest(state: &AppState, tool: &str, args: Value) -> Response {
    match execute_named_tool(state, tool, args).await {
        Ok(out) => (StatusCode::OK, Json(out)).into_response(),
        Err(err) => {
            let code = match err {
                ToolError::InvalidArguments(_) => StatusCode::BAD_REQUEST,
                ToolError::Unknown(_) => StatusCode::NOT_FOUND,
                ToolError::Failed(_) => {
                    tracing::warn!(tool, error = %err, "REST tool call failed");
                    StatusCode::SERVICE_UNAVAILABLE
                }
            };
            (code, Json(json!({ "error": err.to_string() }))).into_response()
        }
    }
}

async fn history_rest(State(state): State<AppState>, Json(args): Json<Value>) -> Response {
    run_rest(&state, "get_recent_coaching_history", args).await
}

async fn timeline_rest(State(state): State<AppState>, Json(args): Json<Value>) -> Response {
    run_rest(&state, "verify_timeline_math", args).await
}

async fn qr_rest(State(state): State<AppState>, Json(args): Json<Value>) -> Response {
    run_rest(&state, "generate_qr_svg", args).await
}
