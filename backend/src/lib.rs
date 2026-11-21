pub mod config;
pub mod handlers;
pub mod middleware;
pub mod models;
pub mod repository;
pub mod service;

use axum::{
    routing::{get, patch, post},
    Router,
};
pub use config::Config;
pub use middleware::AppState;
pub use repository::Repository;
pub use service::Service;
use std::sync::Arc;
use tower_http::cors::{Any, CorsLayer};
use tower_http::trace::TraceLayer;

pub fn create_app(state: Arc<AppState>) -> Router {
    let cors = CorsLayer::new()
        .allow_origin(Any)
        .allow_methods(Any)
        .allow_headers(Any)
        .expose_headers([
            "X-RateLimit-Limit".parse().unwrap(),
            "X-RateLimit-Remaining".parse().unwrap(),
            "X-Request-ID".parse().unwrap(),
            "X-Cache-Hit".parse().unwrap(),
            "X-CBS-Environment".parse().unwrap(),
        ]);

    let v1_routes = Router::new()
        .route("/system/health", get(handlers::health_check))
        .route("/health", get(handlers::health_check))
        .route("/system/reset", post(handlers::reset_sandbox))
        // Auth & Roles
        .route("/auth/token", post(handlers::issue_token))
        .route("/auth/refresh", post(handlers::rotate_refresh_token))
        .route("/auth/me", get(handlers::get_auth_me))
        .route("/auth/roles", get(handlers::list_roles_matrix))
        .route("/auth/sandbox-key", get(handlers::get_sandbox_keys).post(handlers::get_sandbox_keys))
        // CIF & Customers
        .route("/customers/onboard", post(handlers::onboard_customer))
        // Accounts
        .route("/accounts", get(handlers::list_accounts))
        .route("/accounts/:account_number/balance", get(handlers::get_account_balance))
        .route("/accounts/:account_number/statement", get(handlers::get_account_statement))
        .route("/accounts/:account_number/unblock-pin", post(handlers::unblock_account_pin))
        .route("/accounts/:account_number/status", patch(handlers::update_account_status))
        // Transactions
        .route("/transactions", get(handlers::list_transactions))
        .route("/transactions/:ref_no", get(handlers::get_transaction_by_ref))
        .route("/transactions/transfer", post(handlers::execute_transfer))
        .route("/transactions/cash", post(handlers::execute_cash_transaction))
        // Virtual Accounts
        .route("/virtual-accounts", post(handlers::create_virtual_account))
        .route("/virtual-accounts/:va_number", get(handlers::get_virtual_account))
        .route("/virtual-accounts/:va_number/pay", post(handlers::pay_virtual_account))
        // Regulatory & Admin
        .route("/ledger/trial-balance", get(handlers::get_trial_balance))
        .route("/audit-logs", get(handlers::list_audit_logs))
        .route("/admin/dashboard", get(handlers::get_admin_dashboard));

    Router::new()
        .route("/", get(handlers::root_welcome))
        .route("/docs", get(handlers::scalar_docs))
        .route("/openapi.yaml", get(handlers::openapi_spec))
        .route("/system/health", get(handlers::health_check))
        .route("/health", get(handlers::health_check))
        .nest("/v1", v1_routes.clone())
        .nest("/trial/v1", v1_routes.clone())
        .nest("/dev/v1", v1_routes)
        .layer(cors)
        .layer(TraceLayer::new_for_http())
        .with_state(state)
}
