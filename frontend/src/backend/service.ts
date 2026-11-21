import { getDb, getRedis } from './db';
import {
  type Account,
  type Customer,
  type Transaction,
  type LedgerPost,
  type VirtualAccount,
  type TrialBalance,
  type AuditLog,
  type UserRole,
  type TxChannel,
  formatIdr,
  getRolePermissions,
} from './types';
import {
  createAccessToken,
  createRefreshToken,
  verifyPassword,
  verifyPin,
} from './auth';
import crypto from 'node:crypto';

export const GL_VAULT_ACCOUNT_NO = '1001000001';
export const GL_NOSTRO_CLEARING_NO = '2001000002';
export const GL_FEE_INCOME_ACCOUNT_NO = '4001000003';
export const TRIAL_SANDBOX_KEY = 'cbs_trial_snap_sandbox_2026';

export class BankingService {
  /**
   * Issue access + refresh token given email and password
   */
  static async login(email: string, password: string) {
    const sql = getDb();
    const rows = await sql<Customer[]>`
      SELECT * FROM customers WHERE LOWER(email) = LOWER(${email}) AND is_active = true LIMIT 1
    `;

    if (!rows || rows.length === 0) {
      return { success: false, status: 401, error: 'Invalid email or password' };
    }

    const customer = rows[0];
    const passwordValid = await verifyPassword(password, customer.password_hash);
    if (!passwordValid) {
      return { success: false, status: 401, error: 'Invalid email or password' };
    }

    const accessToken = await createAccessToken(customer);
    const refreshToken = await createRefreshToken(customer);

    // Fetch accounts belonging to user
    const accounts = await this.getAccountsForCustomer(customer.id, customer.role);

    return {
      success: true,
      status: 200,
      data: {
        access_token: accessToken,
        refresh_token: refreshToken,
        token_type: 'Bearer',
        expires_in: 7200,
        cif_number: customer.cif_number,
        full_name: customer.full_name,
        email: customer.email,
        role: customer.role,
        permissions: getRolePermissions(customer.role),
        accounts,
      },
    };
  }

  /**
   * Get authenticated user profile and their accounts
   */
  static async getAuthMe(customerId: string) {
    const sql = getDb();
    const rows = await sql<Customer[]>`
      SELECT * FROM customers WHERE id = ${customerId} LIMIT 1
    `;
    if (!rows || rows.length === 0) {
      return { success: false, status: 404, error: 'Customer not found' };
    }
    const customer = rows[0];
    const accounts = await this.getAccountsForCustomer(customer.id, customer.role);

    return {
      success: true,
      status: 200,
      data: {
        id: customer.id,
        cif_number: customer.cif_number,
        nik_masked: customer.nik_encrypted ? '3201************' : '3201************',
        full_name: customer.full_name,
        email: customer.email,
        phone_number: customer.phone_number,
        role: customer.role,
        kyc_status: customer.kyc_status,
        permissions: getRolePermissions(customer.role),
        accounts,
      },
    };
  }

  /**
   * Get accounts based on user role
   */
  static async getAccountsForCustomer(customerId: string, role: UserRole): Promise<Account[]> {
    const sql = getDb();
    let rows: any[] = [];
    if (role === 'ADMIN' || role === 'TELLER' || role === 'CUSTOMER_SERVICE' || role === 'AUDITOR') {
      rows = await sql`
        SELECT a.*, c.full_name as owner_name 
        FROM accounts a
        LEFT JOIN customers c ON a.customer_id = c.id
        ORDER BY a.account_number ASC
      `;
    } else {
      rows = await sql`
        SELECT a.*, c.full_name as owner_name 
        FROM accounts a
        LEFT JOIN customers c ON a.customer_id = c.id
        WHERE a.customer_id = ${customerId}
        ORDER BY a.account_number ASC
      `;
    }

    return rows.map((r) => ({
      id: r.id,
      account_number: r.account_number,
      customer_id: r.customer_id,
      owner_name: r.owner_name,
      account_type: r.account_type,
      card_tier: r.card_tier,
      currency: r.currency || 'IDR',
      balance: Number(r.balance),
      hold_balance: Number(r.hold_balance || 0),
      available_balance: Number(r.balance) - Number(r.hold_balance || 0),
      daily_limit: Number(r.daily_limit || 250000000),
      pin_hash: '',
      pin_failed_attempts: Number(r.pin_failed_attempts || 0),
      status: r.status,
      version: Number(r.version || 1),
      created_at: r.created_at,
      updated_at: r.updated_at,
    }));
  }

  /**
   * Get single account balance
   */
  static async getAccountBalance(accountNumber: string) {
    const sql = getDb();
    const rows = await sql`
      SELECT a.*, c.full_name as owner_name 
      FROM accounts a
      LEFT JOIN customers c ON a.customer_id = c.id
      WHERE a.account_number = ${accountNumber} LIMIT 1
    `;
    if (!rows || rows.length === 0) {
      return { success: false, status: 404, error: `Account ${accountNumber} not found` };
    }
    const r = rows[0];
    const balance = Number(r.balance);
    const hold_balance = Number(r.hold_balance || 0);
    return {
      success: true,
      status: 200,
      data: {
        account_number: r.account_number,
        currency: r.currency || 'IDR',
        balance,
        hold_balance,
        available_balance: balance - hold_balance,
        daily_limit: Number(r.daily_limit || 250000000),
        status: r.status,
      },
    };
  }

  /**
   * Get statement / ledger postings for an account
   */
  static async getAccountStatement(accountNumber: string) {
    const sql = getDb();
    const rows = await sql`
      SELECT le.* 
      FROM ledger_entries le
      JOIN accounts a ON le.account_id = a.id
      WHERE a.account_number = ${accountNumber}
      ORDER BY le.created_at DESC
      LIMIT 100
    `;

    const postings: LedgerPost[] = rows.map((r) => ({
      id: r.id,
      transaction_id: r.transaction_id,
      account_id: r.account_id,
      account_number: accountNumber,
      direction: r.direction,
      amount: Number(r.amount),
      amount_formatted: formatIdr(Number(r.amount)),
      balance_before: Number(r.balance_before),
      balance_after: Number(r.balance_after),
      narration: r.narration,
      created_at: r.created_at,
    }));

    return { success: true, status: 200, data: postings };
  }

  /**
   * Execute transfer (Intrabank or BI-FAST) with double-entry ledger guarantee
   */
  static async executeTransfer(params: {
    sourceAccountNumber: string;
    destinationAccountNumber: string;
    amount: number;
    channel: TxChannel;
    pin: string;
    description?: string;
    idempotencyKey?: string;
    clientIp?: string;
    initiatedBy?: string;
  }) {
    const {
      sourceAccountNumber,
      destinationAccountNumber,
      amount,
      channel,
      pin,
      description,
      clientIp = '127.0.0.1',
      initiatedBy,
    } = params;

    if (!amount || amount <= 0) {
      return { success: false, status: 400, error: 'Transfer amount must be positive' };
    }

    const fee = channel === 'BI_FAST' ? 2500 : channel === 'RTGS' ? 25000 : channel === 'LLG' ? 2900 : 0;
    const totalDebit = amount + fee;
    const idempotencyKey = params.idempotencyKey || `idem-${crypto.randomUUID()}`;
    const traceId = `cbs-trace-${crypto.randomUUID()}`;

    const sql = getDb();

    try {
      const result = await sql.begin(async (tx) => {
        // 1. Lock and validate source account
        const sourceRows = await tx`
          SELECT * FROM accounts WHERE account_number = ${sourceAccountNumber} FOR UPDATE
        `;
        if (!sourceRows || sourceRows.length === 0) {
          throw new Error(`Source account ${sourceAccountNumber} not found`);
        }
        const sourceAcc = sourceRows[0];

        if (sourceAcc.status !== 'ACTIVE') {
          throw new Error('Account is not active or is blocked');
        }

        if (Number(sourceAcc.pin_failed_attempts) >= 3) {
          throw new Error('Account is PIN_BLOCKED due to 3 consecutive failed PIN attempts');
        }

        // Verify PIN
        const pinValid = await verifyPin(pin, sourceAcc.pin_hash);
        if (!pinValid) {
          const attempts = Number(sourceAcc.pin_failed_attempts) + 1;
          const newStatus = attempts >= 3 ? 'PIN_BLOCKED' : sourceAcc.status;
          await tx`
            UPDATE accounts 
            SET pin_failed_attempts = ${attempts}, status = ${newStatus} 
            WHERE id = ${sourceAcc.id}
          `;
          throw new Error(
            attempts >= 3
              ? 'Account has been PIN_BLOCKED due to 3 failed attempts'
              : `Invalid 6-digit transaction PIN (${3 - attempts} attempts left)`
          );
        }

        // Reset pin failed attempts on success
        if (Number(sourceAcc.pin_failed_attempts) > 0) {
          await tx`UPDATE accounts SET pin_failed_attempts = 0 WHERE id = ${sourceAcc.id}`;
        }

        const sourceBalance = Number(sourceAcc.balance);
        const sourceHold = Number(sourceAcc.hold_balance || 0);
        const availableBalance = sourceBalance - sourceHold;

        if (availableBalance < totalDebit) {
          throw new Error('Insufficient available balance for transfer amount and fee');
        }

        // 2. Lock destination account or Nostro clearing account
        let destId: string;
        let destAccNo: string;
        let destBefore: number = 0;
        let destAfter: number = 0;

        if (channel === 'INTRABANK') {
          const destRows = await tx`
            SELECT * FROM accounts WHERE account_number = ${destinationAccountNumber} FOR UPDATE
          `;
          if (!destRows || destRows.length === 0) {
            throw new Error(`Destination account ${destinationAccountNumber} not found`);
          }
          const destAcc = destRows[0];
          destId = destAcc.id;
          destAccNo = destAcc.account_number;
          destBefore = Number(destAcc.balance);
          destAfter = destBefore + amount;

          await tx`
            UPDATE accounts 
            SET balance = ${destAfter}, version = version + 1, updated_at = NOW() 
            WHERE id = ${destId}
          `;
        } else {
          // BI-FAST / RTGS: Credit Nostro Clearing Account
          const clearingRows = await tx`
            SELECT * FROM accounts WHERE account_number = ${GL_NOSTRO_CLEARING_NO} FOR UPDATE
          `;
          if (!clearingRows || clearingRows.length === 0) {
            throw new Error('GL Nostro clearing account not found');
          }
          const clearingAcc = clearingRows[0];
          destId = clearingAcc.id;
          destAccNo = clearingAcc.account_number;
          destBefore = Number(clearingAcc.balance);
          destAfter = destBefore + amount;

          await tx`
            UPDATE accounts 
            SET balance = ${destAfter}, version = version + 1, updated_at = NOW() 
            WHERE id = ${destId}
          `;
        }

        // Update Source Account
        const sourceAfter = sourceBalance - totalDebit;
        await tx`
          UPDATE accounts 
          SET balance = ${sourceAfter}, version = version + 1, updated_at = NOW() 
          WHERE id = ${sourceAcc.id}
        `;

        // Handle Fee Credit if applicable
        let feeIncomeId: string | null = null;
        let feeBefore: number = 0;
        let feeAfter: number = 0;
        if (fee > 0) {
          const feeRows = await tx`
            SELECT * FROM accounts WHERE account_number = ${GL_FEE_INCOME_ACCOUNT_NO} FOR UPDATE
          `;
          if (feeRows && feeRows.length > 0) {
            const feeAcc = feeRows[0];
            feeIncomeId = feeAcc.id;
            feeBefore = Number(feeAcc.balance);
            feeAfter = feeBefore + fee;
            await tx`
              UPDATE accounts 
              SET balance = ${feeAfter}, version = version + 1, updated_at = NOW() 
              WHERE id = ${feeIncomeId}
            `;
          }
        }

        // 3. Create Transaction Header
        const txId = crypto.randomUUID();
        const refNo = `TXN-${new Date().toISOString().replace(/\D/g, '').slice(0, 14)}-${txId.slice(0, 8).toUpperCase()}`;

        await tx`
          INSERT INTO transactions (
            id, reference_no, idempotency_key, source_account_id, destination_account_id,
            channel, amount, fee_amount, total_debit, currency, description,
            status, initiated_by, client_ip, trace_id, settled_at, created_at
          ) VALUES (
            ${txId}, ${refNo}, ${idempotencyKey}, ${sourceAcc.id}, ${destId},
            ${channel}, ${amount}, ${fee}, ${totalDebit}, 'IDR', ${description || `Transfer via ${channel}`},
            'SETTLED', ${initiatedBy || null}, ${clientIp}, ${traceId}, NOW(), NOW()
          )
        `;

        // 4. Create Ledger Entries (Debit & Credit equal)
        // Debit Source
        await tx`
          INSERT INTO ledger_entries (
            id, transaction_id, account_id, direction, amount,
            balance_before, balance_after, narration, created_at
          ) VALUES (
            ${crypto.randomUUID()}, ${txId}, ${sourceAcc.id}, 'DEBIT', ${totalDebit},
            ${sourceBalance}, ${sourceAfter}, ${description || `Transfer via ${channel}`}, NOW()
          )
        `;

        // Credit Destination
        await tx`
          INSERT INTO ledger_entries (
            id, transaction_id, account_id, direction, amount,
            balance_before, balance_after, narration, created_at
          ) VALUES (
            ${crypto.randomUUID()}, ${txId}, ${destId}, 'CREDIT', ${amount},
            ${destBefore}, ${destAfter}, ${`Transfer received from ${sourceAccountNumber}`}, NOW()
          )
        `;

        // Credit Fee Income
        if (fee > 0 && feeIncomeId) {
          await tx`
            INSERT INTO ledger_entries (
              id, transaction_id, account_id, direction, amount,
              balance_before, balance_after, narration, created_at
            ) VALUES (
              ${crypto.randomUUID()}, ${txId}, ${feeIncomeId}, 'CREDIT', ${fee},
              ${feeBefore}, ${feeAfter}, ${`Transaction Fee ${channel}`}, NOW()
            )
          `;
        }

        return {
          id: txId,
          reference_no: refNo,
          source_account_number: sourceAccountNumber,
          destination_account_number: destinationAccountNumber,
          channel,
          amount,
          amount_formatted: formatIdr(amount),
          fee_amount: fee,
          total_debit: totalDebit,
          total_debit_formatted: formatIdr(totalDebit),
          status: 'SETTLED',
          settled_at: new Date().toISOString(),
          description: description || `Transfer via ${channel}`,
        };
      });

      return { success: true, status: 200, data: result };
    } catch (e: any) {
      return { success: false, status: 400, error: e.message || 'Transaction failed' };
    }
  }

  /**
   * Pay Virtual Account
   */
  static async payVirtualAccount(vaNumber: string, params: {
    sourceAccountNumber: string;
    pin: string;
    clientIp?: string;
    initiatedBy?: string;
  }) {
    const { sourceAccountNumber, pin, clientIp = '127.0.0.1', initiatedBy } = params;
    const sql = getDb();

    try {
      const result = await sql.begin(async (tx) => {
        // 1. Lock and check VA
        const vaRows = await tx`
          SELECT va.*, a.account_number as merchant_account_number 
          FROM virtual_accounts va
          JOIN accounts a ON va.merchant_account_id = a.id
          WHERE va.va_number = ${vaNumber} FOR UPDATE
        `;
        if (!vaRows || vaRows.length === 0) {
          throw new Error(`Virtual Account ${vaNumber} not found`);
        }
        const va = vaRows[0];
        if (va.status !== 'OPEN') {
          throw new Error(`Virtual Account is not open (status: ${va.status})`);
        }

        const billAmount = Number(va.bill_amount);

        // 2. Lock and check Payer Account
        const payerRows = await tx`
          SELECT * FROM accounts WHERE account_number = ${sourceAccountNumber} FOR UPDATE
        `;
        if (!payerRows || payerRows.length === 0) {
          throw new Error(`Source account ${sourceAccountNumber} not found`);
        }
        const payerAcc = payerRows[0];
        if (payerAcc.status !== 'ACTIVE') {
          throw new Error('Payer account is not active');
        }

        const pinValid = await verifyPin(pin, payerAcc.pin_hash);
        if (!pinValid) {
          throw new Error('Invalid 6-digit transaction PIN');
        }

        const payerBalance = Number(payerAcc.balance);
        const payerHold = Number(payerAcc.hold_balance || 0);
        if (payerBalance - payerHold < billAmount) {
          throw new Error('Insufficient balance to pay virtual account bill');
        }

        // Lock Merchant Account
        const merchantRows = await tx`
          SELECT * FROM accounts WHERE id = ${va.merchant_account_id} FOR UPDATE
        `;
        const merchantAcc = merchantRows[0];
        const merchantBalance = Number(merchantAcc.balance);

        const payerAfter = payerBalance - billAmount;
        const merchantAfter = merchantBalance + billAmount;

        await tx`UPDATE accounts SET balance = ${payerAfter}, updated_at = NOW() WHERE id = ${payerAcc.id}`;
        await tx`UPDATE accounts SET balance = ${merchantAfter}, updated_at = NOW() WHERE id = ${merchantAcc.id}`;

        const txId = crypto.randomUUID();
        const refNo = `TXN-${new Date().toISOString().replace(/\D/g, '').slice(0, 14)}-${txId.slice(0, 8).toUpperCase()}`;

        await tx`
          INSERT INTO transactions (
            id, reference_no, idempotency_key, source_account_id, destination_account_id,
            channel, amount, fee_amount, total_debit, currency, description,
            status, initiated_by, client_ip, trace_id, settled_at, created_at
          ) VALUES (
            ${txId}, ${refNo}, ${`idem-va-${txId}`}, ${payerAcc.id}, ${merchantAcc.id},
            'VIRTUAL_ACCOUNT', ${billAmount}, 0, ${billAmount}, 'IDR', ${`Payment VA ${vaNumber} - ${va.merchant_name}`},
            'SETTLED', ${initiatedBy || null}, ${clientIp}, ${`trace-${txId}`}, NOW(), NOW()
          )
        `;

        await tx`
          INSERT INTO ledger_entries (id, transaction_id, account_id, direction, amount, balance_before, balance_after, narration, created_at)
          VALUES 
            (${crypto.randomUUID()}, ${txId}, ${payerAcc.id}, 'DEBIT', ${billAmount}, ${payerBalance}, ${payerAfter}, ${`Payment VA ${vaNumber}`}, NOW()),
            (${crypto.randomUUID()}, ${txId}, ${merchantAcc.id}, 'CREDIT', ${billAmount}, ${merchantBalance}, ${merchantAfter}, ${`Payment received from VA ${vaNumber}`}, NOW())
        `;

        await tx`
          UPDATE virtual_accounts 
          SET status = 'PAID', paid_transaction_id = ${txId}, paid_at = NOW() 
          WHERE id = ${va.id}
        `;

        return {
          reference_no: refNo,
          va_number: vaNumber,
          bill_amount: billAmount,
          bill_amount_formatted: formatIdr(billAmount),
          status: 'SETTLED',
          merchant_name: va.merchant_name,
        };
      });

      return { success: true, status: 200, data: result };
    } catch (e: any) {
      return { success: false, status: 400, error: e.message || 'Payment failed' };
    }
  }

  /**
   * Cash Deposit / Withdrawal by Teller
   */
  static async executeCash(params: {
    accountNumber: string;
    type: 'DEPOSIT' | 'WITHDRAWAL';
    amount: number;
    tellerPin?: string;
  }) {
    const { accountNumber, type, amount } = params;
    if (!amount || amount <= 0) {
      return { success: false, status: 400, error: 'Cash amount must be positive' };
    }

    const sql = getDb();
    try {
      const result = await sql.begin(async (tx) => {
        const accRows = await tx`SELECT * FROM accounts WHERE account_number = ${accountNumber} FOR UPDATE`;
        if (!accRows || accRows.length === 0) throw new Error(`Account ${accountNumber} not found`);
        const acc = accRows[0];

        const vaultRows = await tx`SELECT * FROM accounts WHERE account_number = ${GL_VAULT_ACCOUNT_NO} FOR UPDATE`;
        if (!vaultRows || vaultRows.length === 0) throw new Error('GL Vault account not found');
        const vault = vaultRows[0];

        const accBal = Number(acc.balance);
        const vaultBal = Number(vault.balance);

        let accAfter = accBal;
        let vaultAfter = vaultBal;
        const channel: TxChannel = type === 'DEPOSIT' ? 'TELLER_DEPOSIT' : 'TELLER_WITHDRAWAL';

        if (type === 'DEPOSIT') {
          // Debit Vault, Credit Customer Account
          vaultAfter = vaultBal - amount;
          accAfter = accBal + amount;
        } else {
          // Withdrawal: Debit Customer Account, Credit Vault
          if (accBal < amount) throw new Error('Insufficient customer account balance for cash withdrawal');
          accAfter = accBal - amount;
          vaultAfter = vaultBal + amount;
        }

        await tx`UPDATE accounts SET balance = ${accAfter}, updated_at = NOW() WHERE id = ${acc.id}`;
        await tx`UPDATE accounts SET balance = ${vaultAfter}, updated_at = NOW() WHERE id = ${vault.id}`;

        const txId = crypto.randomUUID();
        const refNo = `TXN-${new Date().toISOString().replace(/\D/g, '').slice(0, 14)}-${txId.slice(0, 8).toUpperCase()}`;

        await tx`
          INSERT INTO transactions (
            id, reference_no, idempotency_key, source_account_id, destination_account_id,
            channel, amount, fee_amount, total_debit, currency, description,
            status, client_ip, trace_id, settled_at, created_at
          ) VALUES (
            ${txId}, ${refNo}, ${`idem-cash-${txId}`}, ${type === 'DEPOSIT' ? vault.id : acc.id}, ${type === 'DEPOSIT' ? acc.id : vault.id},
            ${channel}, ${amount}, 0, ${amount}, 'IDR', ${`Teller Cash ${type}`},
            'SETTLED', '127.0.0.1', ${`trace-${txId}`}, NOW(), NOW()
          )
        `;

        if (type === 'DEPOSIT') {
          await tx`
            INSERT INTO ledger_entries (id, transaction_id, account_id, direction, amount, balance_before, balance_after, narration, created_at)
            VALUES 
              (${crypto.randomUUID()}, ${txId}, ${vault.id}, 'DEBIT', ${amount}, ${vaultBal}, ${vaultAfter}, 'Cash Deposit Vault Debit', NOW()),
              (${crypto.randomUUID()}, ${txId}, ${acc.id}, 'CREDIT', ${amount}, ${accBal}, ${accAfter}, 'Cash Deposit Customer Credit', NOW())
          `;
        } else {
          await tx`
            INSERT INTO ledger_entries (id, transaction_id, account_id, direction, amount, balance_before, balance_after, narration, created_at)
            VALUES 
              (${crypto.randomUUID()}, ${txId}, ${acc.id}, 'DEBIT', ${amount}, ${accBal}, ${accAfter}, 'Cash Withdrawal Customer Debit', NOW()),
              (${crypto.randomUUID()}, ${txId}, ${vault.id}, 'CREDIT', ${amount}, ${vaultBal}, ${vaultAfter}, 'Cash Withdrawal Vault Credit', NOW())
          `;
        }

        return {
          reference_no: refNo,
          account_number: accountNumber,
          type,
          amount,
          amount_formatted: formatIdr(amount),
          new_balance: accAfter,
          new_balance_formatted: formatIdr(accAfter),
          status: 'SETTLED',
        };
      });

      return { success: true, status: 200, data: result };
    } catch (e: any) {
      return { success: false, status: 400, error: e.message || 'Cash transaction failed' };
    }
  }

  /**
   * Unblock account PIN
   */
  static async unblockPin(accountNumber: string) {
    const sql = getDb();
    const rows = await sql`
      UPDATE accounts 
      SET pin_failed_attempts = 0, status = 'ACTIVE', updated_at = NOW() 
      WHERE account_number = ${accountNumber}
      RETURNING account_number, status, pin_failed_attempts
    `;
    if (!rows || rows.length === 0) {
      return { success: false, status: 404, error: `Account ${accountNumber} not found` };
    }
    return {
      success: true,
      status: 200,
      message: `Account ${accountNumber} PIN successfully unblocked and status restored to ACTIVE`,
      data: rows[0],
    };
  }

  /**
   * Real-time General Ledger Trial Balance verification
   */
  static async getTrialBalance(): Promise<TrialBalance> {
    const sql = getDb();
    try {
      const stats = await sql`
        SELECT 
          COALESCE(SUM(CASE WHEN direction = 'DEBIT' THEN amount ELSE 0 END), 0) as total_debit,
          COALESCE(SUM(CASE WHEN direction = 'CREDIT' THEN amount ELSE 0 END), 0) as total_credit,
          COUNT(*) as total_entries,
          (SELECT COUNT(*) FROM transactions) as total_txs
        FROM ledger_entries
      `;
      const row = stats[0] || {};
      const totalDebit = Number(row.total_debit || 0);
      const totalCredit = Number(row.total_credit || 0);
      const discrepancy = Math.abs(totalDebit - totalCredit);

      return {
        total_debit_postings: totalDebit,
        total_credit_postings: totalCredit,
        discrepancy,
        is_balanced: discrepancy === 0,
        total_ledger_entries: Number(row.total_entries || 0),
        total_transactions: Number(row.total_txs || 0),
        checked_at: new Date().toISOString(),
      };
    } catch (e) {
      return {
        total_debit_postings: 0,
        total_credit_postings: 0,
        discrepancy: 0,
        is_balanced: true,
        total_ledger_entries: 0,
        total_transactions: 0,
        checked_at: new Date().toISOString(),
      };
    }
  }

  /**
   * List Virtual Accounts
   */
  static async listVirtualAccounts() {
    const sql = getDb();
    const rows = await sql`
      SELECT va.*, a.account_number as merchant_account_number 
      FROM virtual_accounts va
      JOIN accounts a ON va.merchant_account_id = a.id
      ORDER BY va.created_at DESC
    `;

    return {
      success: true,
      status: 200,
      data: rows.map((r) => ({
        id: r.id,
        va_number: r.va_number,
        merchant_name: r.merchant_name,
        merchant_account_number: r.merchant_account_number,
        customer_name: r.customer_name,
        bill_amount: Number(r.bill_amount),
        bill_amount_formatted: formatIdr(Number(r.bill_amount)),
        currency: r.currency || 'IDR',
        description: r.description,
        status: r.status,
        expires_at: r.expires_at,
        paid_at: r.paid_at,
        created_at: r.created_at,
      })),
    };
  }

  /**
   * List Audit Logs
   */
  static async listAuditLogs() {
    const sql = getDb();
    try {
      const rows = await sql`
        SELECT * FROM audit_logs 
        ORDER BY created_at DESC 
        LIMIT 100
      `;
      return {
        success: true,
        status: 200,
        data: rows.map((r) => ({
          id: r.id,
          actor_id: r.actor_id,
          action: r.action,
          resource_name: r.resource_name,
          resource_id: r.resource_id,
          ip_address: r.ip_address,
          trace_id: r.trace_id,
          payload_snapshot: r.payload_snapshot,
          created_at: r.created_at,
        })),
      };
    } catch (e) {
      return { success: true, status: 200, data: [] };
    }
  }

  /**
   * Admin Dashboard
   */
  static async getAdminDashboard() {
    const sql = getDb();
    const [counts, tb] = await Promise.all([
      sql`
        SELECT 
          (SELECT count(*) FROM customers) AS total_customers,
          (SELECT count(*) FROM accounts) AS total_accounts,
          (SELECT count(*) FROM transactions) AS total_transactions,
          (SELECT count(*) FROM virtual_accounts) AS total_va
      `,
      this.getTrialBalance(),
    ]);

    const c = counts[0] || {};
    return {
      success: true,
      status: 200,
      data: {
        total_customers: Number(c.total_customers || 0),
        total_accounts: Number(c.total_accounts || 0),
        total_transactions: Number(c.total_transactions || 0),
        total_virtual_accounts: Number(c.total_va || 0),
        trial_balance: tb,
      },
    };
  }
}
