-- name: PasswordCredentialExists :one
SELECT EXISTS (
SELECT 1
  FROM auth_credentials
  WHERE cred_kind = 'password'
  AND identifier = $1
);

-- name: CreateUser :one
INSERT INTO users DEFAULT VALUES
RETURNING id;

-- name: CreateCredential :exec
INSERT INTO auth_credentials (user_id, cred_kind, identifier, secret)
VALUES ($1, $2, $3, $4);
