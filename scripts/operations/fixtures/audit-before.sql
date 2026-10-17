
SELECT
    count(*) FILTER (WHERE delivered_at IS NULL AND attempts = 0 AND (lease_until IS NULL OR lease_until <= $1)),
    count(*) FILTER (WHERE delivered_at IS NULL AND lease_until > $1),
    count(*) FILTER (WHERE delivered_at IS NULL AND attempts > 0 AND (lease_until IS NULL OR lease_until <= $1)),
    0,
    count(*) FILTER (WHERE delivered_at IS NOT NULL),
    min(created_at) FILTER (WHERE delivered_at IS NULL),
    (SELECT left(last_error, 64) FROM audit.outbox WHERE last_error IS NOT NULL ORDER BY available_at DESC LIMIT 1),
    NULL::timestamptz
FROM audit.outbox