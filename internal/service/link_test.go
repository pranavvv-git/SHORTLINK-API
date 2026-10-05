package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aadityya4real/Url-shortener-service/internal/model"
	"github.com/aadityya4real/Url-shortener-service/internal/repository"
)

type fakeRepository struct {
	links map[string]*model.Link
}

func (r *fakeRepository) Create(_ context.Context, link *model.Link) error {
	if _, exists := r.links[link.Code]; exists {
		return repository.ErrConflict
	}
	copy := *link
	copy.ID = int64(len(r.links) + 1)
	link.ID = copy.ID
	r.links[link.Code] = &copy
	return nil
}

func (r *fakeRepository) GetByCode(_ context.Context, code string) (*model.Link, error) {
	link, exists := r.links[code]
	if !exists {
		return nil, repository.ErrNotFound
	}
	copy := *link
	return &copy, nil
}

func (r *fakeRepository) Resolve(ctx context.Context, code string, _ time.Time) (*model.Link, error) {
	link, err := r.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	link.Visits++
	r.links[code].Visits++
	return link, nil
}

func (r *fakeRepository) Delete(_ context.Context, code string) error {
	if _, exists := r.links[code]; !exists {
		return repository.ErrNotFound
	}
	delete(r.links, code)
	return nil
}

func (r *fakeRepository) GetByUserID(_ context.Context, userID int64) ([]model.Link, error) {
	var result []model.Link
	for _, link := range r.links {
		if link.UserID != nil && *link.UserID == userID {
			result = append(result, *link)
		}
	}
	return result, nil
}

type fixedGenerator struct {
	code string
}

func (g fixedGenerator) Generate(int) (string, error) {
	return g.code, nil
}

func TestCreateWithGeneratedCode(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{links: make(map[string]*model.Link)}
	svc := NewLinkService(repo, fixedGenerator{code: "abcd123"}, 7)
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	link, err := svc.Create(context.Background(), CreateInput{
		OriginalURL: "https://example.com/path",
		ExpiresIn:   time.Hour,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if link.Code != "abcd123" {
		t.Fatalf("Create() code = %q, want abcd123", link.Code)
	}
	if link.ExpiresAt == nil || !link.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("Create() expires_at = %v, want %v", link.ExpiresAt, now.Add(time.Hour))
	}
}

func TestCreateRejectsInvalidURL(t *testing.T) {
	t.Parallel()

	svc := NewLinkService(
		&fakeRepository{links: make(map[string]*model.Link)},
		fixedGenerator{code: "Abcd123"},
		7,
	)

	_, err := svc.Create(context.Background(), CreateInput{OriginalURL: "javascript:alert(1)"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create() error = %v, want ErrInvalidInput", err)
	}
}

func TestCreateRejectsDuplicateCustomAlias(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{links: make(map[string]*model.Link)}
	svc := NewLinkService(repo, fixedGenerator{code: "unused1"}, 7)
	input := CreateInput{OriginalURL: "https://example.com", CustomAlias: "docs"}

	if _, err := svc.Create(context.Background(), input); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if _, err := svc.Create(context.Background(), input); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("second Create() error = %v, want ErrConflict", err)
	}
}

func TestCreateRejectsReservedAlias(t *testing.T) {
	t.Parallel()

	svc := NewLinkService(
		&fakeRepository{links: make(map[string]*model.Link)},
		fixedGenerator{code: "unused1"},
		7,
	)

	_, err := svc.Create(context.Background(), CreateInput{
		OriginalURL: "https://example.com",
		CustomAlias: "healthz",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create() error = %v, want ErrInvalidInput", err)
	}
}
