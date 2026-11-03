package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cbs-bank/enterprise-portfolio-api/internal/model"
	"github.com/cbs-bank/enterprise-portfolio-api/pkg/response"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotFound             = errors.New("resource not found")
	ErrAccountNotActive     = errors.New("account is not active or is blocked")
	ErrAccountPINBlocked    = errors.New("account is PIN_BLOCKED due to 3 consecutive failed PIN attempts; contact Customer Service to unblock")
	ErrInvalidPIN           = errors.New("invalid 6-digit transaction PIN")
	ErrInsufficientFunds    = errors.New("insufficient available balance for transaction amount and fee")
	ErrDailyLimitExceeded   = errors.New("transaction exceeds remaining daily transfer velocity limit for card tier")
	ErrVANotOpen            = errors.New("virtual account is already PAID, EXPIRED, or CANCELLED")
	ErrDuplicateEmail       = errors.New("customer email is already registered")
	ErrUnbalancedJournal    = errors.New("critical ledger invariant violation: total debit does not equal total credit")
	ErrForbiddenAccount     = errors.New("you do not have permission to access or transact from an account you do not own")
)

// DemoSandboxRawKey is the pre-provisioned SNAP-BI Open Banking Sandbox Key.
const DemoSandboxRawKey = "cbs_live_snap_sandbox_2026"

// Internal General Ledger Account Numbers
const (
	GLVaultAccountNo     = "1001000001" // GL Kas Utama / Central Reserve Vault
	GLNostroClearingNo   = "2001000002" // GL Nostro BI-FAST / RTGS / LLG Settlement
	GLFeeIncomeAccountNo = "4001000003" // GL Pendapatan Biaya Admin Transfer
)

// TrialBalanceSummary proves double-entry integrity across the entire General Ledger.
type TrialBalanceSummary struct {
	TotalDebitPostings  int64     `json:"total_debit_postings"`
	TotalCreditPostings int64     `json:"total_credit_postings"`
	Discrepancy         int64     `json:"discrepancy"`
	IsBalanced          bool      `json:"is_balanced"`
	TotalLedgerEntries  int       `json:"total_ledger_entries"`
	TotalTransactions   int       `json:"total_transactions"`
	CheckedAt           time.Time `json:"checked_at"`
}

// Repository manages Core Banking ACID transactions, Double-Entry Ledger postings, Accounts, CIF, and VAs.
type Repository struct {
	db   *sql.DB
	salt string

	mu              sync.RWMutex
	customers       map[string]*model.Customer       // by ID
	customersByMail map[string]string                // email -> customer ID
	accounts        map[string]*model.Account        // by AccountNumber
	accountsByID    map[string]*model.Account        // by ID
	transactions    []model.Transaction              // newest first
	ledgerEntries   []model.LedgerPost               // newest first
	virtualAccounts map[string]*model.VirtualAccount // by VANumber
	apiKeys         map[string]model.APIKey          // by KeyHash
	auditLogs       []model.AuditLog                 // newest first
}

// New initializes the Core Banking Repository with balanced initial General Ledger & Customer seed data.
func New(db *sql.DB, apiKeySalt string) *Repository {
	r := &Repository{
		db:              db,
		salt:            apiKeySalt,
		customers:       make(map[string]*model.Customer),
		customersByMail: make(map[string]string),
		accounts:        make(map[string]*model.Account),
		accountsByID:    make(map[string]*model.Account),
		virtualAccounts: make(map[string]*model.VirtualAccount),
		apiKeys:         make(map[string]model.APIKey),
	}
	r.seedCoreBankingData()
	return r
}

func (r *Repository) seedCoreBankingData() {
	now := time.Now().UTC()

	// 1. Seed Sandbox API Key (SNAP-BI Partner)
	demoHash := r.HashAPIKey(DemoSandboxRawKey)
	exp := now.Add(365 * 24 * time.Hour)
	r.apiKeys[demoHash] = model.APIKey{
		ID:              "c2eebc99-9c0b-4ef8-bb6d-6bb9bd380a33",
		PartnerName:     "SNAP-BI Open Banking Playground Partner",
		KeyHash:         demoHash,
		Prefix:          "cbs_live_",
		RateLimitPerMin: 120,
		IsActive:        true,
		ExpiresAt:       &exp,
		CreatedAt:       now,
	}

	// Uniform demo password ("Password#2026") for all pre-seeded accounts so testing is effortless
	defaultPassHash, _ := bcrypt.GenerateFromPassword([]byte("Password#2026"), bcrypt.MinCost)

	pin123456, _ := bcrypt.GenerateFromPassword([]byte("123456"), bcrypt.MinCost)
	pin654321, _ := bcrypt.GenerateFromPassword([]byte("654321"), bcrypt.MinCost)
	pin888888, _ := bcrypt.GenerateFromPassword([]byte("888888"), bcrypt.MinCost)

	// 2. Seed System & Bank Staff Profiles across all RBAC Roles
	systemCIF := &model.Customer{
		ID:           "00000000-0000-0000-0000-000000000001",
		CIFNumber:    "CIF-00000001",
		NIKEncrypted: "0000********0001",
		FullName:     "PT Bank Nusantara Core Tbk (Treasury & GL)",
		Email:        "treasury@cbs-bank.co.id",
		PhoneNumber:  "1500999",
		PasswordHash: string(defaultPassHash),
		Role:         model.RoleAdmin,
		KYCStatus:    model.KYCVerified,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	adminUser := &model.Customer{
		ID:           "00000000-0000-0000-0000-000000000002",
		CIFNumber:    "CIF-00000002",
		NIKEncrypted: "3171********0002",
		FullName:     "Hendra Gunawan (Core Banking Admin)",
		Email:        "admin@cbs-bank.co.id",
		PhoneNumber:  "+628111000001",
		PasswordHash: string(defaultPassHash),
		Role:         model.RoleAdmin,
		KYCStatus:    model.KYCVerified,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	tellerUser := &model.Customer{
		ID:           "00000000-0000-0000-0000-000000000003",
		CIFNumber:    "CIF-00000003",
		NIKEncrypted: "3171********0003",
		FullName:     "Rina Wijaya (Head Teller Cabang Sudirman)",
		Email:        "teller@cbs-bank.co.id",
		PhoneNumber:  "+628111000002",
		PasswordHash: string(defaultPassHash),
		Role:         model.RoleTeller,
		KYCStatus:    model.KYCVerified,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	csUser := &model.Customer{
		ID:           "00000000-0000-0000-0000-000000000005",
		CIFNumber:    "CIF-00000005",
		NIKEncrypted: "3171********0005",
		FullName:     "Nadia Kusuma (Customer Service Cabang Sudirman)",
		Email:        "cs@cbs-bank.co.id",
		PhoneNumber:  "+628111000005",
		PasswordHash: string(defaultPassHash),
		Role:         model.RoleCustomerService,
		KYCStatus:    model.KYCVerified,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	auditorUser := &model.Customer{
		ID:           "00000000-0000-0000-0000-000000000004",
		CIFNumber:    "CIF-00000004",
		NIKEncrypted: "3171********0004",
		FullName:     "Dr. Prasetyo Wibowo (Internal & OJK Auditor)",
		Email:        "auditor@cbs-bank.co.id",
		PhoneNumber:  "+628111000003",
		PasswordHash: string(defaultPassHash),
		Role:         model.RoleAuditor,
		KYCStatus:    model.KYCVerified,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// 3. Seed Customer & Corporate Merchant CIF Profiles
	budi := &model.Customer{
		ID:           "a1eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		CIFNumber:    "CIF-90010001",
		NIKEncrypted: response.MaskNIK("3171041508900001"),
		FullName:     "Budi Santoso",
		Email:        "budi.santoso@cbs-bank.co.id",
		PhoneNumber:  "+6281290001001",
		PasswordHash: string(defaultPassHash),
		Role:         model.RoleCustomer,
		KYCStatus:    model.KYCVerified,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	siti := &model.Customer{
		ID:           "b2eebc99-9c0b-4ef8-bb6d-6bb9bd380a22",
		CIFNumber:    "CIF-90010002",
		NIKEncrypted: response.MaskNIK("3173082211940002"),
		FullName:     "Siti Rahmawati",
		Email:        "siti.rahma@cbs-bank.co.id",
		PhoneNumber:  "+6281290002002",
		PasswordHash: string(defaultPassHash),
		Role:         model.RoleCustomer,
		KYCStatus:    model.KYCVerified,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	merchant := &model.Customer{
		ID:           "c3eebc99-9c0b-4ef8-bb6d-6bb9bd380a88",
		CIFNumber:    "CIF-90018888",
		NIKEncrypted: response.MaskNIK("3174010101888888"),
		FullName:     "PT Toko Nusantara Digital Tbk",
		Email:        "merchant@tokonusantara.co.id",
		PhoneNumber:  "+622150888888",
		PasswordHash: string(defaultPassHash),
		Role:         model.RoleMerchant,
		KYCStatus:    model.KYCVerified,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	for _, c := range []*model.Customer{systemCIF, adminUser, tellerUser, csUser, auditorUser, budi, siti, merchant} {
		r.customers[c.ID] = c
		r.customersByMail[strings.ToLower(c.Email)] = c.ID
	}

	// 4. Seed Internal General Ledger Accounts & Customer Accounts
	glVault := &model.Account{
		ID:               "gl000000-0000-0000-0000-000000000001",
		AccountNumber:    GLVaultAccountNo,
		CustomerID:       systemCIF.ID,
		OwnerName:        "GL Kas Utama & Cadangan Likuiditas Bank",
		AccountType:      model.AccountGLInternal,
		CardTier:         model.TierPriority,
		Currency:         "IDR",
		Balance:          100_000_000_000 - (500_000_000 + 75_000_000 + 250_000_000),
		AvailableBalance: 100_000_000_000 - (500_000_000 + 75_000_000 + 250_000_000),
		DailyLimit:       999_999_999_999,
		PINHash:          string(pin888888),
		Status:           model.AccountActive,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	glNostro := &model.Account{
		ID:               "gl000000-0000-0000-0000-000000000002",
		AccountNumber:    GLNostroClearingNo,
		CustomerID:       systemCIF.ID,
		OwnerName:        "GL Nostro Settlement BI-FAST & RTGS Bank Indonesia",
		AccountType:      model.AccountGLInternal,
		CardTier:         model.TierPriority,
		Currency:         "IDR",
		Balance:          0,
		AvailableBalance: 0,
		DailyLimit:       999_999_999_999,
		PINHash:          string(pin888888),
		Status:           model.AccountActive,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	glFeeIncome := &model.Account{
		ID:               "gl000000-0000-0000-0000-000000000003",
		AccountNumber:    GLFeeIncomeAccountNo,
		CustomerID:       systemCIF.ID,
		OwnerName:        "GL Pendapatan Fee Transaksi & Administrasi",
		AccountType:      model.AccountGLInternal,
		CardTier:         model.TierPriority,
		Currency:         "IDR",
		Balance:          0,
		AvailableBalance: 0,
		DailyLimit:       999_999_999_999,
		PINHash:          string(pin888888),
		Status:           model.AccountActive,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	accBudi := &model.Account{
		ID:               "acc00000-0000-0000-0000-000052403001",
		AccountNumber:    "5240301001",
		CustomerID:       budi.ID,
		OwnerName:        budi.FullName,
		AccountType:      model.AccountSavings,
		CardTier:         model.TierPriority,
		Currency:         "IDR",
		Balance:          500_000_000, // Rp 500.000.000
		HoldBalance:      0,
		AvailableBalance: 500_000_000,
		DailyLimit:       model.DailyLimitForTier(model.TierPriority),
		PINHash:          string(pin123456),
		Status:           model.AccountActive,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	accSiti := &model.Account{
		ID:               "acc00000-0000-0000-0000-000052403002",
		AccountNumber:    "5240302002",
		CustomerID:       siti.ID,
		OwnerName:        siti.FullName,
		AccountType:      model.AccountSavings,
		CardTier:         model.TierPlatinum,
		Currency:         "IDR",
		Balance:          75_000_000, // Rp 75.000.000
		HoldBalance:      0,
		AvailableBalance: 75_000_000,
		DailyLimit:       model.DailyLimitForTier(model.TierPlatinum),
		PINHash:          string(pin654321),
		Status:           model.AccountActive,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	accMerchant := &model.Account{
		ID:               "acc00000-0000-0000-0000-000052403888",
		AccountNumber:    "5240308888",
		CustomerID:       merchant.ID,
		OwnerName:        merchant.FullName,
		AccountType:      model.AccountChecking,
		CardTier:         model.TierPriority,
		Currency:         "IDR",
		Balance:          250_000_000, // Rp 250.000.000
		HoldBalance:      0,
		AvailableBalance: 250_000_000,
		DailyLimit:       model.DailyLimitForTier(model.TierPriority),
		PINHash:          string(pin888888),
		Status:           model.AccountActive,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	for _, acc := range []*model.Account{glVault, glNostro, glFeeIncome, accBudi, accSiti, accMerchant} {
		r.accounts[acc.AccountNumber] = acc
		r.accountsByID[acc.ID] = acc
	}

	// 5. Record Balanced Genesis Double-Entry Ledger Postings for Initial Account Funding
	genesisTxID := "tx000000-0000-0000-0000-000000000001"
	refGenesis := "NCB-GENESIS-OPENING-001"
	totalInitial := int64(500_000_000 + 75_000_000 + 250_000_000)

	genesisPostings := []model.LedgerPost{
		{
			ID:            uuid.New().String(),
			TransactionID: genesisTxID,
			ReferenceNo:   refGenesis,
			AccountID:     glVault.ID,
			AccountNumber: glVault.AccountNumber,
			Direction:     model.DirectionDebit,
			Amount:        totalInitial,
			BalanceBefore: 100_000_000_000,
			BalanceAfter:  100_000_000_000 - totalInitial,
			Narration:     "Genesis Core Capital Allocation to Customer Accounts",
			CreatedAt:     now,
		},
		{
			ID:            uuid.New().String(),
			TransactionID: genesisTxID,
			ReferenceNo:   refGenesis,
			AccountID:     accBudi.ID,
			AccountNumber: accBudi.AccountNumber,
			Direction:     model.DirectionCredit,
			Amount:        500_000_000,
			BalanceBefore: 0,
			BalanceAfter:  500_000_000,
			Narration:     "Initial Opening Deposit - Savings Priority",
			CreatedAt:     now,
		},
		{
			ID:            uuid.New().String(),
			TransactionID: genesisTxID,
			ReferenceNo:   refGenesis,
			AccountID:     accSiti.ID,
			AccountNumber: accSiti.AccountNumber,
			Direction:     model.DirectionCredit,
			Amount:        75_000_000,
			BalanceBefore: 0,
			BalanceAfter:  75_000_000,
			Narration:     "Initial Opening Deposit - Savings Platinum",
			CreatedAt:     now,
		},
		{
			ID:            uuid.New().String(),
			TransactionID: genesisTxID,
			ReferenceNo:   refGenesis,
			AccountID:     accMerchant.ID,
			AccountNumber: accMerchant.AccountNumber,
			Direction:     model.DirectionCredit,
			Amount:        250_000_000,
			BalanceBefore: 0,
			BalanceAfter:  250_000_000,
			Narration:     "Initial Opening Deposit - Corporate Checking Merchant",
			CreatedAt:     now,
		},
	}

	r.transactions = append(r.transactions, model.Transaction{
		ID:                  genesisTxID,
		ReferenceNo:         refGenesis,
		IdempotencyKey:      "idem-genesis-001",
		SourceAccountID:     glVault.ID,
		SourceAccountNo:     glVault.AccountNumber,
		Channel:             model.ChannelTellerDeposit,
		Amount:              totalInitial,
		AmountFormatted:     response.FormatIDR(totalInitial),
		FeeAmount:           0,
		TotalDebit:          totalInitial,
		TotalDebitFormatted: response.FormatIDR(totalInitial),
		Currency:            "IDR",
		Description:         "Core Banking Genesis Capital Allocation",
		Status:              model.TxSettled,
		ClientIP:            "127.0.0.1",
		TraceID:             "SYSTEM-GENESIS-0001",
		LedgerPostings:      genesisPostings,
		SettledAt:           now,
		CreatedAt:           now,
	})
	r.ledgerEntries = append(r.ledgerEntries, genesisPostings...)

	// 6. Seed Active Virtual Account Bill (88000081290001001)
	vaExp := now.Add(24 * time.Hour)
	r.virtualAccounts["88000081290001001"] = &model.VirtualAccount{
		ID:                "va000000-0000-0000-0000-000000000001",
		VANumber:          "88000081290001001",
		MerchantName:      merchant.FullName,
		MerchantAccountID: accMerchant.ID,
		MerchantAccountNo: accMerchant.AccountNumber,
		CustomerName:      budi.FullName,
		BillAmount:        1_250_000, // Rp 1.250.000
		BillFormatted:     response.FormatIDR(1_250_000),
		Currency:          "IDR",
		Description:       "Order #INV/20261104/MPL/9918231 - Enterprise Cloud Server Subscription",
		Status:            model.VAOpen,
		ExpiresAt:         vaExp,
		CreatedAt:         now,
	}
}

// HashAPIKey computes SHA-256(raw_key + salt).
func (r *Repository) HashAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey + ":" + r.salt))
	return hex.EncodeToString(sum[:])
}

// GetCustomerByEmail retrieves a customer/staff profile by email.
func (r *Repository) GetCustomerByEmail(ctx context.Context, email string) (*model.Customer, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	r.mu.RLock()
	defer r.mu.RUnlock()

	id, ok := r.customersByMail[email]
	if !ok {
		return nil, ErrNotFound
	}
	c, ok := r.customers[id]
	if !ok || !c.IsActive {
		return nil, ErrNotFound
	}
	cp := *c
	return &cp, nil
}

// GetCustomerByID retrieves a customer/staff profile by ID.
func (r *Repository) GetCustomerByID(ctx context.Context, id string) (*model.Customer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c, ok := r.customers[id]
	if !ok || !c.IsActive {
		return nil, ErrNotFound
	}
	cp := *c
	return &cp, nil
}

// ListAccountsByCustomerID returns all accounts owned by a specific customer ID.
func (r *Repository) ListAccountsByCustomerID(ctx context.Context, customerID string) []model.Account {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var list []model.Account
	for _, acc := range r.accounts {
		if acc.CustomerID == customerID {
			cp := *acc
			cp.AvailableBalance = cp.Balance - cp.HoldBalance
			list = append(list, cp)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].AccountNumber < list[j].AccountNumber
	})
	return list
}

// ListAccounts returns all customer accounts (and optionally internal GL accounts for ADMIN/AUDITOR).
func (r *Repository) ListAccounts(ctx context.Context, includeGL bool) []model.Account {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var list []model.Account
	for _, acc := range r.accounts {
		if !includeGL && acc.AccountType == model.AccountGLInternal {
			continue
		}
		cp := *acc
		cp.AvailableBalance = cp.Balance - cp.HoldBalance
		list = append(list, cp)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].AccountNumber < list[j].AccountNumber
	})
	return list
}

// GetAccountByNumber returns an account along with its calculated today's outgoing usage.
func (r *Repository) GetAccountByNumber(ctx context.Context, accountNo string) (*model.Account, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	acc, ok := r.accounts[accountNo]
	if !ok {
		return nil, 0, ErrNotFound
	}
	cp := *acc
	cp.AvailableBalance = cp.Balance - cp.HoldBalance
	usedToday := r.todayOutgoingTotalLocked(acc.ID)
	return &cp, usedToday, nil
}

func (r *Repository) todayOutgoingTotalLocked(accountID string) int64 {
	now := time.Now().UTC()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	var used int64
	for _, tx := range r.transactions {
		if tx.SourceAccountID == accountID && tx.Status == model.TxSettled && tx.CreatedAt.After(startOfDay) {
			if tx.Channel == model.ChannelIntrabank || tx.Channel == model.ChannelBIFast || tx.Channel == model.ChannelRTGS || tx.Channel == model.ChannelLLG {
				used += tx.Amount
			}
		}
	}
	return used
}

// GetAccountStatement returns the transaction history and double-entry ledger postings for an account.
func (r *Repository) GetAccountStatement(ctx context.Context, accountNo string, limit int) (*model.Account, []model.LedgerPost, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	acc, ok := r.accounts[accountNo]
	if !ok {
		return nil, nil, ErrNotFound
	}
	cp := *acc
	cp.AvailableBalance = cp.Balance - cp.HoldBalance

	var entries []model.LedgerPost
	for _, entry := range r.ledgerEntries {
		if entry.AccountID == acc.ID {
			entries = append(entries, entry)
			if len(entries) >= limit {
				break
			}
		}
	}
	return &cp, entries, nil
}

// UnblockAccountPIN resets PINFailedAttempts to 0, restores status to ACTIVE, and optionally sets a new 6-digit PIN.
func (r *Repository) UnblockAccountPIN(ctx context.Context, accountNo, newRawPIN string) (*model.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	acc, ok := r.accounts[accountNo]
	if !ok {
		return nil, ErrNotFound
	}
	if newRawPIN != "" {
		pinHash, err := bcrypt.GenerateFromPassword([]byte(newRawPIN), bcrypt.MinCost)
		if err != nil {
			return nil, err
		}
		acc.PINHash = string(pinHash)
	}
	acc.PINFailedAttempts = 0
	acc.Status = model.AccountActive
	acc.Version++
	acc.UpdatedAt = time.Now().UTC()

	cp := *acc
	return &cp, nil
}

// FeeForChannel calculates the administrative transfer fee according to SNAP-BI clearing rules.
func FeeForChannel(channel model.TxChannel) int64 {
	switch channel {
	case model.ChannelIntrabank:
		return 0
	case model.ChannelBIFast:
		return 2_500
	case model.ChannelLLG:
		return 2_900
	case model.ChannelRTGS:
		return 30_000
	default:
		return 0
	}
}

func generateReferenceNo(prefix string) string {
	now := time.Now().UTC()
	return fmt.Sprintf("NCB-%s-%s-%06d", prefix, now.Format("20060102"), rand.Intn(900000)+100000)
}

func verifyAndUpdatePINLocked(acc *model.Account, rawPIN string) error {
	if acc.Status == model.AccountPINBlocked {
		return ErrAccountPINBlocked
	}
	if acc.Status != model.AccountActive {
		return ErrAccountNotActive
	}
	if err := bcrypt.CompareHashAndPassword([]byte(acc.PINHash), []byte(rawPIN)); err != nil {
		acc.PINFailedAttempts++
		if acc.PINFailedAttempts >= 3 {
			acc.Status = model.AccountPINBlocked
			return ErrAccountPINBlocked
		}
		return ErrInvalidPIN
	}
	acc.PINFailedAttempts = 0
	return nil
}

// VerifyDoubleEntryInvariant checks that sum(DEBIT) == sum(CREDIT) for a set of postings before committing.
func VerifyDoubleEntryInvariant(postings []model.LedgerPost) error {
	var debitSum, creditSum int64
	for _, p := range postings {
		if p.Direction == model.DirectionDebit {
			debitSum += p.Amount
		} else if p.Direction == model.DirectionCredit {
			creditSum += p.Amount
		}
	}
	if debitSum <= 0 || debitSum != creditSum {
		return ErrUnbalancedJournal
	}
	return nil
}

// ExecuteTransfer performs an ACID Double-Entry Fund Transfer (Intrabank, BI-FAST, RTGS, or LLG).
func (r *Repository) ExecuteTransfer(
	ctx context.Context,
	sourceAccNo, destAccNo string,
	channel model.TxChannel,
	extBankCode, extAccName string,
	amount int64,
	rawPIN, description, idempotencyKey, initiatedBy, clientIP, traceID string,
) (*model.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	srcAcc, ok := r.accounts[sourceAccNo]
	if !ok {
		return nil, fmt.Errorf("source account '%s': %w", sourceAccNo, ErrNotFound)
	}

	// 1. Verify PIN & Account Status (auto-blocks after 3 failures)
	if err := verifyAndUpdatePINLocked(srcAcc, rawPIN); err != nil {
		return nil, err
	}

	// 2. Calculate Fees & Total Debit
	fee := FeeForChannel(channel)
	totalDebit := amount + fee
	available := srcAcc.Balance - srcAcc.HoldBalance
	if available < totalDebit {
		return nil, ErrInsufficientFunds
	}

	// 3. Check Daily Velocity Limit for Card Tier
	usedToday := r.todayOutgoingTotalLocked(srcAcc.ID)
	if usedToday+amount > srcAcc.DailyLimit {
		return nil, ErrDailyLimitExceeded
	}

	// 4. Resolve Destination Account (Customer Account for INTRABANK, or GL Nostro for Interbank Clearing)
	var destAcc *model.Account
	if channel == model.ChannelIntrabank {
		d, exists := r.accounts[destAccNo]
		if !exists {
			return nil, fmt.Errorf("destination account '%s': %w", destAccNo, ErrNotFound)
		}
		if d.Status != model.AccountActive {
			return nil, fmt.Errorf("destination account '%s': %w", destAccNo, ErrAccountNotActive)
		}
		destAcc = d
	} else {
		destAcc = r.accounts[GLNostroClearingNo]
	}

	glFeeAcc := r.accounts[GLFeeIncomeAccountNo]
	now := time.Now().UTC()
	txID := uuid.New().String()
	refNo := generateReferenceNo(string(channel))

	if description == "" {
		description = fmt.Sprintf("%s Transfer to %s", channel, destAccNo)
	}

	// 5. Compute Balances & Construct Balanced Double-Entry Ledger Postings
	srcBefore := srcAcc.Balance
	srcAfter := srcBefore - totalDebit

	destBefore := destAcc.Balance
	destAfter := destBefore + amount

	postings := []model.LedgerPost{
		{
			ID:            uuid.New().String(),
			TransactionID: txID,
			ReferenceNo:   refNo,
			AccountID:     srcAcc.ID,
			AccountNumber: srcAcc.AccountNumber,
			Direction:     model.DirectionDebit,
			Amount:        totalDebit,
			BalanceBefore: srcBefore,
			BalanceAfter:  srcAfter,
			Narration:     fmt.Sprintf("DB %s Transfer to %s (%s)", channel, destAccNo, description),
			CreatedAt:     now,
		},
		{
			ID:            uuid.New().String(),
			TransactionID: txID,
			ReferenceNo:   refNo,
			AccountID:     destAcc.ID,
			AccountNumber: destAcc.AccountNumber,
			Direction:     model.DirectionCredit,
			Amount:        amount,
			BalanceBefore: destBefore,
			BalanceAfter:  destAfter,
			Narration:     fmt.Sprintf("CR %s Transfer from %s (%s)", channel, srcAcc.AccountNumber, description),
			CreatedAt:     now,
		},
	}

	if fee > 0 && glFeeAcc != nil {
		feeBefore := glFeeAcc.Balance
		feeAfter := feeBefore + fee
		postings = append(postings, model.LedgerPost{
			ID:            uuid.New().String(),
			TransactionID: txID,
			ReferenceNo:   refNo,
			AccountID:     glFeeAcc.ID,
			AccountNumber: glFeeAcc.AccountNumber,
			Direction:     model.DirectionCredit,
			Amount:        fee,
			BalanceBefore: feeBefore,
			BalanceAfter:  feeAfter,
			Narration:     fmt.Sprintf("CR %s Transfer Fee Revenue (%s)", channel, refNo),
			CreatedAt:     now,
		})
	}

	// 6. Enforce Double-Entry Invariant (SUM(DEBIT) == SUM(CREDIT))
	if err := VerifyDoubleEntryInvariant(postings); err != nil {
		return nil, err
	}

	// 7. Commit Account Balance Mutations & Optimistic Lock Version Increment
	srcAcc.Balance = srcAfter
	srcAcc.AvailableBalance = srcAcc.Balance - srcAcc.HoldBalance
	srcAcc.Version++
	srcAcc.UpdatedAt = now

	destAcc.Balance = destAfter
	destAcc.AvailableBalance = destAcc.Balance - destAcc.HoldBalance
	destAcc.Version++
	destAcc.UpdatedAt = now

	if fee > 0 && glFeeAcc != nil {
		glFeeAcc.Balance += fee
		glFeeAcc.AvailableBalance = glFeeAcc.Balance - glFeeAcc.HoldBalance
		glFeeAcc.Version++
		glFeeAcc.UpdatedAt = now
	}

	txRecord := model.Transaction{
		ID:                   txID,
		ReferenceNo:          refNo,
		IdempotencyKey:       idempotencyKey,
		SourceAccountID:      srcAcc.ID,
		SourceAccountNo:      srcAcc.AccountNumber,
		DestinationAccountID: destAcc.ID,
		DestinationAccountNo: destAccNo,
		ExternalBankCode:     extBankCode,
		ExternalAccountNo:    destAccNo,
		ExternalAccountName:  extAccName,
		Channel:              channel,
		Amount:               amount,
		AmountFormatted:      response.FormatIDR(amount),
		FeeAmount:            fee,
		TotalDebit:           totalDebit,
		TotalDebitFormatted:  response.FormatIDR(totalDebit),
		Currency:             "IDR",
		Description:          description,
		Status:               model.TxSettled,
		InitiatedBy:          initiatedBy,
		ClientIP:             clientIP,
		TraceID:              traceID,
		LedgerPostings:       postings,
		SettledAt:            now,
		CreatedAt:            now,
	}

	r.transactions = append([]model.Transaction{txRecord}, r.transactions...)
	r.ledgerEntries = append(postings, r.ledgerEntries...)

	r.persistTxAndPostingsToPostgres(ctx, txRecord, postings, []*model.Account{srcAcc, destAcc, glFeeAcc})

	return &txRecord, nil
}

// ExecuteCashTransaction performs a Teller/ATM Cash Deposit or Cash Withdrawal against the Bank's GL Central Vault.
func (r *Repository) ExecuteCashTransaction(
	ctx context.Context,
	accountNo, operation string,
	amount int64,
	rawPIN, description, idempotencyKey, initiatedBy, clientIP, traceID string,
) (*model.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	custAcc, ok := r.accounts[accountNo]
	if !ok {
		return nil, ErrNotFound
	}
	glVault := r.accounts[GLVaultAccountNo]
	now := time.Now().UTC()
	txID := uuid.New().String()

	var channel model.TxChannel
	var postings []model.LedgerPost
	var refNo string

	if operation == "DEPOSIT" {
		if custAcc.Status != model.AccountActive {
			return nil, ErrAccountNotActive
		}
		channel = model.ChannelTellerDeposit
		refNo = generateReferenceNo("DEP")
		if description == "" {
			description = "Setor Tunai Teller / Cash Deposit"
		}

		vaultBefore := glVault.Balance
		vaultAfter := vaultBefore - amount
		custBefore := custAcc.Balance
		custAfter := custBefore + amount

		postings = []model.LedgerPost{
			{
				ID:            uuid.New().String(),
				TransactionID: txID,
				ReferenceNo:   refNo,
				AccountID:     glVault.ID,
				AccountNumber: glVault.AccountNumber,
				Direction:     model.DirectionDebit,
				Amount:        amount,
				BalanceBefore: vaultBefore,
				BalanceAfter:  vaultAfter,
				Narration:     fmt.Sprintf("DB GL Kas Utama - Setor Tunai Rekening %s", custAcc.AccountNumber),
				CreatedAt:     now,
			},
			{
				ID:            uuid.New().String(),
				TransactionID: txID,
				ReferenceNo:   refNo,
				AccountID:     custAcc.ID,
				AccountNumber: custAcc.AccountNumber,
				Direction:     model.DirectionCredit,
				Amount:        amount,
				BalanceBefore: custBefore,
				BalanceAfter:  custAfter,
				Narration:     fmt.Sprintf("CR Setor Tunai (%s)", description),
				CreatedAt:     now,
			},
		}

		if err := VerifyDoubleEntryInvariant(postings); err != nil {
			return nil, err
		}

		glVault.Balance = vaultAfter
		glVault.AvailableBalance = glVault.Balance
		glVault.Version++

		custAcc.Balance = custAfter
		custAcc.AvailableBalance = custAcc.Balance - custAcc.HoldBalance
		custAcc.Version++
		custAcc.UpdatedAt = now
	} else {
		if err := verifyAndUpdatePINLocked(custAcc, rawPIN); err != nil {
			return nil, err
		}
		if custAcc.Balance-custAcc.HoldBalance < amount {
			return nil, ErrInsufficientFunds
		}
		channel = model.ChannelTellerWithdrawal
		refNo = generateReferenceNo("WDR")
		if description == "" {
			description = "Tarik Tunai Teller / ATM Cash Withdrawal"
		}

		custBefore := custAcc.Balance
		custAfter := custBefore - amount
		vaultBefore := glVault.Balance
		vaultAfter := vaultBefore + amount

		postings = []model.LedgerPost{
			{
				ID:            uuid.New().String(),
				TransactionID: txID,
				ReferenceNo:   refNo,
				AccountID:     custAcc.ID,
				AccountNumber: custAcc.AccountNumber,
				Direction:     model.DirectionDebit,
				Amount:        amount,
				BalanceBefore: custBefore,
				BalanceAfter:  custAfter,
				Narration:     fmt.Sprintf("DB Tarik Tunai (%s)", description),
				CreatedAt:     now,
			},
			{
				ID:            uuid.New().String(),
				TransactionID: txID,
				ReferenceNo:   refNo,
				AccountID:     glVault.ID,
				AccountNumber: glVault.AccountNumber,
				Direction:     model.DirectionCredit,
				Amount:        amount,
				BalanceBefore: vaultBefore,
				BalanceAfter:  vaultAfter,
				Narration:     fmt.Sprintf("CR GL Kas Utama - Tarik Tunai Rekening %s", custAcc.AccountNumber),
				CreatedAt:     now,
			},
		}

		if err := VerifyDoubleEntryInvariant(postings); err != nil {
			return nil, err
		}

		custAcc.Balance = custAfter
		custAcc.AvailableBalance = custAcc.Balance - custAcc.HoldBalance
		custAcc.Version++
		custAcc.UpdatedAt = now

		glVault.Balance = vaultAfter
		glVault.AvailableBalance = glVault.Balance
		glVault.Version++
	}

	txRecord := model.Transaction{
		ID:                   txID,
		ReferenceNo:          refNo,
		IdempotencyKey:       idempotencyKey,
		SourceAccountID:      custAcc.ID,
		SourceAccountNo:      custAcc.AccountNumber,
		DestinationAccountID: glVault.ID,
		DestinationAccountNo: glVault.AccountNumber,
		Channel:              channel,
		Amount:               amount,
		AmountFormatted:      response.FormatIDR(amount),
		FeeAmount:            0,
		TotalDebit:           amount,
		TotalDebitFormatted:  response.FormatIDR(amount),
		Currency:             "IDR",
		Description:          description,
		Status:               model.TxSettled,
		InitiatedBy:          initiatedBy,
		ClientIP:             clientIP,
		TraceID:              traceID,
		LedgerPostings:       postings,
		SettledAt:            now,
		CreatedAt:            now,
	}

	r.transactions = append([]model.Transaction{txRecord}, r.transactions...)
	r.ledgerEntries = append(postings, r.ledgerEntries...)
	r.persistTxAndPostingsToPostgres(ctx, txRecord, postings, []*model.Account{custAcc, glVault})
	return &txRecord, nil
}

// CreateVirtualAccount generates a new Virtual Account bill (prefix 88000).
func (r *Repository) CreateVirtualAccount(
	ctx context.Context,
	merchantAccNo, merchantName, custPhoneOrID, custName string,
	billAmount int64,
	description string,
) (*model.VirtualAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	merchAcc, ok := r.accounts[merchantAccNo]
	if !ok {
		return nil, fmt.Errorf("merchant account '%s': %w", merchantAccNo, ErrNotFound)
	}

	cleanDigits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, custPhoneOrID)
	if len(cleanDigits) > 12 {
		cleanDigits = cleanDigits[len(cleanDigits)-12:]
	}
	vaNumber := fmt.Sprintf("88000%s", cleanDigits)
	if _, exists := r.virtualAccounts[vaNumber]; exists {
		vaNumber = fmt.Sprintf("88000%s%03d", cleanDigits, rand.Intn(900)+100)
	}

	now := time.Now().UTC()
	va := &model.VirtualAccount{
		ID:                uuid.New().String(),
		VANumber:          vaNumber,
		MerchantName:      merchantName,
		MerchantAccountID: merchAcc.ID,
		MerchantAccountNo: merchAcc.AccountNumber,
		CustomerName:      custName,
		BillAmount:        billAmount,
		BillFormatted:     response.FormatIDR(billAmount),
		Currency:          "IDR",
		Description:       description,
		Status:            model.VAOpen,
		ExpiresAt:         now.Add(24 * time.Hour),
		CreatedAt:         now,
	}
	r.virtualAccounts[va.VANumber] = va
	cp := *va
	return &cp, nil
}

// GetVirtualAccount looks up a Virtual Account by VA Number.
func (r *Repository) GetVirtualAccount(ctx context.Context, vaNumber string) (*model.VirtualAccount, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	va, ok := r.virtualAccounts[vaNumber]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *va
	return &cp, nil
}

// PayVirtualAccount settles an open Virtual Account bill atomically via Double-Entry bookkeeping.
func (r *Repository) PayVirtualAccount(
	ctx context.Context,
	vaNumber, sourceAccNo, rawPIN, idempotencyKey, initiatedBy, clientIP, traceID string,
) (*model.VirtualAccount, *model.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	va, ok := r.virtualAccounts[vaNumber]
	if !ok {
		return nil, nil, ErrNotFound
	}
	if va.Status != model.VAStatus(model.VAOpen) || time.Now().UTC().After(va.ExpiresAt) {
		return nil, nil, ErrVANotOpen
	}

	srcAcc, ok := r.accounts[sourceAccNo]
	if !ok {
		return nil, nil, fmt.Errorf("source account '%s': %w", sourceAccNo, ErrNotFound)
	}
	if err := verifyAndUpdatePINLocked(srcAcc, rawPIN); err != nil {
		return nil, nil, err
	}

	if srcAcc.Balance-srcAcc.HoldBalance < va.BillAmount {
		return nil, nil, ErrInsufficientFunds
	}

	merchAcc, ok := r.accounts[va.MerchantAccountNo]
	if !ok {
		return nil, nil, fmt.Errorf("merchant settlement account '%s': %w", va.MerchantAccountNo, ErrNotFound)
	}

	now := time.Now().UTC()
	txID := uuid.New().String()
	refNo := generateReferenceNo("VA")

	srcBefore := srcAcc.Balance
	srcAfter := srcBefore - va.BillAmount
	merchBefore := merchAcc.Balance
	merchAfter := merchBefore + va.BillAmount

	postings := []model.LedgerPost{
		{
			ID:            uuid.New().String(),
			TransactionID: txID,
			ReferenceNo:   refNo,
			AccountID:     srcAcc.ID,
			AccountNumber: srcAcc.AccountNumber,
			Direction:     model.DirectionDebit,
			Amount:        va.BillAmount,
			BalanceBefore: srcBefore,
			BalanceAfter:  srcAfter,
			Narration:     fmt.Sprintf("DB Pembayaran Virtual Account %s - %s", va.VANumber, va.MerchantName),
			CreatedAt:     now,
		},
		{
			ID:            uuid.New().String(),
			TransactionID: txID,
			ReferenceNo:   refNo,
			AccountID:     merchAcc.ID,
			AccountNumber: merchAcc.AccountNumber,
			Direction:     model.DirectionCredit,
			Amount:        va.BillAmount,
			BalanceBefore: merchBefore,
			BalanceAfter:  merchAfter,
			Narration:     fmt.Sprintf("CR Settlement Virtual Account %s (%s)", va.VANumber, va.CustomerName),
			CreatedAt:     now,
		},
	}

	if err := VerifyDoubleEntryInvariant(postings); err != nil {
		return nil, nil, err
	}

	srcAcc.Balance = srcAfter
	srcAcc.AvailableBalance = srcAcc.Balance - srcAcc.HoldBalance
	srcAcc.Version++
	srcAcc.UpdatedAt = now

	merchAcc.Balance = merchAfter
	merchAcc.AvailableBalance = merchAcc.Balance - merchAcc.HoldBalance
	merchAcc.Version++
	merchAcc.UpdatedAt = now

	va.Status = model.VAPaid
	va.PaidTransactionID = txID
	va.PaidAt = &now

	txRecord := model.Transaction{
		ID:                   txID,
		ReferenceNo:          refNo,
		IdempotencyKey:       idempotencyKey,
		SourceAccountID:      srcAcc.ID,
		SourceAccountNo:      srcAcc.AccountNumber,
		DestinationAccountID: merchAcc.ID,
		DestinationAccountNo: merchAcc.AccountNumber,
		Channel:              model.ChannelVirtualAccount,
		Amount:               va.BillAmount,
		AmountFormatted:      response.FormatIDR(va.BillAmount),
		FeeAmount:            0,
		TotalDebit:           va.BillAmount,
		TotalDebitFormatted:  response.FormatIDR(va.BillAmount),
		Currency:             "IDR",
		Description:          fmt.Sprintf("VA Payment %s - %s", va.VANumber, va.Description),
		Status:               model.TxSettled,
		InitiatedBy:          initiatedBy,
		ClientIP:             clientIP,
		TraceID:              traceID,
		LedgerPostings:       postings,
		SettledAt:            now,
		CreatedAt:            now,
	}

	r.transactions = append([]model.Transaction{txRecord}, r.transactions...)
	r.ledgerEntries = append(postings, r.ledgerEntries...)
	r.persistTxAndPostingsToPostgres(ctx, txRecord, postings, []*model.Account{srcAcc, merchAcc})

	vaCopy := *va
	return &vaCopy, &txRecord, nil
}

// OnboardCustomer creates a new CIF profile, opens a 10-digit bank account, and funds the initial deposit from GL Vault.
func (r *Repository) OnboardCustomer(
	ctx context.Context,
	nik, fullName, email, phone, password string,
	accType model.AccountType,
	tier model.CardTier,
	rawPIN string,
	initialDeposit int64,
	idempotencyKey, clientIP, traceID string,
) (*model.Customer, *model.Account, *model.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	emailLower := strings.ToLower(strings.TrimSpace(email))
	if _, exists := r.customersByMail[emailLower]; exists {
		return nil, nil, nil, ErrDuplicateEmail
	}

	passHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		return nil, nil, nil, err
	}
	pinHash, err := bcrypt.GenerateFromPassword([]byte(rawPIN), bcrypt.MinCost)
	if err != nil {
		return nil, nil, nil, err
	}

	now := time.Now().UTC()
	cifNo := fmt.Sprintf("CIF-90%06d", len(r.customers)+1001)
	accNo := fmt.Sprintf("5240%06d", len(r.accounts)+305000)

	cust := &model.Customer{
		ID:           uuid.New().String(),
		CIFNumber:    cifNo,
		NIKEncrypted: response.MaskNIK(nik),
		FullName:     fullName,
		Email:        emailLower,
		PhoneNumber:  phone,
		PasswordHash: string(passHash),
		Role:         model.RoleCustomer,
		KYCStatus:    model.KYCVerified,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	acc := &model.Account{
		ID:               uuid.New().String(),
		AccountNumber:    accNo,
		CustomerID:       cust.ID,
		OwnerName:        cust.FullName,
		AccountType:      accType,
		CardTier:         tier,
		Currency:         "IDR",
		Balance:          initialDeposit,
		HoldBalance:      0,
		AvailableBalance: initialDeposit,
		DailyLimit:       model.DailyLimitForTier(tier),
		PINHash:          string(pinHash),
		Status:           model.AccountActive,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	glVault := r.accounts[GLVaultAccountNo]
	vaultBefore := glVault.Balance
	vaultAfter := vaultBefore - initialDeposit
	glVault.Balance = vaultAfter
	glVault.AvailableBalance = vaultAfter
	glVault.Version++

	txID := uuid.New().String()
	refNo := generateReferenceNo("ONB")
	postings := []model.LedgerPost{
		{
			ID:            uuid.New().String(),
			TransactionID: txID,
			ReferenceNo:   refNo,
			AccountID:     glVault.ID,
			AccountNumber: glVault.AccountNumber,
			Direction:     model.DirectionDebit,
			Amount:        initialDeposit,
			BalanceBefore: vaultBefore,
			BalanceAfter:  vaultAfter,
			Narration:     fmt.Sprintf("DB GL Kas Utama - Setoran Awal Pembukaan Rekening %s", acc.AccountNumber),
			CreatedAt:     now,
		},
		{
			ID:            uuid.New().String(),
			TransactionID: txID,
			ReferenceNo:   refNo,
			AccountID:     acc.ID,
			AccountNumber: acc.AccountNumber,
			Direction:     model.DirectionCredit,
			Amount:        initialDeposit,
			BalanceBefore: 0,
			BalanceAfter:  initialDeposit,
			Narration:     fmt.Sprintf("CR Setoran Awal Pembukaan Rekening %s (%s)", acc.AccountType, acc.CardTier),
			CreatedAt:     now,
		},
	}

	if err := VerifyDoubleEntryInvariant(postings); err != nil {
		return nil, nil, nil, err
	}

	txRecord := model.Transaction{
		ID:                   txID,
		ReferenceNo:          refNo,
		IdempotencyKey:       idempotencyKey,
		SourceAccountID:      glVault.ID,
		SourceAccountNo:      glVault.AccountNumber,
		DestinationAccountID: acc.ID,
		DestinationAccountNo: acc.AccountNumber,
		Channel:              model.ChannelTellerDeposit,
		Amount:               initialDeposit,
		AmountFormatted:      response.FormatIDR(initialDeposit),
		FeeAmount:            0,
		TotalDebit:           initialDeposit,
		TotalDebitFormatted:  response.FormatIDR(initialDeposit),
		Currency:             "IDR",
		Description:          "Initial Account Opening Deposit",
		Status:               model.TxSettled,
		InitiatedBy:          cust.ID,
		ClientIP:             clientIP,
		TraceID:              traceID,
		LedgerPostings:       postings,
		SettledAt:            now,
		CreatedAt:            now,
	}

	r.customers[cust.ID] = cust
	r.customersByMail[emailLower] = cust.ID
	r.accounts[acc.AccountNumber] = acc
	r.accountsByID[acc.ID] = acc
	r.transactions = append([]model.Transaction{txRecord}, r.transactions...)
	r.ledgerEntries = append(postings, r.ledgerEntries...)

	r.persistCustomerAndAccountToPostgres(ctx, cust, acc)
	r.persistTxAndPostingsToPostgres(ctx, txRecord, postings, []*model.Account{glVault, acc})

	custCopy := *cust
	accCopy := *acc
	return &custCopy, &accCopy, &txRecord, nil
}

// OpenAdditionalAccount opens a new bank account (e.g. CHECKING, TIME_DEPOSIT, SAVINGS) for an existing Customer.
func (r *Repository) OpenAdditionalAccount(
	ctx context.Context,
	customerID, cifNumber string,
	accType model.AccountType,
	tier model.CardTier,
	rawPIN string,
	initialDeposit int64,
	idempotencyKey, clientIP, traceID string,
) (*model.Account, *model.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var cust *model.Customer
	if cifNumber != "" {
		for _, c := range r.customers {
			if strings.EqualFold(c.CIFNumber, cifNumber) {
				cust = c
				break
			}
		}
	} else if customerID != "" {
		cust = r.customers[customerID]
	}
	if cust == nil {
		return nil, nil, ErrNotFound
	}

	pinHash, err := bcrypt.GenerateFromPassword([]byte(rawPIN), bcrypt.MinCost)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	accNo := fmt.Sprintf("5240%06d", len(r.accounts)+305000)
	acc := &model.Account{
		ID:               uuid.New().String(),
		AccountNumber:    accNo,
		CustomerID:       cust.ID,
		OwnerName:        cust.FullName,
		AccountType:      accType,
		CardTier:         tier,
		Currency:         "IDR",
		Balance:          initialDeposit,
		HoldBalance:      0,
		AvailableBalance: initialDeposit,
		DailyLimit:       model.DailyLimitForTier(tier),
		PINHash:          string(pinHash),
		Status:           model.AccountActive,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	glVault := r.accounts[GLVaultAccountNo]
	vaultBefore := glVault.Balance
	vaultAfter := vaultBefore - initialDeposit
	glVault.Balance = vaultAfter
	glVault.AvailableBalance = vaultAfter
	glVault.Version++

	txID := uuid.New().String()
	refNo := generateReferenceNo("ACC")
	postings := []model.LedgerPost{
		{
			ID:            uuid.New().String(),
			TransactionID: txID,
			ReferenceNo:   refNo,
			AccountID:     glVault.ID,
			AccountNumber: glVault.AccountNumber,
			Direction:     model.DirectionDebit,
			Amount:        initialDeposit,
			BalanceBefore: vaultBefore,
			BalanceAfter:  vaultAfter,
			Narration:     fmt.Sprintf("DB GL Kas Utama - Setoran Awal Rekening Tambahan %s", acc.AccountNumber),
			CreatedAt:     now,
		},
		{
			ID:            uuid.New().String(),
			TransactionID: txID,
			ReferenceNo:   refNo,
			AccountID:     acc.ID,
			AccountNumber: acc.AccountNumber,
			Direction:     model.DirectionCredit,
			Amount:        initialDeposit,
			BalanceBefore: 0,
			BalanceAfter:  initialDeposit,
			Narration:     fmt.Sprintf("CR Setoran Awal Rekening %s (%s)", acc.AccountType, acc.CardTier),
			CreatedAt:     now,
		},
	}

	if err := VerifyDoubleEntryInvariant(postings); err != nil {
		return nil, nil, err
	}

	txRecord := model.Transaction{
		ID:                   txID,
		ReferenceNo:          refNo,
		IdempotencyKey:       idempotencyKey,
		SourceAccountID:      glVault.ID,
		SourceAccountNo:      glVault.AccountNumber,
		DestinationAccountID: acc.ID,
		DestinationAccountNo: acc.AccountNumber,
		Channel:              model.ChannelTellerDeposit,
		Amount:               initialDeposit,
		AmountFormatted:      response.FormatIDR(initialDeposit),
		FeeAmount:            0,
		TotalDebit:           initialDeposit,
		TotalDebitFormatted:  response.FormatIDR(initialDeposit),
		Currency:             "IDR",
		Description:          fmt.Sprintf("Pembukaan Rekening Tambahan %s (%s)", acc.AccountType, cust.CIFNumber),
		Status:               model.TxSettled,
		InitiatedBy:          cust.ID,
		ClientIP:             clientIP,
		TraceID:              traceID,
		LedgerPostings:       postings,
		SettledAt:            now,
		CreatedAt:            now,
	}

	r.accounts[acc.AccountNumber] = acc
	r.accountsByID[acc.ID] = acc
	r.transactions = append([]model.Transaction{txRecord}, r.transactions...)
	r.ledgerEntries = append(postings, r.ledgerEntries...)

	r.persistCustomerAndAccountToPostgres(ctx, nil, acc)
	r.persistTxAndPostingsToPostgres(ctx, txRecord, postings, []*model.Account{glVault, acc})

	accCopy := *acc
	return &accCopy, &txRecord, nil
}

// UpdateAccountStatus changes an account's status (ACTIVE, FROZEN, DORMANT, CLOSED).
func (r *Repository) UpdateAccountStatus(ctx context.Context, accountNo string, newStatus model.AccountStatus) (*model.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	acc, ok := r.accounts[accountNo]
	if !ok {
		return nil, ErrNotFound
	}
	acc.Status = newStatus
	acc.Version++
	acc.UpdatedAt = time.Now().UTC()

	if r.db != nil {
		_, _ = r.db.ExecContext(ctx, `UPDATE accounts SET status = $1, version = $2, updated_at = $3 WHERE account_number = $4`,
			acc.Status, acc.Version, acc.UpdatedAt, acc.AccountNumber)
	}

	cp := *acc
	return &cp, nil
}

// ListTransactions returns transaction history filtered by customer ownership (if customerID != ""), account number, or channel.
func (r *Repository) ListTransactions(ctx context.Context, customerID, accountNoFilter, channelFilter string, limit int) []model.Transaction {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	ownedSet := make(map[string]bool)
	if customerID != "" {
		for _, acc := range r.accounts {
			if acc.CustomerID == customerID {
				ownedSet[acc.AccountNumber] = true
			}
		}
	}

	var result []model.Transaction
	for _, tx := range r.transactions {
		if customerID != "" {
			if !ownedSet[tx.SourceAccountNo] && !ownedSet[tx.DestinationAccountNo] {
				continue
			}
		}
		if accountNoFilter != "" && tx.SourceAccountNo != accountNoFilter && tx.DestinationAccountNo != accountNoFilter {
			continue
		}
		if channelFilter != "" && !strings.EqualFold(string(tx.Channel), channelFilter) {
			continue
		}
		result = append(result, tx)
		if len(result) >= limit {
			break
		}
	}
	return result
}

// GetTransactionByReference looks up a specific transaction receipt and its double-entry postings by ReferenceNo.
func (r *Repository) GetTransactionByReference(ctx context.Context, referenceNo string) (*model.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, tx := range r.transactions {
		if strings.EqualFold(tx.ReferenceNo, referenceNo) {
			cp := tx
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

// GetAdminDashboardMetrics compiles an executive Core Banking Command Center overview for ADMIN and AUDITOR roles.
func (r *Repository) GetAdminDashboardMetrics(ctx context.Context) map[string]interface{} {
	tb := r.GetTrialBalance(ctx)

	r.mu.RLock()
	defer r.mu.RUnlock()

	var totalCustomerDeposits int64
	var activeCustomerAccounts, blockedOrFrozenAccounts int
	var flaggedAccounts []map[string]interface{}

	for _, acc := range r.accounts {
		if acc.AccountType == model.AccountGLInternal {
			continue
		}
		totalCustomerDeposits += acc.Balance
		if acc.Status == model.AccountActive {
			activeCustomerAccounts++
		} else {
			blockedOrFrozenAccounts++
			flaggedAccounts = append(flaggedAccounts, map[string]interface{}{
				"account_number":      acc.AccountNumber,
				"owner_name":          acc.OwnerName,
				"status":              acc.Status,
				"pin_failed_attempts": acc.PINFailedAttempts,
				"balance_formatted":   response.FormatIDR(acc.Balance),
			})
		}
	}

	channelStats := make(map[string]map[string]interface{})
	for _, tx := range r.transactions {
		ch := string(tx.Channel)
		stat, exists := channelStats[ch]
		if !exists {
			stat = map[string]interface{}{
				"tx_count":     0,
				"total_volume": int64(0),
				"total_fee":    int64(0),
			}
			channelStats[ch] = stat
		}
		stat["tx_count"] = stat["tx_count"].(int) + 1
		stat["total_volume"] = stat["total_volume"].(int64) + tx.Amount
		stat["total_fee"] = stat["total_fee"].(int64) + tx.FeeAmount
	}

	var openVAs, paidVAs int
	for _, va := range r.virtualAccounts {
		if va.Status == model.VAOpen {
			openVAs++
		} else if va.Status == model.VAPaid {
			paidVAs++
		}
	}

	glVault := r.accounts[GLVaultAccountNo]
	glNostro := r.accounts[GLNostroClearingNo]
	glFee := r.accounts[GLFeeIncomeAccountNo]

	persistenceMode := "IN_MEMORY_ZERO_CONFIG (Set DATABASE_URL in .env to persist to Supabase PostgreSQL)"
	if r.db != nil {
		if err := r.db.PingContext(ctx); err == nil {
			persistenceMode = "SUPABASE_POSTGRESQL_ACID_CONNECTED"
		}
	}

	return map[string]interface{}{
		"persistence_engine": persistenceMode,
		"Macro_Liquidity_And_DPK": map[string]interface{}{
			"total_customer_deposits_dpk":           totalCustomerDeposits,
			"total_customer_deposits_dpk_formatted": response.FormatIDR(totalCustomerDeposits),
			"gl_central_vault_reserve":              glVault.Balance,
			"gl_central_vault_formatted":            response.FormatIDR(glVault.Balance),
			"gl_nostro_clearing_settlement":         glNostro.Balance,
			"gl_nostro_clearing_formatted":          response.FormatIDR(glNostro.Balance),
			"gl_fee_revenue_income":                 glFee.Balance,
			"gl_fee_revenue_formatted":              response.FormatIDR(glFee.Balance),
		},
		"Customer_And_Account_Health": map[string]interface{}{
			"total_cif_profiles":         len(r.customers),
			"active_customer_accounts":   activeCustomerAccounts,
			"blocked_or_frozen_accounts": blockedOrFrozenAccounts,
			"flagged_accounts_watchlist": flaggedAccounts,
		},
		"Virtual_Account_Billing_Summary": map[string]interface{}{
			"open_bills": openVAs,
			"paid_bills": paidVAs,
		},
		"Clearing_Channel_Breakdown":     channelStats,
		"General_Ledger_Trial_Balance":   tb,
		"Recent_Regulatory_Audit_Events": len(r.auditLogs),
	}
}

// persistCustomerAndAccountToPostgres writes newly created Customer and Account rows to Supabase PostgreSQL when r.db != nil.
func (r *Repository) persistCustomerAndAccountToPostgres(ctx context.Context, cust *model.Customer, acc *model.Account) {
	if r.db == nil {
		return
	}
	if cust != nil {
		_, _ = r.db.ExecContext(ctx, `
			INSERT INTO customers (id, cif_number, nik_encrypted, full_name, email, phone_number, password_hash, role, kyc_status, is_active, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (email) DO NOTHING`,
			cust.ID, cust.CIFNumber, cust.NIKEncrypted, cust.FullName, cust.Email, cust.PhoneNumber,
			cust.PasswordHash, cust.Role, cust.KYCStatus, cust.IsActive, cust.CreatedAt, cust.UpdatedAt,
		)
	}
	if acc != nil {
		_, _ = r.db.ExecContext(ctx, `
			INSERT INTO accounts (id, account_number, customer_id, account_type, card_tier, currency, balance, hold_balance, daily_limit, pin_hash, pin_failed_attempts, status, version, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			ON CONFLICT (account_number) DO UPDATE SET balance = EXCLUDED.balance, status = EXCLUDED.status, version = EXCLUDED.version, updated_at = EXCLUDED.updated_at`,
			acc.ID, acc.AccountNumber, acc.CustomerID, acc.AccountType, acc.CardTier, acc.Currency,
			acc.Balance, acc.HoldBalance, acc.DailyLimit, acc.PINHash, acc.PINFailedAttempts,
			acc.Status, acc.Version, acc.CreatedAt, acc.UpdatedAt,
		)
	}
}

// persistTxAndPostingsToPostgres executes an ACID SQL transaction to persist the transaction header, double-entry postings, and updated account balances in Supabase PostgreSQL.
func (r *Repository) persistTxAndPostingsToPostgres(ctx context.Context, txRecord model.Transaction, postings []model.LedgerPost, updatedAccounts []*model.Account) {
	if r.db == nil {
		return
	}
	sqlTx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return
	}
	defer func() { _ = sqlTx.Rollback() }()

	for _, acc := range updatedAccounts {
		if acc == nil {
			continue
		}
		_, _ = sqlTx.ExecContext(ctx, `
			UPDATE accounts SET balance = $1, hold_balance = $2, pin_failed_attempts = $3, status = $4, version = $5, updated_at = $6
			WHERE id = $7`,
			acc.Balance, acc.HoldBalance, acc.PINFailedAttempts, acc.Status, acc.Version, acc.UpdatedAt, acc.ID,
		)
	}

	_, _ = sqlTx.ExecContext(ctx, `
		INSERT INTO transactions (id, reference_no, idempotency_key, source_account_id, destination_account_id, external_bank_code, external_account_no, external_account_name, channel, amount, fee_amount, total_debit, currency, description, status, client_ip, trace_id, settled_at, created_at)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		txRecord.ID, txRecord.ReferenceNo, txRecord.IdempotencyKey, txRecord.SourceAccountID, txRecord.DestinationAccountID,
		txRecord.ExternalBankCode, txRecord.ExternalAccountNo, txRecord.ExternalAccountName, txRecord.Channel,
		txRecord.Amount, txRecord.FeeAmount, txRecord.TotalDebit, txRecord.Currency, txRecord.Description,
		txRecord.Status, txRecord.ClientIP, txRecord.TraceID, txRecord.SettledAt, txRecord.CreatedAt,
	)

	for _, p := range postings {
		_, _ = sqlTx.ExecContext(ctx, `
			INSERT INTO ledger_entries (id, transaction_id, account_id, direction, amount, balance_before, balance_after, narration, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (id) DO NOTHING`,
			p.ID, p.TransactionID, p.AccountID, p.Direction, p.Amount, p.BalanceBefore, p.BalanceAfter, p.Narration, p.CreatedAt,
		)
	}

	_ = sqlTx.Commit()
}

// GetTrialBalance computes the General Ledger Trial Balance across all ledger postings.
func (r *Repository) GetTrialBalance(ctx context.Context) TrialBalanceSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var totalDebit, totalCredit int64
	for _, entry := range r.ledgerEntries {
		if entry.Direction == model.DirectionDebit {
			totalDebit += entry.Amount
		} else if entry.Direction == model.DirectionCredit {
			totalCredit += entry.Amount
		}
	}

	return TrialBalanceSummary{
		TotalDebitPostings:  totalDebit,
		TotalCreditPostings: totalCredit,
		Discrepancy:         totalDebit - totalCredit,
		IsBalanced:          totalDebit == totalCredit && totalDebit > 0,
		TotalLedgerEntries:  len(r.ledgerEntries),
		TotalTransactions:   len(r.transactions),
		CheckedAt:           time.Now().UTC(),
	}
}

// CreateAPIKey stores a hashed Open Banking partner API key.
func (r *Repository) CreateAPIKey(ctx context.Context, partnerName, rawKey, prefix string, rateLimit int, expiresAt time.Time) (*model.APIKey, error) {
	keyHash := r.HashAPIKey(rawKey)
	ak := model.APIKey{
		ID:              uuid.New().String(),
		PartnerName:     partnerName,
		KeyHash:         keyHash,
		Prefix:          prefix,
		RateLimitPerMin: rateLimit,
		IsActive:        true,
		ExpiresAt:       &expiresAt,
		CreatedAt:       time.Now().UTC(),
	}

	r.mu.Lock()
	r.apiKeys[keyHash] = ak
	r.mu.Unlock()
	return &ak, nil
}

// ValidateAPIKey verifies a raw API key by looking up its salted SHA-256 hash.
func (r *Repository) ValidateAPIKey(ctx context.Context, rawKey string) (*model.APIKey, error) {
	keyHash := r.HashAPIKey(rawKey)
	r.mu.RLock()
	ak, ok := r.apiKeys[keyHash]
	r.mu.RUnlock()
	if !ok || !ak.IsActive {
		return nil, ErrNotFound
	}
	if ak.ExpiresAt != nil && time.Now().After(*ak.ExpiresAt) {
		return nil, ErrNotFound
	}
	return &ak, nil
}

// RecordAuditLog writes an immutable OJK/BI financial audit log entry.
func (r *Repository) RecordAuditLog(ctx context.Context, entry model.AuditLog) {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	entry.CreatedAt = time.Now().UTC()

	r.mu.Lock()
	r.auditLogs = append([]model.AuditLog{entry}, r.auditLogs...)
	if len(r.auditLogs) > 500 {
		r.auditLogs = r.auditLogs[:500]
	}
	r.mu.Unlock()

	if r.db != nil {
		payloadJSON, _ := json.Marshal(entry.PayloadSnapshot)
		_, _ = r.db.ExecContext(ctx, `
			INSERT INTO audit_logs (id, actor_id, action, resource_name, resource_id, ip_address, trace_id, payload_snapshot, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			entry.ID, entry.ActorID, entry.Action, entry.ResourceName, entry.ResourceID, entry.IPAddress, entry.TraceID, payloadJSON, entry.CreatedAt,
		)
	}
}

// ListAuditLogs returns the latest regulatory audit trail entries.
func (r *Repository) ListAuditLogs(ctx context.Context, limit int) ([]model.AuditLog, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.auditLogs) < limit {
		limit = len(r.auditLogs)
	}
	out := make([]model.AuditLog, limit)
	copy(out, r.auditLogs[:limit])
	return out, nil
}
