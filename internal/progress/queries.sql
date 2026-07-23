-- name: CreateUserProgress :exec
INSERT INTO user_progress (user_id, competency)
VALUES ($1, $2);

-- name: GetUserProgress :one
SELECT competency
FROM user_progress
WHERE user_id = $1;
