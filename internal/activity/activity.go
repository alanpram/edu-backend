// Package activity menyimpan audit trail tanpa mengganggu operasi utama.
package activity

import (
	"context"
	"encoding/json"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct{ Pool *pgxpool.Pool }

func (s *Service) Record(ctx context.Context, userID int, name, ip string, metadata map[string]any) {
	if userID <= 0 {
		return
	}
	var payload any
	if metadata != nil {
		encoded, err := json.Marshal(metadata)
		if err == nil {
			payload = string(encoded)
		}
	}
	var ipValue any
	if ip != "" {
		ipValue = ip
	}
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO activity_log (user_id, aktivitas, ip_address, metadata, created_at)
		VALUES ($1, $2, $3, $4::jsonb, now())`, userID, name, ipValue, payload)
	if err != nil {
		log.Printf("[ACTIVITY LOG FAILED] user=%d activity=%s err=%v", userID, name, err)
	}
}
