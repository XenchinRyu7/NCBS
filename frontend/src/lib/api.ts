export const BACKEND_URL =
  import.meta.env.BACKEND_URL ||
  (typeof process !== 'undefined' && process.env.BACKEND_URL) ||
  '';

export interface TokenResponse {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
  cif_number: string;
  full_name: string;
  email: string;
  role: string;
  permissions: string[];
  accounts: Account[];
}

export interface Account {
  id: string;
  account_number: string;
  customer_id: string;
  owner_name?: string;
  account_type: string;
  card_tier: string;
  currency: string;
  balance: number;
  available_balance: number;
  hold_balance: number;
  daily_limit: number;
  pin_failed_attempts: number;
  status: string;
  version: number;
  created_at: string;
}

export interface Transaction {
  id: string;
  reference_no: string;
  source_account_number?: string;
  destination_account_number?: string;
  channel: string;
  amount: number;
  amount_formatted: string;
  fee_amount: number;
  total_debit: number;
  total_debit_formatted: string;
  description: string;
  status: string;
  settled_at: string;
}

export interface LedgerPost {
  id: string;
  transaction_id: string;
  account_number: string;
  direction: 'DEBIT' | 'CREDIT';
  amount: number;
  amount_formatted: string;
  balance_before: number;
  balance_after: number;
  narration: string;
  created_at: string;
}

export interface VirtualAccount {
  id: string;
  va_number: string;
  merchant_name: string;
  merchant_account_number: string;
  customer_name: string;
  bill_amount: number;
  bill_amount_formatted: string;
  currency: string;
  description: string;
  status: 'OPEN' | 'PAID' | 'EXPIRED' | 'CANCELLED';
  expires_at: string;
  paid_at?: string;
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
  action: string;
  resource_name: string;
  resource_id: string;
  ip_address: string;
  trace_id: string;
  payload_snapshot?: any;
  created_at: string;
}

export async function apiCall<T>(
  path: string,
  options: RequestInit & { token?: string } = {}
): Promise<{ ok: boolean; status: number; data?: T; error?: any }> {
  const url = `${BACKEND_URL}${path}`;
  const headers = new Headers(options.headers || {});

  if (!headers.has('Content-Type') && options.body) {
    headers.set('Content-Type', 'application/json');
  }

  if (options.token) {
    headers.set('Authorization', `Bearer ${options.token}`);
  }

  try {
    const res = await fetch(url, {
      ...options,
      headers,
    });

    const contentType = res.headers.get('content-type');
    let parsed: any = null;
    if (contentType && contentType.includes('application/json')) {
      parsed = await res.json();
    } else {
      parsed = await res.text();
    }

    if (!res.ok) {
      return { ok: false, status: res.status, error: parsed };
    }

    // Unpack data if wrapped in ApiResponse
    const data = parsed?.data !== undefined ? parsed.data : parsed;
    return { ok: true, status: res.status, data };
  } catch (err: any) {
    return { ok: false, status: 500, error: { message: err?.message || 'Network error connecting to CBS backend' } };
  }
}
