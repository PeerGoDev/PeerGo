package reseedreward

import (
	"context"
	"log/slog"
	"time"
)

func Run(ctx context.Context, repository *PostgresRepository, logger *slog.Logger) error {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		for index := 0; index < 24 && ctx.Err() == nil; index++ {
			settlementCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			processed, err := repository.SettleNext(settlementCtx, time.Now())
			cancel()
			if err != nil && ctx.Err() == nil {
				logger.ErrorContext(ctx, "reseed reward settlement failed", "error", err)
			}
			if err != nil || !processed {
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
