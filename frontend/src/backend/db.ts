import postgres from 'postgres';

const DATABASE_URL =
  process.env.DATABASE_URL ||
  'postgresql://postgres.nqlfztdhaaayzhjmujjy:ncbspassword2026@aws-0-ap-southeast-2.pooler.supabase.com:5432/postgres?sslmode=require';

let sqlInstance: postgres.Sql | null = null;

export function getDb(): postgres.Sql {
  if (!sqlInstance) {
    sqlInstance = postgres(DATABASE_URL, {
      ssl: { rejectUnauthorized: false },
      max: 10,
      idle_timeout: 20,
      connect_timeout: 10,
    });
  }
  return sqlInstance;
}

export function getRedisConfig(): { url: string; token: string } | null {
  let restUrl = process.env.UPSTASH_REDIS_REST_URL;
  let restToken = process.env.UPSTASH_REDIS_REST_TOKEN;

  if (!restUrl && process.env.UPSTASH_REDIS_URL) {
    try {
      const u = new URL(process.env.UPSTASH_REDIS_URL);
      restUrl = `https://${u.hostname}`;
      restToken = u.password;
    } catch (e) {
      // fallback
    }
  }

  if (restUrl && restToken) {
    return { url: restUrl, token: restToken };
  }

  // Built-in verified credentials fallback
  return {
    url: 'https://superb-starfish-43445.upstash.io',
    token: 'Aam1AAIgcDE3NmQwZDM2YWNhMzM0ZGZmODI1NzQ3NGM1YjZhMzA1NA',
  };
}

export async function checkDatabaseHealth(): Promise<'ONLINE_CONNECTED' | 'CONNECTION_DEGRADED'> {
  try {
    const sql = getDb();
    const res = await sql`SELECT 1 as alive`;
    return res && res.length > 0 ? 'ONLINE_CONNECTED' : 'CONNECTION_DEGRADED';
  } catch (err) {
    console.warn('[DB] Supabase ping failed:', err);
    return 'CONNECTION_DEGRADED';
  }
}

export async function checkRedisHealth(): Promise<'ONLINE_CONNECTED' | 'CONNECTION_DEGRADED'> {
  try {
    const cfg = getRedisConfig();
    if (!cfg) return 'CONNECTION_DEGRADED';
    const res = await fetch(`${cfg.url}/ping`, {
      headers: { Authorization: `Bearer ${cfg.token}` },
    });
    if (!res.ok) return 'CONNECTION_DEGRADED';
    const data = await res.json();
    return data && data.result === 'PONG' ? 'ONLINE_CONNECTED' : 'CONNECTION_DEGRADED';
  } catch (err) {
    console.warn('[Redis] Upstash ping failed:', err);
    return 'CONNECTION_DEGRADED';
  }
}
