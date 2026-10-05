package repository

import (
	"context"
	"errors"
	"time"

	"github.com/aadityya4real/Url-shortener-service/internal/model"
)

var (
	ErrNotFound = errors.New("link not found")
	ErrConflict = errors.New("short code already exists")
	ErrUserNotFound = errors.New("user not found")
	ErrSessionNotFound = errors.New("session not found")
)

type LinkRepository interface {
	Create(ctx context.Context, link *model.Link) error
	GetByCode(ctx context.Context, code string) (*model.Link, error)
	Resolve(ctx context.Context, code string, now time.Time) (*model.Link, error)
	Delete(ctx context.Context, code string) error
	GetByUserID(ctx context.Context, userID int64) ([]model.Link, error)
}

type UserRepository interface {
	Create(ctx context.Context, user *model.User) error
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	GetByID(ctx context.Context, id int64) (*model.User, error)
}

type SessionRepository interface {
	Create(ctx context.Context, session *model.Session) error
	Get(ctx context.Context, id string) (*model.Session, error)
	Delete(ctx context.Context, id string) error
}
