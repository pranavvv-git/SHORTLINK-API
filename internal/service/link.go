package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aadityya4real/Url-shortener-service/internal/model"
	"github.com/aadityya4real/Url-shortener-service/internal/repository"
)

const maxGenerationAttempts = 5

var (
	ErrInvalidInput      = errors.New("invalid input")
	ErrExpired           = errors.New("link has expired")
	ErrUnauthorizedLink  = errors.New("unauthorized: link belongs to another user")
	aliasPattern         = regexp.MustCompile(`^[A-Za-z0-9_-]{4,32}$`)
	reservedCodes        = map[string]struct{}{
		"api":     {},
		"healthz": {},
		"readyz":  {},
	}
)

type CodeGenerator interface {
	Generate(length int) (string, error)
}

type CreateInput struct {
	OriginalURL string
	CustomAlias string
	ExpiresIn   time.Duration
	UserID      *int64
}

type LinkService struct {
	repository repository.LinkRepository
	generator  CodeGenerator
	codeLength int
	now        func() time.Time
}

func NewLinkService(repo repository.LinkRepository, generator CodeGenerator, codeLength int) *LinkService {
	return &LinkService{repository: repo, generator: generator, codeLength: codeLength, now: time.Now}
}

func (s *LinkService) Create(ctx context.Context, input CreateInput) (*model.Link, error) {
	normalizedURL, err := validateURL(input.OriginalURL)
	if err != nil { return nil, err }
	if input.ExpiresIn < 0 { return nil, fmt.Errorf("%w: expires_in_seconds cannot be negative", ErrInvalidInput) }
	now := s.now().UTC()
	link := &model.Link{OriginalURL: normalizedURL, CreatedAt: now, UserID: input.UserID}
	if input.ExpiresIn > 0 { expiresAt := now.Add(input.ExpiresIn); link.ExpiresAt = &expiresAt }
	if input.CustomAlias != "" {
		input.CustomAlias = strings.ToLower(input.CustomAlias)
		if !aliasPattern.MatchString(input.CustomAlias) { return nil, fmt.Errorf("%w: custom_alias must be 4-32 letters, numbers, underscores, or hyphens", ErrInvalidInput) }
		if isReservedCode(input.CustomAlias) { return nil, fmt.Errorf("%w: custom_alias is reserved", ErrInvalidInput) }
		link.Code = input.CustomAlias
		if err := s.repository.Create(ctx, link); err != nil { return nil, err }
		return link, nil
	}
	for range maxGenerationAttempts {
		link.Code, err = s.generator.Generate(s.codeLength)
		if err != nil { return nil, fmt.Errorf("generate short code: %w", err) }
		link.Code = strings.ToLower(link.Code)
		if isReservedCode(link.Code) { continue }
		err = s.repository.Create(ctx, link)
		if err == nil { return link, nil }
		if !errors.Is(err, repository.ErrConflict) { return nil, err }
	}
	return nil, fmt.Errorf("generate a unique short code after %d attempts", maxGenerationAttempts)
}

func (s *LinkService) Get(ctx context.Context, code string) (*model.Link, error) { code = strings.ToLower(code); if err := validateCode(code); err != nil { return nil, err }; link, err := s.repository.GetByCode(ctx, code); if err != nil { return nil, err }; if link.IsExpired(s.now().UTC()) { return nil, ErrExpired }; return link, nil }
func (s *LinkService) Resolve(ctx context.Context, code string) (*model.Link, error) { code = strings.ToLower(code); if err := validateCode(code); err != nil { return nil, err }; now := s.now().UTC(); link, err := s.repository.Resolve(ctx, code, now); if errors.Is(err, repository.ErrNotFound) { existing, getErr := s.repository.GetByCode(ctx, code); if getErr == nil && existing.IsExpired(now) { return nil, ErrExpired } }; return link, err }

func (s *LinkService) Delete(ctx context.Context, code string, requestingUserID *int64) error {
	code = strings.ToLower(code)
	if err := validateCode(code); err != nil {
		return err
	}
	link, err := s.repository.GetByCode(ctx, code)
	if err != nil {
		return err
	}
	if link.UserID != nil {
		if requestingUserID == nil || *requestingUserID != *link.UserID {
			return ErrUnauthorizedLink
		}
	}
	return s.repository.Delete(ctx, code)
}

func (s *LinkService) GetByUserID(ctx context.Context, userID int64) ([]model.Link, error) {
	return s.repository.GetByUserID(ctx, userID)
}
func validateURL(raw string) (string, error) { raw = strings.TrimSpace(raw); if len(raw)==0 || len(raw)>2048 { return "", fmt.Errorf("%w: url must be between 1 and 2048 characters", ErrInvalidInput)}; parsed, err := url.ParseRequestURI(raw); if err != nil || parsed.Host=="" || (parsed.Scheme!="http" && parsed.Scheme!="https") { return "", fmt.Errorf("%w: url must be an absolute http or https URL", ErrInvalidInput)}; return parsed.String(), nil }
func validateCode(code string) error { if !aliasPattern.MatchString(code) { return fmt.Errorf("%w: invalid short code", ErrInvalidInput)}; return nil }
func isReservedCode(code string) bool { _, reserved := reservedCodes[strings.ToLower(code)]; return reserved }
