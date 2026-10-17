
SELECT active_membership.membership_id,
       COALESCE(sum(item.active_seconds), 0)::bigint
FROM economy.seeding_reward_evidence_user_hours AS item
JOIN economy.seeding_reward_evidence_windows AS evidence_window
  ON evidence_window.window_start = item.window_start
 AND evidence_window.status = 'complete'
JOIN LATERAL (
    SELECT transition.membership_id, transition.to_status
    FROM workgroups.membership_transitions AS transition
    WHERE transition.user_id = item.user_id
      AND transition.group_kind = 'retention'
      AND transition.occurred_at <= evidence_window.window_start
    ORDER BY transition.occurred_at DESC, transition.state_version DESC, transition.id DESC
    LIMIT 1
) AS active_membership ON active_membership.to_status = 'active'
WHERE active_membership.membership_id = ANY($1::uuid[])
  AND evidence_window.window_start >= $2
  AND evidence_window.window_end <= $3
GROUP BY active_membership.membership_id