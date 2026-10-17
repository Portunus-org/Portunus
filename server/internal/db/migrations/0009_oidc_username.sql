-- The login name of an account of a provider, as the provider gives it. It
-- is shown as it is and follows the provider at every sign-in. The unique
-- `username` of such an account is an internal handle nobody sees, so that
-- a name at the provider never collides with an account here.
ALTER TABLE users ADD COLUMN oidc_username TEXT NOT NULL DEFAULT '';
