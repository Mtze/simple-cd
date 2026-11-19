package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type Session struct {
	IDHash    string    `db:"id_hash"`
	Subject   string    `db:"subject"`
	Username  string    `db:"username"`
	Name      string    `db:"name"`
	Email     string    `db:"email"`
	Roles     []string  `db:"roles"`
	IDToken   string    `db:"id_token"`
	ExpiresAt time.Time `db:"expires_at"`
	CreatedAt time.Time `db:"created_at"`
}

func (s *Store) CreateSession(ctx context.Context, x Session) error {
	if x.Roles == nil {
		x.Roles = []string{}
	}
	_, err := s.db.Exec(ctx, `INSERT INTO sessions (id_hash, subject, username, name, email, roles, id_token, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, x.IDHash, x.Subject, x.Username, x.Name, x.Email, x.Roles, x.IDToken, x.ExpiresAt)
	return err
}

// Session returns a session that has not expired.
func (s *Store) Session(ctx context.Context, idHash string) (*Session, error) {
	rows, err := s.db.Query(ctx, `SELECT id_hash, subject, username, name, email, roles, id_token, expires_at, created_at
		FROM sessions WHERE id_hash = $1 AND expires_at > now()`, idHash)
	if err != nil {
		return nil, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[Session])
	return x, notFound(err)
}

func (s *Store) DeleteSession(ctx context.Context, idHash string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE id_hash = $1`, idHash)
	return err
}

type AuthRequest struct {
	State     string    `db:"state"`
	Verifier  string    `db:"verifier"`
	Nonce     string    `db:"nonce"`
	ReturnTo  string    `db:"return_to"`
	ExpiresAt time.Time `db:"expires_at"`
}

func (s *Store) CreateAuthRequest(ctx context.Context, r AuthRequest) error {
	_, err := s.db.Exec(ctx, `INSERT INTO auth_requests (state, verifier, nonce, return_to, expires_at) VALUES ($1,$2,$3,$4,$5)`,
		r.State, r.Verifier, r.Nonce, r.ReturnTo, r.ExpiresAt)
	return err
}

// TakeAuthRequest returns and deletes a pending login, so a state is usable once.
func (s *Store) TakeAuthRequest(ctx context.Context, state string) (*AuthRequest, error) {
	rows, err := s.db.Query(ctx, `DELETE FROM auth_requests WHERE state = $1 AND expires_at > now()
		RETURNING state, verifier, nonce, return_to, expires_at`, state)
	if err != nil {
		return nil, err
	}
	r, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[AuthRequest])
	return r, notFound(err)
}

// DeleteExpiredAuth removes expired sessions and login attempts.
func (s *Store) DeleteExpiredAuth(ctx context.Context) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, `DELETE FROM auth_requests WHERE expires_at < now()`)
	return err
}
