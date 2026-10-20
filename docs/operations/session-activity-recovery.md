# Web activity projection recovery

Successful web login and authenticated session touches now advance identity.user_activity from the actual server-observed web session time. The session and projection write are atomic. Each current row advances at most once per five minutes across sessions, preserves newer timestamps and increments its version. No request history or new table is added. Expired, revoked and rejected sessions cannot create activity.

The current projection can be rebuilt from max(last_seen_at) of real web sessions, preserving the exact observed timestamp. Do not infer web activity from Tracker announces, manufacture timestamps for legacy users or change the reward activity policy. Keep a private before/after recovery manifest and verify source coverage before committing.

Existing invitation reward windows are immutable. Restoring current activity can reveal underpayments caused by the previous stale projection. Compare original receipts with a read-only recomputation before deciding on a separate append-only compensation operation; never rewrite receipts, replay paid windows or automatically treat every historical user as active.
