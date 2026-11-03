package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cbs-bank/enterprise-portfolio-api/pkg/repository"
)

func loginAs(t *testing.T, email string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": "Password#2026",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/token", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	Handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("failed to login as %s: status %d, body: %s", email, rec.Code, rec.Body.String())
	}

	var env struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return env.Data.AccessToken
}

func TestSystemHealthAndRolesGuide(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/system/health", nil)
	rec := httptest.NewRecorder()
	Handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	gl := body["general_ledger_integrity"].(map[string]interface{})
	if gl["is_balanced"] != true {
		t.Fatalf("expected general ledger to be balanced, got %v", gl)
	}

	// Verify /v1/auth/roles endpoint
	reqRoles := httptest.NewRequest(http.MethodGet, "/v1/auth/roles", nil)
	recRoles := httptest.NewRecorder()
	Handler(recRoles, reqRoles)
	if recRoles.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on /v1/auth/roles, got %d", recRoles.Code)
	}
}

func TestCustomerOwnershipAndRBACBoundaries(t *testing.T) {
	budiToken := loginAs(t, "budi.santoso@cbs-bank.co.id")

	// 1. Budi can check his own profile via GET /v1/auth/me
	reqMe := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	reqMe.Header.Set("Authorization", "Bearer "+budiToken)
	recMe := httptest.NewRecorder()
	Handler(recMe, reqMe)
	if recMe.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on /v1/auth/me, got %d", recMe.Code)
	}

	// 2. Budi CAN check his own account balance (5240301001)
	reqOwn := httptest.NewRequest(http.MethodGet, "/v1/accounts/5240301001/balance", nil)
	reqOwn.Header.Set("Authorization", "Bearer "+budiToken)
	recOwn := httptest.NewRecorder()
	Handler(recOwn, reqOwn)
	if recOwn.Code != http.StatusOK {
		t.Fatalf("expected 200 OK when Budi checks his own balance, got %d", recOwn.Code)
	}

	// 3. Budi CANNOT check Siti's account balance (5240302002) -> must return 403 Forbidden
	reqOther := httptest.NewRequest(http.MethodGet, "/v1/accounts/5240302002/balance", nil)
	reqOther.Header.Set("Authorization", "Bearer "+budiToken)
	recOther := httptest.NewRecorder()
	Handler(recOther, reqOther)
	if recOther.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden when Customer checks another customer's balance, got %d", recOther.Code)
	}

	// 4. Budi CANNOT access OJK Auditor Trial Balance -> must return 403 Forbidden
	reqTB := httptest.NewRequest(http.MethodGet, "/v1/ledger/trial-balance", nil)
	reqTB.Header.Set("Authorization", "Bearer "+budiToken)
	recTB := httptest.NewRecorder()
	Handler(recTB, reqTB)
	if recTB.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden when Customer accesses trial balance, got %d", recTB.Code)
	}
}

func TestIntrabankAndBIFastTransferWithIdempotency(t *testing.T) {
	budiToken := loginAs(t, "budi.santoso@cbs-bank.co.id")

	// 1. Execute Intrabank Transfer (Budi 5240301001 -> Siti 5240302002, Rp 5.000.000)
	txPayload := []byte(`{
		"source_account_number": "5240301001",
		"destination_account_number": "5240302002",
		"channel": "INTRABANK",
		"amount": 5000000,
		"pin": "123456",
		"description": "Transfer Sesama Bank"
	}`)

	req1 := httptest.NewRequest(http.MethodPost, "/v1/transactions/transfer", bytes.NewReader(txPayload))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer "+budiToken)
	req1.Header.Set("X-Idempotency-Key", "idem-intrabank-001")
	rec1 := httptest.NewRecorder()
	Handler(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on Intrabank transfer, got %d: %s", rec1.Code, rec1.Body.String())
	}

	// 2. Replay identical X-Idempotency-Key -> must return 200 OK with X-Cache-Hit: Idempotency-Replay and NOT double-debit
	reqReplay := httptest.NewRequest(http.MethodPost, "/v1/transactions/transfer", bytes.NewReader(txPayload))
	reqReplay.Header.Set("Content-Type", "application/json")
	reqReplay.Header.Set("Authorization", "Bearer "+budiToken)
	reqReplay.Header.Set("X-Idempotency-Key", "idem-intrabank-001")
	recReplay := httptest.NewRecorder()
	Handler(recReplay, reqReplay)

	if recReplay.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on idempotent replay, got %d", recReplay.Code)
	}
	if recReplay.Header().Get("X-Cache-Hit") != "Idempotency-Replay" {
		t.Fatalf("expected X-Cache-Hit: Idempotency-Replay")
	}

	// 3. Execute BI-FAST Transfer (Rp 10.000.000 + Rp 2.500 fee)
	biFastPayload := []byte(`{
		"source_account_number": "5240301001",
		"destination_account_number": "1370012345678",
		"channel": "BI_FAST",
		"external_bank_code": "BMRIIDJA",
		"external_account_name": "PT Mandiri Sekuritas",
		"amount": 10000000,
		"pin": "123456",
		"description": "Topup RDN via BI-FAST"
	}`)
	reqBIFast := httptest.NewRequest(http.MethodPost, "/v1/transactions/transfer", bytes.NewReader(biFastPayload))
	reqBIFast.Header.Set("Content-Type", "application/json")
	reqBIFast.Header.Set("Authorization", "Bearer "+budiToken)
	reqBIFast.Header.Set("X-Idempotency-Key", "idem-bifast-001")
	recBIFast := httptest.NewRecorder()
	Handler(recBIFast, reqBIFast)

	if recBIFast.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on BI-FAST transfer, got %d: %s", recBIFast.Code, recBIFast.Body.String())
	}

	// 4. Verify Budi's balance: 500.000.000 - 5.000.000 - 10.002.500 = 484.997.500
	reqBal := httptest.NewRequest(http.MethodGet, "/v1/accounts/5240301001/balance", nil)
	reqBal.Header.Set("Authorization", "Bearer "+budiToken)
	recBal := httptest.NewRecorder()
	Handler(recBal, reqBal)

	if recBal.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on balance inquiry, got %d", recBal.Code)
	}
	var balEnv struct {
		Data struct {
			Balance int64 `json:"balance"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recBal.Body.Bytes(), &balEnv)
	if balEnv.Data.Balance != 484_997_500 {
		t.Fatalf("expected balance 484997500 after Intrabank & BI-FAST, got %d", balEnv.Data.Balance)
	}
}

func TestVirtualAccountInquiryAndSettlement(t *testing.T) {
	budiToken := loginAs(t, "budi.santoso@cbs-bank.co.id")

	// 1. Inquiry Virtual Account 88000081290001001
	reqInq := httptest.NewRequest(http.MethodGet, "/v1/virtual-accounts/88000081290001001", nil)
	reqInq.Header.Set("Authorization", "Bearer "+budiToken)
	recInq := httptest.NewRecorder()
	Handler(recInq, reqInq)

	if recInq.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on VA inquiry, got %d", recInq.Code)
	}

	// 2. Pay Virtual Account from Budi (5240301001)
	payPayload := []byte(`{"source_account_number":"5240301001","pin":"123456"}`)
	reqPay := httptest.NewRequest(http.MethodPost, "/v1/virtual-accounts/88000081290001001/pay", bytes.NewReader(payPayload))
	reqPay.Header.Set("Content-Type", "application/json")
	reqPay.Header.Set("Authorization", "Bearer "+budiToken)
	reqPay.Header.Set("X-Idempotency-Key", "idem-va-pay-001")
	recPay := httptest.NewRecorder()
	Handler(recPay, reqPay)

	if recPay.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on VA payment, got %d: %s", recPay.Code, recPay.Body.String())
	}

	// 3. Paying an already PAID Virtual Account with a new idempotency key must fail with 409 Conflict
	reqPayAgain := httptest.NewRequest(http.MethodPost, "/v1/virtual-accounts/88000081290001001/pay", bytes.NewReader(payPayload))
	reqPayAgain.Header.Set("Content-Type", "application/json")
	reqPayAgain.Header.Set("Authorization", "Bearer "+budiToken)
	reqPayAgain.Header.Set("X-Idempotency-Key", "idem-va-pay-002")
	recPayAgain := httptest.NewRecorder()
	Handler(recPayAgain, reqPayAgain)

	if recPayAgain.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict when paying an already settled VA, got %d", recPayAgain.Code)
	}
}

func TestOnboardingPINLockoutAndCSUnblockFlow(t *testing.T) {
	onboardPayload := []byte(`{
		"nik": "3171041908950099",
		"full_name": "Dimas Anggara",
		"email": "dimas.anggara@cbs-bank.co.id",
		"phone_number": "+6281188889999",
		"password": "Password#2026",
		"account_type": "SAVINGS",
		"card_tier": "GOLD",
		"pin": "998877",
		"initial_deposit": 5000000
	}`)

	reqOnb := httptest.NewRequest(http.MethodPost, "/v1/customers/onboard", bytes.NewReader(onboardPayload))
	reqOnb.Header.Set("Content-Type", "application/json")
	reqOnb.Header.Set("X-Idempotency-Key", "idem-onboard-dimas-001")
	recOnb := httptest.NewRecorder()
	Handler(recOnb, reqOnb)

	if recOnb.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on onboarding, got %d: %s", recOnb.Code, recOnb.Body.String())
	}

	var onbEnv struct {
		Data struct {
			Account struct {
				AccountNumber string `json:"account_number"`
			} `json:"account"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recOnb.Body.Bytes(), &onbEnv)
	newAccNo := onbEnv.Data.Account.AccountNumber

	// Attempt 3 consecutive wrong PINs -> 3rd attempt must lock the account (403 PIN_BLOCKED)
	for i := 1; i <= 3; i++ {
		badTx := []byte(`{
			"source_account_number": "` + newAccNo + `",
			"destination_account_number": "5240302002",
			"channel": "INTRABANK",
			"amount": 100000,
			"pin": "000000"
		}`)
		reqBad := httptest.NewRequest(http.MethodPost, "/v1/transactions/transfer", bytes.NewReader(badTx))
		reqBad.Header.Set("Content-Type", "application/json")
		reqBad.Header.Set("X-API-Key", repository.DemoSandboxRawKey)
		reqBad.Header.Set("X-Idempotency-Key", "idem-bad-pin-"+string(rune('0'+i)))
		recBad := httptest.NewRecorder()
		Handler(recBad, reqBad)

		if i < 3 && recBad.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401 Unauthorized for wrong PIN, got %d", i, recBad.Code)
		}
		if i == 3 && recBad.Code != http.StatusForbidden {
			t.Fatalf("attempt 3: expected 403 Forbidden (PIN_BLOCKED), got %d: %s", recBad.Code, recBad.Body.String())
		}
	}

	// Login as CUSTOMER_SERVICE (cs@cbs-bank.co.id) to unblock Dimas's PIN!
	csToken := loginAs(t, "cs@cbs-bank.co.id")
	unblockPayload := []byte(`{"new_pin":"998877","reason":"Verifikasi KTP di Cabang"}`)
	reqUnblock := httptest.NewRequest(http.MethodPost, "/v1/accounts/"+newAccNo+"/unblock-pin", bytes.NewReader(unblockPayload))
	reqUnblock.Header.Set("Content-Type", "application/json")
	reqUnblock.Header.Set("Authorization", "Bearer "+csToken)
	recUnblock := httptest.NewRecorder()
	Handler(recUnblock, reqUnblock)

	if recUnblock.Code != http.StatusOK {
		t.Fatalf("expected 200 OK when CS unblocks PIN, got %d: %s", recUnblock.Code, recUnblock.Body.String())
	}
}

func TestTellerCashAndAuditorTrialBalanceInvariant(t *testing.T) {
	// 1. Login as TELLER (teller@cbs-bank.co.id) and execute Cash Deposit Rp 15.000.000 to Siti (5240302002)
	tellerToken := loginAs(t, "teller@cbs-bank.co.id")
	cashPayload := []byte(`{
		"account_number": "5240302002",
		"operation": "DEPOSIT",
		"amount": 15000000,
		"description": "Setor Tunai Teller Cabang Sudirman"
	}`)
	reqCash := httptest.NewRequest(http.MethodPost, "/v1/transactions/cash", bytes.NewReader(cashPayload))
	reqCash.Header.Set("Content-Type", "application/json")
	reqCash.Header.Set("Authorization", "Bearer "+tellerToken)
	reqCash.Header.Set("X-Idempotency-Key", "idem-cash-dep-001")
	recCash := httptest.NewRecorder()
	Handler(recCash, reqCash)

	if recCash.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on Teller Cash Deposit, got %d: %s", recCash.Code, recCash.Body.String())
	}

	// 2. Login as AUDITOR (auditor@cbs-bank.co.id) and verify General Ledger Trial Balance (SUM(DEBIT) == SUM(CREDIT))
	auditorToken := loginAs(t, "auditor@cbs-bank.co.id")
	reqTB := httptest.NewRequest(http.MethodGet, "/v1/ledger/trial-balance", nil)
	reqTB.Header.Set("Authorization", "Bearer "+auditorToken)
	recTB := httptest.NewRecorder()
	Handler(recTB, reqTB)

	if recTB.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on Trial Balance, got %d", recTB.Code)
	}

	var tbEnv struct {
		Data repository.TrialBalanceSummary `json:"data"`
	}
	_ = json.Unmarshal(recTB.Body.Bytes(), &tbEnv)
	if !tbEnv.Data.IsBalanced || tbEnv.Data.Discrepancy != 0 {
		t.Fatalf("CRITICAL: General Ledger Trial Balance is out of balance! %+v", tbEnv.Data)
	}

	// 3. Verify Admin Executive Dashboard (GET /v1/admin/dashboard) & Transaction History (GET /v1/transactions)
	adminToken := loginAs(t, "admin@cbs-bank.co.id")
	reqDash := httptest.NewRequest(http.MethodGet, "/v1/admin/dashboard", nil)
	reqDash.Header.Set("Authorization", "Bearer "+adminToken)
	recDash := httptest.NewRecorder()
	Handler(recDash, reqDash)
	if recDash.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on /v1/admin/dashboard, got %d: %s", recDash.Code, recDash.Body.String())
	}

	reqTxs := httptest.NewRequest(http.MethodGet, "/v1/transactions?channel=BI_FAST", nil)
	reqTxs.Header.Set("Authorization", "Bearer "+adminToken)
	recTxs := httptest.NewRecorder()
	Handler(recTxs, reqTxs)
	if recTxs.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on /v1/transactions, got %d", recTxs.Code)
	}
}
