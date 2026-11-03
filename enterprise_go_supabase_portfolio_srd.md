# Nusantara Core Banking System (CBS) — Technical Requirement & Production Specification

- **System Classification:** Tier-1 Core Banking Ledger & SNAP-BI Open Banking API
- **Engine Target:** Go (Golang 1.22+) Serverless & High-Concurrency Runtime
- **Transactional Database:** Supabase (PostgreSQL 15+ ACID Compliant Engine)
- **Distributed Lock & Velocity Engine:** Upstash Redis (Lua Scripting & Atomic Locks)
- **Deployment Platform:** Vercel Go Runtime / Containerized Cloud Run Ready
- **API Standard:** OpenAPI 3.1 / Scalar UI / RFC 7807 Problem Details

---

## 1. Architectural Vision & Banking Engineering Invariants

Sistem ini dirancang dengan standar arsitektur **Enterprise Core Banking System (CBS)** Tier-1 dan standar **SNAP BI (Standar Nasional Open API Pembayaran Bank Indonesia)**:

1. **Strict Double-Entry Bookkeeping (Immutable General Ledger):**
   Setiap pergerakan dana wajib menghasilkan jurnal akuntansi berpasangan (**DEBIT** dan **CREDIT**) yang seimbang ($\sum \text{Debit} = \sum \text{Credit}$). Tabel `ledger_entries` bersifat **Append-Only (Immutable)** — saldo tidak pernah di-*edit* secara bebas tanpa jurnal transaksi yang sah.
2. **Zero Floating-Point Arithmetic:**
   Seluruh nominal uang direpresentasikan menggunakan integer `int64` (satuan Rupiah penuh / minor unit) di level aplikasi dan `BIGINT` di PostgreSQL untuk mengeliminasi *floating-point rounding error* IEEE-754.
3. **Deadlock-Free Pessimistic Row Locking:**
   Saat memproses transfer antar-rekening secara bersamaan (misal Rekening A transfer ke B di saat yang sama B transfer ke A), sistem mengunci baris rekening dengan **Deterministic Lexicographical Lock Ordering** (`SELECT ... FROM accounts WHERE id IN ($1, $2) ORDER BY id ASC FOR UPDATE`) sehingga **mustahil terjadi AB-BA Deadlock**.
4. **Exactly-Once Execution (Idempotency Engine):**
   Seluruh mutasi finansial wajib menyertakan `X-Idempotency-Key`. Kombinasi *Redis Distributed Lock* (`SETNX`) dan *Database Unique Constraint* menjamin tidak ada transaksi ganda (*double-debit*) meskipun jaringan *timeout* atau *client* melakukan *retry* agresif.
5. **Multi-Rail Clearing & Tiered Velocity Limits:**
   Mendukung jalur transaksi **Intrabank (Overbooking)**, **BI-FAST** (biaya Rp 2.500, maks Rp 250 Juta), **RTGS** (biaya Rp 30.000, min > Rp 100 Juta), **LLG/SKNBI** (biaya Rp 2.900), serta **Virtual Account (Inquiry & Payment)** dengan pembatasan limit harian sesuai tier kartu (`SILVER`, `GOLD`, `PLATINUM`, `PRIORITY`).
6. **Zero-Trust 6-Role RBAC, 6-Digit PIN Lockout & OJK/BI Audit Trail:**
   Pemisahan wewenang (*Segregation of Duties*) menggunakan 6 Role (`CUSTOMER`, `MERCHANT`, `TELLER`, `CUSTOMER_SERVICE`, `AUDITOR`, `ADMIN`), verifikasi **6-Digit Transaction PIN** (otomatis memblokir akun setelah 3x salah PIN), serta pencatatan `audit_logs` dengan *PII Masking* dan `X-Request-ID`.

---

## 2. Role-Based Access Control (RBAC) & Alur Login

### 2.1 Cara Kerja Login & Autentikasi
1. **Login via JWT (`POST /v1/auth/token`)**:
   - Kirim `email` dan `password`.
   - Server mengembalikan `access_token` (berlaku 15 menit), `refresh_token` (7 hari + HttpOnly Cookie), daftar `permissions`, serta daftar `owned_accounts` (rekening milik user tersebut).
   - Gunakan header `Authorization: Bearer <access_token>` pada request berikutnya.
2. **Cek Profil & Hak Akses Saat Ini (`GET /v1/auth/me`)**:
   - Mengembalikan identitas CIF, role aktif, daftar permission, dan daftar rekening yang dimiliki.
3. **Cek Daftar Role & Akun Demo (`GET /v1/auth/roles`)**:
   - Menampilkan matriks lengkap ke-6 role beserta akun demo siap pakai.
4. **Open Banking Partner Sandbox Key (`X-API-Key: cbs_live_snap_sandbox_2026`)**:
   - Untuk mempermudah pengujian langsung di Scalar UI (`/docs`) tanpa harus copy-paste token berulang kali.

### 2.2 Matriks 6 Role Perbankan (Segregation of Duties)

| Role | Email Demo (`Password#2026`) | Bisa Ngapain Aja (Hak Akses) | Batasan Ketat (Zero-Trust) |
| :--- | :--- | :--- | :--- |
| **`CUSTOMER`**<br>*(Nasabah Perorangan)* | `budi.santoso@cbs-bank.co.id`<br>Rek: `5240301001` (PIN `123456`)<br><br>`siti.rahma@cbs-bank.co.id`<br>Rek: `5240302002` (PIN `654321`) | - Cek saldo & mutasi rekening **miliknya sendiri**<br>- Transfer dana (`INTRABANK`, `BI_FAST`, `RTGS`, `LLG`) dari rekening sendiri<br>- Inquiry & bayar tagihan Virtual Account | **Dilarang** mengintip saldo/mutasi rekening nasabah lain (`403 Forbidden`), tidak bisa setor tunai sendiri, tidak bisa buka blokir PIN sendiri. |
| **`MERCHANT`**<br>*(Nasabah Korporasi / Biller)* | `merchant@tokonusantara.co.id`<br>Rek Giro: `5240308888` (PIN `888888`) | - Semua fitur rekening miliknya sendiri<br>- **Generate Tagihan Virtual Account** (`POST /v1/virtual-accounts`) untuk pelanggan | Hanya bisa melihat rekening korporasi miliknya sendiri. |
| **`TELLER`**<br>*(Petugas Teller Cabang)* | `teller@cbs-bank.co.id` | - Memproses **Setor Tunai (`DEPOSIT`)** & **Tarik Tunai (`WITHDRAWAL`)** nasabah (`POST /v1/transactions/cash`)<br>- Cek informasi rekening nasabah | Tidak bisa melakukan transfer antar-rekening nasabah atau melihat log audit OJK. |
| **`CUSTOMER_SERVICE`**<br>*(CS Cabang)* | `cs@cbs-bank.co.id` | - **Membuka blokir PIN (`PIN_BLOCKED`)** nasabah (`POST /v1/accounts/{no}/unblock-pin`)<br>- Membantu pembukaan CIF/Rekening baru & cek mutasi nasabah | Tidak bisa melakukan setor/tarik tunai maupun transfer dana. |
| **`AUDITOR`**<br>*(Auditor Internal & OJK)* | `auditor@cbs-bank.co.id` | - **Read-Only Pengawasan**: Cek **Trial Balance** (`GET /v1/ledger/trial-balance`), **Audit Trail** (`GET /v1/audit-logs`), & Rekening GL Internal (`?include_gl=true`) | **100% Read-Only** — dilarang melakukan transaksi finansial apa pun. |
| **`ADMIN`**<br>*(Core Banking Admin)* | `admin@cbs-bank.co.id`<br>atau `X-API-Key` | - Akses penuh ke seluruh operasional Core Banking & konfigurasi sistem | Seluruh aksi tercatat di `audit_logs`. |

---

## 3. Production Database Schema (PostgreSQL / Supabase DDL)

Tersedia lengkap di `migrations/001_init_schema.sql`:

```sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TYPE user_role AS ENUM ('CUSTOMER', 'MERCHANT', 'TELLER', 'CUSTOMER_SERVICE', 'AUDITOR', 'ADMIN');
CREATE TYPE kyc_status AS ENUM ('UNVERIFIED', 'VERIFIED', 'REJECTED', 'ENHANCED_DUE_DILIGENCE');
CREATE TYPE account_type AS ENUM ('SAVINGS', 'SAVINGS_YOUTH', 'CHECKING', 'TIME_DEPOSIT', 'GL_INTERNAL');
CREATE TYPE card_tier AS ENUM ('SILVER', 'GOLD', 'PLATINUM', 'PRIORITY');
CREATE TYPE account_status AS ENUM ('ACTIVE', 'DORMANT', 'FROZEN', 'PIN_BLOCKED', 'CLOSED');
CREATE TYPE tx_channel AS ENUM ('INTRABANK', 'BI_FAST', 'RTGS', 'LLG', 'VIRTUAL_ACCOUNT', 'TELLER_DEPOSIT', 'TELLER_WITHDRAWAL');
CREATE TYPE tx_status AS ENUM ('PENDING', 'SETTLED', 'FAILED', 'REVERSED');
CREATE TYPE entry_direction AS ENUM ('DEBIT', 'CREDIT');
CREATE TYPE va_status AS ENUM ('OPEN', 'PAID', 'EXPIRED', 'CANCELLED');
CREATE TYPE audit_action AS ENUM ('CREATE', 'UPDATE', 'DELETE', 'AUTHENTICATE', 'TRANSFER', 'VA_PAYMENT', 'PIN_FAILURE', 'PIN_UNBLOCK');
```

---

## 4. Core Banking Fee, Tier & Clearing Rules

### 4.1 Card Tier & Daily Transfer Velocity Limits
| Card Tier | Limit Transfer Harian (IDR) | Segmen Rekening |
| :--- | :--- | :--- |
| **`SILVER`** | Rp 50.000.000 | Tabungan Reguler / Youth |
| **`GOLD`** | Rp 100.000.000 | Tabungan Gold |
| **`PLATINUM`** | Rp 250.000.000 | Tabungan Platinum |
| **`PRIORITY`** | Rp 1.000.000.000 | Nasabah Prioritas & Giro Korporasi |

### 4.2 Clearing Channels & Fee Schedule
| Channel | Biaya Admin (IDR) | Batas Nominal per Transaksi | Mekanisme Buku Besar (Double-Entry) |
| :--- | :--- | :--- | :--- |
| **`INTRABANK`** | Rp 0 | Sesuai Limit Harian Kartu | Debit Rekening Pengirim, Credit Rekening Penerima |
| **`BI_FAST`** | Rp 2.500 | Rp 10.000 – Rp 250.000.000 | Debit Pengirim (Nominal+Fee), Credit GL Nostro BI-FAST, Credit GL Fee Income |
| **`RTGS`** | Rp 30.000 | > Rp 100.000.000 | Debit Pengirim (Nominal+Fee), Credit GL Nostro RTGS, Credit GL Fee Income |
| **`LLG`** | Rp 2.900 | Rp 10.000 – Rp 500.000.000 | Debit Pengirim (Nominal+Fee), Credit GL Nostro LLG, Credit GL Fee Income |
| **`VIRTUAL_ACCOUNT`** | Rp 0 | Sesuai Tagihan VA (`bill_amount`) | Debit Rekening Pembayar, Credit Rekening Merchant & Update Status VA = `PAID` |
| **`TELLER_DEPOSIT`** | Rp 0 | > Rp 10.000 | Debit GL Kas Utama (Vault), Credit Rekening Nasabah |
| **`TELLER_WITHDRAWAL`** | Rp 0 | > Rp 10.000 | Debit Rekening Nasabah, Credit GL Kas Utama (Vault) |

---

## 5. End-to-End API Specification

| Method | Endpoint | Deskripsi | Role yang Diizinkan |
| :--- | :--- | :--- | :--- |
| `GET` | `/v1/system/health` | Status kesehatan Core Ledger, DB, Redis & GL Integrity Check | Public |
| `GET` | `/v1/auth/roles` | Panduan 6 Role Perbankan, Akun Demo & Matriks Hak Akses | Public |
| `POST` | `/v1/auth/token` | Login (Mengembalikan JWT + `permissions` + `owned_accounts`) | Public |
| `POST` | `/v1/auth/refresh` | One-Time Refresh Token Rotation (Redis JTI Blacklist) | Public |
| `GET` | `/v1/auth/me` | Cek Profil Sesi Saat Ini, Role, Permission & Rekening Miliknya | Semua Role |
| `POST` | `/v1/customers/onboard` | Pembukaan CIF & Rekening Baru + Set 6-Digit PIN (`X-Idempotency-Key`) | Public / `TELLER` / `CS` |
| `GET` | `/v1/accounts` | Daftar rekening (Nasabah: milik sendiri; Staf: seluruh rekening) | Semua Role |
| `GET` | `/v1/accounts/{accountNumber}/balance` | Inquiry Saldo Real-Time & Sisa Limit Harian | Pemilik Rekening / Staf |
| `GET` | `/v1/accounts/{accountNumber}/statement` | Mutasi Rekening (Double-Entry DEBIT/CREDIT history) | Pemilik Rekening / Staf |
| `POST` | `/v1/accounts/{accountNumber}/unblock-pin` | Buka Blokir PIN (`PIN_BLOCKED` -> `ACTIVE`) & Reset PIN | `CUSTOMER_SERVICE`, `ADMIN` |
| `POST` | `/v1/transactions/transfer` | Transfer Dana (`INTRABANK`, `BI_FAST`, `RTGS`, `LLG`) | `CUSTOMER`, `MERCHANT`, `ADMIN` |
| `POST` | `/v1/transactions/cash` | Setor Tunai (`DEPOSIT`) & Tarik Tunai (`WITHDRAWAL`) | `TELLER`, `ADMIN` |
| `POST` | `/v1/virtual-accounts` | Generate Tagihan Virtual Account (`88000...`) | `MERCHANT`, `ADMIN` |
| `GET` | `/v1/virtual-accounts/{vaNumber}` | Inquiry Tagihan Virtual Account sebelum bayar | Semua Role |
| `POST` | `/v1/virtual-accounts/{vaNumber}/pay` | Pembayaran Tagihan Virtual Account (ACID Settlement) | `CUSTOMER`, `MERCHANT`, `ADMIN` |
| `GET` | `/v1/ledger/trial-balance` | Neraca Saldo Buku Besar (Verifikasi $\sum \text{Debit} = \sum \text{Credit}$) | `AUDITOR`, `ADMIN` |
| `GET` | `/v1/audit-logs` | Jejak Audit Regulasi OJK/BI dengan PII Masking | `AUDITOR`, `ADMIN` |
