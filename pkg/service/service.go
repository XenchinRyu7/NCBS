package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cbs-bank/enterprise-portfolio-api/pkg/config"
	"github.com/cbs-bank/enterprise-portfolio-api/pkg/database"
	"github.com/cbs-bank/enterprise-portfolio-api/pkg/dto"
	"github.com/cbs-bank/enterprise-portfolio-api/pkg/model"
	"github.com/cbs-bank/enterprise-portfolio-api/pkg/repository"
	"github.com/cbs-bank/enterprise-portfolio-api/pkg/response"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrTokenRevoked       = errors.New("refresh token has already been rotated or revoked")
)

// Claims represents custom JWT claims for Zero-Trust Core Banking RBAC.
type Claims struct {
	UserID    string         `json:"sub"`
	CIFNumber string         `json:"cif"`
	FullName  string         `json:"name"`
	Email     string         `json:"email"`
	Role      model.UserRole `json:"role"`
	TokenType string         `json:"token_type"` // "access" or "refresh"
	jwt.RegisteredClaims
}

// Service coordinates Core Banking business logic, Double-Entry Ledger operations, Redis locks, and OJK/BI audit trails.
type Service struct {
	cfg   *config.Config
	store *database.Store
	repo  *repository.Repository
}

// New creates a new Core Banking Service instance.
func New(cfg *config.Config, store *database.Store, repo *repository.Repository) *Service {
	return &Service{
		cfg:   cfg,
		store: store,
		repo:  repo,
	}
}

// AuthenticateCustomer verifies customer/staff credentials and issues an Access + Refresh JWT pair along with role permissions & owned accounts.
func (s *Service) AuthenticateCustomer(ctx context.Context, email, password, clientIP, traceID string) (*dto.TokenResponse, error) {
	cust, err := s.repo.GetCustomerByEmail(ctx, email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(cust.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	tokens, err := s.issueTokenPair(ctx, cust)
	if err != nil {
		return nil, err
	}

	s.repo.RecordAuditLog(ctx, model.AuditLog{
		ActorID:      &cust.ID,
		Action:       model.AuditAuthenticate,
		ResourceName: "customers",
		ResourceID:   cust.CIFNumber,
		IPAddress:    clientIP,
		TraceID:      traceID,
		PayloadSnapshot: map[string]interface{}{
			"cif_number":   cust.CIFNumber,
			"email_masked": response.MaskEmail(cust.Email),
			"role":         cust.Role,
		},
	})

	return tokens, nil
}

// GetAuthProfile returns the current authenticated user's profile, role permissions, and owned bank accounts.
func (s *Service) GetAuthProfile(ctx context.Context, actorID string, actorRole model.UserRole) map[string]interface{} {
	cust, err := s.repo.GetCustomerByID(ctx, actorID)
	if err != nil {
		return map[string]interface{}{
			"actor_id":    actorID,
			"role":        actorRole,
			"auth_mode":   "SNAP_BI_SANDBOX_API_KEY",
			"permissions": model.PermissionsForRole(actorRole),
		}
	}

	owned := s.repo.ListAccountsByCustomerID(ctx, cust.ID)
	return map[string]interface{}{
		"customer_id":    cust.ID,
		"cif_number":     cust.CIFNumber,
		"full_name":      cust.FullName,
		"email":          cust.Email,
		"phone_number":   cust.PhoneNumber,
		"role":           cust.Role,
		"kyc_status":     cust.KYCStatus,
		"permissions":    model.PermissionsForRole(cust.Role),
		"owned_accounts": owned,
	}
}

// RotateRefreshToken validates a refresh token, blacklists its JTI in Redis (one-time rotation), and issues a new token pair.
func (s *Service) RotateRefreshToken(ctx context.Context, refreshTokenStr, clientIP, traceID string) (*dto.TokenResponse, error) {
	claims, err := s.VerifyJWT(refreshTokenStr)
	if err != nil || claims.TokenType != "refresh" {
		return nil, ErrInvalidToken
	}

	blacklistKey := "jwt:blacklist:" + claims.ID
	if !s.store.SetNX(ctx, blacklistKey, "revoked", 7*24*time.Hour) {
		return nil, ErrTokenRevoked
	}

	cust := &model.Customer{
		ID:        claims.UserID,
		CIFNumber: claims.CIFNumber,
		FullName:  claims.FullName,
		Email:     claims.Email,
		Role:      claims.Role,
	}

	tokens, err := s.issueTokenPair(ctx, cust)
	if err != nil {
		return nil, err
	}

	s.repo.RecordAuditLog(ctx, model.AuditLog{
		ActorID:      &cust.ID,
		Action:       model.AuditAuthenticate,
		ResourceName: "jwt_refresh_rotation",
		ResourceID:   claims.ID,
		IPAddress:    clientIP,
		TraceID:      traceID,
		PayloadSnapshot: map[string]interface{}{
			"rotated_jti": claims.ID,
			"cif_number":  cust.CIFNumber,
			"role":        cust.Role,
		},
	})

	return tokens, nil
}

func (s *Service) issueTokenPair(ctx context.Context, cust *model.Customer) (*dto.TokenResponse, error) {
	now := time.Now().UTC()
	accessExp := now.Add(15 * time.Minute)
	refreshExp := now.Add(7 * 24 * time.Hour)

	accessClaims := Claims{
		UserID:    cust.ID,
		CIFNumber: cust.CIFNumber,
		FullName:  cust.FullName,
		Email:     cust.Email,
		Role:      cust.Role,
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Issuer:    "cbs-bank-core-ledger",
			Subject:   cust.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(accessExp),
		},
	}

	refreshClaims := Claims{
		UserID:    cust.ID,
		CIFNumber: cust.CIFNumber,
		FullName:  cust.FullName,
		Email:     cust.Email,
		Role:      cust.Role,
		TokenType: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Issuer:    "cbs-bank-core-ledger",
			Subject:   cust.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(refreshExp),
		},
	}

	secret := []byte(s.cfg.JWTSecret)
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(secret)
	if err != nil {
		return nil, err
	}

	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(secret)
	if err != nil {
		return nil, err
	}

	owned := s.repo.ListAccountsByCustomerID(ctx, cust.ID)

	return &dto.TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    900,
		CIFNumber:    cust.CIFNumber,
		FullName:     cust.FullName,
		Email:        cust.Email,
		Role:         string(cust.Role),
		Permissions:  model.PermissionsForRole(cust.Role),
		Accounts:     owned,
	}, nil
}

// VerifyJWT parses and validates a JWT token string.
func (s *Service) VerifyJWT(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(s.cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// OnboardCustomer registers a new CIF and Bank Account with an initial double-entry deposit.
func (s *Service) OnboardCustomer(ctx context.Context, req dto.OnboardCustomerRequest, idempotencyKey, clientIP, traceID string) (*model.Customer, *model.Account, *model.Transaction, error) {
	cust, acc, tx, err := s.repo.OnboardCustomer(
		ctx,
		req.NIK, req.FullName, req.Email, req.PhoneNumber, req.Password,
		model.AccountType(req.AccountType),
		model.CardTier(req.CardTier),
		req.PIN,
		req.InitialDeposit,
		idempotencyKey, clientIP, traceID,
	)
	if err != nil {
		return nil, nil, nil, err
	}

	s.repo.RecordAuditLog(ctx, model.AuditLog{
		ActorID:      &cust.ID,
		Action:       model.AuditCreate,
		ResourceName: "accounts",
		ResourceID:   acc.AccountNumber,
		IPAddress:    clientIP,
		TraceID:      traceID,
		PayloadSnapshot: map[string]interface{}{
			"cif_number":      cust.CIFNumber,
			"account_masked":  response.MaskAccountNumber(acc.AccountNumber),
			"email_masked":    response.MaskEmail(cust.Email),
			"nik_masked":      cust.NIKEncrypted,
			"account_type":    acc.AccountType,
			"card_tier":       acc.CardTier,
			"initial_deposit": response.FormatIDR(req.InitialDeposit),
			"reference_no":    tx.ReferenceNo,
		},
	})

	return cust, acc, tx, nil
}

// ListAccounts returns owned accounts for CUSTOMER/MERCHANT, or all accounts for Bank Staff (TELLER, CS, AUDITOR, ADMIN).
func (s *Service) ListAccounts(ctx context.Context, actorID string, actorRole model.UserRole, includeGL bool) []model.Account {
	if actorRole == model.RoleCustomer || actorRole == model.RoleMerchant {
		return s.repo.ListAccountsByCustomerID(ctx, actorID)
	}
	if actorRole != model.RoleAdmin && actorRole != model.RoleAuditor {
		includeGL = false
	}
	return s.repo.ListAccounts(ctx, includeGL)
}

func isCustomerRestrictedRole(role model.UserRole) bool {
	return role == model.RoleCustomer || role == model.RoleMerchant
}

// GetAccountBalance returns real-time balance, hold amount, and remaining daily velocity limit.
func (s *Service) GetAccountBalance(ctx context.Context, accountNo, actorID string, actorRole model.UserRole) (map[string]interface{}, error) {
	acc, usedToday, err := s.repo.GetAccountByNumber(ctx, accountNo)
	if err != nil {
		return nil, err
	}
	if isCustomerRestrictedRole(actorRole) && acc.CustomerID != actorID {
		return nil, repository.ErrForbiddenAccount
	}

	remainingLimit := acc.DailyLimit - usedToday
	if remainingLimit < 0 {
		remainingLimit = 0
	}

	return map[string]interface{}{
		"account_number":            acc.AccountNumber,
		"owner_name":                acc.OwnerName,
		"account_type":              acc.AccountType,
		"card_tier":                 acc.CardTier,
		"status":                    acc.Status,
		"currency":                  acc.Currency,
		"balance":                   acc.Balance,
		"balance_formatted":         response.FormatIDR(acc.Balance),
		"hold_balance":              acc.HoldBalance,
		"available_balance":         acc.AvailableBalance,
		"available_formatted":       response.FormatIDR(acc.AvailableBalance),
		"daily_transfer_limit":      acc.DailyLimit,
		"daily_used_amount":         usedToday,
		"remaining_daily_limit":     remainingLimit,
		"remaining_limit_formatted": response.FormatIDR(remainingLimit),
		"version":                   acc.Version,
		"updated_at":                acc.UpdatedAt,
	}, nil
}

// GetAccountStatement returns the account mutation (DEBIT/CREDIT ledger postings).
func (s *Service) GetAccountStatement(ctx context.Context, accountNo, actorID string, actorRole model.UserRole, limit int) (*model.Account, []model.LedgerPost, error) {
	acc, entries, err := s.repo.GetAccountStatement(ctx, accountNo, limit)
	if err != nil {
		return nil, nil, err
	}
	if isCustomerRestrictedRole(actorRole) && acc.CustomerID != actorID {
		return nil, nil, repository.ErrForbiddenAccount
	}
	return acc, entries, nil
}

// UnblockAccountPIN allows CUSTOMER_SERVICE or ADMIN to unblock a PIN_BLOCKED account.
func (s *Service) UnblockAccountPIN(ctx context.Context, accountNo string, req dto.UnblockPINRequest, actorID, clientIP, traceID string) (*model.Account, error) {
	acc, err := s.repo.UnblockAccountPIN(ctx, accountNo, req.NewPIN)
	if err != nil {
		return nil, err
	}

	var actorPtr *string
	if actorID != "" {
		actorPtr = &actorID
	}
	s.repo.RecordAuditLog(ctx, model.AuditLog{
		ActorID:      actorPtr,
		Action:       model.AuditPINUnblock,
		ResourceName: "accounts",
		ResourceID:   response.MaskAccountNumber(accountNo),
		IPAddress:    clientIP,
		TraceID:      traceID,
		PayloadSnapshot: map[string]interface{}{
			"account_masked": response.MaskAccountNumber(accountNo),
			"pin_reset":      req.NewPIN != "",
			"reason":         req.Reason,
			"new_status":     acc.Status,
		},
	})
	return acc, nil
}

// ExecuteTransfer performs an ACID Double-Entry Fund Transfer (Intrabank, BI-FAST, RTGS, LLG).
func (s *Service) ExecuteTransfer(ctx context.Context, req dto.TransferRequest, idempotencyKey, actorID string, actorRole model.UserRole, clientIP, traceID string) (*model.Transaction, error) {
	if isCustomerRestrictedRole(actorRole) {
		srcAcc, _, err := s.repo.GetAccountByNumber(ctx, req.SourceAccountNumber)
		if err != nil {
			return nil, err
		}
		if srcAcc.CustomerID != actorID {
			return nil, repository.ErrForbiddenAccount
		}
	}

	tx, err := s.repo.ExecuteTransfer(
		ctx,
		req.SourceAccountNumber,
		req.DestinationAccountNumber,
		model.TxChannel(req.Channel),
		req.ExternalBankCode,
		req.ExternalAccountName,
		req.Amount,
		req.PIN,
		req.Description,
		idempotencyKey,
		actorID,
		clientIP,
		traceID,
	)
	if err != nil {
		if errors.Is(err, repository.ErrInvalidPIN) || errors.Is(err, repository.ErrAccountPINBlocked) {
			s.repo.RecordAuditLog(ctx, model.AuditLog{
				Action:       model.AuditPINFailure,
				ResourceName: "accounts",
				ResourceID:   response.MaskAccountNumber(req.SourceAccountNumber),
				IPAddress:    clientIP,
				TraceID:      traceID,
				PayloadSnapshot: map[string]interface{}{
					"source_account_masked": response.MaskAccountNumber(req.SourceAccountNumber),
					"reason":                err.Error(),
				},
			})
		}
		return nil, err
	}

	var actorPtr *string
	if actorID != "" {
		actorPtr = &actorID
	}
	s.repo.RecordAuditLog(ctx, model.AuditLog{
		ActorID:      actorPtr,
		Action:       model.AuditTransfer,
		ResourceName: "transactions",
		ResourceID:   tx.ReferenceNo,
		IPAddress:    clientIP,
		TraceID:      traceID,
		PayloadSnapshot: map[string]interface{}{
			"reference_no":               tx.ReferenceNo,
			"channel":                    tx.Channel,
			"source_account_masked":      response.MaskAccountNumber(req.SourceAccountNumber),
			"destination_account_masked": response.MaskAccountNumber(req.DestinationAccountNumber),
			"amount":                     tx.Amount,
			"fee_amount":                 tx.FeeAmount,
			"total_debit":                tx.TotalDebit,
		},
	})

	return tx, nil
}

// ExecuteCashTransaction performs a Teller Deposit or Withdrawal.
func (s *Service) ExecuteCashTransaction(ctx context.Context, req dto.CashTransactionRequest, idempotencyKey, actorID, clientIP, traceID string) (*model.Transaction, error) {
	tx, err := s.repo.ExecuteCashTransaction(
		ctx,
		req.AccountNumber,
		req.Operation,
		req.Amount,
		req.PIN,
		req.Description,
		idempotencyKey,
		actorID,
		clientIP,
		traceID,
	)
	if err != nil {
		return nil, err
	}

	var actorPtr *string
	if actorID != "" {
		actorPtr = &actorID
	}
	s.repo.RecordAuditLog(ctx, model.AuditLog{
		ActorID:      actorPtr,
		Action:       model.AuditTransfer,
		ResourceName: "cash_transactions",
		ResourceID:   tx.ReferenceNo,
		IPAddress:    clientIP,
		TraceID:      traceID,
		PayloadSnapshot: map[string]interface{}{
			"reference_no":   tx.ReferenceNo,
			"operation":      req.Operation,
			"account_masked": response.MaskAccountNumber(req.AccountNumber),
			"amount":         tx.AmountFormatted,
		},
	})

	return tx, nil
}

// CreateVirtualAccount generates a Virtual Account billing record.
func (s *Service) CreateVirtualAccount(ctx context.Context, req dto.CreateVirtualAccountRequest, clientIP, traceID string) (*model.VirtualAccount, error) {
	va, err := s.repo.CreateVirtualAccount(
		ctx,
		req.MerchantAccountNumber,
		req.MerchantName,
		req.CustomerPhoneOrID,
		req.CustomerName,
		req.BillAmount,
		req.Description,
	)
	if err != nil {
		return nil, err
	}

	s.repo.RecordAuditLog(ctx, model.AuditLog{
		Action:       model.AuditCreate,
		ResourceName: "virtual_accounts",
		ResourceID:   va.VANumber,
		IPAddress:    clientIP,
		TraceID:      traceID,
		PayloadSnapshot: map[string]interface{}{
			"va_number":     va.VANumber,
			"merchant_name": va.MerchantName,
			"bill_amount":   va.BillFormatted,
		},
	})
	return va, nil
}

// GetVirtualAccount retrieves a Virtual Account bill inquiry.
func (s *Service) GetVirtualAccount(ctx context.Context, vaNumber string) (*model.VirtualAccount, error) {
	return s.repo.GetVirtualAccount(ctx, vaNumber)
}

// PayVirtualAccount settles an open Virtual Account bill.
func (s *Service) PayVirtualAccount(ctx context.Context, vaNumber string, req dto.PayVirtualAccountRequest, idempotencyKey, actorID string, actorRole model.UserRole, clientIP, traceID string) (*model.VirtualAccount, *model.Transaction, error) {
	if isCustomerRestrictedRole(actorRole) {
		srcAcc, _, err := s.repo.GetAccountByNumber(ctx, req.SourceAccountNumber)
		if err != nil {
			return nil, nil, err
		}
		if srcAcc.CustomerID != actorID {
			return nil, nil, repository.ErrForbiddenAccount
		}
	}

	va, tx, err := s.repo.PayVirtualAccount(ctx, vaNumber, req.SourceAccountNumber, req.PIN, idempotencyKey, actorID, clientIP, traceID)
	if err != nil {
		return nil, nil, err
	}

	var actorPtr *string
	if actorID != "" {
		actorPtr = &actorID
	}
	s.repo.RecordAuditLog(ctx, model.AuditLog{
		ActorID:      actorPtr,
		Action:       model.AuditVAPayment,
		ResourceName: "virtual_accounts",
		ResourceID:   va.VANumber,
		IPAddress:    clientIP,
		TraceID:      traceID,
		PayloadSnapshot: map[string]interface{}{
			"va_number":             va.VANumber,
			"reference_no":          tx.ReferenceNo,
			"source_account_masked": response.MaskAccountNumber(req.SourceAccountNumber),
			"bill_amount":           va.BillFormatted,
		},
	})

	return va, tx, nil
}

// GetTrialBalance returns the General Ledger Trial Balance verification summary.
func (s *Service) GetTrialBalance(ctx context.Context) repository.TrialBalanceSummary {
	return s.repo.GetTrialBalance(ctx)
}

// GenerateSandboxKey issues a new salted SHA-256 SNAP-BI Partner API key.
func (s *Service) GenerateSandboxKey(ctx context.Context, partnerName, clientIP, traceID string) (string, *model.APIKey, error) {
	if partnerName == "" {
		partnerName = "SNAP-BI Open Banking Partner"
	}
	prefix := "cbs_live_"
	rawKey := fmt.Sprintf("%s%s", prefix, uuid.New().String())
	expiresAt := time.Now().UTC().Add(30 * 24 * time.Hour)

	ak, err := s.repo.CreateAPIKey(ctx, partnerName, rawKey, prefix, s.cfg.DefaultRateLim, expiresAt)
	if err != nil {
		return "", nil, err
	}

	s.repo.RecordAuditLog(ctx, model.AuditLog{
		Action:       model.AuditCreate,
		ResourceName: "api_keys",
		ResourceID:   ak.ID,
		IPAddress:    clientIP,
		TraceID:      traceID,
		PayloadSnapshot: map[string]interface{}{
			"partner_name": partnerName,
			"prefix":       prefix,
			"rate_limit":   ak.RateLimitPerMin,
		},
	})

	return rawKey, ak, nil
}

// ListAuditLogs returns the regulatory audit trail.
func (s *Service) ListAuditLogs(ctx context.Context, limit int) ([]model.AuditLog, error) {
	return s.repo.ListAuditLogs(ctx, limit)
}

// OpenAdditionalAccount opens a new bank account for an existing Customer CIF.
func (s *Service) OpenAdditionalAccount(ctx context.Context, req dto.OpenAdditionalAccountRequest, idempotencyKey, actorID string, actorRole model.UserRole, clientIP, traceID string) (*model.Account, *model.Transaction, error) {
	targetCustID := actorID
	targetCIF := req.CIFNumber
	if isCustomerRestrictedRole(actorRole) {
		// Customers can only open accounts for their own CIF
		targetCIF = ""
	}

	acc, tx, err := s.repo.OpenAdditionalAccount(
		ctx,
		targetCustID, targetCIF,
		model.AccountType(req.AccountType),
		model.CardTier(req.CardTier),
		req.PIN,
		req.InitialDeposit,
		idempotencyKey, clientIP, traceID,
	)
	if err != nil {
		return nil, nil, err
	}

	var actorPtr *string
	if actorID != "" {
		actorPtr = &actorID
	}
	s.repo.RecordAuditLog(ctx, model.AuditLog{
		ActorID:      actorPtr,
		Action:       model.AuditCreate,
		ResourceName: "accounts",
		ResourceID:   acc.AccountNumber,
		IPAddress:    clientIP,
		TraceID:      traceID,
		PayloadSnapshot: map[string]interface{}{
			"account_masked":  response.MaskAccountNumber(acc.AccountNumber),
			"account_type":    acc.AccountType,
			"card_tier":       acc.CardTier,
			"initial_deposit": response.FormatIDR(req.InitialDeposit),
			"reference_no":    tx.ReferenceNo,
		},
	})

	return acc, tx, nil
}

// UpdateAccountStatus changes an account status (ACTIVE, FROZEN, DORMANT, CLOSED) for ADMIN and CUSTOMER_SERVICE.
func (s *Service) UpdateAccountStatus(ctx context.Context, accountNo string, req dto.UpdateAccountStatusRequest, actorID, clientIP, traceID string) (*model.Account, error) {
	acc, err := s.repo.UpdateAccountStatus(ctx, accountNo, model.AccountStatus(req.Status))
	if err != nil {
		return nil, err
	}

	var actorPtr *string
	if actorID != "" {
		actorPtr = &actorID
	}
	s.repo.RecordAuditLog(ctx, model.AuditLog{
		ActorID:      actorPtr,
		Action:       model.AuditUpdate,
		ResourceName: "accounts",
		ResourceID:   response.MaskAccountNumber(accountNo),
		IPAddress:    clientIP,
		TraceID:      traceID,
		PayloadSnapshot: map[string]interface{}{
			"account_masked": response.MaskAccountNumber(accountNo),
			"new_status":     acc.Status,
			"reason":         req.Reason,
		},
	})
	return acc, nil
}

// ListTransactions returns transaction history (restricted to owned accounts for CUSTOMER/MERCHANT, or full bank history for Staff).
func (s *Service) ListTransactions(ctx context.Context, actorID string, actorRole model.UserRole, accountFilter, channelFilter string) []model.Transaction {
	custIDFilter := ""
	if isCustomerRestrictedRole(actorRole) {
		custIDFilter = actorID
	}
	return s.repo.ListTransactions(ctx, custIDFilter, accountFilter, channelFilter, 50)
}

// GetTransactionByReference retrieves a single transaction receipt and its double-entry postings.
func (s *Service) GetTransactionByReference(ctx context.Context, referenceNo, actorID string, actorRole model.UserRole) (*model.Transaction, error) {
	tx, err := s.repo.GetTransactionByReference(ctx, referenceNo)
	if err != nil {
		return nil, err
	}
	if isCustomerRestrictedRole(actorRole) {
		owned := s.repo.ListAccountsByCustomerID(ctx, actorID)
		allowed := false
		for _, a := range owned {
			if a.AccountNumber == tx.SourceAccountNo || a.AccountNumber == tx.DestinationAccountNo {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, repository.ErrForbiddenAccount
		}
	}
	return tx, nil
}

// GetAdminDashboardMetrics returns the executive Core Banking Command Center metrics for ADMIN and AUDITOR.
func (s *Service) GetAdminDashboardMetrics(ctx context.Context) map[string]interface{} {
	return s.repo.GetAdminDashboardMetrics(ctx)
}

// ResetSandboxState resets all Trial/Sandbox demo accounts, balances, VAs, and General Ledger entries back to genesis state.
func (s *Service) ResetSandboxState(ctx context.Context, clientIP, traceID string) {
	s.repo.ResetSandboxState()
	s.repo.RecordAuditLog(ctx, model.AuditLog{
		Action:       model.AuditUpdate,
		ResourceName: "trial_sandbox_environment",
		ResourceID:   "GENESIS_RESET",
		IPAddress:    clientIP,
		TraceID:      traceID,
		PayloadSnapshot: map[string]interface{}{
			"event": "Sandbox balances and Virtual Accounts restored to clean initial state",
		},
	})
}
