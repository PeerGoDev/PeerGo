import { type RouteConfig, index, route } from "@react-router/dev/routes"

export default [
  index("routes/home.tsx"),
  route("login", "routes/login.tsx"),
  route("register", "routes/register.tsx"),
  route("forgot-password", "routes/forgot-password.tsx"),
  route("reset-password", "routes/reset-password.tsx"),
  route("verify-email", "routes/verify-email.tsx"),
  route("restrictions", "routes/restrictions.tsx"),
  route("upload", "routes/upload.tsx"),
  route("search", "routes/search.tsx"),
  route("torrents", "routes/torrents.tsx"),
  route("torrents/:torrentId", "routes/torrents.$torrentId.tsx"),
  route("list", "routes/legacy-list.tsx"),
  route("torrent/:torrentId", "routes/legacy-torrent.$torrentId.tsx"),
  route("announcements", "routes/announcements.tsx"),
  route("notifications", "routes/notifications.tsx"),
  route("social", "routes/social.tsx"),
  route("social/post/:postId", "routes/social.post.$postId.tsx"),
  route("social/user/:username", "routes/social.user.$username.tsx"),
  route("messages", "routes/legacy-messages.tsx"),
  route("user/:username", "routes/user.$username.tsx"),
  route(
    "announcements/:announcementId",
    "routes/announcements.$announcementId.tsx"
  ),
  route("account", "routes/account.tsx"),
  route("account/email", "routes/account.email.tsx"),
  route("account/security", "routes/account.security.tsx"),
  route("account/rss", "routes/account.rss.tsx"),
  route("account/traffic", "routes/account.traffic.tsx"),
  route("account/seedbox", "routes/account.seedbox.tsx"),
  route(
    "account/download-restriction",
    "routes/account.download-restriction.tsx"
  ),
  route("account/ratio", "routes/account.ratio.tsx"),
  route("account/assessment", "routes/account.assessment.tsx"),
  route("account/invitations", "routes/account.invitations.tsx"),
  route("account/economy", "routes/account.economy.tsx"),
  route("account/hnr", "routes/account.hnr.tsx"),
  route("account/submissions", "routes/account.submissions.tsx"),
  route("review", "routes/legacy-review.tsx"),
  route("review/queue", "routes/review.queue.tsx"),
  route("account/bookmarks", "routes/account.bookmarks.tsx"),
  route("account/purchases", "routes/account.purchases.tsx"),
  route("account/promotions", "routes/account.promotions.tsx"),
  route("account/permissions", "routes/account.permissions.tsx"),
  route("workgroups", "routes/workgroups.tsx"),
  route("medals", "routes/medals.tsx"),
  route("admin", "routes/legacy-admin.tsx"),
  route("staff", "routes/staff.tsx"),
  route("staff/setup", "routes/staff.setup.tsx"),
  route("staff/users", "routes/staff.users.tsx"),
  route("staff/assessments", "routes/staff.assessments.tsx"),
  route("staff/workgroups", "routes/staff.workgroups.tsx"),
  route(
    "staff/content/announcements",
    "routes/staff.content.announcements.tsx"
  ),
  route("staff/content/comments", "routes/staff.content.comments.tsx"),
  route("staff/content/torrents", "routes/staff.content.torrents.tsx"),
  route(
    "staff/content/torrent-reviews",
    "routes/staff.content.torrent-reviews.tsx"
  ),
  route(
    "staff/content/torrent-purchases",
    "routes/staff.content.torrent-purchases.tsx"
  ),
  route("staff/content/promotions", "routes/staff.content.promotions.tsx"),
  route("staff/content/categories", "routes/staff.content.categories.tsx"),
  route("staff/enroll", "routes/staff.enroll.tsx"),
  route("staff/governance", "routes/staff.governance.tsx"),
  route("staff/settings/site", "routes/staff.settings.site.tsx"),
  route("staff/settings/storage", "routes/staff.settings.storage.tsx"),
  route("staff/settings/email", "routes/staff.settings.email.tsx"),
  route("staff/settings/vip-profile", "routes/staff.settings.vip-profile.tsx"),
  route("staff/settings/rss", "routes/staff.settings.rss.tsx"),
  route(
    "staff/settings/seeding-rewards",
    "routes/staff.settings.seeding-rewards.tsx"
  ),
  route(
    "staff/settings/progression/levels",
    "routes/staff.settings.progression.levels.tsx"
  ),
  route("staff/settings/medals", "routes/staff.settings.medals.tsx"),
  route(
    "staff/settings/activity-rewards",
    "routes/staff.settings.activity-rewards.tsx"
  ),
  route("staff/settings/magic-usage", "routes/staff.settings.magic-usage.tsx"),
  route("staff/settings/tracker", "routes/staff.settings.tracker.tsx"),
  route("staff/settings/torrents", "routes/staff.settings.torrents.tsx"),
  route("staff/settings/seedbox", "routes/staff.settings.seedbox.tsx"),
  route("staff/settings/ratio-hnr", "routes/staff.settings.ratio-hnr.tsx"),
  route("staff/settings/promotions", "routes/staff.settings.promotions.tsx"),
  route("staff/operations/tracker", "routes/staff.operations.tracker.tsx"),
  route("staff/operations/workers", "routes/staff.operations.workers.tsx"),
  route("staff/operations/incidents", "routes/staff.operations.incidents.tsx"),
  route(
    "staff/settings/registration",
    "routes/staff.settings.registration.tsx"
  ),
  route("*", "routes/catch-all.tsx"),
] satisfies RouteConfig
