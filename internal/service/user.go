package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/aadityya4real/Url-shortener-service/internal/model"
	"github.com/aadityya4real/Url-shortener-service/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserAlreadyExists = errors.New("user with this email already exists")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrSessionExpired     = errors.New("session has expired")
)

type UserService struct {
	userRepo    repository.UserRepository
	sessionRepo repository.SessionRepository
	sessionTTL  time.Duration
	now         func() time.Time
}

func NewUserService(userRepo repository.UserRepository, sessionRepo repository.SessionRepository, sessionTTL time.Duration) *UserService {
	return &UserService{
		userRepo:    userRepo,
		sessionRepo: sessionRepo,
		sessionTTL:  sessionTTL,
		now:         time.Now,
	}
}

func (s *UserService) SignUp(ctx context.Context, email, password string) (*model.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, fmt.Errorf("%w: invalid email format", ErrInvalidInput)
	}

	if len(password) < 8 {
		return nil, fmt.Errorf("%w: password must be at least 8 characters long", ErrInvalidInput)
	}

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &model.User{
		Email:        email,
		PasswordHash: string(hashedBytes),
		CreatedAt:    s.now().UTC(),
	}

	err = s.userRepo.Create(ctx, user)
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, ErrUserAlreadyExists
		}
		return nil, err
	}

	return user, nil
}

func (s *UserService) LogIn(ctx context.Context, email, password string) (*model.Session, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate session token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	session := &model.Session{
		ID:        token,
		UserID:    user.ID,
		ExpiresAt: s.now().UTC().Add(s.sessionTTL),
	}

	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, err
	}

	return session, nil
}

func (s *UserService) LogOut(ctx context.Context, sessionID string) error {
	err := s.sessionRepo.Delete(ctx, sessionID)
	if err != nil && !errors.Is(err, repository.ErrSessionNotFound) {
		return err
	}
	return nil
}

func (s *UserService) Authenticate(ctx context.Context, sessionID string) (*model.User, error) {
	session, err := s.sessionRepo.Get(ctx, sessionID)
	if err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			return nil, ErrSessionExpired
		}
		return nil, err
	}

	if session.IsExpired(s.now().UTC()) {
		// Clean up expired session asynchronously or synchronously
		_ = s.sessionRepo.Delete(ctx, sessionID)
		return nil, ErrSessionExpired
	}

	user, err := s.userRepo.GetByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrSessionExpired
		}
		return nil, err
	}

	return user, nil
}
