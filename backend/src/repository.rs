use crate::models::*;
use chrono::Utc;
use parking_lot::RwLock;
use std::collections::HashMap;
use std::sync::Arc;
use uuid::Uuid;

pub const GL_VAULT_ACCOUNT_NO: &str = "1001000001";
pub const GL_NOSTRO_CLEARING_NO: &str = "2001000002";
pub const GL_FEE_INCOME_ACCOUNT_NO: &str = "4001000003";

pub const DEMO_SANDBOX_RAW_KEY: &str = "cbs_live_snap_sandbox_2026";
pub const TRIAL_SANDBOX_RAW_KEY: &str = "cbs_trial_snap_sandbox_2026";

#[derive(Clone)]
pub struct Repository {
    state: Arc<RwLock<RepoState>>,
    api_key_salt: String,
}

struct RepoState {
    customers: HashMap<String, Customer>,       // by id
    customers_by_email: HashMap<String, String>, // email -> id
    accounts: HashMap<String, Account>,         // by account_number
    accounts_by_id: HashMap<String, String>,    // id -> account_number
    transactions: Vec<Transaction>,             // newest first
    ledger_entries: Vec<LedgerPost>,           // newest first
    virtual_accounts: HashMap<String, VirtualAccount>, // by va_number
    api_keys: HashMap<String, ApiKey>,          // by key_hash
    audit_logs: Vec<AuditLog>,                  // newest first
    idempotency_cache: HashMap<String, (u16, serde_json::Value)>,
}

#[derive(Debug)]
pub enum BankingError {
    NotFound(String),
    InvalidCredentials,
    InvalidToken,
    TokenRevoked,
    AccountNotActive,
    AccountPinBlocked,
    InvalidPin { attempts_left: u32 },
    InsufficientFunds,
    DailyLimitExceeded { limit: i64, remaining: i64 },
    VaNotOpen(String),
    DuplicateEmail,
    UnbalancedJournal,
    Forbidden(String),
    BadRequest(String),
}

impl std::fmt::Display for BankingError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            BankingError::NotFound(m) => write!(f, "Resource Not Found: {}", m),
            BankingError::InvalidCredentials => write!(f, "Invalid email or password"),
            BankingError::InvalidToken => write!(f, "Invalid or expired token"),
            BankingError::TokenRevoked => write!(f, "Refresh token has already been rotated or revoked"),
            BankingError::AccountNotActive => write!(f, "Account is not active or is blocked"),
            BankingError::AccountPinBlocked => write!(f, "Account is PIN_BLOCKED due to 3 consecutive failed PIN attempts; contact Customer Service to unblock"),
            BankingError::InvalidPin { attempts_left } => write!(f, "Invalid 6-digit transaction PIN ({} attempts left)", attempts_left),
            BankingError::InsufficientFunds => write!(f, "Insufficient available balance for transaction amount and fee"),
            BankingError::DailyLimitExceeded { limit, remaining } => write!(f, "Daily limit of {} exceeded (remaining: {})", format_idr(*limit), format_idr(*remaining)),
            BankingError::VaNotOpen(status) => write!(f, "Virtual account is not open (status: {})", status),
            BankingError::DuplicateEmail => write!(f, "Email is already registered"),
            BankingError::UnbalancedJournal => write!(f, "Critical ledger invariant violation: total debit does not equal total credit"),
            BankingError::Forbidden(m) => write!(f, "Forbidden: {}", m),
            BankingError::BadRequest(m) => write!(f, "Bad Request: {}", m),
        }
    }
}

impl std::error::Error for BankingError {}

impl Repository {
    pub fn new(api_key_salt: String) -> Self {
        let repo = Self {
            state: Arc::new(RwLock::new(RepoState {
                customers: HashMap::new(),
                customers_by_email: HashMap::new(),
                accounts: HashMap::new(),
                accounts_by_id: HashMap::new(),
                transactions: Vec::new(),
                ledger_entries: Vec::new(),
                virtual_accounts: HashMap::new(),
                api_keys: HashMap::new(),
                audit_logs: Vec::new(),
                idempotency_cache: HashMap::new(),
            })),
            api_key_salt,
        };
        repo.seed_genesis_data();
        repo
    }

    pub fn hash_api_key(&self, raw_key: &str) -> String {
        use std::collections::hash_map::DefaultHasher;
        use std::hash::{Hash, Hasher};
        let mut hasher = DefaultHasher::new();
        format!("{}:{}", raw_key, self.api_key_salt).hash(&mut hasher);
        format!("{:x}", hasher.finish())
    }

    pub fn reset_sandbox(&self) {
        let mut state = self.state.write();
        state.customers.clear();
        state.customers_by_email.clear();
        state.accounts.clear();
        state.accounts_by_id.clear();
        state.transactions.clear();
        state.ledger_entries.clear();
        state.virtual_accounts.clear();
        state.api_keys.clear();
        state.audit_logs.clear();
        state.idempotency_cache.clear();
        drop(state);
        self.seed_genesis_data();
    }

    fn seed_genesis_data(&self) {
        let now = Utc::now();
        let mut state = self.state.write();

        // Passwords & PIN hashes using bcrypt
        let default_pass_hash = bcrypt::hash("Password#2026", bcrypt::DEFAULT_COST).unwrap();
        let pin123456 = bcrypt::hash("123456", bcrypt::DEFAULT_COST).unwrap();
        let pin654321 = bcrypt::hash("654321", bcrypt::DEFAULT_COST).unwrap();
        let pin888888 = bcrypt::hash("888888", bcrypt::DEFAULT_COST).unwrap();

        // 1. Seed API Keys
        let demo_hash = self.hash_api_key(DEMO_SANDBOX_RAW_KEY);
        let trial_hash = self.hash_api_key(TRIAL_SANDBOX_RAW_KEY);
        let exp = now + chrono::Duration::days(365);

        state.api_keys.insert(
            demo_hash.clone(),
            ApiKey {
                id: "c2eebc99-9c0b-4ef8-bb6d-6bb9bd380a33".to_string(),
                partner_name: "SNAP-BI Open Banking Playground Partner".to_string(),
                key_hash: demo_hash,
                prefix: "cbs_live_".to_string(),
                rate_limit_per_min: 120,
                is_active: true,
                expires_at: Some(exp),
                created_at: now,
            },
        );

        state.api_keys.insert(
            trial_hash.clone(),
            ApiKey {
                id: "d4eebc99-9c0b-4ef8-bb6d-6bb9bd380a44".to_string(),
                partner_name: "API Playground Trial Environment Key".to_string(),
                key_hash: trial_hash,
                prefix: "cbs_trial_".to_string(),
                rate_limit_per_min: 120,
                is_active: true,
                expires_at: Some(exp),
                created_at: now,
            },
        );

        // 2. Customers
        let customers = vec![
            Customer {
                id: "00000000-0000-0000-0000-000000000001".to_string(),
                cif_number: "CIF-00000001".to_string(),
                nik_masked: "0000********0001".to_string(),
                full_name: "PT Bank Nusantara Core Tbk (Treasury & GL)".to_string(),
                email: "treasury@cbs-bank.co.id".to_string(),
                phone_number: "1500999".to_string(),
                password_hash: default_pass_hash.clone(),
                role: UserRole::Admin,
                kyc_status: KYCStatus::Verified,
                is_active: true,
                created_at: now,
                updated_at: now,
            },
            Customer {
                id: "00000000-0000-0000-0000-000000000002".to_string(),
                cif_number: "CIF-00000002".to_string(),
                nik_masked: "3171********0002".to_string(),
                full_name: "Hendra Gunawan (Core Banking Admin)".to_string(),
                email: "admin@cbs-bank.co.id".to_string(),
                phone_number: "+628111000001".to_string(),
                password_hash: default_pass_hash.clone(),
                role: UserRole::Admin,
                kyc_status: KYCStatus::Verified,
                is_active: true,
                created_at: now,
                updated_at: now,
            },
            Customer {
                id: "00000000-0000-0000-0000-000000000003".to_string(),
                cif_number: "CIF-00000003".to_string(),
                nik_masked: "3171********0003".to_string(),
                full_name: "Rina Wijaya (Head Teller Cabang Sudirman)".to_string(),
                email: "teller@cbs-bank.co.id".to_string(),
                phone_number: "+628111000002".to_string(),
                password_hash: default_pass_hash.clone(),
                role: UserRole::Teller,
                kyc_status: KYCStatus::Verified,
                is_active: true,
                created_at: now,
                updated_at: now,
            },
            Customer {
                id: "00000000-0000-0000-0000-000000000005".to_string(),
                cif_number: "CIF-00000005".to_string(),
                nik_masked: "3171********0005".to_string(),
                full_name: "Nadia Kusuma (Customer Service Cabang Sudirman)".to_string(),
                email: "cs@cbs-bank.co.id".to_string(),
                phone_number: "+628111000005".to_string(),
                password_hash: default_pass_hash.clone(),
                role: UserRole::CustomerService,
                kyc_status: KYCStatus::Verified,
                is_active: true,
                created_at: now,
                updated_at: now,
            },
            Customer {
                id: "00000000-0000-0000-0000-000000000004".to_string(),
                cif_number: "CIF-00000004".to_string(),
                nik_masked: "3171********0004".to_string(),
                full_name: "Dr. Prasetyo Wibowo (Internal & OJK Auditor)".to_string(),
                email: "auditor@cbs-bank.co.id".to_string(),
                phone_number: "+628111000003".to_string(),
                password_hash: default_pass_hash.clone(),
                role: UserRole::Auditor,
                kyc_status: KYCStatus::Verified,
                is_active: true,
                created_at: now,
                updated_at: now,
            },
            Customer {
                id: "a1eebc99-9c0b-4ef8-bb6d-6bb9bd380a11".to_string(),
                cif_number: "CIF-90010001".to_string(),
                nik_masked: "3171********0001".to_string(),
                full_name: "Budi Santoso".to_string(),
                email: "budi.santoso@cbs-bank.co.id".to_string(),
                phone_number: "+6281290001001".to_string(),
                password_hash: default_pass_hash.clone(),
                role: UserRole::Customer,
                kyc_status: KYCStatus::Verified,
                is_active: true,
                created_at: now,
                updated_at: now,
            },
            Customer {
                id: "b2eebc99-9c0b-4ef8-bb6d-6bb9bd380a22".to_string(),
                cif_number: "CIF-90010002".to_string(),
                nik_masked: "3173********0002".to_string(),
                full_name: "Siti Rahmawati".to_string(),
                email: "siti.rahma@cbs-bank.co.id".to_string(),
                phone_number: "+6281290002002".to_string(),
                password_hash: default_pass_hash.clone(),
                role: UserRole::Customer,
                kyc_status: KYCStatus::Verified,
                is_active: true,
                created_at: now,
                updated_at: now,
            },
            Customer {
                id: "c3eebc99-9c0b-4ef8-bb6d-6bb9bd380a88".to_string(),
                cif_number: "CIF-90018888".to_string(),
                nik_masked: "3174********8888".to_string(),
                full_name: "PT Toko Nusantara Digital Tbk".to_string(),
                email: "merchant@tokonusantara.co.id".to_string(),
                phone_number: "+622150888888".to_string(),
                password_hash: default_pass_hash.clone(),
                role: UserRole::Merchant,
                kyc_status: KYCStatus::Verified,
                is_active: true,
                created_at: now,
                updated_at: now,
            },
        ];

        for c in customers {
            state.customers_by_email.insert(c.email.to_lowercase(), c.id.clone());
            state.customers.insert(c.id.clone(), c);
        }

        // 3. Accounts
        let accounts = vec![
            Account {
                id: "00000000-0000-0000-0000-000000000901".to_string(),
                account_number: GL_VAULT_ACCOUNT_NO.to_string(),
                customer_id: "00000000-0000-0000-0000-000000000001".to_string(),
                owner_name: Some("GL Kas Utama & Cadangan Likuiditas Bank".to_string()),
                account_type: AccountType::GlInternal,
                card_tier: CardTier::Priority,
                currency: "IDR".to_string(),
                balance: 100_000_000_000 - (500_000_000 + 75_000_000 + 250_000_000),
                hold_balance: 0,
                available_balance: 100_000_000_000 - (500_000_000 + 75_000_000 + 250_000_000),
                daily_limit: 999_999_999_999,
                pin_hash: pin888888.clone(),
                pin_failed_attempts: 0,
                status: AccountStatus::Active,
                version: 1,
                created_at: now,
                updated_at: now,
            },
            Account {
                id: "00000000-0000-0000-0000-000000000902".to_string(),
                account_number: GL_NOSTRO_CLEARING_NO.to_string(),
                customer_id: "00000000-0000-0000-0000-000000000001".to_string(),
                owner_name: Some("GL Nostro Settlement BI-FAST & RTGS Bank Indonesia".to_string()),
                account_type: AccountType::GlInternal,
                card_tier: CardTier::Priority,
                currency: "IDR".to_string(),
                balance: 0,
                hold_balance: 0,
                available_balance: 0,
                daily_limit: 999_999_999_999,
                pin_hash: pin888888.clone(),
                pin_failed_attempts: 0,
                status: AccountStatus::Active,
                version: 1,
                created_at: now,
                updated_at: now,
            },
            Account {
                id: "00000000-0000-0000-0000-000000000903".to_string(),
                account_number: GL_FEE_INCOME_ACCOUNT_NO.to_string(),
                customer_id: "00000000-0000-0000-0000-000000000001".to_string(),
                owner_name: Some("GL Pendapatan Fee Transaksi & Administrasi".to_string()),
                account_type: AccountType::GlInternal,
                card_tier: CardTier::Priority,
                currency: "IDR".to_string(),
                balance: 0,
                hold_balance: 0,
                available_balance: 0,
                daily_limit: 999_999_999_999,
                pin_hash: pin888888.clone(),
                pin_failed_attempts: 0,
                status: AccountStatus::Active,
                version: 1,
                created_at: now,
                updated_at: now,
            },
            Account {
                id: "acc00000-0000-0000-0000-000052403001".to_string(),
                account_number: "5240301001".to_string(),
                customer_id: "a1eebc99-9c0b-4ef8-bb6d-6bb9bd380a11".to_string(),
                owner_name: Some("Budi Santoso".to_string()),
                account_type: AccountType::Savings,
                card_tier: CardTier::Priority,
                currency: "IDR".to_string(),
                balance: 500_000_000,
                hold_balance: 0,
                available_balance: 500_000_000,
                daily_limit: CardTier::Priority.daily_limit(),
                pin_hash: pin123456.clone(),
                pin_failed_attempts: 0,
                status: AccountStatus::Active,
                version: 1,
                created_at: now,
                updated_at: now,
            },
            Account {
                id: "acc00000-0000-0000-0000-000052403002".to_string(),
                account_number: "5240302002".to_string(),
                customer_id: "b2eebc99-9c0b-4ef8-bb6d-6bb9bd380a22".to_string(),
                owner_name: Some("Siti Rahmawati".to_string()),
                account_type: AccountType::Savings,
                card_tier: CardTier::Platinum,
                currency: "IDR".to_string(),
                balance: 75_000_000,
                hold_balance: 0,
                available_balance: 75_000_000,
                daily_limit: CardTier::Platinum.daily_limit(),
                pin_hash: pin654321.clone(),
                pin_failed_attempts: 0,
                status: AccountStatus::Active,
                version: 1,
                created_at: now,
                updated_at: now,
            },
            Account {
                id: "acc00000-0000-0000-0000-000052403888".to_string(),
                account_number: "5240308888".to_string(),
                customer_id: "c3eebc99-9c0b-4ef8-bb6d-6bb9bd380a88".to_string(),
                owner_name: Some("PT Toko Nusantara Digital Tbk".to_string()),
                account_type: AccountType::Checking,
                card_tier: CardTier::Priority,
                currency: "IDR".to_string(),
                balance: 250_000_000,
                hold_balance: 0,
                available_balance: 250_000_000,
                daily_limit: CardTier::Priority.daily_limit(),
                pin_hash: pin888888.clone(),
                pin_failed_attempts: 0,
                status: AccountStatus::Active,
                version: 1,
                created_at: now,
                updated_at: now,
            },
        ];

        for a in accounts {
            state.accounts_by_id.insert(a.id.clone(), a.account_number.clone());
            state.accounts.insert(a.account_number.clone(), a);
        }

        // 4. Genesis Double-Entry Ledger Postings
        let genesis_tx_id = "10000000-0000-0000-0000-000000000001";
        let total_initial = 500_000_000 + 75_000_000 + 250_000_000;
        let genesis_postings = vec![
            LedgerPost {
                id: Uuid::new_v4().to_string(),
                transaction_id: genesis_tx_id.to_string(),
                account_id: "00000000-0000-0000-0000-000000000901".to_string(),
                account_number: GL_VAULT_ACCOUNT_NO.to_string(),
                direction: EntryDirection::Debit,
                amount: total_initial,
                amount_formatted: format_idr(total_initial),
                balance_before: 100_000_000_000,
                balance_after: 100_000_000_000 - total_initial,
                narration: "Genesis Core Capital Allocation to Customer Accounts".to_string(),
                created_at: now,
            },
            LedgerPost {
                id: Uuid::new_v4().to_string(),
                transaction_id: genesis_tx_id.to_string(),
                account_id: "acc00000-0000-0000-0000-000052403001".to_string(),
                account_number: "5240301001".to_string(),
                direction: EntryDirection::Credit,
                amount: 500_000_000,
                amount_formatted: format_idr(500_000_000),
                balance_before: 0,
                balance_after: 500_000_000,
                narration: "Initial Opening Deposit - Savings Priority".to_string(),
                created_at: now,
            },
            LedgerPost {
                id: Uuid::new_v4().to_string(),
                transaction_id: genesis_tx_id.to_string(),
                account_id: "acc00000-0000-0000-0000-000052403002".to_string(),
                account_number: "5240302002".to_string(),
                direction: EntryDirection::Credit,
                amount: 75_000_000,
                amount_formatted: format_idr(75_000_000),
                balance_before: 0,
                balance_after: 75_000_000,
                narration: "Initial Opening Deposit - Savings Platinum".to_string(),
                created_at: now,
            },
            LedgerPost {
                id: Uuid::new_v4().to_string(),
                transaction_id: genesis_tx_id.to_string(),
                account_id: "acc00000-0000-0000-0000-000052403888".to_string(),
                account_number: "5240308888".to_string(),
                direction: EntryDirection::Credit,
                amount: 250_000_000,
                amount_formatted: format_idr(250_000_000),
                balance_before: 0,
                balance_after: 250_000_000,
                narration: "Initial Opening Deposit - Checking Priority".to_string(),
                created_at: now,
            },
        ];

        for p in genesis_postings {
            state.ledger_entries.push(p);
        }

        // 5. Virtual Account
        state.virtual_accounts.insert(
            "88000081290001001".to_string(),
            VirtualAccount {
                id: "b0000000-0000-0000-0000-000000000001".to_string(),
                va_number: "88000081290001001".to_string(),
                merchant_name: "PT Toko Nusantara Digital Tbk".to_string(),
                merchant_account_id: "acc00000-0000-0000-0000-000052403888".to_string(),
                merchant_account_number: "5240308888".to_string(),
                customer_name: "Budi Santoso".to_string(),
                bill_amount: 1_250_000,
                bill_amount_formatted: format_idr(1_250_000),
                currency: "IDR".to_string(),
                description: "Order #INV-2026-99812 MacBook Sleeve & Hub".to_string(),
                status: VAStatus::Open,
                paid_transaction_id: None,
                expires_at: now + chrono::Duration::days(7),
                paid_at: None,
                created_at: now,
            },
        );
    }

    // Customer operations
    pub fn get_customer_by_email(&self, email: &str) -> Result<Customer, BankingError> {
        let state = self.state.read();
        let id = state
            .customers_by_email
            .get(&email.to_lowercase())
            .ok_or(BankingError::InvalidCredentials)?;
        state
            .customers
            .get(id)
            .cloned()
            .ok_or(BankingError::InvalidCredentials)
    }

    pub fn get_customer_by_id(&self, id: &str) -> Result<Customer, BankingError> {
        let state = self.state.read();
        state
            .customers
            .get(id)
            .cloned()
            .ok_or_else(|| BankingError::NotFound(format!("Customer with ID {}", id)))
    }

    // Account operations
    pub fn get_account_by_number(&self, acc_no: &str) -> Result<Account, BankingError> {
        let state = self.state.read();
        state
            .accounts
            .get(acc_no)
            .cloned()
            .ok_or_else(|| BankingError::NotFound(format!("Account {}", acc_no)))
    }

    pub fn list_accounts_by_customer_id(&self, customer_id: &str) -> Vec<Account> {
        let state = self.state.read();
        state
            .accounts
            .values()
            .filter(|a| a.customer_id == customer_id)
            .cloned()
            .collect()
    }

    pub fn list_all_accounts(&self, include_gl: bool) -> Vec<Account> {
        let state = self.state.read();
        state
            .accounts
            .values()
            .filter(|a| include_gl || a.account_type != AccountType::GlInternal)
            .cloned()
            .collect()
    }

    // API Key validation
    pub fn validate_api_key(&self, raw_key: &str) -> Option<ApiKey> {
        let hash = self.hash_api_key(raw_key);
        let state = self.state.read();
        state.api_keys.get(&hash).cloned().filter(|k| k.is_active)
    }

    // Idempotency Cache
    pub fn get_idempotent_response(&self, key: &str) -> Option<(u16, serde_json::Value)> {
        let state = self.state.read();
        state.idempotency_cache.get(key).cloned()
    }

    pub fn set_idempotent_response(&self, key: &str, status: u16, val: serde_json::Value) {
        let mut state = self.state.write();
        state.idempotency_cache.insert(key.to_string(), (status, val));
    }

    // Verification helpers
    fn verify_pin_internal(account: &mut Account, pin: &str) -> Result<(), BankingError> {
        if account.status == AccountStatus::PinBlocked {
            return Err(BankingError::AccountPinBlocked);
        }
        if account.status != AccountStatus::Active {
            return Err(BankingError::AccountNotActive);
        }

        let is_valid = bcrypt::verify(pin, &account.pin_hash).unwrap_or(false);
        if !is_valid {
            account.pin_failed_attempts += 1;
            if account.pin_failed_attempts >= 3 {
                account.status = AccountStatus::PinBlocked;
                return Err(BankingError::AccountPinBlocked);
            }
            let attempts_left = 3 - account.pin_failed_attempts;
            return Err(BankingError::InvalidPin { attempts_left });
        }

        account.pin_failed_attempts = 0;
        Ok(())
    }

    // Core Banking Double-Entry Transfers
    pub fn execute_transfer(
        &self,
        req: TransferRequest,
        idempotency_key: String,
        initiated_by: Option<String>,
        client_ip: String,
        trace_id: String,
    ) -> Result<Transaction, BankingError> {
        if req.amount <= 0 {
            return Err(BankingError::BadRequest("Transfer amount must be positive".to_string()));
        }

        let mut state = self.state.write();
        let now = Utc::now();
        let fee = req.channel.fee();
        let total_debit = req.amount + fee;

        // 1. Validate Source Account
        let source_acc = state
            .accounts
            .get_mut(&req.source_account_number)
            .ok_or_else(|| BankingError::NotFound(format!("Source account {}", req.source_account_number)))?;

        Self::verify_pin_internal(source_acc, &req.pin)?;

        if source_acc.available_balance < total_debit {
            return Err(BankingError::InsufficientFunds);
        }

        // Daily limit check
        if total_debit > source_acc.daily_limit {
            return Err(BankingError::DailyLimitExceeded {
                limit: source_acc.daily_limit,
                remaining: source_acc.daily_limit,
            });
        }

        let source_id = source_acc.id.clone();
        let source_acc_no = source_acc.account_number.clone();
        let source_before = source_acc.balance;
        source_acc.balance -= total_debit;
        source_acc.available_balance -= total_debit;
        source_acc.version += 1;
        source_acc.updated_at = now;
        let source_after = source_acc.balance;

        let tx_id = Uuid::new_v4().to_string();
        let ref_no = format!("TXN-{}-{}", now.format("%Y%m%d%H%M%S"), &tx_id[0..8].to_uppercase());
        let mut postings = Vec::new();

        // Debit Source
        postings.push(LedgerPost {
            id: Uuid::new_v4().to_string(),
            transaction_id: tx_id.clone(),
            account_id: source_id.clone(),
            account_number: source_acc_no.clone(),
            direction: EntryDirection::Debit,
            amount: total_debit,
            amount_formatted: format_idr(total_debit),
            balance_before: source_before,
            balance_after: source_after,
            narration: req.description.clone().unwrap_or_else(|| format!("Transfer via {:?}", req.channel)),
            created_at: now,
        });

        // Destination Credit
        let (dest_id, dest_acc_no) = match req.channel {
            TxChannel::Intrabank => {
                let dest_acc = state
                    .accounts
                    .get_mut(&req.destination_account_number)
                    .ok_or_else(|| BankingError::NotFound(format!("Destination account {}", req.destination_account_number)))?;
                if dest_acc.status != AccountStatus::Active {
                    return Err(BankingError::AccountNotActive);
                }
                let d_before = dest_acc.balance;
                dest_acc.balance += req.amount;
                dest_acc.available_balance += req.amount;
                dest_acc.version += 1;
                dest_acc.updated_at = now;
                let d_after = dest_acc.balance;

                postings.push(LedgerPost {
                    id: Uuid::new_v4().to_string(),
                    transaction_id: tx_id.clone(),
                    account_id: dest_acc.id.clone(),
                    account_number: dest_acc.account_number.clone(),
                    direction: EntryDirection::Credit,
                    amount: req.amount,
                    amount_formatted: format_idr(req.amount),
                    balance_before: d_before,
                    balance_after: d_after,
                    narration: format!("Transfer Received from {}", source_acc_no),
                    created_at: now,
                });
                (Some(dest_acc.id.clone()), Some(dest_acc.account_number.clone()))
            }
            TxChannel::BiFast | TxChannel::Rtgs | TxChannel::Llg => {
                // Credit Nostro Clearing Account
                let nostro_acc = state
                    .accounts
                    .get_mut(GL_NOSTRO_CLEARING_NO)
                    .ok_or_else(|| BankingError::NotFound("GL Nostro Account".to_string()))?;
                let n_before = nostro_acc.balance;
                nostro_acc.balance += req.amount;
                nostro_acc.available_balance += req.amount;
                nostro_acc.version += 1;
                nostro_acc.updated_at = now;
                let n_after = nostro_acc.balance;

                postings.push(LedgerPost {
                    id: Uuid::new_v4().to_string(),
                    transaction_id: tx_id.clone(),
                    account_id: nostro_acc.id.clone(),
                    account_number: nostro_acc.account_number.clone(),
                    direction: EntryDirection::Credit,
                    amount: req.amount,
                    amount_formatted: format_idr(req.amount),
                    balance_before: n_before,
                    balance_after: n_after,
                    narration: format!("Nostro Outgoing Settlement to {} / {}", req.external_bank_code.as_deref().unwrap_or(""), req.destination_account_number),
                    created_at: now,
                });
                (None, Some(req.destination_account_number.clone()))
            }
            _ => return Err(BankingError::BadRequest("Unsupported channel for transfer endpoint".to_string())),
        };

        // Fee Credit
        if fee > 0 {
            let fee_acc = state
                .accounts
                .get_mut(GL_FEE_INCOME_ACCOUNT_NO)
                .ok_or_else(|| BankingError::NotFound("GL Fee Income Account".to_string()))?;
            let f_before = fee_acc.balance;
            fee_acc.balance += fee;
            fee_acc.available_balance += fee;
            fee_acc.version += 1;
            fee_acc.updated_at = now;
            let f_after = fee_acc.balance;

            postings.push(LedgerPost {
                id: Uuid::new_v4().to_string(),
                transaction_id: tx_id.clone(),
                account_id: fee_acc.id.clone(),
                account_number: fee_acc.account_number.clone(),
                direction: EntryDirection::Credit,
                amount: fee,
                amount_formatted: format_idr(fee),
                balance_before: f_before,
                balance_after: f_after,
                narration: format!("Admin Fee for {:?}", req.channel),
                created_at: now,
            });
        }

        // Validate Invariant
        let total_debit_post: i64 = postings.iter().filter(|p| p.direction == EntryDirection::Debit).map(|p| p.amount).sum();
        let total_credit_post: i64 = postings.iter().filter(|p| p.direction == EntryDirection::Credit).map(|p| p.amount).sum();
        if total_debit_post != total_credit_post {
            return Err(BankingError::UnbalancedJournal);
        }

        for p in &postings {
            state.ledger_entries.insert(0, p.clone());
        }

        let tx = Transaction {
            id: tx_id,
            reference_no: ref_no,
            idempotency_key,
            source_account_id: Some(source_id),
            source_account_number: Some(source_acc_no),
            destination_account_id: dest_id,
            destination_account_number: dest_acc_no,
            external_bank_code: req.external_bank_code,
            external_account_number: Some(req.destination_account_number),
            external_account_name: req.external_account_name,
            channel: req.channel,
            amount: req.amount,
            amount_formatted: format_idr(req.amount),
            fee_amount: fee,
            total_debit,
            total_debit_formatted: format_idr(total_debit),
            currency: "IDR".to_string(),
            description: req.description.unwrap_or_else(|| "Transfer".to_string()),
            status: TxStatus::Settled,
            initiated_by,
            client_ip,
            trace_id,
            ledger_postings: postings,
            settled_at: now,
            created_at: now,
        };

        state.transactions.insert(0, tx.clone());
        Ok(tx)
    }

    // Cash operations by Branch Teller
    pub fn execute_cash_transaction(
        &self,
        req: CashTransactionRequest,
        idempotency_key: String,
        teller_id: String,
        client_ip: String,
        trace_id: String,
    ) -> Result<Transaction, BankingError> {
        if req.amount <= 0 {
            return Err(BankingError::BadRequest("Cash amount must be positive".to_string()));
        }

        let mut state = self.state.write();
        let now = Utc::now();
        let is_deposit = req.operation.to_uppercase() == "DEPOSIT";

        let cust_acc = state
            .accounts
            .get_mut(&req.account_number)
            .ok_or_else(|| BankingError::NotFound(format!("Account {}", req.account_number)))?;
        if cust_acc.status != AccountStatus::Active {
            return Err(BankingError::AccountNotActive);
        }

        let cust_id = cust_acc.id.clone();
        let cust_acc_no = cust_acc.account_number.clone();
        let cust_before = cust_acc.balance;

        if !is_deposit && cust_acc.available_balance < req.amount {
            return Err(BankingError::InsufficientFunds);
        }

        if is_deposit {
            cust_acc.balance += req.amount;
            cust_acc.available_balance += req.amount;
        } else {
            cust_acc.balance -= req.amount;
            cust_acc.available_balance -= req.amount;
        }
        cust_acc.version += 1;
        cust_acc.updated_at = now;
        let cust_after = cust_acc.balance;

        // Vault counterparty
        let vault_acc = state
            .accounts
            .get_mut(GL_VAULT_ACCOUNT_NO)
            .ok_or_else(|| BankingError::NotFound("GL Vault Account".to_string()))?;
        let vault_id = vault_acc.id.clone();
        let vault_acc_no = vault_acc.account_number.clone();
        let vault_before = vault_acc.balance;

        if is_deposit {
            vault_acc.balance += req.amount;
            vault_acc.available_balance += req.amount;
        } else {
            vault_acc.balance -= req.amount;
            vault_acc.available_balance -= req.amount;
        }
        vault_acc.version += 1;
        vault_acc.updated_at = now;
        let vault_after = vault_acc.balance;

        let tx_id = Uuid::new_v4().to_string();
        let ref_no = format!("CSH-{}-{}", now.format("%Y%m%d%H%M%S"), &tx_id[0..8].to_uppercase());
        let channel = if is_deposit { TxChannel::TellerDeposit } else { TxChannel::TellerWithdrawal };

        let postings = if is_deposit {
            vec![
                LedgerPost {
                    id: Uuid::new_v4().to_string(),
                    transaction_id: tx_id.clone(),
                    account_id: vault_id.clone(),
                    account_number: vault_acc_no.clone(),
                    direction: EntryDirection::Debit,
                    amount: req.amount,
                    amount_formatted: format_idr(req.amount),
                    balance_before: vault_before,
                    balance_after: vault_after,
                    narration: format!("Teller Cash Deposit Inflow to {}", cust_acc_no),
                    created_at: now,
                },
                LedgerPost {
                    id: Uuid::new_v4().to_string(),
                    transaction_id: tx_id.clone(),
                    account_id: cust_id.clone(),
                    account_number: cust_acc_no.clone(),
                    direction: EntryDirection::Credit,
                    amount: req.amount,
                    amount_formatted: format_idr(req.amount),
                    balance_before: cust_before,
                    balance_after: cust_after,
                    narration: req.description.clone().unwrap_or_else(|| "Cash Deposit at Branch Counter".to_string()),
                    created_at: now,
                },
            ]
        } else {
            vec![
                LedgerPost {
                    id: Uuid::new_v4().to_string(),
                    transaction_id: tx_id.clone(),
                    account_id: cust_id.clone(),
                    account_number: cust_acc_no.clone(),
                    direction: EntryDirection::Debit,
                    amount: req.amount,
                    amount_formatted: format_idr(req.amount),
                    balance_before: cust_before,
                    balance_after: cust_after,
                    narration: req.description.clone().unwrap_or_else(|| "Cash Withdrawal at Branch Counter".to_string()),
                    created_at: now,
                },
                LedgerPost {
                    id: Uuid::new_v4().to_string(),
                    transaction_id: tx_id.clone(),
                    account_id: vault_id.clone(),
                    account_number: vault_acc_no.clone(),
                    direction: EntryDirection::Credit,
                    amount: req.amount,
                    amount_formatted: format_idr(req.amount),
                    balance_before: vault_before,
                    balance_after: vault_after,
                    narration: format!("Teller Cash Outflow from {}", cust_acc_no),
                    created_at: now,
                },
            ]
        };

        for p in &postings {
            state.ledger_entries.insert(0, p.clone());
        }

        let tx = Transaction {
            id: tx_id,
            reference_no: ref_no,
            idempotency_key,
            source_account_id: if is_deposit { Some(vault_id.clone()) } else { Some(cust_id.clone()) },
            source_account_number: if is_deposit { Some(vault_acc_no.clone()) } else { Some(cust_acc_no.clone()) },
            destination_account_id: if is_deposit { Some(cust_id) } else { Some(vault_id) },
            destination_account_number: if is_deposit { Some(cust_acc_no) } else { Some(vault_acc_no) },
            external_bank_code: None,
            external_account_number: None,
            external_account_name: None,
            channel,
            amount: req.amount,
            amount_formatted: format_idr(req.amount),
            fee_amount: 0,
            total_debit: req.amount,
            total_debit_formatted: format_idr(req.amount),
            currency: "IDR".to_string(),
            description: req.description.unwrap_or_else(|| if is_deposit { "Cash Deposit".to_string() } else { "Cash Withdrawal".to_string() }),
            status: TxStatus::Settled,
            initiated_by: Some(teller_id),
            client_ip,
            trace_id,
            ledger_postings: postings,
            settled_at: now,
            created_at: now,
        };

        state.transactions.insert(0, tx.clone());
        Ok(tx)
    }

    // Virtual Account Operations
    pub fn create_virtual_account(&self, req: CreateVARequest) -> Result<VirtualAccount, BankingError> {
        let mut state = self.state.write();
        let now = Utc::now();

        let merchant_acc = state
            .accounts
            .get(&req.merchant_account_number)
            .ok_or_else(|| BankingError::NotFound(format!("Merchant account {}", req.merchant_account_number)))?;
        let merchant_id = merchant_acc.id.clone();
        let merchant_name = merchant_acc.owner_name.clone().unwrap_or_else(|| "Merchant".to_string());

        let va_number = format!("88000{}", &Uuid::new_v4().to_string().replace('-', "")[0..11]);
        let expiry_days = req.expiry_days.unwrap_or(7);
        let expires_at = now + chrono::Duration::days(expiry_days);

        let va = VirtualAccount {
            id: Uuid::new_v4().to_string(),
            va_number: va_number.clone(),
            merchant_name,
            merchant_account_id: merchant_id,
            merchant_account_number: req.merchant_account_number,
            customer_name: req.customer_name,
            bill_amount: req.bill_amount,
            bill_amount_formatted: format_idr(req.bill_amount),
            currency: "IDR".to_string(),
            description: req.description.unwrap_or_else(|| "Virtual Account Billing".to_string()),
            status: VAStatus::Open,
            paid_transaction_id: None,
            expires_at,
            paid_at: None,
            created_at: now,
        };

        state.virtual_accounts.insert(va_number, va.clone());
        Ok(va)
    }

    pub fn get_virtual_account(&self, va_no: &str) -> Result<VirtualAccount, BankingError> {
        let state = self.state.read();
        state
            .virtual_accounts
            .get(va_no)
            .cloned()
            .ok_or_else(|| BankingError::NotFound(format!("Virtual Account {}", va_no)))
    }

    pub fn pay_virtual_account(
        &self,
        va_no: &str,
        req: PayVARequest,
        idempotency_key: String,
        client_ip: String,
        trace_id: String,
    ) -> Result<Transaction, BankingError> {
        let mut state = self.state.write();
        let now = Utc::now();

        let va = state
            .virtual_accounts
            .get(va_no)
            .ok_or_else(|| BankingError::NotFound(format!("Virtual Account {}", va_no)))?;

        if va.status != VAStatus::Open {
            return Err(BankingError::VaNotOpen(format!("{:?}", va.status)));
        }

        let bill_amount = va.bill_amount;
        let merchant_acc_no = va.merchant_account_number.clone();
        let va_desc = va.description.clone();

        // 1. Debit Payer Account
        let payer_acc = state
            .accounts
            .get_mut(&req.source_account_number)
            .ok_or_else(|| BankingError::NotFound(format!("Payer account {}", req.source_account_number)))?;

        Self::verify_pin_internal(payer_acc, &req.pin)?;

        if payer_acc.available_balance < bill_amount {
            return Err(BankingError::InsufficientFunds);
        }

        let payer_id = payer_acc.id.clone();
        let payer_acc_no = payer_acc.account_number.clone();
        let payer_before = payer_acc.balance;
        payer_acc.balance -= bill_amount;
        payer_acc.available_balance -= bill_amount;
        payer_acc.version += 1;
        payer_acc.updated_at = now;
        let payer_after = payer_acc.balance;

        // 2. Credit Merchant Account
        let merchant_acc = state
            .accounts
            .get_mut(&merchant_acc_no)
            .ok_or_else(|| BankingError::NotFound(format!("Merchant account {}", merchant_acc_no)))?;
        let merchant_id = merchant_acc.id.clone();
        let merchant_before = merchant_acc.balance;
        merchant_acc.balance += bill_amount;
        merchant_acc.available_balance += bill_amount;
        merchant_acc.version += 1;
        merchant_acc.updated_at = now;
        let merchant_after = merchant_acc.balance;

        let tx_id = Uuid::new_v4().to_string();
        let ref_no = format!("VAP-{}-{}", now.format("%Y%m%d%H%M%S"), &tx_id[0..8].to_uppercase());

        let postings = vec![
            LedgerPost {
                id: Uuid::new_v4().to_string(),
                transaction_id: tx_id.clone(),
                account_id: payer_id.clone(),
                account_number: payer_acc_no.clone(),
                direction: EntryDirection::Debit,
                amount: bill_amount,
                amount_formatted: format_idr(bill_amount),
                balance_before: payer_before,
                balance_after: payer_after,
                narration: format!("VA Payment to {}: {}", va_no, va_desc),
                created_at: now,
            },
            LedgerPost {
                id: Uuid::new_v4().to_string(),
                transaction_id: tx_id.clone(),
                account_id: merchant_id.clone(),
                account_number: merchant_acc_no.clone(),
                direction: EntryDirection::Credit,
                amount: bill_amount,
                amount_formatted: format_idr(bill_amount),
                balance_before: merchant_before,
                balance_after: merchant_after,
                narration: format!("VA Settlement from {}: {}", payer_acc_no, va_no),
                created_at: now,
            },
        ];

        for p in &postings {
            state.ledger_entries.insert(0, p.clone());
        }

        let tx = Transaction {
            id: tx_id.clone(),
            reference_no: ref_no,
            idempotency_key,
            source_account_id: Some(payer_id),
            source_account_number: Some(payer_acc_no),
            destination_account_id: Some(merchant_id),
            destination_account_number: Some(merchant_acc_no),
            external_bank_code: None,
            external_account_number: None,
            external_account_name: None,
            channel: TxChannel::VirtualAccount,
            amount: bill_amount,
            amount_formatted: format_idr(bill_amount),
            fee_amount: 0,
            total_debit: bill_amount,
            total_debit_formatted: format_idr(bill_amount),
            currency: "IDR".to_string(),
            description: format!("VA Payment: {}", va_desc),
            status: TxStatus::Settled,
            initiated_by: None,
            client_ip,
            trace_id,
            ledger_postings: postings,
            settled_at: now,
            created_at: now,
        };

        state.transactions.insert(0, tx.clone());

        // Update VA status
        if let Some(va_mut) = state.virtual_accounts.get_mut(va_no) {
            va_mut.status = VAStatus::Paid;
            va_mut.paid_transaction_id = Some(tx_id);
            va_mut.paid_at = Some(now);
        }

        Ok(tx)
    }

    // Customer Service operations
    pub fn unblock_account_pin(&self, acc_no: &str, new_pin: &str) -> Result<Account, BankingError> {
        let mut state = self.state.write();
        let acc = state
            .accounts
            .get_mut(acc_no)
            .ok_or_else(|| BankingError::NotFound(format!("Account {}", acc_no)))?;

        let new_hash = bcrypt::hash(new_pin, bcrypt::DEFAULT_COST)
            .map_err(|e| BankingError::BadRequest(format!("Failed to hash pin: {}", e)))?;

        acc.pin_hash = new_hash;
        acc.pin_failed_attempts = 0;
        acc.status = AccountStatus::Active;
        acc.updated_at = Utc::now();

        Ok(acc.clone())
    }

    pub fn update_account_status(&self, acc_no: &str, status: AccountStatus) -> Result<Account, BankingError> {
        let mut state = self.state.write();
        let acc = state
            .accounts
            .get_mut(acc_no)
            .ok_or_else(|| BankingError::NotFound(format!("Account {}", acc_no)))?;
        acc.status = status;
        acc.updated_at = Utc::now();
        Ok(acc.clone())
    }

    // Onboarding new Customer & Account
    pub fn onboard_customer(
        &self,
        req: OnboardCustomerRequest,
        idempotency_key: String,
        client_ip: String,
        trace_id: String,
    ) -> Result<(Customer, Account, Transaction), BankingError> {
        let mut state = self.state.write();
        let now = Utc::now();

        if state.customers_by_email.contains_key(&req.email.to_lowercase()) {
            return Err(BankingError::DuplicateEmail);
        }

        let cust_id = Uuid::new_v4().to_string();
        let cif_number = format!("CIF-{}", &cust_id[0..8].to_uppercase());
        let pass_hash = bcrypt::hash(&req.password, bcrypt::DEFAULT_COST)
            .map_err(|e| BankingError::BadRequest(e.to_string()))?;
        let pin_hash = bcrypt::hash(&req.pin, bcrypt::DEFAULT_COST)
            .map_err(|e| BankingError::BadRequest(e.to_string()))?;

        let customer = Customer {
            id: cust_id.clone(),
            cif_number: cif_number.clone(),
            nik_masked: format!("{}********{}", &req.nik[0..4.min(req.nik.len())], &req.nik[req.nik.len().saturating_sub(4)..]),
            full_name: req.full_name.clone(),
            email: req.email.clone(),
            phone_number: req.phone_number.clone(),
            password_hash: pass_hash,
            role: UserRole::Customer,
            kyc_status: KYCStatus::Verified,
            is_active: true,
            created_at: now,
            updated_at: now,
        };

        state.customers_by_email.insert(req.email.to_lowercase(), cust_id.clone());
        state.customers.insert(cust_id.clone(), customer.clone());

        // Account
        let acc_id = Uuid::new_v4().to_string();
        let acc_no = format!("52403{}", &acc_id[0..5].chars().filter(|c| c.is_ascii_digit()).collect::<String>());
        let acc_no = if acc_no.len() < 10 {
            format!("{:0<10}", acc_no)
        } else {
            acc_no
        };

        let initial_deposit = req.initial_deposit;
        let account = Account {
            id: acc_id.clone(),
            account_number: acc_no.clone(),
            customer_id: cust_id.clone(),
            owner_name: Some(req.full_name),
            account_type: req.account_type,
            card_tier: req.card_tier,
            currency: "IDR".to_string(),
            balance: initial_deposit,
            hold_balance: 0,
            available_balance: initial_deposit,
            daily_limit: req.card_tier.daily_limit(),
            pin_hash,
            pin_failed_attempts: 0,
            status: AccountStatus::Active,
            version: 1,
            created_at: now,
            updated_at: now,
        };

        state.accounts_by_id.insert(acc_id.clone(), acc_no.clone());
        state.accounts.insert(acc_no.clone(), account.clone());

        // Vault counterparty for initial deposit
        let vault_acc = state
            .accounts
            .get_mut(GL_VAULT_ACCOUNT_NO)
            .ok_or_else(|| BankingError::NotFound("GL Vault Account".to_string()))?;
        let vault_id = vault_acc.id.clone();
        let vault_acc_no = vault_acc.account_number.clone();
        let vault_before = vault_acc.balance;
        vault_acc.balance -= initial_deposit;
        vault_acc.available_balance -= initial_deposit;
        vault_acc.version += 1;
        vault_acc.updated_at = now;
        let vault_after = vault_acc.balance;

        let tx_id = Uuid::new_v4().to_string();
        let ref_no = format!("ONB-{}-{}", now.format("%Y%m%d%H%M%S"), &tx_id[0..8].to_uppercase());

        let postings = vec![
            LedgerPost {
                id: Uuid::new_v4().to_string(),
                transaction_id: tx_id.clone(),
                account_id: vault_id.clone(),
                account_number: vault_acc_no.clone(),
                direction: EntryDirection::Debit,
                amount: initial_deposit,
                amount_formatted: format_idr(initial_deposit),
                balance_before: vault_before,
                balance_after: vault_after,
                narration: format!("Initial Deposit Onboarding for CIF {}", cif_number),
                created_at: now,
            },
            LedgerPost {
                id: Uuid::new_v4().to_string(),
                transaction_id: tx_id.clone(),
                account_id: acc_id.clone(),
                account_number: acc_no.clone(),
                direction: EntryDirection::Credit,
                amount: initial_deposit,
                amount_formatted: format_idr(initial_deposit),
                balance_before: 0,
                balance_after: initial_deposit,
                narration: "Initial Account Opening Deposit".to_string(),
                created_at: now,
            },
        ];

        for p in &postings {
            state.ledger_entries.insert(0, p.clone());
        }

        let tx = Transaction {
            id: tx_id,
            reference_no: ref_no,
            idempotency_key,
            source_account_id: Some(vault_id),
            source_account_number: Some(vault_acc_no),
            destination_account_id: Some(acc_id),
            destination_account_number: Some(acc_no),
            external_bank_code: None,
            external_account_number: None,
            external_account_name: None,
            channel: TxChannel::Intrabank,
            amount: initial_deposit,
            amount_formatted: format_idr(initial_deposit),
            fee_amount: 0,
            total_debit: initial_deposit,
            total_debit_formatted: format_idr(initial_deposit),
            currency: "IDR".to_string(),
            description: "Onboarding Initial Deposit".to_string(),
            status: TxStatus::Settled,
            initiated_by: Some(cust_id),
            client_ip,
            trace_id,
            ledger_postings: postings,
            settled_at: now,
            created_at: now,
        };

        state.transactions.insert(0, tx.clone());
        Ok((customer, account, tx))
    }

    // Statements & Transactions query
    pub fn get_account_statement(&self, acc_no: &str) -> Vec<LedgerPost> {
        let state = self.state.read();
        state
            .ledger_entries
            .iter()
            .filter(|p| p.account_number == acc_no)
            .cloned()
            .collect()
    }

    pub fn list_transactions(&self) -> Vec<Transaction> {
        let state = self.state.read();
        state.transactions.clone()
    }

    pub fn get_transaction_by_ref(&self, ref_no: &str) -> Result<Transaction, BankingError> {
        let state = self.state.read();
        state
            .transactions
            .iter()
            .find(|t| t.reference_no == ref_no || t.id == ref_no)
            .cloned()
            .ok_or_else(|| BankingError::NotFound(format!("Transaction {}", ref_no)))
    }

    // Auditor Trial Balance
    pub fn get_trial_balance(&self) -> TrialBalanceSummary {
        let state = self.state.read();
        let total_debit: i64 = state
            .ledger_entries
            .iter()
            .filter(|p| p.direction == EntryDirection::Debit)
            .map(|p| p.amount)
            .sum();
        let total_credit: i64 = state
            .ledger_entries
            .iter()
            .filter(|p| p.direction == EntryDirection::Credit)
            .map(|p| p.amount)
            .sum();
        let discrepancy = total_debit - total_credit;
        let is_balanced = discrepancy == 0;

        TrialBalanceSummary {
            total_debit_postings: total_debit,
            total_credit_postings: total_credit,
            discrepancy,
            is_balanced,
            total_ledger_entries: state.ledger_entries.len(),
            total_transactions: state.transactions.len(),
            checked_at: Utc::now(),
        }
    }

    // Audit logs
    pub fn record_audit_log(
        &self,
        actor_id: Option<String>,
        action: AuditAction,
        resource_name: String,
        resource_id: String,
        ip_address: String,
        trace_id: String,
        payload: Option<serde_json::Value>,
    ) {
        let mut state = self.state.write();
        state.audit_logs.insert(
            0,
            AuditLog {
                id: Uuid::new_v4().to_string(),
                actor_id,
                action,
                resource_name,
                resource_id,
                ip_address,
                trace_id,
                payload_snapshot: payload,
                created_at: Utc::now(),
            },
        );
    }

    pub fn list_audit_logs(&self) -> Vec<AuditLog> {
        let state = self.state.read();
        state.audit_logs.clone()
    }

    pub fn list_virtual_accounts(&self) -> Vec<VirtualAccount> {
        let state = self.state.read();
        state.virtual_accounts.values().cloned().collect()
    }
}
