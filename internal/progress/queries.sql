-- name: CreateUserProgress :exec
INSERT INTO user_progress (user_id, competency)
VALUES ($1, $2);

-- name: GetUserProgress :one
SELECT competency
FROM user_progress
WHERE user_id = $1;

-- name: GetUserProgressForUpdate :one
SELECT competency
FROM user_progress
WHERE user_id = $1
FOR UPDATE;

-- name: UpdateUserProgress :exec
UPDATE user_progress
SET competency = $2, updated_at = now()
WHERE user_id = $1;
