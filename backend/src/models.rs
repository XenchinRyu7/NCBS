use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum UserRole {
    Customer,
    Merchant,
    Teller,
    CustomerService,
    Auditor,
    Admin,
}

impl UserRole {
    pub fn permissions(&self) -> Vec<&'static str> {
        match self {
            UserRole::Customer => vec![
                "auth:me",
                "account:read:own",
                "account:statement:own",
                "transaction:transfer:own",
                "virtual_account:inquiry",
                "virtual_account:pay",
            ],
            UserRole::Merchant => vec![
                "auth:me",
                "account:read:own",
                "account:statement:own",
                "transaction:transfer:own",
                "virtual_account:create",
                "virtual_account:inquiry",
                "virtual_account:pay",
            ],
            UserRole::Teller => vec![
                "auth:me",
                "customer:onboard",
                "account:read:any",
                "transaction:cash_deposit",
                "transaction:cash_withdrawal",
            ],
            UserRole::CustomerService => vec![
                "auth:me",
                "customer:onboard",
                "account:read:any",
                "account:statement:any",
                "account:unblock_pin",
            ],
            UserRole::Auditor => vec![
                "auth:me",
                "account:read:any",
                "account:read:gl_internal",
                "account:statement:any",
                "ledger:trial_balance",
                "audit_logs:read",
            ],
            UserRole::Admin => vec![
                "auth:me",
                "customer:onboard",
                "account:read:any",
                "account:read:gl_internal",
                "account:statement:any",
                "account:unblock_pin",
                "transaction:transfer:any",
                "transaction:cash_deposit",
                "transaction:cash_withdrawal",
                "virtual_account:create",
                "virtual_account:inquiry",
                "virtual_account:pay",
                "ledger:trial_balance",
                "audit_logs:read",
            ],
        }
    }

    pub fn can_access_all_accounts(&self) -> bool {
        matches!(self, UserRole::Teller | UserRole::CustomerService | UserRole::Auditor | UserRole::Admin)
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum KYCStatus {
    Unverified,
    Verified,
    Rejected,
    EnhancedDueDiligence,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum AccountType {
    Savings,
    SavingsYouth,
    Checking,
    TimeDeposit,
    GlInternal,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum CardTier {
    Silver,
    Gold,
    Platinum,
    Priority,
}

impl CardTier {
    pub fn daily_limit(&self) -> i64 {
        match self {
            CardTier::Silver => 50_000_000,
            CardTier::Gold => 100_000_000,
            CardTier::Platinum => 250_000_000,
            CardTier::Priority => 1_000_000_000,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum AccountStatus {
    Active,
    Dormant,
    Frozen,
    PinBlocked,
    Closed,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum TxChannel {
    Intrabank,
    BiFast,
    Rtgs,
    Llg,
    VirtualAccount,
    TellerDeposit,
    TellerWithdrawal,
}

impl TxChannel {
    pub fn fee(&self) -> i64 {
        match self {
            TxChannel::BiFast => 2_500,
            TxChannel::Rtgs => 25_000,
            TxChannel::Llg => 2_900,
            _ => 0,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum TxStatus {
    Pending,
    Settled,
    Failed,
    Reversed,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum EntryDirection {
    Debit,
    Credit,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum VAStatus {
    Open,
    Paid,
    Expired,
    Cancelled,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum AuditAction {
    Create,
    Update,
    Delete,
    Authenticate,
    Transfer,
    VaPayment,
    PinFailure,
    PinUnblock,
}

pub fn format_idr(amount: i64) -> String {
    let s = amount.abs().to_string();
    let mut result = String::new();
    let chars: Vec<char> = s.chars().rev().collect();
    for (i, c) in chars.iter().enumerate() {
        if i > 0 && i % 3 == 0 {
            result.push('.');
        }
        result.push(*c);
    }
    let formatted: String = result.chars().rev().collect();
    if amount < 0 {
        format!("-Rp {}", formatted)
    } else {
        format!("Rp {}", formatted)
    }
}

pub fn mask_email(email: &str) -> String {
    let parts: Vec<&str> = email.split('@').collect();
    if parts.len() != 2 {
        return email.to_string();
    }
    let name = parts[0];
    let domain = parts[1];
    if name.len() <= 2 {
        format!("{}***@{}", name, domain)
    } else {
        format!("{}{}***@{}", &name[0..1], &name[1..2], domain)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Customer {
    pub id: String,
    pub cif_number: String,
    pub nik_masked: String,
    pub full_name: String,
    pub email: String,
    pub phone_number: String,
    #[serde(skip_serializing)]
    pub password_hash: String,
    pub role: UserRole,
    pub kyc_status: KYCStatus,
    pub is_active: bool,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Account {
    pub id: String,
    pub account_number: String,
    pub customer_id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub owner_name: Option<String>,
    pub account_type: AccountType,
    pub card_tier: CardTier,
    pub currency: String,
    pub balance: i64,
    pub hold_balance: i64,
    pub available_balance: i64,
    pub daily_limit: i64,
    #[serde(skip_serializing)]
    pub pin_hash: String,
    pub pin_failed_attempts: u32,
    pub status: AccountStatus,
    pub version: i64,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct LedgerPost {
    pub id: String,
    pub transaction_id: String,
    pub account_id: String,
    pub account_number: String,
    pub direction: EntryDirection,
    pub amount: i64,
    pub amount_formatted: String,
    pub balance_before: i64,
    pub balance_after: i64,
    pub narration: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Transaction {
    pub id: String,
    pub reference_no: String,
    pub idempotency_key: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub source_account_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub source_account_number: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub destination_account_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub destination_account_number: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub external_bank_code: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub external_account_number: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub external_account_name: Option<String>,
    pub channel: TxChannel,
    pub amount: i64,
    pub amount_formatted: String,
    pub fee_amount: i64,
    pub total_debit: i64,
    pub total_debit_formatted: String,
    pub currency: String,
    pub description: String,
    pub status: TxStatus,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub initiated_by: Option<String>,
    pub client_ip: String,
    pub trace_id: String,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub ledger_postings: Vec<LedgerPost>,
    pub settled_at: DateTime<Utc>,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct VirtualAccount {
    pub id: String,
    pub va_number: String,
    pub merchant_name: String,
    pub merchant_account_id: String,
    pub merchant_account_number: String,
    pub customer_name: String,
    pub bill_amount: i64,
    pub bill_amount_formatted: String,
    pub currency: String,
    pub description: String,
    pub status: VAStatus,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub paid_transaction_id: Option<String>,
    pub expires_at: DateTime<Utc>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub paid_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ApiKey {
    pub id: String,
    pub partner_name: String,
    pub key_hash: String,
    pub prefix: String,
    pub rate_limit_per_min: u32,
    pub is_active: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub expires_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct AuditLog {
    pub id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub actor_id: Option<String>,
    pub action: AuditAction,
    pub resource_name: String,
    pub resource_id: String,
    pub ip_address: String,
    pub trace_id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub payload_snapshot: Option<serde_json::Value>,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct TrialBalanceSummary {
    pub total_debit_postings: i64,
    pub total_credit_postings: i64,
    pub discrepancy: i64,
    pub is_balanced: bool,
    pub total_ledger_entries: usize,
    pub total_transactions: usize,
    pub checked_at: DateTime<Utc>,
}

// DTOs
#[derive(Clone, Debug, Deserialize)]
pub struct LoginRequest {
    pub email: String,
    pub password: String,
}

#[derive(Clone, Debug, Deserialize)]
pub struct RefreshTokenRequest {
    pub refresh_token: String,
}

#[derive(Clone, Debug, Serialize)]
pub struct TokenResponse {
    pub access_token: String,
    pub refresh_token: String,
    pub token_type: String,
    pub expires_in: u64,
    pub cif_number: String,
    pub full_name: String,
    pub email: String,
    pub role: UserRole,
    pub permissions: Vec<&'static str>,
    pub accounts: Vec<Account>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct OnboardCustomerRequest {
    pub nik: String,
    pub full_name: String,
    pub email: String,
    pub phone_number: String,
    pub password: String,
    pub account_type: AccountType,
    pub card_tier: CardTier,
    pub pin: String,
    pub initial_deposit: i64,
}

#[derive(Clone, Debug, Deserialize)]
pub struct OpenAccountRequest {
    pub customer_id: Option<String>,
    pub account_type: AccountType,
    pub card_tier: CardTier,
    pub pin: String,
    pub initial_deposit: i64,
}

#[derive(Clone, Debug, Deserialize)]
pub struct TransferRequest {
    pub source_account_number: String,
    pub destination_account_number: String,
    pub channel: TxChannel,
    #[serde(default)]
    pub external_bank_code: Option<String>,
    #[serde(default)]
    pub external_account_name: Option<String>,
    pub amount: i64,
    pub pin: String,
    #[serde(default)]
    pub description: Option<String>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct CashTransactionRequest {
    pub account_number: String,
    pub operation: String, // "DEPOSIT" or "WITHDRAWAL"
    pub amount: i64,
    #[serde(default)]
    pub description: Option<String>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct CreateVARequest {
    pub merchant_account_number: String,
    pub customer_name: String,
    pub bill_amount: i64,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default)]
    pub expiry_days: Option<i64>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct PayVARequest {
    pub source_account_number: String,
    pub pin: String,
}

#[derive(Clone, Debug, Deserialize)]
pub struct UnblockPINRequest {
    pub new_pin: String,
    #[serde(default)]
    pub reason: Option<String>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct UpdateAccountStatusRequest {
    pub status: AccountStatus,
    #[serde(default)]
    pub reason: Option<String>,
}

#[derive(Clone, Debug, Serialize)]
pub struct ApiResponse<T: Serialize> {
    pub success: bool,
    pub message: String,
    pub data: T,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub metadata: Option<serde_json::Value>,
}

#[derive(Clone, Debug, Serialize)]
pub struct ProblemDetails {
    pub r#type: String,
    pub title: String,
    pub status: u16,
    pub detail: String,
    pub instance: String,
    pub timestamp: DateTime<Utc>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub invalid_params: Option<serde_json::Value>,
}
