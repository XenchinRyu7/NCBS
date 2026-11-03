package model

import "time"

type UserRole string

const (
	RoleCustomer        UserRole = "CUSTOMER"         // Nasabah Perorangan
	RoleMerchant        UserRole = "MERCHANT"         // Nasabah Korporasi / Biller Virtual Account
	RoleTeller          UserRole = "TELLER"           // Petugas Teller Cabang (Setor/Tarik Tunai)
	RoleCustomerService UserRole = "CUSTOMER_SERVICE" // CS Cabang (Onboarding, Buka Blokir PIN)
	RoleAuditor         UserRole = "AUDITOR"          // Auditor Internal / OJK (Read-Only Ledger & Audit)
	RoleAdmin           UserRole = "ADMIN"            // Core Banking System Administrator
)

// PermissionsForRole returns the explicit list of banking capabilities granted to a role.
func PermissionsForRole(role UserRole) []string {
	switch role {
	case RoleCustomer:
		return []string{
			"auth:me",
			"account:read:own",
			"account:statement:own",
			"transaction:transfer:own",
			"virtual_account:inquiry",
			"virtual_account:pay",
		}
	case RoleMerchant:
		return []string{
			"auth:me",
			"account:read:own",
			"account:statement:own",
			"transaction:transfer:own",
			"virtual_account:create",
			"virtual_account:inquiry",
			"virtual_account:pay",
		}
	case RoleTeller:
		return []string{
			"auth:me",
			"customer:onboard",
			"account:read:any",
			"transaction:cash_deposit",
			"transaction:cash_withdrawal",
		}
	case RoleCustomerService:
		return []string{
			"auth:me",
			"customer:onboard",
			"account:read:any",
			"account:statement:any",
			"account:unblock_pin",
		}
	case RoleAuditor:
		return []string{
			"auth:me",
			"account:read:any",
			"account:read:gl_internal",
			"account:statement:any",
			"ledger:trial_balance",
			"audit_logs:read",
		}
	case RoleAdmin:
		return []string{
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
		}
	default:
		return []string{}
	}
}

type KYCStatus string

const (
	KYCUnverified KYCStatus = "UNVERIFIED"
	KYCVerified   KYCStatus = "VERIFIED"
	KYCRejected   KYCStatus = "REJECTED"
	KYCEDD        KYCStatus = "ENHANCED_DUE_DILIGENCE"
)

type AccountType string

const (
	AccountSavings      AccountType = "SAVINGS"       // Tabungan Reguler
	AccountSavingsYouth AccountType = "SAVINGS_YOUTH" // Tabungan Muda
	AccountChecking     AccountType = "CHECKING"      // Rekening Giro Korporasi
	AccountTimeDeposit  AccountType = "TIME_DEPOSIT"  // Simpanan Berjangka
	AccountGLInternal   AccountType = "GL_INTERNAL"   // Rekening Buku Besar Internal Bank
)

type CardTier string

const (
	TierSilver   CardTier = "SILVER"   // Limit Rp 50.000.000 / hari
	TierGold     CardTier = "GOLD"     // Limit Rp 100.000.000 / hari
	TierPlatinum CardTier = "PLATINUM" // Limit Rp 250.000.000 / hari
	TierPriority CardTier = "PRIORITY" // Limit Rp 1.000.000.000 / hari
)

func DailyLimitForTier(tier CardTier) int64 {
	switch tier {
	case TierSilver:
		return 50_000_000
	case TierGold:
		return 100_000_000
	case TierPlatinum:
		return 250_000_000
	case TierPriority:
		return 1_000_000_000
	default:
		return 50_000_000
	}
}

type AccountStatus string

const (
	AccountActive     AccountStatus = "ACTIVE"
	AccountDormant    AccountStatus = "DORMANT"
	AccountFrozen     AccountStatus = "FROZEN"
	AccountPINBlocked AccountStatus = "PIN_BLOCKED"
	AccountClosed     AccountStatus = "CLOSED"
)

type TxChannel string

const (
	ChannelIntrabank        TxChannel = "INTRABANK"
	ChannelBIFast           TxChannel = "BI_FAST"
	ChannelRTGS             TxChannel = "RTGS"
	ChannelLLG              TxChannel = "LLG"
	ChannelVirtualAccount   TxChannel = "VIRTUAL_ACCOUNT"
	ChannelTellerDeposit    TxChannel = "TELLER_DEPOSIT"
	ChannelTellerWithdrawal TxChannel = "TELLER_WITHDRAWAL"
)

type TxStatus string

const (
	TxPending  TxStatus = "PENDING"
	TxSettled  TxStatus = "SETTLED"
	TxFailed   TxStatus = "FAILED"
	TxReversed TxStatus = "REVERSED"
)

type EntryDirection string

const (
	DirectionDebit  EntryDirection = "DEBIT"
	DirectionCredit EntryDirection = "CREDIT"
)

type VAStatus string

const (
	VAOpen      VAStatus = "OPEN"
	VAPaid      VAStatus = "PAID"
	VAExpired   VAStatus = "EXPIRED"
	VACancelled VAStatus = "CANCELLED"
)

type AuditAction string

const (
	AuditCreate       AuditAction = "CREATE"
	AuditUpdate       AuditAction = "UPDATE"
	AuditDelete       AuditAction = "DELETE"
	AuditAuthenticate AuditAction = "AUTHENTICATE"
	AuditTransfer     AuditAction = "TRANSFER"
	AuditVAPayment    AuditAction = "VA_PAYMENT"
	AuditPINFailure   AuditAction = "PIN_FAILURE"
	AuditPINUnblock   AuditAction = "PIN_UNBLOCK"
)

// Customer represents the customers (CIF) table.
type Customer struct {
	ID           string    `json:"id"`
	CIFNumber    string    `json:"cif_number"`
	NIKEncrypted string    `json:"nik_masked"`
	FullName     string    `json:"full_name"`
	Email        string    `json:"email"`
	PhoneNumber  string    `json:"phone_number"`
	PasswordHash string    `json:"-"`
	Role         UserRole  `json:"role"`
	KYCStatus    KYCStatus `json:"kyc_status"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Account represents a bank account or internal General Ledger (GL) account.
type Account struct {
	ID                string        `json:"id"`
	AccountNumber     string        `json:"account_number"`
	CustomerID        string        `json:"customer_id"`
	OwnerName         string        `json:"owner_name,omitempty"`
	AccountType       AccountType   `json:"account_type"`
	CardTier          CardTier      `json:"card_tier"`
	Currency          string        `json:"currency"`
	Balance           int64         `json:"balance"`
	HoldBalance       int64         `json:"hold_balance"`
	AvailableBalance  int64         `json:"available_balance"`
	DailyLimit        int64         `json:"daily_limit"`
	PINHash           string        `json:"-"`
	PINFailedAttempts int           `json:"pin_failed_attempts"`
	Status            AccountStatus `json:"status"`
	Version           int64         `json:"version"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

// Transaction represents the header of a financial transaction.
type Transaction struct {
	ID                   string       `json:"id"`
	ReferenceNo          string       `json:"reference_no"`
	IdempotencyKey       string       `json:"idempotency_key"`
	SourceAccountID      string       `json:"source_account_id,omitempty"`
	SourceAccountNo      string       `json:"source_account_number,omitempty"`
	DestinationAccountID string       `json:"destination_account_id,omitempty"`
	DestinationAccountNo string       `json:"destination_account_number,omitempty"`
	ExternalBankCode     string       `json:"external_bank_code,omitempty"`
	ExternalAccountNo    string       `json:"external_account_number,omitempty"`
	ExternalAccountName  string       `json:"external_account_name,omitempty"`
	Channel              TxChannel    `json:"channel"`
	Amount               int64        `json:"amount"`
	AmountFormatted      string       `json:"amount_formatted"`
	FeeAmount            int64        `json:"fee_amount"`
	TotalDebit           int64        `json:"total_debit"`
	TotalDebitFormatted  string       `json:"total_debit_formatted"`
	Currency             string       `json:"currency"`
	Description          string       `json:"description"`
	Status               TxStatus     `json:"status"`
	InitiatedBy          string       `json:"initiated_by,omitempty"`
	ClientIP             string       `json:"client_ip"`
	TraceID              string       `json:"trace_id"`
	LedgerPostings       []LedgerPost `json:"ledger_postings,omitempty"`
	SettledAt            time.Time    `json:"settled_at"`
	CreatedAt            time.Time    `json:"created_at"`
}

// LedgerPost represents an immutable single leg (DEBIT or CREDIT) in the General Ledger.
type LedgerPost struct {
	ID            string         `json:"id"`
	TransactionID string         `json:"transaction_id"`
	ReferenceNo   string         `json:"reference_no,omitempty"`
	AccountID     string         `json:"account_id"`
	AccountNumber string         `json:"account_number"`
	Direction     EntryDirection `json:"direction"`
	Amount        int64          `json:"amount"`
	BalanceBefore int64          `json:"balance_before"`
	BalanceAfter  int64          `json:"balance_after"`
	Narration     string         `json:"narration"`
	CreatedAt     time.Time      `json:"created_at"`
}

// VirtualAccount represents a Virtual Account billing record.
type VirtualAccount struct {
	ID                string     `json:"id"`
	VANumber          string     `json:"va_number"`
	MerchantName      string     `json:"merchant_name"`
	MerchantAccountID string     `json:"merchant_account_id"`
	MerchantAccountNo string     `json:"merchant_account_number"`
	CustomerName      string     `json:"customer_name"`
	BillAmount        int64      `json:"bill_amount"`
	BillFormatted     string     `json:"bill_amount_formatted"`
	Currency          string     `json:"currency"`
	Description       string     `json:"description"`
	Status            VAStatus   `json:"status"`
	PaidTransactionID string     `json:"paid_transaction_id,omitempty"`
	ExpiresAt         time.Time  `json:"expires_at"`
	PaidAt            *time.Time `json:"paid_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

// APIKey represents an Open Banking Partner API Key.
type APIKey struct {
	ID              string     `json:"id"`
	PartnerName     string     `json:"partner_name"`
	KeyHash         string     `json:"-"`
	Prefix          string     `json:"prefix"`
	RateLimitPerMin int        `json:"rate_limit_per_min"`
	IsActive        bool       `json:"is_active"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// AuditLog represents an immutable OJK/BI regulatory audit trail entry.
type AuditLog struct {
	ID              string                 `json:"id"`
	ActorID         *string                `json:"actor_id,omitempty"`
	Action          AuditAction            `json:"action"`
	ResourceName    string                 `json:"resource_name"`
	ResourceID      string                 `json:"resource_id,omitempty"`
	IPAddress       string                 `json:"ip_address"`
	TraceID         string                 `json:"trace_id"`
	PayloadSnapshot map[string]interface{} `json:"payload_snapshot,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
}
