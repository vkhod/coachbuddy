//! Router tests: the MCP protocol over Streamable HTTP, the REST routes, and
//! health. They run the real router in process.
//!
//! Without a database, `unreachable_state()` points at a port where nothing
//! listens, so every MongoDB call fails fast. History tests against a real
//! MongoDB run when `MCP_TEST_MONGO_URI` is set (CI sets it).

use crate::{app, AppState};
use axum::body::{to_bytes, Body};
use axum::http::{Request, StatusCode};
use axum::Router;
use mongodb::bson::doc;
use serde_json::{json, Value};
use std::time::Duration;
use tower::ServiceExt;

const PROTOCOL: &str = "2025-06-18";

async fn unreachable_state() -> AppState {
    let mut opts = mongodb::options::ClientOptions::parse("mongodb://127.0.0.1:1")
        .await
        .unwrap();
    opts.server_selection_timeout = Some(Duration::from_millis(300));
    opts.connect_timeout = Some(Duration::from_millis(300));
    let client = mongodb::Client::with_options(opts).unwrap();
    AppState {
        db: client.database("coachbuddy_test"),
    }
}

fn router(state: AppState) -> Router {
    app::router(state, vec!["localhost".into()])
}

fn mcp_request(body: Value) -> Request<Body> {
    Request::post("/mcp")
        .header("host", "localhost")
        .header("content-type", "application/json")
        .header("accept", "application/json, text/event-stream")
        .header("mcp-protocol-version", PROTOCOL)
        .body(Body::from(body.to_string()))
        .unwrap()
}

async fn send(router: Router, req: Request<Body>) -> (StatusCode, String) {
    let resp = router.oneshot(req).await.unwrap();
    let status = resp.status();
    let bytes = to_bytes(resp.into_body(), 4 << 20).await.unwrap();
    (status, String::from_utf8(bytes.to_vec()).unwrap())
}

async fn rpc(router: Router, body: Value) -> Value {
    let (status, text) = send(router, mcp_request(body)).await;
    assert_eq!(status, StatusCode::OK, "{text}");
    serde_json::from_str(&text).unwrap_or_else(|e| panic!("not JSON ({e}): {text}"))
}

fn call(id: i64, name: &str, arguments: Value) -> Value {
    json!({"jsonrpc": "2.0", "id": id, "method": "tools/call",
           "params": {"name": name, "arguments": arguments}})
}

// --- MCP protocol ---

#[tokio::test]
async fn initialize_negotiates_the_client_version() {
    let r = rpc(
        router(unreachable_state().await),
        json!({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
            "protocolVersion": PROTOCOL, "capabilities": {},
            "clientInfo": {"name": "test", "version": "1"}}}),
    )
    .await;
    assert_eq!(r["result"]["protocolVersion"], PROTOCOL);
    assert_eq!(r["result"]["serverInfo"]["name"], "coachbuddy-mcp-rust");
    assert!(r["result"]["capabilities"]["tools"].is_object());
}

#[tokio::test]
async fn initialized_notification_is_202_with_no_body() {
    let (status, body) = send(
        router(unreachable_state().await),
        mcp_request(json!({"jsonrpc": "2.0", "method": "notifications/initialized"})),
    )
    .await;
    assert_eq!(status, StatusCode::ACCEPTED);
    assert!(body.is_empty(), "body: {body}");
}

#[tokio::test]
async fn tools_list_has_the_three_tools() {
    let r = rpc(
        router(unreachable_state().await),
        json!({"jsonrpc": "2.0", "id": 2, "method": "tools/list"}),
    )
    .await;
    let mut names: Vec<&str> = r["result"]["tools"]
        .as_array()
        .unwrap()
        .iter()
        .map(|t| t["name"].as_str().unwrap())
        .collect();
    names.sort();
    assert_eq!(
        names,
        [
            "generate_qr_svg",
            "get_recent_coaching_history",
            "verify_timeline_math"
        ]
    );
}

#[tokio::test]
async fn timeline_call_returns_structured_content() {
    let r = rpc(
        router(unreachable_state().await),
        call(
            3,
            "verify_timeline_math",
            json!({"totalMinutes": 60, "blocks": [
                {"startMin": 0, "endMin": 30, "label": "Build"},
                {"startMin": 30, "endMin": 60, "label": "Engine"}]}),
        ),
    )
    .await;
    let res = &r["result"];
    assert_eq!(res["isError"], false);
    assert_eq!(res["structuredContent"]["isValid"], true);
    assert_eq!(res["structuredContent"]["feasibilityStatus"], "OPTIMAL");
}

#[tokio::test]
async fn bad_arguments_are_a_tool_error_the_agent_can_read() {
    let r = rpc(
        router(unreachable_state().await),
        call(4, "verify_timeline_math", json!({"blocks": "not a list"})),
    )
    .await;
    assert!(r.get("error").is_none(), "must be a tool result: {r}");
    assert_eq!(r["result"]["isError"], true);
    let text = r["result"]["content"][0]["text"].as_str().unwrap();
    assert!(
        text.contains("Invalid arguments for verify_timeline_math"),
        "{text}"
    );
}

#[tokio::test]
async fn unknown_tool_is_a_protocol_error() {
    let r = rpc(
        router(unreachable_state().await),
        call(5, "nope", json!({})),
    )
    .await;
    assert_eq!(r["error"]["code"], -32602);
    assert!(r.get("result").is_none());
}

#[tokio::test]
async fn history_with_mongodb_down_is_a_tool_error_not_an_empty_history() {
    let r = rpc(
        router(unreachable_state().await),
        call(6, "get_recent_coaching_history", json!({"coachId": "c1"})),
    )
    .await;
    assert_eq!(r["result"]["isError"], true, "{r}");
    assert!(r["result"].get("structuredContent").is_none());
    let text = r["result"]["content"][0]["text"].as_str().unwrap();
    assert!(text.contains("MongoDB"), "{text}");
}

#[tokio::test]
async fn qr_with_empty_data_is_a_tool_error() {
    let r = rpc(
        router(unreachable_state().await),
        call(7, "generate_qr_svg", json!({"data": ""})),
    )
    .await;
    assert_eq!(r["result"]["isError"], true);
    assert!(!r.to_string().contains("<svg"));
}

#[tokio::test]
async fn request_without_streamable_http_accept_is_406() {
    let req = Request::post("/mcp")
        .header("host", "localhost")
        .header("content-type", "application/json")
        .body(Body::from(
            json!({"jsonrpc": "2.0", "id": 8, "method": "tools/list"}).to_string(),
        ))
        .unwrap();
    let (status, _) = send(router(unreachable_state().await), req).await;
    assert_eq!(status, StatusCode::NOT_ACCEPTABLE);
}

#[tokio::test]
async fn unknown_host_is_rejected() {
    let mut req = mcp_request(json!({"jsonrpc": "2.0", "id": 9, "method": "tools/list"}));
    req.headers_mut()
        .insert("host", "evil.example".parse().unwrap());
    let (status, _) = send(router(unreachable_state().await), req).await;
    assert_eq!(status, StatusCode::FORBIDDEN);
}

// --- REST routes and health ---

fn rest(path: &str, body: Value) -> Request<Body> {
    Request::post(path)
        .header("content-type", "application/json")
        .body(Body::from(body.to_string()))
        .unwrap()
}

#[tokio::test]
async fn rest_history_with_mongodb_down_is_503() {
    let (status, body) = send(
        router(unreachable_state().await),
        rest("/tools/history", json!({"coachId": "c1"})),
    )
    .await;
    assert_eq!(status, StatusCode::SERVICE_UNAVAILABLE, "{body}");
    assert!(!body.contains("recentWarmups"), "{body}");
}

#[tokio::test]
async fn rest_qr() {
    let (status, body) = send(
        router(unreachable_state().await),
        rest(
            "/tools/qr",
            json!({"data": "https://cocoach.vkhod.dev/s/cb-0123456789ab"}),
        ),
    )
    .await;
    assert_eq!(status, StatusCode::OK);
    assert!(body.contains("<svg"));

    let (status, body) = send(
        router(unreachable_state().await),
        rest("/tools/qr", json!({"data": "  "})),
    )
    .await;
    assert_eq!(status, StatusCode::BAD_REQUEST);
    assert!(!body.contains("<svg"));
}

#[tokio::test]
async fn health_with_mongodb_down_is_503() {
    let (status, body) = send(
        router(unreachable_state().await),
        Request::get("/health").body(Body::empty()).unwrap(),
    )
    .await;
    assert_eq!(status, StatusCode::SERVICE_UNAVAILABLE);
    let v: Value = serde_json::from_str(&body).unwrap();
    assert_eq!(v["checks"]["mongodb"], "unavailable");
}

// --- Against a real MongoDB ---

async fn real_state() -> Option<(AppState, String)> {
    let uri = std::env::var("MCP_TEST_MONGO_URI").ok()?;
    let client = mongodb::Client::with_uri_str(&uri).await.unwrap();
    let name = format!("coachbuddy_test_{}", std::process::id());
    Some((
        AppState {
            db: client.database(&name),
        },
        name,
    ))
}

#[tokio::test]
async fn real_mongodb_history_and_health() {
    let Some((state, _)) = real_state().await else {
        eprintln!("skipped: set MCP_TEST_MONGO_URI to run");
        return;
    };
    let coll = state
        .db
        .collection::<mongodb::bson::Document>("wod_sessions");
    coll.insert_many(
        vec![
            doc! {"_id": "old", "coachId": "c1", "status": "completed", "createdAt": "2026-11-16T10:00:00Z",
                  "warmup": {"gameTitle": "Partner Mirror", "drillNames": ["Scap Pulls"]}},
            doc! {"_id": "new", "coachId": "c1", "status": "completed", "createdAt": "2026-11-17T10:00:00Z",
                  "warmup": {"gameTitle": "Tabata Engine Relay", "drillNames": ["Air Squats"]}},
            doc! {"_id": "draft", "coachId": "c1", "status": "draft", "createdAt": "2026-11-18T10:00:00Z",
                  "warmup": {"gameTitle": "Not finished"}},
            doc! {"_id": "other", "coachId": "c2", "status": "completed", "createdAt": "2026-11-18T10:00:00Z",
                  "warmup": {"gameTitle": "Another coach"}},
        ],
        None,
    )
    .await
    .unwrap();

    let r = rpc(
        router(state.clone()),
        call(
            1,
            "get_recent_coaching_history",
            json!({"coachId": "c1", "limit": 5}),
        ),
    )
    .await;
    let out = &r["result"]["structuredContent"];
    assert_eq!(r["result"]["isError"], false, "{r}");
    assert_eq!(out["count"], 2, "only c1's completed sessions: {out}");
    assert_eq!(out["recentWarmups"][0]["gameTitle"], "Tabata Engine Relay");
    assert_eq!(out["recentWarmups"][1]["gameTitle"], "Partner Mirror");

    let (status, _) = send(
        router(state.clone()),
        Request::get("/health").body(Body::empty()).unwrap(),
    )
    .await;
    assert_eq!(status, StatusCode::OK);

    state.db.drop(None).await.unwrap();
}
