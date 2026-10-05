package model

import "time"

type Link struct {
	ID          int64      `json:"id"`
	Code        string     `json:"code"`
	OriginalURL string     `json:"original_url"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Visits      int64      `json:"visits"`
	UserID      *int64     `json:"user_id,omitempty"`
}

func (l Link) IsExpired(now time.Time) bool {
	return l.ExpiresAt != nil && !l.ExpiresAt.After(now)
}
