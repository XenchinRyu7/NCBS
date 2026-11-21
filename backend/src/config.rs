use std::env;

#[derive(Clone, Debug)]
pub struct Config {
    pub port: u16,
    pub environment: String,
    pub jwt_secret: String,
    pub api_key_salt: String,
    pub allowed_origins: Vec<String>,
    pub public_base_url: String,
    pub production_master_key: String,
    pub default_rate_limit: u32,
}

impl Config {
    pub fn from_env() -> Self {
        let port = env::var("PORT")
            .ok()
            .and_then(|p| p.parse().ok())
            .unwrap_or(8080);
        let environment = env::var("CBS_ENV").unwrap_or_else(|_| "PRODUCTION".to_string());
        let jwt_secret = env::var("JWT_SECRET").unwrap_or_else(|_| "cbs-super-secret-jwt-key-2026-production-ready".to_string());
        let api_key_salt = env::var("API_KEY_SALT").unwrap_or_else(|_| "cbs-enterprise-salt-bi-fast-2026".to_string());
        let allowed_origins = env::var("ALLOWED_ORIGINS")
            .unwrap_or_else(|_| "*".to_string())
            .split(',')
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect();
        let public_base_url = env::var("PUBLIC_BASE_URL").unwrap_or_else(|_| format!("http://localhost:{}", port));
        let production_master_key = env::var("PRODUCTION_MASTER_KEY").unwrap_or_else(|_| "cbs_live_snap_sandbox_2026".to_string());
        let default_rate_limit = env::var("RATE_LIMIT_PER_MIN")
            .ok()
            .and_then(|r| r.parse().ok())
            .unwrap_or(120);

        Self {
            port,
            environment,
            jwt_secret,
            api_key_salt,
            allowed_origins,
            public_base_url,
            production_master_key,
            default_rate_limit,
        }
    }
}
