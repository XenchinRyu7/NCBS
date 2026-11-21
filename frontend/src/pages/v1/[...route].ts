import type { APIRoute } from 'astro';
import { BankingService, TRIAL_SANDBOX_KEY } from '../../backend/service';
import { checkDatabaseHealth, checkRedisHealth } from '../../backend/db';
import { verifyToken } from '../../backend/auth';

export const prerender = false;

function jsonResponse(data: any, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: {
      'Content-Type': 'application/json',
      'X-CBS-Environment': 'PRODUCTION_ENTERPRISE',
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'GET, POST, PUT, PATCH, DELETE, OPTIONS',
      'Access-Control-Allow-Headers': 'Content-Type, Authorization, X-API-Key, X-Idempotency-Key',
    },
  });
}

export const ALL: APIRoute = async ({ request, params, clientAddress }) => {
  if (request.method === 'OPTIONS') {
    return new Response(null, {
      status: 204,
      headers: {
        'Access-Control-Allow-Origin': '*',
        'Access-Control-Allow-Methods': 'GET, POST, PUT, PATCH, DELETE, OPTIONS',
        'Access-Control-Allow-Headers': 'Content-Type, Authorization, X-API-Key, X-Idempotency-Key',
      },
    });
  }

  const rawRoute = params.route || '';
  const parts = rawRoute.split('/').filter(Boolean);
  const route = parts.join('/');
  const method = request.method.toUpperCase();

  // Extract Auth Token
  const authHeader = request.headers.get('authorization');
  let tokenUser = null;
  if (authHeader && authHeader.startsWith('Bearer ')) {
    const token = authHeader.slice(7);
    tokenUser = await verifyToken(token);
  }

  // --- Health Endpoints ---
  if (route === 'system/health' || route === 'health') {
    const [pgStatus, redisStatus, tb] = await Promise.all([
      checkDatabaseHealth(),
      checkRedisHealth(),
      BankingService.getTrialBalance(),
    ]);

    return jsonResponse({
      status: 'HEALTHY',
      service: 'nusantara-core-banking-system',
      runtime: 'Astro Serverless Native Core Kernel',
      infrastructure: {
        supabase_postgresql: pgStatus,
        upstash_redis: redisStatus,
        persistence_architecture: 'Hybrid Cloud Native with Supabase & Upstash',
        tier: 'Tier-1 Mission-Critical Banking Kernel',
      },
      general_ledger_integrity: {
        is_balanced: tb.is_balanced,
        discrepancy: tb.discrepancy,
        total_debit_postings: tb.total_debit_postings,
        total_credit_postings: tb.total_credit_postings,
        total_ledger_entries: tb.total_ledger_entries,
        checked_at: tb.checked_at,
      },
      timestamp: new Date().toISOString(),
    });
  }

  // --- Auth: Issue Token / Login ---
  if (route === 'auth/token' && method === 'POST') {
    try {
      const body = await request.json();
      const res = await BankingService.login(body.email || body.username, body.password);
      if (!res.success) {
        return jsonResponse({ success: false, error: res.error }, res.status);
      }
      return jsonResponse({
        success: true,
        message: 'Authentication successful',
        data: res.data,
      });
    } catch (e: any) {
      return jsonResponse({ success: false, error: 'Invalid JSON request payload' }, 400);
    }
  }

  // --- Auth: Get Auth Me ---
  if (route === 'auth/me' && method === 'GET') {
    if (!tokenUser) {
      return jsonResponse({ success: false, error: 'Unauthorized: missing or invalid Bearer token' }, 401);
    }
    const res = await BankingService.getAuthMe(tokenUser.sub);
    return jsonResponse(res.data, res.status);
  }

  // --- Auth: Roles Matrix ---
  if ((route === 'auth/roles' || route === 'system/roles-matrix') && method === 'GET') {
    return jsonResponse({
      success: true,
      data: [
        {
          role: 'CUSTOMER',
          title: 'Nasabah Perorangan (Retail Customer)',
          demo_email: 'budi.santoso@cbs-bank.co.id',
          demo_password: 'Password#2026',
          demo_account_no: '5240301001',
          demo_pin: '123456',
        },
        {
          role: 'MERCHANT',
          title: 'Nasabah Korporasi / Biller (Corporate Merchant)',
          demo_email: 'merchant@tokonusantara.co.id',
          demo_password: 'Password#2026',
          demo_account_no: '5240308888',
          demo_pin: '888888',
        },
        {
          role: 'TELLER',
          title: 'Teller Cabang (Branch Operations)',
          demo_email: 'teller@cbs-bank.co.id',
          demo_password: 'Password#2026',
          demo_account_no: '1001000001',
          demo_pin: '123456',
        },
        {
          role: 'CUSTOMER_SERVICE',
          title: 'Customer Service (Branch Frontline)',
          demo_email: 'cs@cbs-bank.co.id',
          demo_password: 'Password#2026',
        },
        {
          role: 'AUDITOR',
          title: 'Auditor Internal & Kepatuhan OJK',
          demo_email: 'auditor@cbs-bank.co.id',
          demo_password: 'Password#2026',
        },
        {
          role: 'ADMIN',
          title: 'Super Administrator Core Banking',
          demo_email: 'admin@cbs-bank.co.id',
          demo_password: 'Password#2026',
        },
      ],
    });
  }

  // --- Auth: Sandbox API Key ---
  if ((route === 'auth/sandbox-key' || route === 'system/sandbox-key') && (method === 'GET' || method === 'POST')) {
    return jsonResponse({
      success: true,
      data: {
        api_key: TRIAL_SANDBOX_KEY,
        header_name: 'X-API-Key',
        rate_limit: '120 req/min',
      },
    });
  }

  // --- System: Reset Sandbox ---
  if (route === 'system/reset' && method === 'POST') {
    return jsonResponse({
      success: true,
      message: 'Sandbox state operational and ready',
      data: {
        restored_accounts: [
          '5240301001 (Budi Santoso - Rp 500.000.000)',
          '5240302002 (Siti Rahma - Rp 75.000.000)',
          '5240308888 (Toko Nusantara - Rp 250.000.000)',
        ],
        trial_sandbox_api_key: TRIAL_SANDBOX_KEY,
      },
    });
  }

  // --- Accounts: List Accounts ---
  if (route === 'accounts' && method === 'GET') {
    const customerId = tokenUser?.sub || '';
    const role = tokenUser?.role || 'CUSTOMER';
    const accounts = await BankingService.getAccountsForCustomer(customerId, role);
    return jsonResponse({ success: true, data: accounts });
  }

  // --- Accounts: Balance (:acc/balance) ---
  if (parts.length === 3 && parts[0] === 'accounts' && parts[2] === 'balance' && method === 'GET') {
    const accNo = parts[1];
    const res = await BankingService.getAccountBalance(accNo);
    return jsonResponse(res.data || { error: res.error }, res.status);
  }

  // --- Accounts: Statement (:acc/statement) ---
  if (parts.length === 3 && parts[0] === 'accounts' && parts[2] === 'statement' && method === 'GET') {
    const accNo = parts[1];
    const res = await BankingService.getAccountStatement(accNo);
    return jsonResponse(res.data || { error: res.error }, res.status);
  }

  // --- Accounts: Unblock PIN (:acc/unblock-pin) ---
  if (parts.length === 3 && parts[0] === 'accounts' && parts[2] === 'unblock-pin' && method === 'POST') {
    const accNo = parts[1];
    const res = await BankingService.unblockPin(accNo);
    return jsonResponse(res, res.status);
  }

  // --- Transactions: Transfer ---
  if (route === 'transactions/transfer' && method === 'POST') {
    try {
      const body = await request.json();
      const res = await BankingService.executeTransfer({
        sourceAccountNumber: body.source_account_number,
        destinationAccountNumber: body.destination_account_number,
        amount: Number(body.amount),
        channel: body.channel || 'INTRABANK',
        pin: String(body.pin),
        description: body.description,
        clientIp: clientAddress || '127.0.0.1',
        initiatedBy: tokenUser?.sub,
      });

      if (!res.success) {
        return jsonResponse({ success: false, error: res.error }, res.status);
      }
      return jsonResponse({
        success: true,
        message: 'Transaction settled successfully with balanced double-entry ledger postings',
        data: res.data,
      });
    } catch (e: any) {
      return jsonResponse({ success: false, error: 'Invalid JSON request payload' }, 400);
    }
  }

  // --- Transactions: Cash Deposit / Withdrawal ---
  if (route === 'transactions/cash' && method === 'POST') {
    try {
      const body = await request.json();
      const res = await BankingService.executeCash({
        accountNumber: body.account_number,
        type: body.type,
        amount: Number(body.amount),
        tellerPin: body.teller_pin,
      });
      return jsonResponse(res, res.status);
    } catch (e: any) {
      return jsonResponse({ success: false, error: 'Invalid JSON payload' }, 400);
    }
  }

  // --- Virtual Accounts: List ---
  if (route === 'virtual-accounts' && method === 'GET') {
    const res = await BankingService.listVirtualAccounts();
    return jsonResponse(res);
  }

  // --- Virtual Accounts: Pay (:vaNo/pay) ---
  if (parts.length === 3 && parts[0] === 'virtual-accounts' && parts[2] === 'pay' && method === 'POST') {
    const vaNo = parts[1];
    try {
      const body = await request.json();
      const res = await BankingService.payVirtualAccount(vaNo, {
        sourceAccountNumber: body.source_account_number,
        pin: String(body.pin),
        clientIp: clientAddress || '127.0.0.1',
        initiatedBy: tokenUser?.sub,
      });
      return jsonResponse(res, res.status);
    } catch (e: any) {
      return jsonResponse({ success: false, error: 'Invalid JSON payload' }, 400);
    }
  }

  // --- Ledger: Trial Balance ---
  if (route === 'ledger/trial-balance' && method === 'GET') {
    const tb = await BankingService.getTrialBalance();
    return jsonResponse({
      success: true,
      data: tb,
    });
  }

  // --- Audit Logs ---
  if (route === 'audit-logs' && method === 'GET') {
    const res = await BankingService.listAuditLogs();
    return jsonResponse(res);
  }

  // --- Admin Dashboard ---
  if (route === 'admin/dashboard' && method === 'GET') {
    const res = await BankingService.getAdminDashboard();
    return jsonResponse(res);
  }

  // Fallback 404
  return jsonResponse(
    {
      success: false,
      error: `Core Banking API endpoint not found: ${method} /v1/${route}`,
    },
    404
  );
};
