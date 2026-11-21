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

    let repo = Repository::new(cfg.api_key_salt.clone());
    let service = Service::new(cfg.clone(), repo.clone());
    let state = Arc::new(AppState { service, repo });

    let app = create_app(state);
    let addr = SocketAddr::from(([0, 0, 0, 0], port));
    let listener = tokio::net::TcpListener::bind(addr).await.unwrap();
    tracing::info!("Listening on http://{}", addr);

    axum::serve(listener, app).await.unwrap();
}
