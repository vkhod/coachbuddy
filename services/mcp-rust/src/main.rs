mod domain;
mod mcp;

use axum::{
    extract::{Json, State},
    routing::{get, post},
    Router,
};
use domain::{
    history::{query_recent_coaching_history, HistoryQueryInput, HistoryQueryOutput},
    qr::{generate_qr_svg, QrGenerateInput, QrGenerateOutput},
    timeline::{verify_timeline_math, VerifyTimelineInput, VerifyTimelineOutput},
};
use mcp::{handle_mcp_request, JsonRpcRequest, JsonRpcResponse};
use serde_json::{json, Value};
use std::env;
use std::net::SocketAddr;
use tower_http::cors::{Any, CorsLayer};
use tracing::{info, warn};

#[derive(Clone)]
pub struct AppState {
    pub db: Option<mongodb::Database>,
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "info".into()),
        )
        .init();

    let port: u16 = env::var("PORT")
        .ok()
        .and_then(|p| p.parse().ok())
        .unwrap_or(8082);

    let mongo_uri = env::var("MONGO_URI").unwrap_or_else(|_| "mongodb://localhost:27017".to_string());
    let mongo_db_name = env::var("MONGO_DB").unwrap_or_else(|_| "coachbuddy".to_string());

    let db = match mongodb::Client::with_uri_str(&mongo_uri).await {
        Ok(client) => {
            info!("Connected to MongoDB at {}", mongo_uri);
            Some(client.database(&mongo_db_name))
        }
        Err(e) => {
            warn!(
                "Could not connect to MongoDB ({}): {}. Running in standalone mode.",
                mongo_uri, e
            );
            None
        }
    };

    let state = AppState { db };

    let cors = CorsLayer::new()
        .allow_origin(Any)
        .allow_methods(Any)
        .allow_headers(Any);

    let app = Router::new()
        .route("/health", get(health_handler))
        .route("/mcp", post(mcp_rpc_handler))
        .route("/tools/history", post(history_rest_handler))
        .route("/tools/timeline", post(timeline_rest_handler))
        .route("/tools/qr", post(qr_rest_handler))
        .layer(cors)
        .with_state(state);

    let addr = SocketAddr::from(([0, 0, 0, 0], port));
    info!("coachbuddy-mcp-rust listening on http://{}", addr);

    let listener = tokio::net::TcpListener::bind(addr)
        .await
        .expect("Failed to bind TCP listener");

    axum::serve(listener, app)
        .await
        .expect("Failed to run Axum server");
}

async fn health_handler(State(state): State<AppState>) -> Json<Value> {
    Json(json!({
        "status": "ok",
        "service": "mcp-rust",
        "version": "0.1.0",
        "mongoConnected": state.db.is_some(),
        "tools": [
            "get_recent_coaching_history",
            "verify_timeline_math",
            "generate_qr_svg"
        ]
    }))
}

async fn mcp_rpc_handler(
    State(state): State<AppState>,
    Json(req): Json<JsonRpcRequest>,
) -> Json<JsonRpcResponse> {
    Json(handle_mcp_request(&state, req).await)
}

async fn history_rest_handler(
    State(state): State<AppState>,
    Json(input): Json<HistoryQueryInput>,
) -> Json<HistoryQueryOutput> {
    if let Some(db) = &state.db {
        match query_recent_coaching_history(db, input.clone()).await {
            Ok(out) => Json(out),
            Err(err) => Json(HistoryQueryOutput {
                coach_id: input.coach_id,
                recent_warmups: vec![],
                count: 0,
                summary: format!("Error querying history: {}", err),
            }),
        }
    } else {
        Json(HistoryQueryOutput {
            coach_id: input.coach_id,
            recent_warmups: vec![],
            count: 0,
            summary: "MongoDB not connected; no past session history available.".to_string(),
        })
    }
}

async fn timeline_rest_handler(
    Json(input): Json<VerifyTimelineInput>,
) -> Json<VerifyTimelineOutput> {
    Json(verify_timeline_math(input))
}

async fn qr_rest_handler(Json(input): Json<QrGenerateInput>) -> Json<QrGenerateOutput> {
    Json(generate_qr_svg(input))
}
