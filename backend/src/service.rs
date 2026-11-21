use crate::config::Config;
use crate::models::*;
use crate::repository::{BankingError, Repository};
use chrono::Utc;
use jsonwebtoken::{decode, encode, DecodingKey, EncodingKey, Header, Validation};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

#[derive(Debug, Serialize, Deserialize)]
pub struct Claims {
    pub sub: String,
    pub cif: String,
    pub name: String,
    pub email: String,
    pub role: UserRole,
    pub token_type: String, // "access" or "refresh"
    pub exp: usize,
    pub iat: usize,
    pub jti: String,
    pub iss: String,
}

#[derive(Clone)]
pub struct Service {
    pub config: Config,
    pub repo: Repository,
}

impl Service {
    pub fn new(config: Config, repo: Repository) -> Self {
        Self { config, repo }
    }

    pub fn authenticate(
        &self,
        req: LoginRequest,
        client_ip: &str,
        trace_id: &str,
    ) -> Result<TokenResponse, BankingError> {
        let customer = self.repo.get_customer_by_email(&req.email)?;
        let is_valid = bcrypt::verify(&req.password, &customer.password_hash).unwrap_or(false);
        if !is_valid {
            return Err(BankingError::InvalidCredentials);
        }

        let tokens = self.issue_token_pair(&customer)?;

        self.repo.record_audit_log(
            Some(customer.id.clone()),
            AuditAction::Authenticate,
            "customers".to_string(),
            customer.cif_number.clone(),
            client_ip.to_string(),
            trace_id.to_string(),
            Some(serde_json::json!({
                "cif_number": customer.cif_number,
                "email_masked": mask_email(&customer.email),
                "role": customer.role,
            })),
        );

        Ok(tokens)
    }

    pub fn issue_token_pair(&self, customer: &Customer) -> Result<TokenResponse, BankingError> {
        let now = Utc::now();
        let access_exp = (now + chrono::Duration::minutes(15)).timestamp() as usize;
        let refresh_exp = (now + chrono::Duration::days(7)).timestamp() as usize;
        let iat = now.timestamp() as usize;

        let access_claims = Claims {
            sub: customer.id.clone(),
            cif: customer.cif_number.clone(),
            name: customer.full_name.clone(),
            email: customer.email.clone(),
            role: customer.role,
            token_type: "access".to_string(),
            exp: access_exp,
            iat,
            jti: Uuid::new_v4().to_string(),
            iss: "cbs-bank-core-ledger".to_string(),
        };

        let refresh_claims = Claims {
            sub: customer.id.clone(),
            cif: customer.cif_number.clone(),
            name: customer.full_name.clone(),
            email: customer.email.clone(),
            role: customer.role,
            token_type: "refresh".to_string(),
            exp: refresh_exp,
            iat,
            jti: Uuid::new_v4().to_string(),
            iss: "cbs-bank-core-ledger".to_string(),
        };

        let key = EncodingKey::from_secret(self.config.jwt_secret.as_bytes());
        let access_token = encode(&Header::default(), &access_claims, &key)
            .map_err(|_| BankingError::BadRequest("Failed to encode access token".to_string()))?;
        let refresh_token = encode(&Header::default(), &refresh_claims, &key)
            .map_err(|_| BankingError::BadRequest("Failed to encode refresh token".to_string()))?;

        let owned_accounts = self.repo.list_accounts_by_customer_id(&customer.id);

        Ok(TokenResponse {
            access_token,
            refresh_token,
            token_type: "Bearer".to_string(),
            expires_in: 900,
            cif_number: customer.cif_number.clone(),
            full_name: customer.full_name.clone(),
            email: customer.email.clone(),
            role: customer.role,
            permissions: customer.role.permissions(),
            accounts: owned_accounts,
        })
    }

    pub fn verify_jwt(&self, token_str: &str) -> Result<Claims, BankingError> {
        let key = DecodingKey::from_secret(self.config.jwt_secret.as_bytes());
        let mut validation = Validation::default();
        validation.set_issuer(&["cbs-bank-core-ledger"]);

        let data = decode::<Claims>(token_str, &key, &validation)
            .map_err(|_| BankingError::InvalidToken)?;
        Ok(data.claims)
    }

    pub fn rotate_refresh_token(
        &self,
        token_str: &str,
        client_ip: &str,
        trace_id: &str,
    ) -> Result<TokenResponse, BankingError> {
        let claims = self.verify_jwt(token_str)?;
        if claims.token_type != "refresh" {
            return Err(BankingError::InvalidToken);
        }

        let customer = self.repo.get_customer_by_id(&claims.sub)?;
        let tokens = self.issue_token_pair(&customer)?;

        self.repo.record_audit_log(
            Some(customer.id.clone()),
            AuditAction::Authenticate,
            "jwt_refresh_rotation".to_string(),
            claims.jti,
            client_ip.to_string(),
            trace_id.to_string(),
            Some(serde_json::json!({
                "cif_number": customer.cif_number,
                "role": customer.role,
            })),
        );

        Ok(tokens)
    }
}
