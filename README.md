# Nusantara Core Banking System (CBS) — Enterprise & SNAP-BI Standard

Sistem Backend **Core Banking System (CBS)** level Enterprise (mengadopsi standar **Double-Entry General Ledger** & **SNAP-BI Open Banking**) menggunakan **Go (1.22+)**, **Supabase PostgreSQL**, **Upstash Redis**, dan **Vercel Serverless Runtime**.

---

## Panduan Login & 6 Role Perbankan (RBAC Matrix)

Semua akun demo menggunakan password yang sama agar mudah diuji: **`Password#2026`**

### Cara Login:
1. Panggil **`POST /v1/auth/token`** dengan body JSON:
   ```json
   {
     "email": "budi.santoso@cbs-bank.co.id",
     "password": "Password#2026"
   }
   ```
2. Response akan langsung mengembalikan:
   - `access_token` (gunakan di header `Authorization: Bearer <access_token>`)
   - `role` & daftar `permissions` (apa saja yang boleh dilakukan role tersebut)
   - `owned_accounts` (daftar nomor rekening milik user yang login)
3. Kapan saja ingin cek sesi yang sedang aktif, panggil **`GET /v1/auth/me`**.
4. Untuk melihat panduan lengkap seluruh role dari API, panggil **`GET /v1/auth/roles`**.

---

### Daftar 6 Role & Kewenangannya (Segregation of Duties)

| Role | Email Login (`Password#2026`) | Rekening & PIN | Bisa Ngapain Aja | Batasan Ketat (Zero-Trust) |
| :--- | :--- | :--- | :--- | :--- |
| **`CUSTOMER`**<br>*(Nasabah Perorangan)* | `budi.santoso@cbs-bank.co.id`<br>`siti.rahma@cbs-bank.co.id` | `5240301001` (PIN `123456`)<br>`5240302002` (PIN `654321`) | - Cek saldo & mutasi rekening **miliknya sendiri**<br>- Transfer dana (`INTRABANK`, `BI_FAST`, `RTGS`, `LLG`) dari rekening sendiri<br>- Cek & bayar tagihan Virtual Account | **Tidak bisa** melihat saldo/mutasi rekening orang lain (`403 Forbidden`), tidak bisa setor tunai sendiri, tidak bisa buka blokir PIN sendiri. |
| **`MERCHANT`**<br>*(Nasabah Korporasi)* | `merchant@tokonusantara.co.id` | Giro `5240308888`<br>(PIN `888888`) | - Semua fitur rekening miliknya<br>- **Generate Tagihan Virtual Account** (`POST /v1/virtual-accounts`) | Hanya bisa mengakses rekening korporasi miliknya sendiri. |
| **`TELLER`**<br>*(Teller Cabang)* | `teller@cbs-bank.co.id` | *(Staf Cabang)* | - Eksekusi **Setor Tunai (`DEPOSIT`)** & **Tarik Tunai (`WITHDRAWAL`)** (`POST /v1/transactions/cash`)<br>- Cek rekening nasabah | Tidak bisa melakukan transfer antar-rekening nasabah atau melihat log audit OJK. |
| **`CUSTOMER_SERVICE`**<br>*(CS Cabang)* | `cs@cbs-bank.co.id` | *(Staf Cabang)* | - **Buka Blokir PIN (`PIN_BLOCKED`)** & Reset PIN nasabah (`POST /v1/accounts/{no}/unblock-pin`)<br>- Bantu onboarding CIF baru & cek mutasi | Tidak bisa melakukan setor/tarik tunai maupun transfer dana. |
| **`AUDITOR`**<br>*(Auditor Internal & OJK)* | `auditor@cbs-bank.co.id` | *(Pengawas)* | - **100% Read-Only**: Cek **Trial Balance** (`GET /v1/ledger/trial-balance`), **Audit Trail** (`GET /v1/audit-logs`), & Rekening GL Internal (`?include_gl=true`) | Dilarang melakukan mutasi/transaksi finansial apa pun. |
| **`ADMIN`**<br>*(Core Banking Admin)* | `admin@cbs-bank.co.id`<br>atau Header `X-API-Key` | `cbs_live_snap_sandbox_2026` | - Akses penuh ke seluruh fitur operasional & manajemen sistem | Seluruh aksi tercatat di `audit_logs`. |

---

## Cara Menjalankan & Menguji

```powershell
# 1. Jalankan seluruh Automated Core Banking Test Suite
go test -v ./...

# 2. Jalankan Server Core Banking Lokal (Port 8080)
go run ./cmd/server
```

Buka **`http://localhost:8080/docs`** untuk mencoba langsung dari **Interactive Scalar OpenAPI Playground**.
