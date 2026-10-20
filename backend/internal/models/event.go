package models

import (
	"time"

	"github.com/google/uuid"
)

type Event struct {
	ID               uuid.UUID `json:"id" db:"id"`
	Title            string    `json:"title" db:"title"`
	TotalTickets     int       `json:"total_tickets" db:"total_tickets"`
	AvailableTickets int       `json:"available_tickets" db:"available_tickets"`
	StartTime        time.Time `json:"start_time" db:"start_time"`
	EndTime          time.Time `json:"end_time" db:"end_time"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
}
