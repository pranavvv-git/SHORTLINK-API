package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aadityya4real/Url-shortener-service/internal/model"
)

type SQLiteLinkRepository struct {
	db *sql.DB
}

func NewSQLiteLink(db *sql.DB) *SQLiteLinkRepository {
	return &SQLiteLinkRepository{db: db}
}

func (r *SQLiteLinkRepository) Create(ctx context.Context, link *model.Link) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO links (code, original_url, created_at, expires_at, visits, user_id)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		link.Code,
		link.OriginalURL,
		formatTime(link.CreatedAt),
		formatNullableTime(link.ExpiresAt),
		link.Visits,
		link.UserID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("insert link: %w", err)
	}

	link.ID, err = result.LastInsertId()
	if err != nil {
		return fmt.Errorf("read inserted id: %w", err)
	}
	return nil
}

func (r *SQLiteLinkRepository) GetByCode(ctx context.Context, code string) (*model.Link, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, code, original_url, created_at, expires_at, visits, user_id
		 FROM links WHERE code = ?`,
		code,
	)
	return scanLink(row)
}

func (r *SQLiteLinkRepository) Resolve(ctx context.Context, code string, now time.Time) (*model.Link, error) {
	nowStr := formatTime(now)
	row := r.db.QueryRowContext(
		ctx,
		`UPDATE links
		 SET visits = visits + 1
		 WHERE code = ? AND (expires_at IS NULL OR julianday(expires_at) > julianday(?))
		 RETURNING id, code, original_url, created_at, expires_at, visits, user_id`,
		code,
		nowStr,
	)
	return scanLink(row)
}

func (r *SQLiteLinkRepository) Delete(ctx context.Context, code string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM links WHERE code = ?`, code)
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected rows: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteLinkRepository) GetByUserID(ctx context.Context, userID int64) ([]model.Link, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, code, original_url, created_at, expires_at, visits, user_id
		 FROM links
		 WHERE user_id = ?
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query links by user: %w", err)
	}
	defer rows.Close()

	var links []model.Link
	for rows.Next() {
		link, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, *link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate links rows: %w", err)
	}
	return links, nil
}

// SQLiteUserRepository implements UserRepository
type SQLiteUserRepository struct {
	db *sql.DB
}

func NewSQLiteUser(db *sql.DB) *SQLiteUserRepository {
	return &SQLiteUserRepository{db: db}
}

func (r *SQLiteUserRepository) Create(ctx context.Context, user *model.User) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO users (email, password_hash, created_at) VALUES (?, ?, ?)`,
		user.Email,
		user.PasswordHash,
		formatTime(user.CreatedAt),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("insert user: %w", err)
	}
	user.ID, err = result.LastInsertId()
	if err != nil {
		return fmt.Errorf("read user insert id: %w", err)
	}
	return nil
}

func (r *SQLiteUserRepository) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	var (
		user      model.User
		createdAt string
	)
	err := r.db.QueryRowContext(
		ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email = ?`,
		email,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query user by email: %w", err)
	}
	user.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse user created_at: %w", err)
	}
	return &user, nil
}

func (r *SQLiteUserRepository) GetByID(ctx context.Context, id int64) (*model.User, error) {
	var (
		user      model.User
		createdAt string
	)
	err := r.db.QueryRowContext(
		ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE id = ?`,
		id,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query user by id: %w", err)
	}
	user.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse user created_at: %w", err)
	}
	return &user, nil
}

// SQLiteSessionRepository implements SessionRepository
type SQLiteSessionRepository struct {
	db *sql.DB
}

func NewSQLiteSession(db *sql.DB) *SQLiteSessionRepository {
	return &SQLiteSessionRepository{db: db}
}

func (r *SQLiteSessionRepository) Create(ctx context.Context, session *model.Session) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`,
		session.ID,
		session.UserID,
		formatTime(session.ExpiresAt),
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *SQLiteSessionRepository) Get(ctx context.Context, id string) (*model.Session, error) {
	var (
		session   model.Session
		expiresAt string
	)
	err := r.db.QueryRowContext(
		ctx,
		`SELECT id, user_id, expires_at FROM sessions WHERE id = ?`,
		id,
	).Scan(&session.ID, &session.UserID, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("query session: %w", err)
	}
	session.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("parse session expires_at: %w", err)
	}
	return &session, nil
}

func (r *SQLiteSessionRepository) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read delete session affected rows: %w", err)
	}
	if affected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanLink(row rowScanner) (*model.Link, error) {
	var (
		link      model.Link
		createdAt string
		expiresAt sql.NullString
		userID    sql.NullInt64
	)
	if err := row.Scan(
		&link.ID,
		&link.Code,
		&link.OriginalURL,
		&createdAt,
		&expiresAt,
		&link.Visits,
		&userID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan link: %w", err)
	}

	var err error
	link.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	if expiresAt.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, expiresAt.String)
		if parseErr != nil {
			return nil, fmt.Errorf("parse expires_at: %w", parseErr)
		}
		link.ExpiresAt = &parsed
	}
	if userID.Valid {
		link.UserID = &userID.Int64
	}

	return &link, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func formatNullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func isUniqueViolation(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
