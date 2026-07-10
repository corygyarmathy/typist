-- name: CreateUserProgress :exec
INSERT INTO user_progress (user_id, competency)
VALUES ($1, $2);
