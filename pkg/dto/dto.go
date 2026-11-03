package dto

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"

	"github.com/cbs-bank/enterprise-portfolio-api/pkg/model"
)

var pinRegex = regexp.MustCompile(`^[0-9]{6}$`)

// LoginRequest represents credentials for JWT token generation.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// TokenResponse represents the JWT access and refresh token pair, plus role & owned accounts info.
type TokenResponse struct {
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	TokenType    string          `json:"token_type"`
	ExpiresIn    int             `json:"expires_in"`
	CIFNumber    string          `json:"cif_number"`
	FullName     string          `json:"full_name"`
	Email        string          `json:"email"`
	Role         string          `json:"role"`
	Permissions  []string        `json:"permissions"`
	Accounts     []model.Account `json:"owned_accounts,omitempty"`
}

// OnboardCustomerRequest represents the payload for opening a new CIF and Bank Account.
type OnboardCustomerRequest struct {
	NIK            string `json:"nik"`
	FullName       string `json:"full_name"`
	Email          string `json:"email"`
	PhoneNumber    string `json:"phone_number"`
	Password       string `json:"password"`
	AccountType    string `json:"account_type"`    // SAVINGS, SAVINGS_YOUTH, CHECKING, TIME_DEPOSIT
	CardTier       string `json:"card_tier"`       // SILVER, GOLD, PLATINUM, PRIORITY
	PIN            string `json:"pin"`             // 6-digit numeric PIN
	InitialDeposit int64  `json:"initial_deposit"` // Minimum Rp 50.000
}

func (r *OnboardCustomerRequest) Validate() error {
	r.NIK = strings.TrimSpace(r.NIK)
	r.FullName = strings.TrimSpace(r.FullName)
	r.Email = strings.TrimSpace(strings.ToLower(r.Email))
	r.PhoneNumber = strings.TrimSpace(r.PhoneNumber)
	r.AccountType = strings.TrimSpace(strings.ToUpper(r.AccountType))
	r.CardTier = strings.TrimSpace(strings.ToUpper(r.CardTier))
	r.PIN = strings.TrimSpace(r.PIN)

	if len(r.NIK) != 16 {
		return errors.New("field 'nik' must be exactly 16 digits (KTP standard)")
	}
	if len(r.FullName) < 3 {
		return errors.New("field 'full_name' must be at least 3 characters")
	}
	if _, err := mail.ParseAddress(r.Email); err != nil {
		return errors.New("field 'email' must be a valid email address")
	}
	if len(r.Password) < 8 {
		return errors.New("field 'password' must be at least 8 characters")
	}
	if !pinRegex.MatchString(r.PIN) {
		return errors.New("field 'pin' must be a 6-digit numeric transaction PIN")
	}
	if r.InitialDeposit < 50_000 {
		return errors.New("field 'initial_deposit' must be at least Rp 50.000")
	}
	if r.AccountType == "" {
		r.AccountType = string(model.AccountSavings)
	}
	if r.CardTier == "" {
		r.CardTier = string(model.TierPlatinum)
	}
	return nil
}

// TransferRequest represents an Intrabank or Interbank (BI-FAST / RTGS / LLG) fund transfer.
type TransferRequest struct {
	SourceAccountNumber      string `json:"source_account_number"`
	DestinationAccountNumber string `json:"destination_account_number"`
	Channel                  string `json:"channel"` // INTRABANK, BI_FAST, RTGS, LLG
	ExternalBankCode         string `json:"external_bank_code,omitempty"`
	ExternalAccountName      string `json:"external_account_name,omitempty"`
	Amount                   int64  `json:"amount"`
	PIN                      string `json:"pin"`
	Description              string `json:"description"`
}

func (r *TransferRequest) Validate() error {
	r.SourceAccountNumber = strings.TrimSpace(r.SourceAccountNumber)
	r.DestinationAccountNumber = strings.TrimSpace(r.DestinationAccountNumber)
	r.Channel = strings.TrimSpace(strings.ToUpper(r.Channel))
	r.ExternalBankCode = strings.TrimSpace(strings.ToUpper(r.ExternalBankCode))
	r.PIN = strings.TrimSpace(r.PIN)

	if r.SourceAccountNumber == "" {
		return errors.New("field 'source_account_number' is required")
	}
	if r.DestinationAccountNumber == "" {
		return errors.New("field 'destination_account_number' is required")
	}
	if r.SourceAccountNumber == r.DestinationAccountNumber && r.Channel == string(model.ChannelIntrabank) {
		return errors.New("source and destination account numbers cannot be identical")
	}
	if !pinRegex.MatchString(r.PIN) {
		return errors.New("field 'pin' must be a 6-digit numeric transaction PIN")
	}
	if r.Channel == "" {
		r.Channel = string(model.ChannelIntrabank)
	}

	switch model.TxChannel(r.Channel) {
	case model.ChannelIntrabank:
		if r.Amount < 10_000 {
			return errors.New("minimum INTRABANK transfer amount is Rp 10.000")
		}
	case model.ChannelBIFast:
		if r.ExternalBankCode == "" {
			return errors.New("field 'external_bank_code' (e.g. BMRIIDJA, BNINIDJA) is required for BI_FAST")
		}
		if r.Amount < 10_000 || r.Amount > 250_000_000 {
			return errors.New("BI_FAST transfer amount must be between Rp 10.000 and Rp 250.000.000")
		}
	case model.ChannelRTGS:
		if r.ExternalBankCode == "" {
			return errors.New("field 'external_bank_code' is required for RTGS")
		}
		if r.Amount <= 100_000_000 {
			return errors.New("RTGS minimum transfer amount is above Rp 100.000.000 (Rp 100.000.001+)")
		}
	case model.ChannelLLG:
		if r.ExternalBankCode == "" {
			return errors.New("field 'external_bank_code' is required for LLG/SKNBI")
		}
		if r.Amount < 10_000 || r.Amount > 500_000_000 {
			return errors.New("LLG transfer amount must be between Rp 10.000 and Rp 500.000.000")
		}
	default:
		return errors.New("unsupported 'channel': use INTRABANK, BI_FAST, RTGS, or LLG")
	}

	return nil
}

// CashTransactionRequest represents Teller/ATM Cash Deposit or Cash Withdrawal.
type CashTransactionRequest struct {
	AccountNumber string `json:"account_number"`
	Operation     string `json:"operation"` // DEPOSIT or WITHDRAWAL
	Amount        int64  `json:"amount"`
	PIN           string `json:"pin,omitempty"`
	Description   string `json:"description"`
}

func (r *CashTransactionRequest) Validate() error {
	r.AccountNumber = strings.TrimSpace(r.AccountNumber)
	r.Operation = strings.TrimSpace(strings.ToUpper(r.Operation))
	if r.AccountNumber == "" {
		return errors.New("field 'account_number' is required")
	}
	if r.Operation != "DEPOSIT" && r.Operation != "WITHDRAWAL" {
		return errors.New("field 'operation' must be DEPOSIT or WITHDRAWAL")
	}
	if r.Amount < 10_000 {
		return errors.New("minimum cash transaction amount is Rp 10.000")
	}
	if r.Operation == "WITHDRAWAL" && !pinRegex.MatchString(strings.TrimSpace(r.PIN)) {
		return errors.New("6-digit 'pin' is required for cash WITHDRAWAL")
	}
	return nil
}

// UnblockPINRequest allows Customer Service (CS) or Admin to unblock a PIN_BLOCKED account and optionally set a new PIN.
type UnblockPINRequest struct {
	NewPIN string `json:"new_pin,omitempty"`
	Reason string `json:"reason"`
}

func (r *UnblockPINRequest) Validate() error {
	r.NewPIN = strings.TrimSpace(r.NewPIN)
	r.Reason = strings.TrimSpace(r.Reason)
	if r.NewPIN != "" && !pinRegex.MatchString(r.NewPIN) {
		return errors.New("field 'new_pin' must be a 6-digit numeric PIN")
	}
	if r.Reason == "" {
		r.Reason = "KYC verified at branch Customer Service desk"
	}
	return nil
}

// OpenAdditionalAccountRequest opens a new bank account for an existing Customer CIF.
type OpenAdditionalAccountRequest struct {
	CIFNumber      string `json:"cif_number,omitempty"` // Optional if Customer opens for themselves via JWT
	AccountType    string `json:"account_type"`         // SAVINGS, SAVINGS_YOUTH, CHECKING, TIME_DEPOSIT
	CardTier       string `json:"card_tier"`            // SILVER, GOLD, PLATINUM, PRIORITY
	PIN            string `json:"pin"`                  // 6-digit numeric PIN
	InitialDeposit int64  `json:"initial_deposit"`      // Minimum Rp 50.000
}

func (r *OpenAdditionalAccountRequest) Validate() error {
	r.CIFNumber = strings.TrimSpace(strings.ToUpper(r.CIFNumber))
	r.AccountType = strings.TrimSpace(strings.ToUpper(r.AccountType))
	r.CardTier = strings.TrimSpace(strings.ToUpper(r.CardTier))
	r.PIN = strings.TrimSpace(r.PIN)

	if r.AccountType == "" {
		r.AccountType = string(model.AccountSavings)
	}
	if r.CardTier == "" {
		r.CardTier = string(model.TierPlatinum)
	}
	if !pinRegex.MatchString(r.PIN) {
		return errors.New("field 'pin' must be a 6-digit numeric transaction PIN")
	}
	if r.InitialDeposit < 50_000 {
		return errors.New("field 'initial_deposit' must be at least Rp 50.000")
	}
	return nil
}

// UpdateAccountStatusRequest allows Admin or CS to FREEZE, DORMANT, or ACTIVATE an account.
type UpdateAccountStatusRequest struct {
	Status string `json:"status"` // ACTIVE, FROZEN, DORMANT, CLOSED
	Reason string `json:"reason"`
}

func (r *UpdateAccountStatusRequest) Validate() error {
	r.Status = strings.TrimSpace(strings.ToUpper(r.Status))
	r.Reason = strings.TrimSpace(r.Reason)
	switch model.AccountStatus(r.Status) {
	case model.AccountActive, model.AccountFrozen, model.AccountDormant, model.AccountClosed:
	default:
		return errors.New("field 'status' must be ACTIVE, FROZEN, DORMANT, or CLOSED")
	}
	if r.Reason == "" {
		return errors.New("field 'reason' is mandatory for regulatory audit trail")
	}
	return nil
}

// CreateVirtualAccountRequest creates a new Virtual Account billing number.
type CreateVirtualAccountRequest struct {
	MerchantAccountNumber string `json:"merchant_account_number"`
	MerchantName          string `json:"merchant_name"`
	CustomerPhoneOrID     string `json:"customer_phone_or_id"`
	CustomerName          string `json:"customer_name"`
	BillAmount            int64  `json:"bill_amount"`
	Description           string `json:"description"`
}

func (r *CreateVirtualAccountRequest) Validate() error {
	r.MerchantAccountNumber = strings.TrimSpace(r.MerchantAccountNumber)
	r.MerchantName = strings.TrimSpace(r.MerchantName)
	r.CustomerPhoneOrID = strings.TrimSpace(r.CustomerPhoneOrID)
	r.CustomerName = strings.TrimSpace(r.CustomerName)

	if r.MerchantAccountNumber == "" {
		return errors.New("field 'merchant_account_number' is required")
	}
	if r.MerchantName == "" {
		return errors.New("field 'merchant_name' is required")
	}
	if len(r.CustomerPhoneOrID) < 6 {
		return errors.New("field 'customer_phone_or_id' must be at least 6 digits")
	}
	if r.CustomerName == "" {
		return errors.New("field 'customer_name' is required")
	}
	if r.BillAmount < 10_000 {
		return errors.New("field 'bill_amount' must be at least Rp 10.000")
	}
	return nil
}

// PayVirtualAccountRequest pays an open Virtual Account bill.
type PayVirtualAccountRequest struct {
	SourceAccountNumber string `json:"source_account_number"`
	PIN                 string `json:"pin"`
}

func (r *PayVirtualAccountRequest) Validate() error {
	r.SourceAccountNumber = strings.TrimSpace(r.SourceAccountNumber)
	r.PIN = strings.TrimSpace(r.PIN)
	if r.SourceAccountNumber == "" {
		return errors.New("field 'source_account_number' is required")
	}
	if !pinRegex.MatchString(r.PIN) {
		return errors.New("field 'pin' must be a 6-digit numeric transaction PIN")
	}
	return nil
}

// GenerateSandboxKeyRequest allows optional metadata when generating an Open Banking partner key.
type GenerateSandboxKeyRequest struct {
	PartnerName string `json:"partner_name"`
}
