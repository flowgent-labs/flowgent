package model

import "time"

// BaseEntity provides common audit and lifecycle fields for all persisted entities.
// Embed this in entity structs to get consistent CreatedAt/UpdatedAt/SoftDelete behavior.
type BaseEntity struct {
	Status    string    `json:"status" yaml:"status"`
	CreatedAt time.Time `json:"created_at" yaml:"created_at"`
	CreatedBy string    `json:"created_by,omitempty" yaml:"created_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at" yaml:"updated_at"`
	UpdatedBy string    `json:"updated_by,omitempty" yaml:"updated_by,omitempty"`
	DelFlag   bool      `json:"del_flag" yaml:"del_flag"`
}

// MarkCreated sets creation audit fields.
func (b *BaseEntity) MarkCreated(by string) {
	now := time.Now()
	b.Status = "ACTIVE"
	b.CreatedAt = now
	b.CreatedBy = by
	b.UpdatedAt = now
	b.UpdatedBy = by
	b.DelFlag = false
}

// MarkUpdated sets update audit fields.
func (b *BaseEntity) MarkUpdated(by string) {
	b.UpdatedAt = time.Now()
	if by != "" {
		b.UpdatedBy = by
	}
}

// MarkDeleted soft-deletes the entity.
func (b *BaseEntity) MarkDeleted(by string) {
	b.DelFlag = true
	b.Status = "DELETED"
	b.MarkUpdated(by)
}

// PageRequest is the input for paginated queries.
type PageRequest struct {
	Page int `json:"page"`
	Size int `json:"size"`
}

// Page represents a paginated result set for UI display.
type Page[T any] struct {
	Items      []*T        `json:"items"`
	TotalCount int64       `json:"total_count"`
	Request    PageRequest `json:"request"`
	TotalPages int         `json:"total_pages"`
}

// NewPage creates a Page from results and total count.
func NewPage[T any](items []*T, totalCount int64, req PageRequest) *Page[T] {
	totalPages := int(totalCount / int64(req.Size))
	if totalCount%int64(req.Size) != 0 {
		totalPages++
	}
	return &Page[T]{
		Items: items, TotalCount: totalCount,
		Request: req, TotalPages: totalPages,
	}
}
