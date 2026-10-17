-- Accounts that sign in through an OIDC provider are known by who issued
-- the identity and its subject there, never by a name or an address: those
-- can be changed at the provider, or be claimed by someone else.
ALTER TABLE users ADD COLUMN oidc_issuer TEXT;
ALTER TABLE users ADD COLUMN oidc_subject TEXT;
-- The name to show next to the login, as the provider gives it.
ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN last_login_at INTEGER;

CREATE UNIQUE INDEX users_oidc_identity ON users (oidc_issuer, oidc_subject) WHERE oidc_subject IS NOT NULL;
