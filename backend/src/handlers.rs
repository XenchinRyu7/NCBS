use crate::middleware::{render_problem, AppState, AuthUser};
use crate::models::*;
use crate::repository::{BankingError, DEMO_SANDBOX_RAW_KEY, TRIAL_SANDBOX_RAW_KEY};
use axum::{
    extract::{Path, Query, State},
    http::{HeaderMap, StatusCode},
    response::{Html, IntoResponse, Response},
    Json,
};
use chrono::Utc;
use serde::Deserialize;
use std::sync::Arc;

// Root Welcome
pub async fn root_welcome(State(state): State<Arc<AppState>>) -> Response {
    let resp = ApiResponse {
        success: true,
        message: "Nusantara Core Banking System (CBS) — SNAP-BI Open Banking Gateway".to_string(),
        data: serde_json::json!({
            "system": "Nusantara Core Banking Ledger & SNAP-BI Open Banking API",
            "version": "2.4.0-enterprise",
            "status": "OPERATIONAL",
            "compliance": "Bank Indonesia SNAP-BI & ISO 20022 Core Ledger",
            "security_gateway": "Zero-Trust WAF & Partner mTLS Enforced",
            "production_ledger": format!("{}/v1", state.service.config.public_base_url),
            "sandbox_gateway": format!("{}/trial/v1", state.service.config.public_base_url),
            "health_check": "/v1/system/health",
        }),
        metadata: None,
    };
    (StatusCode::OK, Json(resp)).into_response()
}

// Health Check
pub async fn health_check(State(state): State<Arc<AppState>>) -> Response {
    let tb = state.repo.get_trial_balance();
    let body = serde_json::json!({
        "status": "HEALTHY",
        "service": "nusantara-core-banking-system",
        "runtime": "Rust Axum Enterprise Engine",
        "general_ledger_integrity": {
            "is_balanced": tb.is_balanced,
            "discrepancy": tb.discrepancy,
            "total_debit_postings": tb.total_debit_postings,
            "total_credit_postings": tb.total_credit_postings,
            "total_ledger_entries": tb.total_ledger_entries,
            "checked_at": tb.checked_at,
        },
        "timestamp": Utc::now()
    });
    (StatusCode::OK, Json(body)).into_response()
}

// Reset Sandbox
pub async fn reset_sandbox(State(state): State<Arc<AppState>>) -> Response {
    state.repo.reset_sandbox();
    let resp = ApiResponse {
        success: true,
        message: "Trial Sandbox environment reset to clean genesis balances".to_string(),
        data: serde_json::json!({
            "environment": "TRIAL_SANDBOX",
            "restored_accounts": [
                "5240301001 (Rp 500.000.000)",
                "5240302002 (Rp 75.000.000)",
                "5240308888 (Rp 250.000.000)"
            ],
            "restored_open_va": "88000081290001001 (Rp 1.250.000)",
            "trial_sandbox_api_key": TRIAL_SANDBOX_RAW_KEY
        }),
        metadata: None,
    };
    (StatusCode::OK, Json(resp)).into_response()
}

// Roles Matrix Directory
pub async fn list_roles_matrix() -> Response {
    let roles = vec![
        serde_json::json!({
            "role": "CUSTOMER",
            "title": "Nasabah Perorangan (Retail Customer)",
            "demo_email": "budi.santoso@cbs-bank.co.id",
            "demo_password": "Password#2026",
            "demo_account_no": "5240301001",
            "demo_pin": "123456",
            "description": "Hanya bisa melihat saldo/mutasi rekening miliknya sendiri, transfer dana dari rekening sendiri, dan membayar Virtual Account.",
            "permissions": UserRole::Customer.permissions(),
        }),
        serde_json::json!({
            "role": "MERCHANT",
            "title": "Nasabah Korporasi / Biller (Corporate Merchant)",
            "demo_email": "merchant@tokonusantara.co.id",
            "demo_password": "Password#2026",
            "demo_account_no": "5240308888",
            "demo_pin": "888888",
            "description": "Bisa mengelola rekening Giro korporasi miliknya dan men-generate tagihan Virtual Account untuk pelanggan.",
            "permissions": UserRole::Merchant.permissions(),
        }),
        serde_json::json!({
            "role": "TELLER",
            "title": "Petugas Teller Cabang (Branch Teller)",
            "demo_email": "teller@cbs-bank.co.id",
            "demo_password": "Password#2026",
            "description": "Berwenang memproses Setor Tunai (DEPOSIT) & Tarik Tunai (WITHDRAWAL) nasabah terhadap GL Kas Utama Bank.",
            "permissions": UserRole::Teller.permissions(),
        }),
        serde_json::json!({
            "role": "CUSTOMER_SERVICE",
            "title": "Customer Service Cabang (Branch CS)",
            "demo_email": "cs@cbs-bank.co.id",
            "demo_password": "Password#2026",
            "description": "Membantu nasabah yang terblokir PIN (PIN_BLOCKED), onboarding CIF nasabah baru, dan perubahan status rekening.",
            "permissions": UserRole::CustomerService.permissions(),
        }),
        serde_json::json!({
            "role": "AUDITOR",
            "title": "Auditor Internal & Pengawas OJK",
            "demo_email": "auditor@cbs-bank.co.id",
            "demo_password": "Password#2026",
            "description": "Akses Read-Only mutlak untuk validasi integritas Double-Entry General Ledger (Trial Balance) & Regulatory Audit Trail OJK.",
            "permissions": UserRole::Auditor.permissions(),
        }),
        serde_json::json!({
            "role": "ADMIN",
            "title": "Core Banking Administrator",
            "demo_email": "admin@cbs-bank.co.id",
            "demo_password": "Password#2026",
            "description": "Akses penuh operasional & Sandbox Master Controller.",
            "permissions": UserRole::Admin.permissions(),
        }),
    ];

    let resp = ApiResponse {
        success: true,
        message: "Nusantara Core Banking RBAC Role Directory".to_string(),
        data: roles,
        metadata: None,
    };
    (StatusCode::OK, Json(resp)).into_response()
}

// Sandbox API Keys
pub async fn get_sandbox_keys() -> Response {
    let resp = ApiResponse {
        success: true,
        message: "SNAP-BI Open Banking Sandbox API Keys".to_string(),
        data: serde_json::json!({
            "production_master_key": DEMO_SANDBOX_RAW_KEY,
            "trial_sandbox_key": TRIAL_SANDBOX_RAW_KEY,
            "header_name": "X-API-Key",
            "note": "Include in HTTP Header 'X-API-Key: cbs_live_snap_sandbox_2026' for instant administrative sandbox bypass."
        }),
        metadata: None,
    };
    (StatusCode::OK, Json(resp)).into_response()
}

// Issue Token (Login)
pub async fn issue_token(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(payload): Json<LoginRequest>,
) -> Response {
    let client_ip = headers
        .get("x-real-ip")
        .or_else(|| headers.get("x-forwarded-for"))
        .and_then(|h| h.to_str().ok())
        .unwrap_or("127.0.0.1");
    let trace_id = headers
        .get("x-request-id")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("trace-local");

    match state.service.authenticate(payload, client_ip, trace_id) {
        Ok(tokens) => {
            let resp = ApiResponse {
                success: true,
                message: "Authentication successful".to_string(),
                data: tokens,
                metadata: None,
            };
            (StatusCode::OK, Json(resp)).into_response()
        }
        Err(err) => render_problem(StatusCode::UNAUTHORIZED, err, "/v1/auth/token"),
    }
}

// Rotate Refresh Token
pub async fn rotate_refresh_token(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(payload): Json<RefreshTokenRequest>,
) -> Response {
    let client_ip = headers
        .get("x-real-ip")
        .or_else(|| headers.get("x-forwarded-for"))
        .and_then(|h| h.to_str().ok())
        .unwrap_or("127.0.0.1");
    let trace_id = headers
        .get("x-request-id")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("trace-local");

    match state.service.rotate_refresh_token(&payload.refresh_token, client_ip, trace_id) {
        Ok(tokens) => {
            let resp = ApiResponse {
                success: true,
                message: "Refresh token rotated successfully".to_string(),
                data: tokens,
                metadata: None,
            };
            (StatusCode::OK, Json(resp)).into_response()
        }
        Err(err) => render_problem(StatusCode::UNAUTHORIZED, err, "/v1/auth/refresh"),
    }
}

// Auth Me Profile
pub async fn get_auth_me(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
) -> Response {
    let user_id = auth.user_id.unwrap_or_default();
    let accounts = state.repo.list_accounts_by_customer_id(&user_id);
    let customer = state.repo.get_customer_by_id(&user_id).ok();

    let profile = serde_json::json!({
        "customer_id": user_id,
        "cif_number": auth.cif.unwrap_or_default(),
        "full_name": auth.full_name.or_else(|| customer.as_ref().map(|c| c.full_name.clone())),
        "email": auth.email.or_else(|| customer.as_ref().map(|c| c.email.clone())),
        "phone_number": customer.as_ref().map(|c| c.phone_number.clone()),
        "role": auth.role,
        "kyc_status": customer.as_ref().map(|c| c.kyc_status).unwrap_or(KYCStatus::Verified),
        "permissions": auth.role.permissions(),
        "owned_accounts": accounts,
    });

    let resp = ApiResponse {
        success: true,
        message: "Current authenticated profile".to_string(),
        data: profile,
        metadata: None,
    };
    (StatusCode::OK, Json(resp)).into_response()
}

// Customer Onboarding
pub async fn onboard_customer(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(payload): Json<OnboardCustomerRequest>,
) -> Response {
    let idempotency_key = headers
        .get("x-idempotency-key")
        .and_then(|h| h.to_str().ok())
        .map(|s| s.to_string())
        .unwrap_or_else(|| uuid::Uuid::new_v4().to_string());

    if let Some((status, cached_body)) = state.repo.get_idempotent_response(&idempotency_key) {
        let mut response = (StatusCode::from_u16(status).unwrap_or(StatusCode::OK), Json(cached_body)).into_response();
        response.headers_mut().insert("X-Cache-Hit", "Idempotency-Replay".parse().unwrap());
        return response;
    }

    let client_ip = headers
        .get("x-real-ip")
        .or_else(|| headers.get("x-forwarded-for"))
        .and_then(|h| h.to_str().ok())
        .unwrap_or("127.0.0.1")
        .to_string();
    let trace_id = headers
        .get("x-request-id")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("trace-local")
        .to_string();

    match state.repo.onboard_customer(payload, idempotency_key.clone(), client_ip, trace_id) {
        Ok((cust, acc, tx)) => {
            let data = serde_json::json!({
                "customer": cust,
                "account": acc,
                "initial_transaction": tx,
            });
            let resp = ApiResponse {
                success: true,
                message: "Customer onboarded successfully".to_string(),
                data: data.clone(),
                metadata: None,
            };
            let json_body = serde_json::to_value(&resp).unwrap();
            state.repo.set_idempotent_response(&idempotency_key, StatusCode::CREATED.as_u16(), json_body);
            (StatusCode::CREATED, Json(resp)).into_response()
        }
        Err(err) => render_problem(StatusCode::BAD_REQUEST, err, "/v1/customers/onboard"),
    }
}

#[derive(Deserialize)]
pub struct ListAccountsQuery {
    pub include_gl: Option<bool>,
}

// List Accounts
pub async fn list_accounts(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
    Query(query): Query<ListAccountsQuery>,
) -> Response {
    let accounts = if auth.role.can_access_all_accounts() {
        let include_gl = query.include_gl.unwrap_or(false) && matches!(auth.role, UserRole::Auditor | UserRole::Admin);
        state.repo.list_all_accounts(include_gl)
    } else if let Some(uid) = auth.user_id {
        state.repo.list_accounts_by_customer_id(&uid)
    } else {
        vec![]
    };

    let resp = ApiResponse {
        success: true,
        message: "Accounts listed successfully".to_string(),
        data: accounts,
        metadata: None,
    };
    (StatusCode::OK, Json(resp)).into_response()
}

// Balance Inquiry
pub async fn get_account_balance(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
    Path(account_number): Path<String>,
) -> Response {
    let account = match state.repo.get_account_by_number(&account_number) {
        Ok(a) => a,
        Err(err) => return render_problem(StatusCode::NOT_FOUND, err, &format!("/v1/accounts/{}/balance", account_number)),
    };

    // RBAC Ownership Check: if customer or merchant, must own the account!
    if !auth.role.can_access_all_accounts() {
        if let Some(uid) = &auth.user_id {
            if &account.customer_id != uid {
                return render_problem(
                    StatusCode::FORBIDDEN,
                    BankingError::Forbidden("You do not own this account".to_string()),
                    &format!("/v1/accounts/{}/balance", account_number),
                );
            }
        }
    }

    let resp = ApiResponse {
        success: true,
        message: "Account balance retrieved".to_string(),
        data: serde_json::json!({
            "account_number": account.account_number,
            "currency": account.currency,
            "balance": account.balance,
            "balance_formatted": format_idr(account.balance),
            "hold_balance": account.hold_balance,
            "available_balance": account.available_balance,
            "available_balance_formatted": format_idr(account.available_balance),
            "status": account.status,
            "card_tier": account.card_tier,
        }),
        metadata: None,
    };
    (StatusCode::OK, Json(resp)).into_response()
}

// Account Statement
pub async fn get_account_statement(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
    Path(account_number): Path<String>,
) -> Response {
    let account = match state.repo.get_account_by_number(&account_number) {
        Ok(a) => a,
        Err(err) => return render_problem(StatusCode::NOT_FOUND, err, &format!("/v1/accounts/{}/statement", account_number)),
    };

    if !auth.role.can_access_all_accounts() {
        if let Some(uid) = &auth.user_id {
            if &account.customer_id != uid {
                return render_problem(
                    StatusCode::FORBIDDEN,
                    BankingError::Forbidden("You do not own this account".to_string()),
                    &format!("/v1/accounts/{}/statement", account_number),
                );
            }
        }
    }

    let statement = state.repo.get_account_statement(&account_number);
    let resp = ApiResponse {
        success: true,
        message: format!("Account statement for {}", account_number),
        data: statement,
        metadata: None,
    };
    (StatusCode::OK, Json(resp)).into_response()
}

// Unblock PIN (CS / Admin)
pub async fn unblock_account_pin(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
    Path(account_number): Path<String>,
    Json(payload): Json<UnblockPINRequest>,
) -> Response {
    if let Err(err) = auth.require_role(&[UserRole::CustomerService, UserRole::Admin]) {
        return render_problem(StatusCode::FORBIDDEN, err, &format!("/v1/accounts/{}/unblock-pin", account_number));
    }

    match state.repo.unblock_account_pin(&account_number, &payload.new_pin) {
        Ok(acc) => {
            let resp = ApiResponse {
                success: true,
                message: format!("PIN unblocked successfully for account {}", account_number),
                data: acc,
                metadata: None,
            };
            (StatusCode::OK, Json(resp)).into_response()
        }
        Err(err) => render_problem(StatusCode::BAD_REQUEST, err, &format!("/v1/accounts/{}/unblock-pin", account_number)),
    }
}

// Update Account Status (CS / Admin)
pub async fn update_account_status(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
    Path(account_number): Path<String>,
    Json(payload): Json<UpdateAccountStatusRequest>,
) -> Response {
    if let Err(err) = auth.require_role(&[UserRole::CustomerService, UserRole::Admin]) {
        return render_problem(StatusCode::FORBIDDEN, err, &format!("/v1/accounts/{}/status", account_number));
    }

    match state.repo.update_account_status(&account_number, payload.status) {
        Ok(acc) => {
            let resp = ApiResponse {
                success: true,
                message: format!("Account status updated to {:?}", payload.status),
                data: acc,
                metadata: None,
            };
            (StatusCode::OK, Json(resp)).into_response()
        }
        Err(err) => render_problem(StatusCode::BAD_REQUEST, err, &format!("/v1/accounts/{}/status", account_number)),
    }
}

// Transfer (Customer / Merchant / Admin)
pub async fn execute_transfer(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
    headers: HeaderMap,
    Json(payload): Json<TransferRequest>,
) -> Response {
    if let Err(err) = auth.require_role(&[UserRole::Customer, UserRole::Merchant, UserRole::Admin]) {
        return render_problem(StatusCode::FORBIDDEN, err, "/v1/transactions/transfer");
    }

    // Ownership check for non-admin
    if !auth.is_api_key && auth.role != UserRole::Admin {
        if let Ok(source_acc) = state.repo.get_account_by_number(&payload.source_account_number) {
            if Some(&source_acc.customer_id) != auth.user_id.as_ref() {
                return render_problem(
                    StatusCode::FORBIDDEN,
                    BankingError::Forbidden("Cannot transfer from an account you do not own".to_string()),
                    "/v1/transactions/transfer",
                );
            }
        }
    }

    let idempotency_key = headers
        .get("x-idempotency-key")
        .and_then(|h| h.to_str().ok())
        .map(|s| s.to_string())
        .unwrap_or_else(|| uuid::Uuid::new_v4().to_string());

    if let Some((status, cached_body)) = state.repo.get_idempotent_response(&idempotency_key) {
        let mut response = (StatusCode::from_u16(status).unwrap_or(StatusCode::OK), Json(cached_body)).into_response();
        response.headers_mut().insert("X-Cache-Hit", "Idempotency-Replay".parse().unwrap());
        return response;
    }

    let client_ip = headers
        .get("x-real-ip")
        .or_else(|| headers.get("x-forwarded-for"))
        .and_then(|h| h.to_str().ok())
        .unwrap_or("127.0.0.1")
        .to_string();
    let trace_id = headers
        .get("x-request-id")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("trace-local")
        .to_string();

    match state.repo.execute_transfer(payload, idempotency_key.clone(), auth.user_id, client_ip, trace_id) {
        Ok(tx) => {
            let resp = ApiResponse {
                success: true,
                message: "Transfer executed and settled successfully".to_string(),
                data: tx,
                metadata: None,
            };
            let json_body = serde_json::to_value(&resp).unwrap();
            state.repo.set_idempotent_response(&idempotency_key, StatusCode::CREATED.as_u16(), json_body);
            (StatusCode::CREATED, Json(resp)).into_response()
        }
        Err(err) => render_problem(StatusCode::BAD_REQUEST, err, "/v1/transactions/transfer"),
    }
}

// Cash Transaction (Teller / Admin)
pub async fn execute_cash_transaction(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
    headers: HeaderMap,
    Json(payload): Json<CashTransactionRequest>,
) -> Response {
    if let Err(err) = auth.require_role(&[UserRole::Teller, UserRole::Admin]) {
        return render_problem(StatusCode::FORBIDDEN, err, "/v1/transactions/cash");
    }

    let idempotency_key = headers
        .get("x-idempotency-key")
        .and_then(|h| h.to_str().ok())
        .map(|s| s.to_string())
        .unwrap_or_else(|| uuid::Uuid::new_v4().to_string());

    if let Some((status, cached_body)) = state.repo.get_idempotent_response(&idempotency_key) {
        let mut response = (StatusCode::from_u16(status).unwrap_or(StatusCode::OK), Json(cached_body)).into_response();
        response.headers_mut().insert("X-Cache-Hit", "Idempotency-Replay".parse().unwrap());
        return response;
    }

    let teller_id = auth.user_id.unwrap_or_else(|| "teller-sys".to_string());
    let client_ip = headers
        .get("x-real-ip")
        .or_else(|| headers.get("x-forwarded-for"))
        .and_then(|h| h.to_str().ok())
        .unwrap_or("127.0.0.1")
        .to_string();
    let trace_id = headers
        .get("x-request-id")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("trace-local")
        .to_string();

    match state.repo.execute_cash_transaction(payload, idempotency_key.clone(), teller_id, client_ip, trace_id) {
        Ok(tx) => {
            let resp = ApiResponse {
                success: true,
                message: "Cash transaction settled".to_string(),
                data: tx,
                metadata: None,
            };
            let json_body = serde_json::to_value(&resp).unwrap();
            state.repo.set_idempotent_response(&idempotency_key, StatusCode::CREATED.as_u16(), json_body);
            (StatusCode::CREATED, Json(resp)).into_response()
        }
        Err(err) => render_problem(StatusCode::BAD_REQUEST, err, "/v1/transactions/cash"),
    }
}

// List Transactions
pub async fn list_transactions(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
) -> Response {
    let all = state.repo.list_transactions();
    let result = if auth.role.can_access_all_accounts() {
        all
    } else if let Some(uid) = auth.user_id {
        let owned_acc_nos: Vec<String> = state
            .repo
            .list_accounts_by_customer_id(&uid)
            .into_iter()
            .map(|a| a.account_number)
            .collect();
        all.into_iter()
            .filter(|t| {
                t.source_account_number.as_ref().map_or(false, |s| owned_acc_nos.contains(s))
                    || t.destination_account_number.as_ref().map_or(false, |d| owned_acc_nos.contains(d))
            })
            .collect()
    } else {
        vec![]
    };

    let resp = ApiResponse {
        success: true,
        message: "Transactions retrieved".to_string(),
        data: result,
        metadata: None,
    };
    (StatusCode::OK, Json(resp)).into_response()
}

// Get Transaction by Reference
pub async fn get_transaction_by_ref(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
    Path(ref_no): Path<String>,
) -> Response {
    match state.repo.get_transaction_by_ref(&ref_no) {
        Ok(tx) => {
            if !auth.role.can_access_all_accounts() {
                if let Some(uid) = auth.user_id {
                    let owned_acc_nos: Vec<String> = state
                        .repo
                        .list_accounts_by_customer_id(&uid)
                        .into_iter()
                        .map(|a| a.account_number)
                        .collect();
                    let matches_source = tx.source_account_number.as_ref().map_or(false, |s| owned_acc_nos.contains(s));
                    let matches_dest = tx.destination_account_number.as_ref().map_or(false, |d| owned_acc_nos.contains(d));
                    if !matches_source && !matches_dest {
                        return render_problem(StatusCode::FORBIDDEN, BankingError::Forbidden("Transaction not accessible".to_string()), &format!("/v1/transactions/{}", ref_no));
                    }
                }
            }
            let resp = ApiResponse {
                success: true,
                message: "Transaction details".to_string(),
                data: tx,
                metadata: None,
            };
            (StatusCode::OK, Json(resp)).into_response()
        }
        Err(err) => render_problem(StatusCode::NOT_FOUND, err, &format!("/v1/transactions/{}", ref_no)),
    }
}

// Create Virtual Account (Merchant / Admin)
pub async fn create_virtual_account(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
    Json(payload): Json<CreateVARequest>,
) -> Response {
    if let Err(err) = auth.require_role(&[UserRole::Merchant, UserRole::Admin]) {
        return render_problem(StatusCode::FORBIDDEN, err, "/v1/virtual-accounts");
    }

    match state.repo.create_virtual_account(payload) {
        Ok(va) => {
            let resp = ApiResponse {
                success: true,
                message: "Virtual Account created successfully".to_string(),
                data: va,
                metadata: None,
            };
            (StatusCode::CREATED, Json(resp)).into_response()
        }
        Err(err) => render_problem(StatusCode::BAD_REQUEST, err, "/v1/virtual-accounts"),
    }
}

// Get Virtual Account Inquiry (All Authenticated)
pub async fn get_virtual_account(
    State(state): State<Arc<AppState>>,
    Path(va_number): Path<String>,
) -> Response {
    match state.repo.get_virtual_account(&va_number) {
        Ok(va) => {
            let resp = ApiResponse {
                success: true,
                message: "Virtual Account inquiry successful".to_string(),
                data: va,
                metadata: None,
            };
            (StatusCode::OK, Json(resp)).into_response()
        }
        Err(err) => render_problem(StatusCode::NOT_FOUND, err, &format!("/v1/virtual-accounts/{}", va_number)),
    }
}

// Pay Virtual Account (Customer / Merchant / Admin)
pub async fn pay_virtual_account(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
    headers: HeaderMap,
    Path(va_number): Path<String>,
    Json(payload): Json<PayVARequest>,
) -> Response {
    if let Err(err) = auth.require_role(&[UserRole::Customer, UserRole::Merchant, UserRole::Admin]) {
        return render_problem(StatusCode::FORBIDDEN, err, &format!("/v1/virtual-accounts/{}/pay", va_number));
    }

    let idempotency_key = headers
        .get("x-idempotency-key")
        .and_then(|h| h.to_str().ok())
        .map(|s| s.to_string())
        .unwrap_or_else(|| uuid::Uuid::new_v4().to_string());

    if let Some((status, cached_body)) = state.repo.get_idempotent_response(&idempotency_key) {
        let mut response = (StatusCode::from_u16(status).unwrap_or(StatusCode::OK), Json(cached_body)).into_response();
        response.headers_mut().insert("X-Cache-Hit", "Idempotency-Replay".parse().unwrap());
        return response;
    }

    let client_ip = headers
        .get("x-real-ip")
        .or_else(|| headers.get("x-forwarded-for"))
        .and_then(|h| h.to_str().ok())
        .unwrap_or("127.0.0.1")
        .to_string();
    let trace_id = headers
        .get("x-request-id")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("trace-local")
        .to_string();

    match state.repo.pay_virtual_account(&va_number, payload, idempotency_key.clone(), client_ip, trace_id) {
        Ok(tx) => {
            let resp = ApiResponse {
                success: true,
                message: "Virtual account payment settled".to_string(),
                data: tx,
                metadata: None,
            };
            let json_body = serde_json::to_value(&resp).unwrap();
            state.repo.set_idempotent_response(&idempotency_key, StatusCode::OK.as_u16(), json_body);
            (StatusCode::OK, Json(resp)).into_response()
        }
        Err(err) => render_problem(StatusCode::BAD_REQUEST, err, &format!("/v1/virtual-accounts/{}/pay", va_number)),
    }
}

// Trial Balance (Auditor / Admin)
pub async fn get_trial_balance(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
) -> Response {
    if let Err(err) = auth.require_role(&[UserRole::Auditor, UserRole::Admin]) {
        return render_problem(StatusCode::FORBIDDEN, err, "/v1/ledger/trial-balance");
    }

    let tb = state.repo.get_trial_balance();
    let resp = ApiResponse {
        success: true,
        message: "General Ledger Trial Balance verified".to_string(),
        data: tb,
        metadata: None,
    };
    (StatusCode::OK, Json(resp)).into_response()
}

// Audit Logs (Auditor / Admin)
pub async fn list_audit_logs(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
) -> Response {
    if let Err(err) = auth.require_role(&[UserRole::Auditor, UserRole::Admin]) {
        return render_problem(StatusCode::FORBIDDEN, err, "/v1/audit-logs");
    }

    let logs = state.repo.list_audit_logs();
    let resp = ApiResponse {
        success: true,
        message: "Regulatory audit logs retrieved".to_string(),
        data: logs,
        metadata: None,
    };
    (StatusCode::OK, Json(resp)).into_response()
}

// Admin Dashboard (Admin / Auditor)
pub async fn get_admin_dashboard(
    State(state): State<Arc<AppState>>,
    auth: AuthUser,
) -> Response {
    if let Err(err) = auth.require_role(&[UserRole::Admin, UserRole::Auditor]) {
        return render_problem(StatusCode::FORBIDDEN, err, "/v1/admin/dashboard");
    }

    let tb = state.repo.get_trial_balance();
    let accounts = state.repo.list_all_accounts(true);
    let transactions = state.repo.list_transactions();
    let virtual_accounts = state.repo.list_virtual_accounts();

    let data = serde_json::json!({
        "trial_balance": tb,
        "total_accounts": accounts.len(),
        "total_transactions": transactions.len(),
        "total_virtual_accounts": virtual_accounts.len(),
        "recent_transactions": transactions.into_iter().take(10).collect::<Vec<_>>(),
    });

    let resp = ApiResponse {
        success: true,
        message: "Admin command center overview".to_string(),
        data,
        metadata: None,
    };
    (StatusCode::OK, Json(resp)).into_response()
}

// Scalar API Docs
pub async fn scalar_docs() -> Html<&'static str> {
    Html(r#"<!doctype html>
<html>
  <head>
    <title>Nusantara Core Banking System — Interactive API Reference</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <link rel="icon" type="image/svg+xml" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>🏦</text></svg>">
  </head>
  <body>
    <script id="api-reference" data-url="/openapi.yaml"></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>"#)
}

// OpenAPI spec
pub async fn openapi_spec() -> Response {
    let yaml = include_str!("../../pkg/openapi/openapi.yaml");
    ([(axum::http::header::CONTENT_TYPE, "application/yaml")], yaml).into_response()
}
