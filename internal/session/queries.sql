-- name: CreateSession :one
INSERT INTO sessions (user_id, wpm, accuracy, completed_at)
      VALUES ($1, $2, $3, $4)
      RETURNING id, wpm, accuracy, completed_at;
