import type { APIRoute } from 'astro';
import { checkDatabaseHealth, checkRedisHealth } from '../backend/db';
import { BankingService } from '../backend/service';

export const prerender = false;

export const GET: APIRoute = async () => {
  const [pgStatus, redisStatus, tb] = await Promise.all([
    checkDatabaseHealth(),
    checkRedisHealth(),
    BankingService.getTrialBalance(),
  ]);

  return new Response(
    JSON.stringify({
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
    }),
    {
      status: 200,
      headers: {
        'Content-Type': 'application/json',
        'X-CBS-Environment': 'PRODUCTION_ENTERPRISE',
      },
    }
  );
};
