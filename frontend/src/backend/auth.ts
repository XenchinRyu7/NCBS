import { SignJWT, jwtVerify } from 'jose';
import bcrypt from 'bcryptjs';
import type { Customer, UserRole } from './types';
import { getRolePermissions } from './types';

const JWT_SECRET_RAW =
  process.env.JWT_SECRET ||
  'change-this-to-a-random-64-character-secret-key-for-production-use';
const SECRET_KEY = new TextEncoder().encode(JWT_SECRET_RAW);

export interface TokenClaims {
  sub: string; // customer_id
  email: string;
  role: UserRole;
  cif: string;
  name: string;
  type: 'access' | 'refresh';
}

export async function createAccessToken(customer: Customer): Promise<string> {
  return await new SignJWT({
    email: customer.email,
    role: customer.role,
    cif: customer.cif_number,
    name: customer.full_name,
    type: 'access',
  })
    .setProtectedHeader({ alg: 'HS256' })
    .setSubject(customer.id)
    .setIssuedAt()
    .setExpirationTime('2h')
    .sign(SECRET_KEY);
}

export async function createRefreshToken(customer: Customer): Promise<string> {
  return await new SignJWT({
    email: customer.email,
    role: customer.role,
    cif: customer.cif_number,
    name: customer.full_name,
    type: 'refresh',
  })
    .setProtectedHeader({ alg: 'HS256' })
    .setSubject(customer.id)
    .setIssuedAt()
    .setExpirationTime('7d')
    .sign(SECRET_KEY);
}

export async function verifyToken(token: string): Promise<TokenClaims | null> {
  try {
    const { payload } = await jwtVerify(token, SECRET_KEY);
    return {
      sub: payload.sub as string,
      email: payload.email as string,
      role: payload.role as UserRole,
      cif: payload.cif as string,
      name: payload.name as string,
      type: payload.type as 'access' | 'refresh',
    };
  } catch (e) {
    return null;
  }
}

export async function verifyPassword(plain: string, hash: string): Promise<boolean> {
  try {
    return await bcrypt.compare(plain, hash);
  } catch (e) {
    return false;
  }
}

export async function verifyPin(plain: string, hash: string): Promise<boolean> {
  try {
    return await bcrypt.compare(plain, hash);
  } catch (e) {
    return false;
  }
}
