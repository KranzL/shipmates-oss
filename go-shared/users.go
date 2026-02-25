package goshared

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (d *DB) CreateUser(ctx context.Context, email, passwordHash string) (*User, error) {
	id := GenerateID()
	now := time.Now()

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4)
		ON CONFLICT (email) DO NOTHING
	`, id, email, passwordHash, now)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	return d.GetUserByEmail(ctx, email)
}

func (d *DB) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var user User
	err := d.db.QueryRowContext(ctx, `
		SELECT id, email, created_at
		FROM users
		WHERE email = $1
	`, email).Scan(&user.ID, &user.Email, &user.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return &user, nil
}

func (d *DB) GetUserByID(ctx context.Context, id string) (*User, error) {
	var user User
	err := d.db.QueryRowContext(ctx, `
		SELECT id, email, created_at
		FROM users
		WHERE id = $1
	`, id).Scan(&user.ID, &user.Email, &user.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return &user, nil
}
