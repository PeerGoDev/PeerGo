
SELECT
    count(*) FILTER (WHERE status = 'pending' AND attempts = 0),
    count(*) FILTER (WHERE status = 'processing'),
    count(*) FILTER (WHERE status = 'pending' AND attempts > 0),
    count(*) FILTER (WHERE status = 'dead'),
    count(*) FILTER (WHERE status = 'completed'),
    min(created_at) FILTER (WHERE status IN ('pending', 'processing')),
    (SELECT last_error_code FROM economy.seeding_reward_work_items WHERE last_error_code IS NOT NULL ORDER BY last_error_at DESC LIMIT 1),
    (SELECT last_error_at FROM economy.seeding_reward_work_items WHERE last_error_code IS NOT NULL ORDER BY last_error_at DESC LIMIT 1)
FROM economy.seeding_reward_work_items