use crate::models::{ProblemDetails, UserRole};
use crate::repository::{BankingError, Repository};
use crate::service::Service;
use axum::{
    async_trait,
    extract::FromRequestParts,
    http::{request::Parts, StatusCode},
    response::{IntoResponse, Response},
    Json,
};
use chrono::Utc;
use std::sync::Arc;

#[derive(Clone, Debug)]
pub struct AuthUser {
    pub user_id: Option<String>,
    pub cif: Option<String>,
    pub email: Option<String>,
    pub full_name: Option<String>,
    pub role: UserRole,
    pub is_api_key: bool,
}

impl AuthUser {
    pub fn require_role(&self, allowed: &[UserRole]) -> Result<(), BankingError> {
        if allowed.contains(&self.role) {
            Ok(())
        } else {
            Err(BankingError::Forbidden(format!(
                "Role {:?} is not authorized for this operation",
                self.role
            )))
        }
    }
}

pub struct AppState {
    pub service: Service,
    pub repo: Repository,
}

#[async_trait]
impl FromRequestParts<Arc<AppState>> for AuthUser {
    type Rejection = Response;

    async fn from_request_parts(
        parts: &mut Parts,
        state: &Arc<AppState>,
    ) -> Result<Self, Self::Rejection> {
        // 1. Check for X-API-Key
        if let Some(api_key_header) = parts.headers.get("x-api-key") {
            if let Ok(api_key_str) = api_key_header.to_str() {
                if let Some(_key) = state.repo.validate_api_key(api_key_str) {
                    return Ok(AuthUser {
                        user_id: Some("00000000-0000-0000-0000-000000000002".to_string()),
                        cif: Some("CIF-SYS-SANDBOX".to_string()),
                        email: Some("sandbox@cbs-bank.co.id".to_string()),
                        full_name: Some("SNAP-BI Sandbox Partner".to_string()),
                        role: UserRole::Admin,
                        is_api_key: true,
                    });
                }
            }
        }

        // 2. Check for Authorization: Bearer <jwt>
        if let Some(auth_header) = parts.headers.get("authorization") {
            if let Ok(auth_str) = auth_header.to_str() {
                if let Some(token) = auth_str.strip_prefix("Bearer ") {
                    match state.service.verify_jwt(token) {
                        Ok(claims) => {
                            return Ok(AuthUser {
                                user_id: Some(claims.sub),
                                cif: Some(claims.cif),
                                email: Some(claims.email),
                                full_name: Some(claims.name),
                                role: claims.role,
                                is_api_key: false,
                            });
                        }
                        Err(_) => {
                            let problem = ProblemDetails {
                                r#type: "https://cbs-bank.co.id/errors/unauthorized".to_string(),
                                title: "Unauthorized".to_string(),
                                status: 401,
                                detail: "Invalid or expired Bearer token".to_string(),
                                instance: parts.uri.path().to_string(),
                                timestamp: Utc::now(),
                                invalid_params: None,
                            };
                            return Err((StatusCode::UNAUTHORIZED, Json(problem)).into_response());
                        }
                    }
                }
            }
        }

        let problem = ProblemDetails {
            r#type: "https://cbs-bank.co.id/errors/unauthorized".to_string(),
            title: "Authentication Required".to_string(),
            status: 401,
            detail: "Missing Authorization Bearer token or X-API-Key header".to_string(),
            instance: parts.uri.path().to_string(),
            timestamp: Utc::now(),
            invalid_params: None,
        };
        Err((StatusCode::UNAUTHORIZED, Json(problem)).into_response())
    }
}

pub fn render_problem(status: StatusCode, err: BankingError, instance: &str) -> Response {
    let (code, rtype, title) = match &err {
        BankingError::NotFound(_) => (StatusCode::NOT_FOUND, "not-found", "Resource Not Found"),
        BankingError::InvalidCredentials => (StatusCode::UNAUTHORIZED, "unauthorized", "Invalid Credentials"),
        BankingError::InvalidToken | BankingError::TokenRevoked => (StatusCode::UNAUTHORIZED, "unauthorized", "Invalid Token"),
        BankingError::Forbidden(_) => (StatusCode::FORBIDDEN, "forbidden", "Forbidden"),
        BankingError::AccountPinBlocked => (StatusCode::FORBIDDEN, "pin-blocked", "Account PIN Blocked"),
        BankingError::InvalidPin { .. } => (StatusCode::UNAUTHORIZED, "invalid-pin", "Invalid PIN"),
        BankingError::InsufficientFunds => (StatusCode::UNPROCESSABLE_ENTITY, "insufficient-funds", "Insufficient Funds"),
        BankingError::DailyLimitExceeded { .. } => (StatusCode::UNPROCESSABLE_ENTITY, "daily-limit-exceeded", "Daily Limit Exceeded"),
        BankingError::VaNotOpen(_) => (StatusCode::CONFLICT, "va-conflict", "Virtual Account Conflict"),
        BankingError::DuplicateEmail => (StatusCode::CONFLICT, "duplicate-email", "Email Already Registered"),
        BankingError::AccountNotActive => (StatusCode::FORBIDDEN, "account-inactive", "Account Inactive"),
        BankingError::UnbalancedJournal => (StatusCode::INTERNAL_SERVER_ERROR, "ledger-unbalanced", "General Ledger Invariant Violation"),
        BankingError::BadRequest(_) => (StatusCode::BAD_REQUEST, "bad-request", "Bad Request"),
    };

    let problem = ProblemDetails {
        r#type: format!("https://cbs-bank.co.id/errors/{}", rtype),
        title: title.to_string(),
        status: code.as_u16(),
        detail: err.to_string(),
        instance: instance.to_string(),
        timestamp: Utc::now(),
        invalid_params: None,
    };

    (status.max(code), Json(problem)).into_response()
}
