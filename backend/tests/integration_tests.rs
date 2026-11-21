use axum::{
    body::Body,
    http::{Request, StatusCode},
};
use backend::{create_app, AppState, Config, Repository, Service};
use http_body_util::BodyExt;
use serde_json::Value;
use std::sync::Arc;
use tower::ServiceExt;

fn setup_test_app() -> axum::Router {
    let cfg = Config::from_env();
    let repo = Repository::new(cfg.api_key_salt.clone());
    let service = Service::new(cfg.clone(), repo.clone());
    let state = Arc::new(AppState { service, repo });
    create_app(state)
}

async fn login_as(app: &axum::Router, email: &str) -> String {
    let payload = serde_json::json!({
        "email": email,
        "password": "Password#2026"
    });
    let req = Request::builder()
        .method("POST")
        .uri("/v1/auth/token")
        .header("Content-Type", "application/json")
        .body(Body::from(payload.to_string()))
        .unwrap();

    let resp = app.clone().oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::OK);

    let bytes = resp.into_body().collect().await.unwrap().to_bytes();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    body["data"]["access_token"].as_str().unwrap().to_string()
}

#[tokio::test]
async fn test_system_health_and_roles_guide() {
    let app = setup_test_app();

    // 1. Health check
    let req = Request::builder()
        .method("GET")
        .uri("/v1/system/health")
        .body(Body::empty())
        .unwrap();

    let resp = app.clone().oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::OK);

    let bytes = resp.into_body().collect().await.unwrap().to_bytes();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    assert_eq!(body["general_ledger_integrity"]["is_balanced"], true);
    assert_eq!(body["general_ledger_integrity"]["discrepancy"], 0);

    // 2. Roles matrix
    let req_roles = Request::builder()
        .method("GET")
        .uri("/v1/auth/roles")
        .body(Body::empty())
        .unwrap();
    let resp_roles = app.clone().oneshot(req_roles).await.unwrap();
    assert_eq!(resp_roles.status(), StatusCode::OK);
}

#[tokio::test]
async fn test_customer_ownership_and_rbac_boundaries() {
    let app = setup_test_app();
    let budi_token = login_as(&app, "budi.santoso@cbs-bank.co.id").await;

    // 1. Budi checks his own profile via GET /v1/auth/me
    let req_me = Request::builder()
        .method("GET")
        .uri("/v1/auth/me")
        .header("Authorization", format!("Bearer {}", budi_token))
        .body(Body::empty())
        .unwrap();
    let resp_me = app.clone().oneshot(req_me).await.unwrap();
    assert_eq!(resp_me.status(), StatusCode::OK);

    // 2. Budi CAN check his own account balance (5240301001)
    let req_own = Request::builder()
        .method("GET")
        .uri("/v1/accounts/5240301001/balance")
        .header("Authorization", format!("Bearer {}", budi_token))
        .body(Body::empty())
        .unwrap();
    let resp_own = app.clone().oneshot(req_own).await.unwrap();
    assert_eq!(resp_own.status(), StatusCode::OK);

    // 3. Budi CANNOT check Siti's account balance (5240302002) -> 403 Forbidden
    let req_other = Request::builder()
        .method("GET")
        .uri("/v1/accounts/5240302002/balance")
        .header("Authorization", format!("Bearer {}", budi_token))
        .body(Body::empty())
        .unwrap();
    let resp_other = app.clone().oneshot(req_other).await.unwrap();
    assert_eq!(resp_other.status(), StatusCode::FORBIDDEN);

    // 4. Budi CANNOT access OJK Auditor Trial Balance -> 403 Forbidden
    let req_tb = Request::builder()
        .method("GET")
        .uri("/v1/ledger/trial-balance")
        .header("Authorization", format!("Bearer {}", budi_token))
        .body(Body::empty())
        .unwrap();
    let resp_tb = app.clone().oneshot(req_tb).await.unwrap();
    assert_eq!(resp_tb.status(), StatusCode::FORBIDDEN);
}

#[tokio::test]
async fn test_intrabank_and_bifast_transfer_with_idempotency() {
    let app = setup_test_app();
    let budi_token = login_as(&app, "budi.santoso@cbs-bank.co.id").await;

    // 1. Intrabank Transfer: Budi (5240301001) -> Siti (5240302002) Rp 5.000.000
    let tx_payload = serde_json::json!({
        "source_account_number": "5240301001",
        "destination_account_number": "5240302002",
        "channel": "INTRABANK",
        "amount": 5_000_000,
        "pin": "123456",
        "description": "Transfer Sesama Bank"
    });

    let req1 = Request::builder()
        .method("POST")
        .uri("/v1/transactions/transfer")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", budi_token))
        .header("X-Idempotency-Key", "idem-intrabank-001")
        .body(Body::from(tx_payload.to_string()))
        .unwrap();
    let resp1 = app.clone().oneshot(req1).await.unwrap();
    assert_eq!(resp1.status(), StatusCode::CREATED);

    // 2. Replay with identical X-Idempotency-Key -> 200 OK with X-Cache-Hit: Idempotency-Replay
    let req_replay = Request::builder()
        .method("POST")
        .uri("/v1/transactions/transfer")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", budi_token))
        .header("X-Idempotency-Key", "idem-intrabank-001")
        .body(Body::from(tx_payload.to_string()))
        .unwrap();
    let resp_replay = app.clone().oneshot(req_replay).await.unwrap();
    assert_eq!(resp_replay.headers().get("X-Cache-Hit").unwrap(), "Idempotency-Replay");

    // 3. BI-FAST Transfer (Rp 10.000.000 + Rp 2.500 fee)
    let bifast_payload = serde_json::json!({
        "source_account_number": "5240301001",
        "destination_account_number": "1370012345678",
        "channel": "BI_FAST",
        "external_bank_code": "BMRIIDJA",
        "external_account_name": "PT Mandiri Sekuritas",
        "amount": 10_000_000,
        "pin": "123456",
        "description": "Topup RDN via BI-FAST"
    });
    let req_bifast = Request::builder()
        .method("POST")
        .uri("/v1/transactions/transfer")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", budi_token))
        .header("X-Idempotency-Key", "idem-bifast-001")
        .body(Body::from(bifast_payload.to_string()))
        .unwrap();
    let resp_bifast = app.clone().oneshot(req_bifast).await.unwrap();
    assert_eq!(resp_bifast.status(), StatusCode::CREATED);

    // 4. Verify Budi's balance: 500.000.000 - 5.000.000 - 10.002.500 = 484.997.500
    let req_bal = Request::builder()
        .method("GET")
        .uri("/v1/accounts/5240301001/balance")
        .header("Authorization", format!("Bearer {}", budi_token))
        .body(Body::empty())
        .unwrap();
    let resp_bal = app.clone().oneshot(req_bal).await.unwrap();
    let bytes = resp_bal.into_body().collect().await.unwrap().to_bytes();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    assert_eq!(body["data"]["balance"], 484_997_500);
}

#[tokio::test]
async fn test_virtual_account_inquiry_and_settlement() {
    let app = setup_test_app();
    let budi_token = login_as(&app, "budi.santoso@cbs-bank.co.id").await;

    // 1. Inquiry Virtual Account
    let req_inq = Request::builder()
        .method("GET")
        .uri("/v1/virtual-accounts/88000081290001001")
        .header("Authorization", format!("Bearer {}", budi_token))
        .body(Body::empty())
        .unwrap();
    let resp_inq = app.clone().oneshot(req_inq).await.unwrap();
    assert_eq!(resp_inq.status(), StatusCode::OK);

    // 2. Pay Virtual Account
    let pay_payload = serde_json::json!({
        "source_account_number": "5240301001",
        "pin": "123456"
    });
    let req_pay = Request::builder()
        .method("POST")
        .uri("/v1/virtual-accounts/88000081290001001/pay")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", budi_token))
        .header("X-Idempotency-Key", "idem-va-pay-001")
        .body(Body::from(pay_payload.to_string()))
        .unwrap();
    let resp_pay = app.clone().oneshot(req_pay).await.unwrap();
    assert_eq!(resp_pay.status(), StatusCode::OK);

    // 3. Paying an already PAID VA with new idempotency key must fail with 409 Conflict
    let req_pay_again = Request::builder()
        .method("POST")
        .uri("/v1/virtual-accounts/88000081290001001/pay")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", budi_token))
        .header("X-Idempotency-Key", "idem-va-pay-002")
        .body(Body::from(pay_payload.to_string()))
        .unwrap();
    let resp_pay_again = app.clone().oneshot(req_pay_again).await.unwrap();
    assert_eq!(resp_pay_again.status(), StatusCode::CONFLICT);
}

#[tokio::test]
async fn test_onboarding_pin_lockout_and_cs_unblock_flow() {
    let app = setup_test_app();

    // 1. Onboard Dimas
    let onboard_payload = serde_json::json!({
        "nik": "3171041908950099",
        "full_name": "Dimas Anggara",
        "email": "dimas.anggara@cbs-bank.co.id",
        "phone_number": "+6281188889999",
        "password": "Password#2026",
        "account_type": "SAVINGS",
        "card_tier": "GOLD",
        "pin": "998877",
        "initial_deposit": 5_000_000
    });
    let req_onb = Request::builder()
        .method("POST")
        .uri("/v1/customers/onboard")
        .header("Content-Type", "application/json")
        .header("X-Idempotency-Key", "idem-onboard-dimas-001")
        .body(Body::from(onboard_payload.to_string()))
        .unwrap();
    let resp_onb = app.clone().oneshot(req_onb).await.unwrap();
    assert_eq!(resp_onb.status(), StatusCode::CREATED);

    let bytes = resp_onb.into_body().collect().await.unwrap().to_bytes();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    let new_acc_no = body["data"]["account"]["account_number"].as_str().unwrap().to_string();

    // 2. 3 consecutive wrong PINs -> 3rd attempt must lock the account (403 PIN_BLOCKED)
    for i in 1..=3 {
        let bad_tx = serde_json::json!({
            "source_account_number": new_acc_no,
            "destination_account_number": "5240302002",
            "channel": "INTRABANK",
            "amount": 100_000,
            "pin": "000000"
        });
        let req_bad = Request::builder()
            .method("POST")
            .uri("/v1/transactions/transfer")
            .header("Content-Type", "application/json")
            .header("X-API-Key", "cbs_live_snap_sandbox_2026")
            .header("X-Idempotency-Key", format!("idem-bad-pin-{}", i))
            .body(Body::from(bad_tx.to_string()))
            .unwrap();
        let resp_bad = app.clone().oneshot(req_bad).await.unwrap();
        if i < 3 {
            assert_eq!(resp_bad.status(), StatusCode::UNAUTHORIZED);
        } else {
            assert_eq!(resp_bad.status(), StatusCode::FORBIDDEN);
        }
    }

    // 3. Login as CUSTOMER_SERVICE (cs@cbs-bank.co.id) to unblock PIN
    let cs_token = login_as(&app, "cs@cbs-bank.co.id").await;
    let unblock_payload = serde_json::json!({
        "new_pin": "998877",
        "reason": "Verifikasi KTP di Cabang"
    });
    let req_unblock = Request::builder()
        .method("POST")
        .uri(format!("/v1/accounts/{}/unblock-pin", new_acc_no))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", cs_token))
        .body(Body::from(unblock_payload.to_string()))
        .unwrap();
    let resp_unblock = app.clone().oneshot(req_unblock).await.unwrap();
    assert_eq!(resp_unblock.status(), StatusCode::OK);
}

#[tokio::test]
async fn test_teller_cash_and_auditor_trial_balance_invariant() {
    let app = setup_test_app();

    // 1. Teller Cash Deposit Rp 15.000.000 to Siti (5240302002)
    let teller_token = login_as(&app, "teller@cbs-bank.co.id").await;
    let cash_payload = serde_json::json!({
        "account_number": "5240302002",
        "operation": "DEPOSIT",
        "amount": 15_000_000,
        "description": "Setoran Tunai Teller Cabang Sudirman"
    });
    let req_cash = Request::builder()
        .method("POST")
        .uri("/v1/transactions/cash")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", teller_token))
        .header("X-Idempotency-Key", "idem-teller-cash-001")
        .body(Body::from(cash_payload.to_string()))
        .unwrap();
    let resp_cash = app.clone().oneshot(req_cash).await.unwrap();
    assert_eq!(resp_cash.status(), StatusCode::CREATED);

    // 2. Auditor checks Trial Balance: must be strictly balanced!
    let auditor_token = login_as(&app, "auditor@cbs-bank.co.id").await;
    let req_tb = Request::builder()
        .method("GET")
        .uri("/v1/ledger/trial-balance")
        .header("Authorization", format!("Bearer {}", auditor_token))
        .body(Body::empty())
        .unwrap();
    let resp_tb = app.clone().oneshot(req_tb).await.unwrap();
    assert_eq!(resp_tb.status(), StatusCode::OK);

    let bytes = resp_tb.into_body().collect().await.unwrap().to_bytes();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    assert_eq!(body["data"]["is_balanced"], true);
    assert_eq!(body["data"]["discrepancy"], 0);
}

