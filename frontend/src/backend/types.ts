export type UserRole =
  | 'CUSTOMER'
  | 'MERCHANT'
  | 'TELLER'
  | 'CUSTOMER_SERVICE'
  | 'AUDITOR'
  | 'ADMIN';

export type KYCStatus =
  | 'UNVERIFIED'
  | 'VERIFIED'
  | 'REJECTED'
  | 'ENHANCED_DUE_DILIGENCE';

export type AccountType =
  | 'SAVINGS'
  | 'SAVINGS_YOUTH'
  | 'CHECKING'
  | 'TIME_DEPOSIT'
  | 'GL_INTERNAL';

export type CardTier = 'SILVER' | 'GOLD' | 'PLATINUM' | 'PRIORITY';

export type AccountStatus =
  | 'ACTIVE'
  | 'DORMANT'
  | 'FROZEN'
  | 'PIN_BLOCKED'
  | 'CLOSED';

export type TxChannel =
  | 'INTRABANK'
  | 'BI_FAST'
  | 'RTGS'
  | 'LLG'
  | 'VIRTUAL_ACCOUNT'
  | 'TELLER_DEPOSIT'
  | 'TELLER_WITHDRAWAL';

export type TxStatus = 'PENDING' | 'SETTLED' | 'FAILED' | 'REVERSED';

export type EntryDirection = 'DEBIT' | 'CREDIT';

export type VAStatus = 'OPEN' | 'PAID' | 'EXPIRED' | 'CANCELLED';

export type AuditAction =
  | 'CREATE'
  | 'UPDATE'
  | 'DELETE'
  | 'AUTHENTICATE'
  | 'TRANSFER'
  | 'VA_PAYMENT'
  | 'PIN_FAILURE'
  | 'PIN_UNBLOCK';

export interface Customer {
  id: string;
  cif_number: string;
  nik_masked: string;
  full_name: string;
  email: string;
  phone_number: string;
  password_hash: string;
  role: UserRole;
  kyc_status: KYCStatus;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface Account {
  id: string;
  account_number: string;
  customer_id: string;
  owner_name?: string;
  account_type: AccountType;
  card_tier: CardTier;
  currency: string;
  balance: number;
  hold_balance: number;
  available_balance: number;
  daily_limit: number;
  pin_hash: string;
  pin_failed_attempts: number;
  status: AccountStatus;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface LedgerPost {
  id: string;
  transaction_id: string;
  account_id: string;
  account_number: string;
  direction: EntryDirection;
  amount: number;
  amount_formatted: string;
  balance_before: number;
  balance_after: number;
  narration: string;
  created_at: string;
}

export interface Transaction {
  id: string;
  reference_no: string;
  idempotency_key: string;
  source_account_id?: string;
  source_account_number?: string;
  destination_account_id?: string;
  destination_account_number?: string;
  external_bank_code?: string;
  external_account_number?: string;
  external_account_name?: string;
  channel: TxChannel;
  amount: number;
  amount_formatted: string;
  fee_amount: number;
  total_debit: number;
  total_debit_formatted: string;
  currency: string;
  description?: string;
  status: TxStatus;
  initiated_by?: string;
  client_ip: string;
  trace_id: string;
  settled_at: string;
  created_at: string;
}

export interface VirtualAccount {
  id: string;
  va_number: string;
  merchant_name: string;
  merchant_account_id: string;
  merchant_account_number: string;
  customer_name: string;
  bill_amount: number;
  bill_amount_formatted: string;
  currency: string;
  description?: string;
  status: VAStatus;
  paid_transaction_id?: string;
  expires_at: string;
  paid_at?: string;
  created_at: string;
}

export interface TrialBalance {
  total_debit_postings: number;
  total_credit_postings: number;
  discrepancy: number;
  is_balanced: boolean;
  total_ledger_entries: number;
  total_transactions: number;
  checked_at: string;
}

export interface AuditLog {
  id: string;
  actor_id?: string;
  action: AuditAction;
  resource_name: string;
  resource_id: string;
  ip_address: string;
  trace_id: string;
  payload_snapshot?: any;
  created_at: string;
}

export interface ApiResponse<T = any> {
  success: boolean;
  message: string;
  data?: T;
  error?: string;
  code?: string;
  metadata?: Record<string, any>;
}

export function formatIdr(amount: number): string {
  const isNeg = amount < 0;
  const absStr = Math.abs(amount).toString();
  const formatted = absStr.replace(/\B(?=(\d{3})+(?!\d))/g, '.');
  return isNeg ? `-Rp ${formatted}` : `Rp ${formatted}`;
}

export function getRolePermissions(role: UserRole): string[] {
  switch (role) {
    case 'CUSTOMER':
      return [
        'auth:me',
        'account:read:own',
        'account:statement:own',
        'transaction:transfer:own',
        'virtual_account:inquiry',
        'virtual_account:pay',
      ];
    case 'MERCHANT':
      return [
        'auth:me',
        'account:read:own',
        'account:statement:own',
        'transaction:transfer:own',
        'virtual_account:create',
        'virtual_account:inquiry',
        'virtual_account:pay',
      ];
    case 'TELLER':
      return [
        'auth:me',
        'customer:onboard',
        'account:read:any',
        'transaction:cash_deposit',
        'transaction:cash_withdrawal',
      ];
    case 'CUSTOMER_SERVICE':
      return [
        'auth:me',
        'customer:onboard',
        'account:read:any',
        'account:statement:any',
        'account:unblock_pin',
      ];
    case 'AUDITOR':
      return [
        'auth:me',
        'account:read:any',
        'account:read:gl_internal',
        'account:statement:any',
        'ledger:trial_balance',
        'audit_logs:read',
      ];
    case 'ADMIN':
      return [
        'auth:me',
        'customer:onboard',
        'account:read:any',
        'account:read:gl_internal',
        'account:statement:any',
        'account:unblock_pin',
        'transaction:transfer:any',
        'transaction:cash_deposit',
        'transaction:cash_withdrawal',
        'virtual_account:create',
        'virtual_account:inquiry',
        'virtual_account:pay',
        'ledger:trial_balance',
        'audit_logs:read',
      ];
  }
}
