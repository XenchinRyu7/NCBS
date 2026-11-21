use backend::{config::Config, create_app, AppState, Repository, Service};
use std::net::SocketAddr;
use std::sync::Arc;

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "info,backend=debug".into()),
        )
        .init();

    let cfg = Config::from_env();
    let port = cfg.port;
    tracing::info!("Starting Nusantara Core Banking System (Rust Axum 2.4.0-enterprise) on port {}", port);

    // 1. Enterprise PostgreSQL Pool Initialization (Supabase)
    let pg_pool = if let Some(ref db_url) = cfg.database_url {
        tracing::info!("Connecting to Enterprise Database Pool (Supabase PostgreSQL)...");
        match sqlx::postgres::PgPoolOptions::new()
            .max_connections(10)
            .acquire_timeout(std::time::Duration::from_secs(10))
            .connect(db_url)
            .await
        {
            Ok(pool) => {
                tracing::info!("Database Connection Pool established successfully (Supabase Online).");
                Some(pool)
            }
            Err(e) => {
                tracing::warn!("Could not connect to PostgreSQL: {}. Operating in high-availability memory mode.", e);
                None
            }
        }
    } else {
        tracing::info!("No DATABASE_URL configured. Operating in high-availability memory mode.");
        None
    };

    // 2. Enterprise Distributed Lock & Cache Initialization (Upstash Redis)
    let redis_conn = if let Some(ref redis_url) = cfg.redis_url {
        tracing::info!("Connecting to Distributed Lock & Velocity Manager (Upstash Redis)...");
        match redis::Client::open(redis_url.as_str()) {
            Ok(client) => match client.get_connection_manager().await {
                Ok(manager) => {
                    tracing::info!("Distributed Lock Manager established successfully (Upstash Redis Online).");
                    Some(manager)
                }
                Err(e) => {
                    tracing::warn!("Failed to establish Redis connection manager: {}. Operating in local lock mode.", e);
                    None
                }
            },
            Err(e) => {
                tracing::warn!("Invalid Redis URL: {}. Operating in local lock mode.", e);
                None
            }
        }
    } else {
        tracing::info!("No UPSTASH_REDIS_URL configured. Operating in local lock mode.");
        None
    };

    let repo = Repository::new_with_adapters(cfg.api_key_salt.clone(), pg_pool, redis_conn);
    let service = Service::new(cfg.clone(), repo.clone());
    let state = Arc::new(AppState { service, repo });

    let app = create_app(state);
    let addr = SocketAddr::from(([0, 0, 0, 0], port));
    let listener = tokio::net::TcpListener::bind(addr).await.unwrap();
    tracing::info!("Listening on http://{}", addr);

    axum::serve(listener, app).await.unwrap();
}
