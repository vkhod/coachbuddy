mod app;
#[allow(dead_code)]
mod contract;
#[cfg(test)]
mod contract_tests;
mod domain;
mod mcp;
#[cfg(test)]
mod tests;

use mongodb::bson::doc;
use std::env;
use std::io::{Read, Write};
use std::net::{SocketAddr, TcpStream};
use std::time::Duration;
use tracing::{error, info};

#[derive(Clone)]
pub struct AppState {
    pub db: mongodb::Database,
}

/// Hosts accepted on `/mcp` when `MCP_ALLOWED_HOSTS` is not set: loopback and
/// the compose service name.
const DEFAULT_ALLOWED_HOSTS: &str = "localhost,127.0.0.1,::1,mcp-rust";

fn port() -> u16 {
    env::var("PORT")
        .ok()
        .and_then(|p| p.parse().ok())
        .unwrap_or(8082)
}

#[tokio::main]
async fn main() {
    // `coachbuddy-mcp-rust healthcheck` is the container health check: the
    // runtime image has no curl or wget.
    if env::args().nth(1).as_deref() == Some("healthcheck") {
        std::process::exit(healthcheck(port()));
    }

    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env().unwrap_or_else(|_| "info".into()),
        )
        .init();

    // MongoDB is required in every environment: no default URI, and the
    // server does not start until a ping succeeds.
    let Some(mongo_uri) = env::var("MONGO_URI").ok().filter(|v| !v.trim().is_empty()) else {
        error!("Required environment variable MONGO_URI is not set (see .env.example)");
        std::process::exit(1);
    };
    let mongo_db_name = env::var("MONGO_DB").unwrap_or_else(|_| "coachbuddy".to_string());
    let db = match connect_mongo(&mongo_uri, &mongo_db_name).await {
        Ok(db) => db,
        Err(e) => {
            error!(
                "Cannot start without MongoDB ({}): {}",
                redact_uri(&mongo_uri),
                e
            );
            std::process::exit(1);
        }
    };
    info!("Connected to MongoDB at {}", redact_uri(&mongo_uri));

    let allowed_hosts: Vec<String> = env::var("MCP_ALLOWED_HOSTS")
        .unwrap_or_else(|_| DEFAULT_ALLOWED_HOSTS.to_string())
        .split(',')
        .map(|h| h.trim().to_string())
        .filter(|h| !h.is_empty())
        .collect();

    let app = app::router(AppState { db }, allowed_hosts);
    let addr = SocketAddr::from(([0, 0, 0, 0], port()));
    info!("coachbuddy-mcp-rust listening on http://{}", addr);

    let listener = tokio::net::TcpListener::bind(addr)
        .await
        .expect("Failed to bind TCP listener");
    axum::serve(listener, app)
        .await
        .expect("Failed to run Axum server");
}

async fn connect_mongo(uri: &str, db_name: &str) -> Result<mongodb::Database, String> {
    let mut opts = mongodb::options::ClientOptions::parse(uri)
        .await
        .map_err(|e| e.to_string())?;
    opts.server_selection_timeout = Some(Duration::from_secs(10));
    let client = mongodb::Client::with_options(opts).map_err(|e| e.to_string())?;
    let db = client.database(db_name);
    db.run_command(doc! { "ping": 1 }, None)
        .await
        .map_err(|e| e.to_string())?;
    Ok(db)
}

/// GET /health on this container. Exit code 0 only for a 200 answer.
fn healthcheck(port: u16) -> i32 {
    let check = || -> std::io::Result<bool> {
        let addr = SocketAddr::from(([127, 0, 0, 1], port));
        let mut stream = TcpStream::connect_timeout(&addr, Duration::from_secs(3))?;
        stream.set_read_timeout(Some(Duration::from_secs(5)))?;
        stream
            .write_all(b"GET /health HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")?;
        let mut head = [0u8; 12];
        stream.read_exact(&mut head)?;
        Ok(head.starts_with(b"HTTP/1.1 200"))
    };
    match check() {
        Ok(true) => 0,
        _ => 1,
    }
}

/// Hides the password in a connection URI before it is logged:
/// `mongodb://user:secret@host/` becomes `mongodb://user:***@host/`.
fn redact_uri(uri: &str) -> String {
    let Some(scheme_end) = uri.find("://") else {
        return uri.to_string();
    };
    let rest = &uri[scheme_end + 3..];
    let Some(at) = rest.find('@') else {
        return uri.to_string();
    };
    let userinfo = &rest[..at];
    match userinfo.find(':') {
        Some(colon) => format!(
            "{}://{}:***{}",
            &uri[..scheme_end],
            &userinfo[..colon],
            &rest[at..]
        ),
        None => uri.to_string(),
    }
}

#[cfg(test)]
mod redact_tests {
    use super::redact_uri;

    #[test]
    fn hides_password() {
        assert_eq!(
            redact_uri("mongodb://coachbuddy:s3cret@mongodb:27017/?authSource=admin"),
            "mongodb://coachbuddy:***@mongodb:27017/?authSource=admin"
        );
    }

    #[test]
    fn leaves_uri_without_password() {
        assert_eq!(
            redact_uri("mongodb://localhost:27017"),
            "mongodb://localhost:27017"
        );
    }
}
