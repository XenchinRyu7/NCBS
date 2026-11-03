package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/cbs-bank/enterprise-portfolio-api/internal/config"
	"github.com/cbs-bank/enterprise-portfolio-api/internal/database"
	"github.com/cbs-bank/enterprise-portfolio-api/internal/dto"
	appmw "github.com/cbs-bank/enterprise-portfolio-api/internal/middleware"
	"github.com/cbs-bank/enterprise-portfolio-api/internal/model"
	"github.com/cbs-bank/enterprise-portfolio-api/internal/repository"
	"github.com/cbs-bank/enterprise-portfolio-api/internal/service"
	"github.com/cbs-bank/enterprise-portfolio-api/pkg/problem"
	"github.com/cbs-bank/enterprise-portfolio-api/pkg/response"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

var (
	cfg    *config.Config
	store  *database.Store
	repo   *repository.Repository
	svc    *service.Service
	router *chi.Mux
)

func init() {
	router = NewRouter()
}

// NewRouter initializes all Core Banking dependencies, security middlewares, and SNAP-BI / Enterprise CBS routes.
func NewRouter() *chi.Mux {
	cfg = config.Load()
	store = database.New(cfg)
	repo = repository.New(store.DB, cfg.APIKeySalt)
	svc = service.New(cfg, store, repo)

	r := chi.NewRouter()

	// Enterprise Global Middlewares
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(appmw.StructuredLogger)
	r.Use(chimw.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Idempotency-Key", "X-API-Key", "X-Request-ID"},
		ExposedHeaders:   []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-Request-ID", "X-Cache-Hit"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(appmw.SlidingWindowRateLimit(store, repo, cfg.DefaultRateLim))

	// Root & Interactive OpenAPI Playground Routes
	r.Get("/", RootWelcomeHandler)
	r.Get("/docs", ScalarDocsHandler)
	r.Get("/openapi.yaml", OpenAPISpecHandler)

	// Core Banking System v1 Routes
	r.Route("/v1", func(v1 chi.Router) {
		// 1. System & Ledger Integrity Health Check
		v1.Get("/system/health", HealthCheckHandler)

		// 2. Zero-Trust Authentication, Role Directory & SNAP-BI Partner Keys
		v1.Post("/auth/token", IssueTokenHandler)
		v1.Post("/auth/refresh", RotateRefreshTokenHandler)
		v1.Post("/auth/sandbox-key", GenerateSandboxKeyHandler)
		v1.Get("/auth/roles", ListRolesMatrixHandler)

		// 3. CIF & Customer Onboarding (Idempotent)
		v1.With(appmw.Idempotency(store)).Post("/customers/onboard", OnboardCustomerHandler)

		// 4. Authenticated Core Banking Operations (All Valid Roles)
		v1.Group(func(auth chi.Router) {
			auth.Use(appmw.RequireRoles(
				svc, repo,
				model.RoleCustomer, model.RoleMerchant, model.RoleTeller,
				model.RoleCustomerService, model.RoleAuditor, model.RoleAdmin,
			))

			// Current Identity, Permissions & Owned Accounts
			auth.Get("/auth/me", GetAuthMeHandler)

			// Accounts, Balance Inquiry & Mutation Statement (Customers see own; Staff see all)
			auth.Get("/accounts", ListAccountsHandler)
			auth.With(appmw.Idempotency(store)).Post("/accounts", OpenAdditionalAccountHandler)
			auth.Get("/accounts/{accountNumber}/balance", GetAccountBalanceHandler)
			auth.Get("/accounts/{accountNumber}/statement", GetAccountStatementHandler)

			// Transaction History & Receipt Lookup (Customers see own; Staff see all)
			auth.Get("/transactions", ListTransactionsHandler)
			auth.Get("/transactions/{referenceNo}", GetTransactionByReferenceHandler)

			// Virtual Account Inquiry (Public to all authenticated users)
			auth.Get("/virtual-accounts/{vaNumber}", GetVirtualAccountHandler)
		})

		// 5. Customer & Merchant Transactional Operations (Transfer & VA Payment)
		v1.Group(func(txGroup chi.Router) {
			txGroup.Use(appmw.RequireRoles(svc, repo, model.RoleCustomer, model.RoleMerchant, model.RoleAdmin))
			txGroup.With(appmw.Idempotency(store)).Post("/transactions/transfer", ExecuteTransferHandler)
			txGroup.With(appmw.Idempotency(store)).Post("/virtual-accounts/{vaNumber}/pay", PayVirtualAccountHandler)
		})

		// 6. Corporate Merchant Virtual Account Billing Creation (MERCHANT or ADMIN)
		v1.With(appmw.RequireRoles(svc, repo, model.RoleMerchant, model.RoleAdmin)).
			Post("/virtual-accounts", CreateVirtualAccountHandler)

		// 7. Teller Branch Cash Operations — Deposit & Withdrawal (TELLER or ADMIN)
		v1.With(
			appmw.RequireRoles(svc, repo, model.RoleTeller, model.RoleAdmin),
			appmw.Idempotency(store),
		).Post("/transactions/cash", ExecuteCashTransactionHandler)

		// 8. Customer Service & Admin Account Lifecycle — Unblock PIN & Freeze/Activate Account
		v1.Group(func(cs chi.Router) {
			cs.Use(appmw.RequireRoles(svc, repo, model.RoleCustomerService, model.RoleAdmin))
			cs.Post("/accounts/{accountNumber}/unblock-pin", UnblockAccountPINHandler)
			cs.Patch("/accounts/{accountNumber}/status", UpdateAccountStatusHandler)
		})

		// 9. Executive Admin Command Center, General Ledger Trial Balance & OJK Audit Trail (AUDITOR or ADMIN)
		v1.Group(func(audit chi.Router) {
			audit.Use(appmw.RequireRoles(svc, repo, model.RoleAuditor, model.RoleAdmin))
			audit.Get("/admin/dashboard", GetAdminDashboardHandler)
			audit.Get("/ledger/trial-balance", GetTrialBalanceHandler)
			audit.Get("/audit-logs", ListAuditLogsHandler)
		})
	})

	// RFC 7807 Not Found & Method Not Allowed Handlers
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		problem.Render(w, req, http.StatusNotFound, "Resource Not Found",
			fmt.Sprintf("The requested banking endpoint '%s' does not exist.", req.URL.Path))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		problem.Render(w, req, http.StatusMethodNotAllowed, "Method Not Allowed",
			fmt.Sprintf("HTTP method '%s' is not supported for '%s'.", req.Method, req.URL.Path))
	})

	return r
}

// Handler is the Vercel Serverless Function entrypoint.
func Handler(w http.ResponseWriter, r *http.Request) {
	router.ServeHTTP(w, r)
}

// RootWelcomeHandler provides Core Banking metadata, pre-seeded accounts, and playground links.
func RootWelcomeHandler(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, r, http.StatusOK, "Nusantara Core Banking System (CBS) — Operational", map[string]interface{}{
		"system":              "Nusantara Core Banking Ledger & SNAP-BI Open Banking API",
		"version":             "2.1.0-enterprise",
		"interactive_docs":    "/docs",
		"roles_guide_url":     "/v1/auth/roles",
		"openapi_spec":        "/openapi.yaml",
		"health_check":        "/v1/system/health",
		"sandbox_api_key":     repository.DemoSandboxRawKey,
		"demo_password_all":   "Password#2026",
		"demo_accounts": []map[string]interface{}{
			{
				"account_number": "5240301001",
				"owner":          "Budi Santoso (SAVINGS - PRIORITY)",
				"login_email":    "budi.santoso@cbs-bank.co.id",
				"role":           "CUSTOMER",
				"pin":            "123456",
				"balance":        "Rp 500.000.000",
			},
			{
				"account_number": "5240302002",
				"owner":          "Siti Rahmawati (SAVINGS - PLATINUM)",
				"login_email":    "siti.rahma@cbs-bank.co.id",
				"role":           "CUSTOMER",
				"pin":            "654321",
				"balance":        "Rp 75.000.000",
			},
			{
				"account_number": "5240308888",
				"owner":          "PT Toko Nusantara Digital Tbk (CHECKING - PRIORITY)",
				"login_email":    "merchant@tokonusantara.co.id",
				"role":           "MERCHANT",
				"pin":            "888888",
				"balance":        "Rp 250.000.000",
			},
		},
		"demo_virtual_account": "88000081290001001",
	}, nil)
}

// ListRolesMatrixHandler returns the complete RBAC role directory, demo credentials, and permissions.
func ListRolesMatrixHandler(w http.ResponseWriter, r *http.Request) {
	roles := []map[string]interface{}{
		{
			"role":            model.RoleCustomer,
			"title":           "Nasabah Perorangan (Retail Customer)",
			"demo_email":      "budi.santoso@cbs-bank.co.id",
			"demo_password":   "Password#2026",
			"demo_account_no": "5240301001",
			"demo_pin":        "123456",
			"description":     "Hanya bisa melihat saldo/mutasi rekening miliknya sendiri, transfer dana (Intrabank, BI-FAST, RTGS, LLG) dari rekening sendiri, dan membayar Virtual Account.",
			"permissions":     model.PermissionsForRole(model.RoleCustomer),
		},
		{
			"role":            model.RoleMerchant,
			"title":           "Nasabah Korporasi / Biller (Corporate Merchant)",
			"demo_email":      "merchant@tokonusantara.co.id",
			"demo_password":   "Password#2026",
			"demo_account_no": "5240308888",
			"demo_pin":        "888888",
			"description":     "Bisa mengelola rekening Giro korporasi miliknya dan men-generate tagihan Virtual Account (POST /v1/virtual-accounts) untuk pelanggan.",
			"permissions":     model.PermissionsForRole(model.RoleMerchant),
		},
		{
			"role":          model.RoleTeller,
			"title":         "Petugas Teller Cabang (Branch Teller)",
			"demo_email":    "teller@cbs-bank.co.id",
			"demo_password": "Password#2026",
			"description":   "Berwenang memproses Setor Tunai (DEPOSIT) & Tarik Tunai (WITHDRAWAL) nasabah terhadap GL Kas Utama Bank (POST /v1/transactions/cash), serta cek rekening nasabah.",
			"permissions":   model.PermissionsForRole(model.RoleTeller),
		},
		{
			"role":          model.RoleCustomerService,
			"title":         "Petugas Customer Service (Branch CS)",
			"demo_email":    "cs@cbs-bank.co.id",
			"demo_password": "Password#2026",
			"description":   "Berwenang membuka CIF/rekening baru, mengecek mutasi nasabah, serta membuka blokir PIN nasabah yang terblokir karena 3x salah PIN (POST /v1/accounts/{accountNumber}/unblock-pin).",
			"permissions":   model.PermissionsForRole(model.RoleCustomerService),
		},
		{
			"role":          model.RoleAuditor,
			"title":         "Auditor Internal & Pengawas OJK/BI (Compliance Auditor)",
			"demo_email":    "auditor@cbs-bank.co.id",
			"demo_password": "Password#2026",
			"description":   "Akses Read-Only penuh untuk menginspeksi Neraca Saldo Buku Besar (GET /v1/ledger/trial-balance), Rekening GL Internal (?include_gl=true), dan Audit Log (GET /v1/audit-logs). Dilarang melakukan mutasi dana.",
			"permissions":   model.PermissionsForRole(model.RoleAuditor),
		},
		{
			"role":          model.RoleAdmin,
			"title":         "Core Banking System Administrator",
			"demo_email":    "admin@cbs-bank.co.id",
			"demo_password": "Password#2026",
			"sandbox_key":   repository.DemoSandboxRawKey,
			"description":   "Superuser operasional & manajemen sistem (juga aktif saat menggunakan header X-API-Key di API Playground).",
			"permissions":   model.PermissionsForRole(model.RoleAdmin),
		},
	}

	response.JSON(w, r, http.StatusOK, "Core Banking RBAC Roles, Credentials & Permission Matrix", roles, map[string]interface{}{
		"how_to_login": "Kirim POST /v1/auth/token dengan {\"email\": \"...\", \"password\": \"Password#2026\"}, lalu gunakan access_token pada header 'Authorization: Bearer <access_token>'.",
	})
}

// GetAuthMeHandler returns the currently logged-in user's profile, role permissions, and owned accounts.
func GetAuthMeHandler(w http.ResponseWriter, r *http.Request) {
	actorID, _ := r.Context().Value(appmw.CtxActorIDKey).(string)
	actorRole, _ := r.Context().Value(appmw.CtxRoleKey).(model.UserRole)

	profile := svc.GetAuthProfile(r.Context(), actorID, actorRole)
	response.JSON(w, r, http.StatusOK, "Authenticated session profile & permissions", profile, nil)
}

// HealthCheckHandler checks DB, Redis, and verifies the Double-Entry General Ledger Trial Balance.
func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	dbStatus := store.PingDB(ctx)
	redisStatus := store.PingRedis(ctx)
	tb := svc.GetTrialBalance(ctx)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "HEALTHY",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"services": map[string]string{
			"supabase_postgres": dbStatus,
			"upstash_redis":     redisStatus,
		},
		"general_ledger_integrity": map[string]interface{}{
			"is_balanced":           tb.IsBalanced,
			"total_debit_postings":  response.FormatIDR(tb.TotalDebitPostings),
			"total_credit_postings": response.FormatIDR(tb.TotalCreditPostings),
			"discrepancy":           tb.Discrepancy,
			"total_transactions":    tb.TotalTransactions,
		},
	})
}

// IssueTokenHandler authenticates a customer/teller/cs/auditor/admin and sets an HttpOnly Refresh Cookie.
func IssueTokenHandler(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Malformed JSON", "Request body must be valid JSON.")
		return
	}

	traceID := chimw.GetReqID(r.Context())
	tokens, err := svc.AuthenticateCustomer(r.Context(), req.Email, req.Password, r.RemoteAddr, traceID)
	if err != nil {
		problem.Render(w, r, http.StatusUnauthorized, "Authentication Failed", "Invalid email or password.")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    tokens.RefreshToken,
		Path:     "/v1/auth/refresh",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   7 * 24 * 3600,
	})

	response.JSON(w, r, http.StatusOK, "Authentication successful", tokens, nil)
}

// RotateRefreshTokenHandler enforces one-time refresh token rotation via Redis JTI blacklist.
func RotateRefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if body.RefreshToken == "" {
		if cookie, err := r.Cookie("refresh_token"); err == nil {
			body.RefreshToken = cookie.Value
		}
	}
	if body.RefreshToken == "" {
		problem.Render(w, r, http.StatusBadRequest, "Missing Refresh Token", "Provide 'refresh_token' in JSON body or HttpOnly cookie.")
		return
	}

	traceID := chimw.GetReqID(r.Context())
	tokens, err := svc.RotateRefreshToken(r.Context(), body.RefreshToken, r.RemoteAddr, traceID)
	if err != nil {
		problem.Render(w, r, http.StatusUnauthorized, "Token Rotation Failed", err.Error())
		return
	}

	response.JSON(w, r, http.StatusOK, "Refresh token rotated successfully", tokens, nil)
}

// GenerateSandboxKeyHandler creates a new salted SHA-256 SNAP-BI Partner API key.
func GenerateSandboxKeyHandler(w http.ResponseWriter, r *http.Request) {
	var req dto.GenerateSandboxKeyRequest
	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	traceID := chimw.GetReqID(r.Context())
	rawKey, ak, err := svc.GenerateSandboxKey(r.Context(), req.PartnerName, r.RemoteAddr, traceID)
	if err != nil {
		problem.Render(w, r, http.StatusInternalServerError, "Key Generation Error", "Failed to generate sandbox API key.")
		return
	}

	response.JSON(w, r, http.StatusCreated, "SNAP-BI Sandbox API Key generated", map[string]interface{}{
		"api_key":      rawKey,
		"key_id":       ak.ID,
		"partner_name": ak.PartnerName,
		"rate_limit":   fmt.Sprintf("%d req/min", ak.RateLimitPerMin),
		"notice":       "Store this raw key securely. Only its salted SHA-256 hash is persisted in the database.",
		"expires_in":   "30 Days",
	}, nil)
}

// OnboardCustomerHandler opens a new CIF, a 10-digit bank account, and posts the initial deposit double-entry journal.
func OnboardCustomerHandler(w http.ResponseWriter, r *http.Request) {
	var req dto.OnboardCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Malformed JSON", "Request body must be valid JSON.")
		return
	}
	if err := req.Validate(); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Validation Error", err.Error())
		return
	}

	idemKey := r.Header.Get("X-Idempotency-Key")
	traceID := chimw.GetReqID(r.Context())

	cust, acc, tx, err := svc.OnboardCustomer(r.Context(), req, idemKey, r.RemoteAddr, traceID)
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateEmail) {
			problem.Render(w, r, http.StatusConflict, "Duplicate Customer", err.Error())
			return
		}
		problem.Render(w, r, http.StatusInternalServerError, "Onboarding Failed", err.Error())
		return
	}

	response.JSON(w, r, http.StatusCreated, "Customer CIF & Bank Account opened successfully", map[string]interface{}{
		"customer":            cust,
		"account":             acc,
		"opening_transaction": tx,
	}, nil)
}

// ListAccountsHandler returns owned accounts for CUSTOMER/MERCHANT, or all accounts for Bank Staff.
func ListAccountsHandler(w http.ResponseWriter, r *http.Request) {
	includeGL := r.URL.Query().Get("include_gl") == "true"
	actorID, _ := r.Context().Value(appmw.CtxActorIDKey).(string)
	actorRole, _ := r.Context().Value(appmw.CtxRoleKey).(model.UserRole)

	accounts := svc.ListAccounts(r.Context(), actorID, actorRole, includeGL)
	response.JSON(w, r, http.StatusOK, "Accounts retrieved successfully", accounts, map[string]interface{}{
		"count":       len(accounts),
		"viewed_role": actorRole,
	})
}

// GetAccountBalanceHandler performs a real-time balance and daily velocity limit inquiry.
func GetAccountBalanceHandler(w http.ResponseWriter, r *http.Request) {
	accountNo := chi.URLParam(r, "accountNumber")
	actorID, _ := r.Context().Value(appmw.CtxActorIDKey).(string)
	actorRole, _ := r.Context().Value(appmw.CtxRoleKey).(model.UserRole)

	bal, err := svc.GetAccountBalance(r.Context(), accountNo, actorID, actorRole)
	if err != nil {
		renderBankingError(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, "Account balance inquiry successful", bal, nil)
}

// GetAccountStatementHandler returns the immutable DEBIT/CREDIT ledger statement for an account.
func GetAccountStatementHandler(w http.ResponseWriter, r *http.Request) {
	accountNo := chi.URLParam(r, "accountNumber")
	actorID, _ := r.Context().Value(appmw.CtxActorIDKey).(string)
	actorRole, _ := r.Context().Value(appmw.CtxRoleKey).(model.UserRole)

	acc, postings, err := svc.GetAccountStatement(r.Context(), accountNo, actorID, actorRole, 50)
	if err != nil {
		renderBankingError(w, r, err)
		return
	}

	response.JSON(w, r, http.StatusOK, "Account statement retrieved", map[string]interface{}{
		"account_number":    acc.AccountNumber,
		"owner_name":        acc.OwnerName,
		"account_type":      acc.AccountType,
		"card_tier":         acc.CardTier,
		"currency":          acc.Currency,
		"current_balance":   acc.Balance,
		"balance_formatted": response.FormatIDR(acc.Balance),
		"statement_entries": postings,
	}, map[string]interface{}{
		"entry_count": len(postings),
	})
}

// UnblockAccountPINHandler allows CUSTOMER_SERVICE or ADMIN to unblock a PIN_BLOCKED account.
func UnblockAccountPINHandler(w http.ResponseWriter, r *http.Request) {
	accountNo := chi.URLParam(r, "accountNumber")
	var req dto.UnblockPINRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			problem.Render(w, r, http.StatusBadRequest, "Malformed JSON", "Request body must be valid JSON.")
			return
		}
	}
	if err := req.Validate(); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Validation Error", err.Error())
		return
	}

	actorID, _ := r.Context().Value(appmw.CtxActorIDKey).(string)
	traceID := chimw.GetReqID(r.Context())

	acc, err := svc.UnblockAccountPIN(r.Context(), accountNo, req, actorID, r.RemoteAddr, traceID)
	if err != nil {
		renderBankingError(w, r, err)
		return
	}

	response.JSON(w, r, http.StatusOK, "Account PIN unblocked and status restored to ACTIVE", acc, nil)
}

// ExecuteTransferHandler executes an ACID Double-Entry Fund Transfer (Intrabank, BI-FAST, RTGS, LLG).
func ExecuteTransferHandler(w http.ResponseWriter, r *http.Request) {
	var req dto.TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Malformed JSON", "Request body must be valid JSON.")
		return
	}
	if err := req.Validate(); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Validation Error", err.Error())
		return
	}

	idemKey := r.Header.Get("X-Idempotency-Key")
	actorID, _ := r.Context().Value(appmw.CtxActorIDKey).(string)
	actorRole, _ := r.Context().Value(appmw.CtxRoleKey).(model.UserRole)
	traceID := chimw.GetReqID(r.Context())

	tx, err := svc.ExecuteTransfer(r.Context(), req, idemKey, actorID, actorRole, r.RemoteAddr, traceID)
	if err != nil {
		renderBankingError(w, r, err)
		return
	}

	response.JSON(w, r, http.StatusCreated, "Fund transfer settled via Double-Entry General Ledger", tx, nil)
}

// ExecuteCashTransactionHandler processes Teller/ATM Cash Deposits & Withdrawals.
func ExecuteCashTransactionHandler(w http.ResponseWriter, r *http.Request) {
	var req dto.CashTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Malformed JSON", "Request body must be valid JSON.")
		return
	}
	if err := req.Validate(); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Validation Error", err.Error())
		return
	}

	idemKey := r.Header.Get("X-Idempotency-Key")
	actorID, _ := r.Context().Value(appmw.CtxActorIDKey).(string)
	traceID := chimw.GetReqID(r.Context())

	tx, err := svc.ExecuteCashTransaction(r.Context(), req, idemKey, actorID, r.RemoteAddr, traceID)
	if err != nil {
		renderBankingError(w, r, err)
		return
	}

	response.JSON(w, r, http.StatusCreated, fmt.Sprintf("Cash %s settled successfully", req.Operation), tx, nil)
}

// CreateVirtualAccountHandler generates a new Virtual Account billing number.
func CreateVirtualAccountHandler(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateVirtualAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Malformed JSON", "Request body must be valid JSON.")
		return
	}
	if err := req.Validate(); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Validation Error", err.Error())
		return
	}

	traceID := chimw.GetReqID(r.Context())
	va, err := svc.CreateVirtualAccount(r.Context(), req, r.RemoteAddr, traceID)
	if err != nil {
		renderBankingError(w, r, err)
		return
	}

	response.JSON(w, r, http.StatusCreated, "Virtual Account billing created", va, nil)
}

// GetVirtualAccountHandler performs a Virtual Account billing inquiry.
func GetVirtualAccountHandler(w http.ResponseWriter, r *http.Request) {
	vaNumber := chi.URLParam(r, "vaNumber")
	va, err := svc.GetVirtualAccount(r.Context(), vaNumber)
	if err != nil {
		problem.Render(w, r, http.StatusNotFound, "Virtual Account Not Found",
			fmt.Sprintf("Virtual Account '%s' was not found.", vaNumber))
		return
	}
	response.JSON(w, r, http.StatusOK, "Virtual Account inquiry successful", va, nil)
}

// PayVirtualAccountHandler settles an open Virtual Account bill atomically.
func PayVirtualAccountHandler(w http.ResponseWriter, r *http.Request) {
	vaNumber := chi.URLParam(r, "vaNumber")
	var req dto.PayVirtualAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Malformed JSON", "Request body must be valid JSON.")
		return
	}
	if err := req.Validate(); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Validation Error", err.Error())
		return
	}

	idemKey := r.Header.Get("X-Idempotency-Key")
	actorID, _ := r.Context().Value(appmw.CtxActorIDKey).(string)
	actorRole, _ := r.Context().Value(appmw.CtxRoleKey).(model.UserRole)
	traceID := chimw.GetReqID(r.Context())

	va, tx, err := svc.PayVirtualAccount(r.Context(), vaNumber, req, idemKey, actorID, actorRole, r.RemoteAddr, traceID)
	if err != nil {
		renderBankingError(w, r, err)
		return
	}

	response.JSON(w, r, http.StatusOK, "Virtual Account payment settled", map[string]interface{}{
		"virtual_account": va,
		"transaction":     tx,
	}, nil)
}

// GetTrialBalanceHandler returns the General Ledger Trial Balance (SUM(DEBIT) == SUM(CREDIT)).
func GetTrialBalanceHandler(w http.ResponseWriter, r *http.Request) {
	tb := svc.GetTrialBalance(r.Context())
	response.JSON(w, r, http.StatusOK, "General Ledger Trial Balance verified", tb, nil)
}

// ListAuditLogsHandler returns recent OJK/BI regulatory audit log records.
func ListAuditLogsHandler(w http.ResponseWriter, r *http.Request) {
	logs, err := svc.ListAuditLogs(r.Context(), 50)
	if err != nil {
		problem.Render(w, r, http.StatusInternalServerError, "Audit Query Failed", "Could not retrieve audit trail.")
		return
	}
	response.JSON(w, r, http.StatusOK, "Regulatory audit trail retrieved", logs, map[string]interface{}{
		"count": len(logs),
	})
}

// OpenAdditionalAccountHandler opens a new bank account (SAVINGS, CHECKING, TIME_DEPOSIT) for an existing CIF.
func OpenAdditionalAccountHandler(w http.ResponseWriter, r *http.Request) {
	var req dto.OpenAdditionalAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Malformed JSON", "Request body must be valid JSON.")
		return
	}
	if err := req.Validate(); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Validation Error", err.Error())
		return
	}

	idemKey := r.Header.Get("X-Idempotency-Key")
	actorID, _ := r.Context().Value(appmw.CtxActorIDKey).(string)
	actorRole, _ := r.Context().Value(appmw.CtxRoleKey).(model.UserRole)
	traceID := chimw.GetReqID(r.Context())

	acc, tx, err := svc.OpenAdditionalAccount(r.Context(), req, idemKey, actorID, actorRole, r.RemoteAddr, traceID)
	if err != nil {
		renderBankingError(w, r, err)
		return
	}

	response.JSON(w, r, http.StatusCreated, "Additional bank account opened successfully", map[string]interface{}{
		"account":             acc,
		"opening_transaction": tx,
	}, nil)
}

// UpdateAccountStatusHandler allows ADMIN or CUSTOMER_SERVICE to freeze, dormant, or activate an account.
func UpdateAccountStatusHandler(w http.ResponseWriter, r *http.Request) {
	accountNo := chi.URLParam(r, "accountNumber")
	var req dto.UpdateAccountStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Malformed JSON", "Request body must be valid JSON.")
		return
	}
	if err := req.Validate(); err != nil {
		problem.Render(w, r, http.StatusBadRequest, "Validation Error", err.Error())
		return
	}

	actorID, _ := r.Context().Value(appmw.CtxActorIDKey).(string)
	traceID := chimw.GetReqID(r.Context())

	acc, err := svc.UpdateAccountStatus(r.Context(), accountNo, req, actorID, r.RemoteAddr, traceID)
	if err != nil {
		renderBankingError(w, r, err)
		return
	}

	response.JSON(w, r, http.StatusOK, fmt.Sprintf("Account status updated to %s", acc.Status), acc, nil)
}

// ListTransactionsHandler returns transaction history (Customer sees own; Staff see all, with ?account=&channel= filters).
func ListTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	actorID, _ := r.Context().Value(appmw.CtxActorIDKey).(string)
	actorRole, _ := r.Context().Value(appmw.CtxRoleKey).(model.UserRole)
	accFilter := r.URL.Query().Get("account")
	chFilter := r.URL.Query().Get("channel")

	txs := svc.ListTransactions(r.Context(), actorID, actorRole, accFilter, chFilter)
	response.JSON(w, r, http.StatusOK, "Transaction history retrieved", txs, map[string]interface{}{
		"count":   len(txs),
		"account": accFilter,
		"channel": chFilter,
	})
}

// GetTransactionByReferenceHandler returns a detailed transaction receipt and its double-entry ledger postings.
func GetTransactionByReferenceHandler(w http.ResponseWriter, r *http.Request) {
	refNo := chi.URLParam(r, "referenceNo")
	actorID, _ := r.Context().Value(appmw.CtxActorIDKey).(string)
	actorRole, _ := r.Context().Value(appmw.CtxRoleKey).(model.UserRole)

	tx, err := svc.GetTransactionByReference(r.Context(), refNo, actorID, actorRole)
	if err != nil {
		renderBankingError(w, r, err)
		return
	}

	response.JSON(w, r, http.StatusOK, "Transaction receipt & double-entry postings retrieved", tx, nil)
}

// GetAdminDashboardHandler returns the executive Core Banking Command Center overview for ADMIN and AUDITOR.
func GetAdminDashboardHandler(w http.ResponseWriter, r *http.Request) {
	metrics := svc.GetAdminDashboardMetrics(r.Context())
	response.JSON(w, r, http.StatusOK, "Core Banking Executive Command Center Dashboard", metrics, nil)
}

func renderBankingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		problem.Render(w, r, http.StatusNotFound, "Account or Resource Not Found", err.Error())
	case errors.Is(err, repository.ErrForbiddenAccount):
		problem.Render(w, r, http.StatusForbidden, "Account Ownership Violation", err.Error())
	case errors.Is(err, repository.ErrInvalidPIN):
		problem.Render(w, r, http.StatusUnauthorized, "Invalid Transaction PIN", err.Error())
	case errors.Is(err, repository.ErrAccountPINBlocked):
		problem.Render(w, r, http.StatusForbidden, "Account PIN Blocked", err.Error())
	case errors.Is(err, repository.ErrAccountNotActive):
		problem.Render(w, r, http.StatusForbidden, "Account Not Active", err.Error())
	case errors.Is(err, repository.ErrInsufficientFunds):
		problem.Render(w, r, http.StatusUnprocessableEntity, "Insufficient Funds", err.Error())
	case errors.Is(err, repository.ErrDailyLimitExceeded):
		problem.Render(w, r, http.StatusUnprocessableEntity, "Daily Velocity Limit Exceeded", err.Error())
	case errors.Is(err, repository.ErrVANotOpen):
		problem.Render(w, r, http.StatusConflict, "Virtual Account Already Settled or Expired", err.Error())
	default:
		problem.Render(w, r, http.StatusInternalServerError, "Core Banking Execution Error", err.Error())
	}
}

// ScalarDocsHandler serves the interactive Scalar OpenAPI Playground pre-configured with the Demo Sandbox Key.
func ScalarDocsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html>
<html>
  <head>
    <title>Nusantara Core Banking System (CBS) — OpenAPI Playground</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
  </head>
  <body>
    <script
      id="api-reference"
      data-url="/openapi.yaml"
      data-configuration='{"theme":"kepler","authentication":{"preferredSecurityScheme":"ApiKeyAuth","apiKey":{"token":"` + repository.DemoSandboxRawKey + `"}}}'></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>`))
}

// OpenAPISpecHandler serves the OpenAPI 3.1 YAML specification.
func OpenAPISpecHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	if data, err := os.ReadFile("docs/openapi.yaml"); err == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}
	if data, err := os.ReadFile("../docs/openapi.yaml"); err == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("openapi: 3.1.0\ninfo:\n  title: Nusantara Core Banking System API\n  version: 2.1.0\n"))
}
