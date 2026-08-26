package authz

import "github.com/peergo/peergo/contracts/go/authzcontractv1"

const (
	ActionAccountEmailConfirmAnonymous            Action = "account.email.confirm.anonymous"
	ActionAccountEmailVerifySelf                  Action = "account.email.verify.self"
	ActionAccountPasswordRecoveryConfirmAnonymous Action = "account.password.recovery.confirm.anonymous"
	ActionAccountPasswordRecoveryRequestAnonymous Action = "account.password.recovery.request.anonymous"
	ActionAccountProfileUpdateSelf                Action = "account.profile.update.self"
	ActionAccountRegisterAnonymous                Action = "account.register.anonymous"
	ActionAccountTotpManageSelf                   Action = "account.totp.manage.self"
	ActionAnnouncementCommentCreateSelf           Action = "announcement.comment.create.self"
	ActionAnnouncementCreate                      Action = "announcement.create"
	ActionAnnouncementManageRead                  Action = "announcement.manage.read"
	ActionAnnouncementPublish                     Action = "announcement.publish"
	ActionAnnouncementRead                        Action = "announcement.read"
	ActionAnnouncementUpdate                      Action = "announcement.update"
	ActionAnnouncementWithdraw                    Action = "announcement.withdraw"
	ActionCapabilityReadSelf                      Action = "authz.capability.read.self"
	ActionStaffCapabilityReadSelf                 Action = "authz.capability.read.staff.self"
	ActionGrantRead                               Action = "authz.grant.read"
	ActionGrantRevokeGovernance                   Action = "authz.grant.revoke.approve.governance"
	ActionGrantRevokeSecurity                     Action = "authz.grant.revoke.approve.security"
	ActionGrantRevokePropose                      Action = "authz.grant.revoke.propose"
	ActionCategoryCreate                          Action = "category.create"
	ActionCategoryManageRead                      Action = "category.manage.read"
	ActionCategoryRead                            Action = "category.read"
	ActionCategoryUpdate                          Action = "category.update"
	ActionCommentDeleteSelf                       Action = "comment.delete.self"
	ActionCommentReportCreateSelf                 Action = "comment.report.create.self"
	ActionCommentUpdateSelf                       Action = "comment.update.self"
	ActionEconomyAttendanceClaimSelf              Action = "economy.attendance.claim.self"
	ActionEconomyAttendancePolicyIssue            Action = "economy.attendance.policy.issue"
	ActionEconomyAttendancePolicyRead             Action = "economy.attendance.policy.read"
	ActionEconomyAttendanceReadSelf               Action = "economy.attendance.read.self"
	ActionEconomyContentTipCreateSelf             Action = "economy.contenttip.create.self"
	ActionEconomyContentTipPolicyIssue            Action = "economy.contenttip.policy.issue"
	ActionEconomyContentTipPolicyRead             Action = "economy.contenttip.policy.read"
	ActionEconomyContentTipReadSelf               Action = "economy.contenttip.read.self"
	ActionEconomyMedalCreate                      Action = "economy.medal.create"
	ActionEconomyMedalManageRead                  Action = "economy.medal.manage.read"
	ActionEconomyMedalPurchaseSelf                Action = "economy.medal.purchase.self"
	ActionEconomyMedalReadSelf                    Action = "economy.medal.read.self"
	ActionEconomyMedalUpdate                      Action = "economy.medal.update"
	ActionEconomyMedalWearSelf                    Action = "economy.medal.wear.self"
	ActionEconomyMemberGiftCreateSelf             Action = "economy.membergift.create.self"
	ActionEconomyMemberGiftPolicyIssue            Action = "economy.membergift.policy.issue"
	ActionEconomyMemberGiftPolicyRead             Action = "economy.membergift.policy.read"
	ActionEconomyMemberGiftReadSelf               Action = "economy.membergift.read.self"
	ActionEconomyReadSelf                         Action = "economy.read.self"
	ActionEconomySeedingRewardPolicyIssue         Action = "economy.seedingreward.policy.issue"
	ActionEconomySeedingRewardPolicyRead          Action = "economy.seedingreward.policy.read"
	ActionHNRAppealCreateSelf                     Action = "hnr.appeal.create.self"
	ActionHNRAssessmentManage                     Action = "hnr.assessment.manage"
	ActionHNRPolicyIssue                          Action = "hnr.policy.issue"
	ActionHNRPolicyRead                           Action = "hnr.policy.read"
	ActionHNRReadSelf                             Action = "hnr.read.self"
	ActionIntegrationMoviePilotManageSelf         Action = "integration.moviepilot.manage.self"
	ActionIntegrationMoviePilotReadSelf           Action = "integration.moviepilot.read.self"
	ActionInvitationIssueSelf                     Action = "invitation.issue.self"
	ActionInvitationReadSelf                      Action = "invitation.read.self"
	ActionInvitationRevokeSelf                    Action = "invitation.revoke.self"
	ActionNewcomerAssessmentExempt                Action = "newcomer.assessment.exempt"
	ActionNewcomerAssessmentRead                  Action = "newcomer.assessment.read"
	ActionNewcomerAssessmentReadSelf              Action = "newcomer.assessment.read.self"
	ActionNewcomerPolicyIssue                     Action = "newcomer.policy.issue"
	ActionNewcomerPolicyRead                      Action = "newcomer.policy.read"
	ActionNotificationArchiveSelf                 Action = "notification.archive.self"
	ActionNotificationFeedbackCreateSelf          Action = "notification.feedback.create.self"
	ActionNotificationReadSelf                    Action = "notification.read.self"
	ActionNotificationReadStateWriteSelf          Action = "notification.read.state.write.self"
	ActionOperationsEmailTest                     Action = "operations.email.test"
	ActionOperationsMonitorRead                   Action = "operations.monitor.read"
	ActionProgressionContributionPolicyIssue      Action = "progression.contribution.policy.issue"
	ActionProgressionContributionPolicyRead       Action = "progression.contribution.policy.read"
	ActionProgressionLevelPolicyIssue             Action = "progression.level.policy.issue"
	ActionProgressionLevelPolicyRead              Action = "progression.level.policy.read"
	ActionPromotionManageRead                     Action = "promotion.manage.read"
	ActionPromotionSchedule                       Action = "promotion.schedule"
	ActionRSSSettingsManageRead                   Action = "rss.settings.manage.read"
	ActionRSSSettingsUpdate                       Action = "rss.settings.update"
	ActionRSSSubscriptionManageSelf               Action = "rss.subscription.manage.self"
	ActionRSSSubscriptionReadSelf                 Action = "rss.subscription.read.self"
	ActionRSSSubscriptionToken                    Action = "rss.subscription.token"
	ActionRatioAppealCreateSelf                   Action = "ratio.appeal.create.self"
	ActionRatioAssessmentManage                   Action = "ratio.assessment.manage"
	ActionRatioAssessmentReadSelf                 Action = "ratio.assessment.read.self"
	ActionRatioPolicyIssue                        Action = "ratio.policy.issue"
	ActionRatioPolicyRead                         Action = "ratio.policy.read"
	ActionSessionCreateSelf                       Action = "session.create.self"
	ActionSessionReadSelf                         Action = "session.read.self"
	ActionSessionRevokeSelf                       Action = "session.revoke.self"
	ActionSiteDisplayManageRead                   Action = "site.display.manage.read"
	ActionSiteDisplayUpdate                       Action = "site.display.update"
	ActionSiteRead                                Action = "site.read"
	ActionSiteRegistrationManageRead              Action = "site.registration.manage.read"
	ActionSiteRegistrationUpdate                  Action = "site.registration.update"
	ActionSocialBoardCreate                       Action = "social.board.create"
	ActionSocialBoardManageRead                   Action = "social.board.manage.read"
	ActionSocialBoardUpdate                       Action = "social.board.update"
	ActionSocialFollowWriteSelf                   Action = "social.follow.write.self"
	ActionSocialMediaCreateSelf                   Action = "social.media.create.self"
	ActionSocialPollVoteSelf                      Action = "social.poll.vote.self"
	ActionSocialPostCommentCreateSelf             Action = "social.post.comment.create.self"
	ActionSocialPostCreateRestrictedSelf          Action = "social.post.create.restricted.self"
	ActionSocialPostCreateSelf                    Action = "social.post.create.self"
	ActionSocialPostDeleteSelf                    Action = "social.post.delete.self"
	ActionSocialPostLikeSelf                      Action = "social.post.like.self"
	ActionSocialPostManageRead                    Action = "social.post.manage.read"
	ActionSocialPostModerate                      Action = "social.post.moderate"
	ActionSocialPostRead                          Action = "social.post.read"
	ActionSocialPostRepostSelf                    Action = "social.post.repost.self"
	ActionSocialPostUpdateSelf                    Action = "social.post.update.self"
	ActionSocialRedPacketClaimSelf                Action = "social.redpacket.claim.self"
	ActionSocialReportRead                        Action = "social.report.read"
	ActionSocialReportResolve                     Action = "social.report.resolve"
	ActionStaffCredentialEnrollSelf               Action = "staff.credential.enroll.self"
	ActionStaffSessionCreateSelf                  Action = "staff.session.create.self"
	ActionStaffSessionReadSelf                    Action = "staff.session.read.self"
	ActionStaffSessionRevokeSelf                  Action = "staff.session.revoke.self"
	ActionTorrentBookmarkReadSelf                 Action = "torrent.bookmark.read.self"
	ActionTorrentBookmarkWriteSelf                Action = "torrent.bookmark.write.self"
	ActionTorrentCommentCreateSelf                Action = "torrent.comment.create.self"
	ActionTorrentContentChangeReview              Action = "torrent.content.change.review"
	ActionTorrentContentChangeSubmitSelf          Action = "torrent.content.change.submit.self"
	ActionTorrentDownload                         Action = "torrent.download"
	ActionTorrentLifecycleUpdate                  Action = "torrent.lifecycle.update"
	ActionTorrentManageRead                       Action = "torrent.manage.read"
	ActionTorrentMetadataUpdateSelf               Action = "torrent.metadata.update.self"
	ActionTorrentPeerReadMember                   Action = "torrent.peer.read.member"
	ActionTorrentPromotionPurchaseSelf            Action = "torrent.promotion.purchase.self"
	ActionTorrentPurchaseCreateSelf               Action = "torrent.purchase.create.self"
	ActionTorrentPurchaseManageRead               Action = "torrent.purchase.manage.read"
	ActionTorrentPurchaseManageRefund             Action = "torrent.purchase.manage.refund"
	ActionTorrentPurchaseManageUpdate             Action = "torrent.purchase.manage.update"
	ActionTorrentPurchaseReadSelf                 Action = "torrent.purchase.read.self"
	ActionTorrentRead                             Action = "torrent.read"
	ActionTorrentReportCreateSelf                 Action = "torrent.report.create.self"
	ActionTorrentReportReview                     Action = "torrent.report.review"
	ActionTorrentReview                           Action = "torrent.review"
	ActionTorrentReviewVote                       Action = "torrent.review.vote"
	ActionTorrentScreenshotChangeReview           Action = "torrent.screenshot.change.review"
	ActionTorrentScreenshotChangeSubmitSelf       Action = "torrent.screenshot.change.submit.self"
	ActionTorrentSubmissionReadSelf               Action = "torrent.submission.read.self"
	ActionTorrentSubmissionResubmitSelf           Action = "torrent.submission.resubmit.self"
	ActionTorrentSubmit                           Action = "torrent.submit"
	ActionTorrentUploadPolicyIssue                Action = "torrent.upload.policy.issue"
	ActionTorrentWithdrawRequestSelf              Action = "torrent.withdraw.request.self"
	ActionTorrentWithdrawReview                   Action = "torrent.withdraw.review"
	ActionTrackerPolicyIssue                      Action = "tracker.policy.issue"
	ActionTrackerPolicyRead                       Action = "tracker.policy.read"
	ActionTrackerSeedboxReadSelf                  Action = "tracker.seedbox.read.self"
	ActionTrackerSeedboxRegistryRead              Action = "tracker.seedbox.registry.read"
	ActionTrackerSeedboxReportCreateSelf          Action = "tracker.seedbox.report.create.self"
	ActionTrackerSeedboxReportDecide              Action = "tracker.seedbox.report.decide"
	ActionTrafficReadSelf                         Action = "traffic.read.self"
	ActionUserAccountAppealCreateRestricted       Action = "user.account.appeal.create.restricted"
	ActionUserAccountAppealDecide                 Action = "user.account.appeal.decide"
	ActionUserAccountAppealRead                   Action = "user.account.appeal.read"
	ActionUserAccountRead                         Action = "user.account.read"
	ActionUserAccountRestrict                     Action = "user.account.restrict"
	ActionUserAccountRestrictionRevoke            Action = "user.account.restriction.revoke"
	ActionUserDownloadRestrictionAppealCreateSelf Action = "user.downloadrestriction.appeal.create.self"
	ActionUserDownloadRestrictionReadSelf         Action = "user.downloadrestriction.read.self"
	ActionUserDownloadRestrictionRestrict         Action = "user.downloadrestriction.restrict"
	ActionUserDownloadRestrictionRevoke           Action = "user.downloadrestriction.revoke"
	ActionUserProfileReadMember                   Action = "user.profile.read.member"
	ActionUserVIPManage                           Action = "user.vip.manage"
	ActionWikiPageCreate                          Action = "wiki.page.create"
	ActionWikiPageManageRead                      Action = "wiki.page.manage.read"
	ActionWikiPageRead                            Action = "wiki.page.read"
	ActionWikiPageReadMember                      Action = "wiki.page.read.member"
	ActionWikiPageRestore                         Action = "wiki.page.restore"
	ActionWikiPageUpdate                          Action = "wiki.page.update"
	ActionWikiPageUpdateAssigned                  Action = "wiki.page.update.assigned"
	ActionWorkgroupApplicationCreateSelf          Action = "workgroup.application.create.self"
	ActionWorkgroupApplicationDecide              Action = "workgroup.application.decide"
	ActionWorkgroupContributionPolicyIssue        Action = "workgroup.contribution.policy.issue"
	ActionWorkgroupContributionReminderIssue      Action = "workgroup.contribution.reminder.issue"
	ActionWorkgroupManageRead                     Action = "workgroup.manage.read"
	ActionWorkgroupMembershipManage               Action = "workgroup.membership.manage"
	ActionWorkgroupReadSelf                       Action = "workgroup.read.self"
	ActionWorkgroupTaskPublish                    Action = "workgroup.task.publish"
	ActionWorkgroupTaskReview                     Action = "workgroup.task.review"
	ActionWorkgroupTaskSubmitSelf                 Action = "workgroup.task.submit.self"
)

// permissionCatalog is alphabetically ordered so database and contract drift
// failures remain deterministic in startup diagnostics and tests.
var permissionCatalog = []PermissionDefinition{
	{
		Action: ActionAccountEmailConfirmAnonymous, Description: "使用一次性凭证确认邮箱所有权", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceAnonymous,
	},
	{
		Action: ActionAccountEmailVerifySelf, Description: "验证自己的登录邮箱", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionAccountPasswordRecoveryConfirmAnonymous, Description: "使用一次性凭证恢复账户密码", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceAnonymous,
	},
	{
		Action: ActionAccountPasswordRecoveryRequestAnonymous, Description: "请求账户密码恢复邮件", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceAnonymous,
	},
	{
		Action: ActionAccountProfileUpdateSelf, Description: "修改自己的公开资料", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionAccountRegisterAnonymous, Description: "按当前准入策略创建普通账户", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceAnonymous,
	},
	{
		Action: ActionAccountTotpManageSelf, Description: "管理自己的 TOTP 与恢复码", Risk: RiskHigh,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionAnnouncementCommentCreateSelf, Description: "在已发布公告下发表评论", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionAnnouncementCreate, Description: "创建公告草稿", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionAnnouncementManageRead, Description: "读取公告编辑与版本视图", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionAnnouncementPublish, Description: "立即或定时发布公告", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionAnnouncementRead, Description: "读取公开公告", Risk: RiskLow,
		Relationship: RelationshipNone, CredentialAudience: AudienceAnonymous,
	},
	{
		Action: ActionAnnouncementUpdate, Description: "追加公告草稿版本", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionAnnouncementWithdraw, Description: "撤回已发布或已排期公告", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionCapabilityReadSelf, Description: "查看自己的当前有效权限", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionStaffCapabilityReadSelf, Description: "查看自己的当前后台能力", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceStaffSession,
		Grantable: true,
	},
	{
		Action: ActionGrantRead, Description: "读取权限、任期与撤权审批状态", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionGrantRevokeGovernance, Description: "以治理职责复核 grant 撤销", Risk: RiskCritical,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionGrantRevokeSecurity, Description: "以安全职责复核 grant 撤销", Risk: RiskCritical,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionGrantRevokePropose, Description: "提议撤销他人的有效 grant", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionCategoryCreate, Description: "创建种子分类", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionCategoryManageRead, Description: "读取分类管理视图", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionCategoryRead, Description: "读取公开种子分类", Risk: RiskLow,
		Relationship: RelationshipNone, CredentialAudience: AudienceAnonymous,
	},
	{
		Action: ActionCategoryUpdate, Description: "更新或停用种子分类", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionCommentDeleteSelf, Description: "删除自己的评论", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionCommentReportCreateSelf, Description: "举报一条他人可见评论", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionCommentUpdateSelf, Description: "编辑自己的评论", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyAttendanceClaimSelf, Description: "完成自己的每日签到并领取魔力值与经验", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyAttendancePolicyIssue, Description: "签发未来生效的不可变签到政策", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyAttendancePolicyRead, Description: "读取签到政策时间线", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyAttendanceReadSelf, Description: "查看自己的签到状态与历史", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyContentTipCreateSelf, Description: "给一条公开内容的作者打赏自己的整数魔力值", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyContentTipPolicyIssue, Description: "签发立即生效的不可变内容打赏政策", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyContentTipPolicyRead, Description: "读取内容打赏政策时间线", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyContentTipReadSelf, Description: "查看自己的内容打赏规则与收发记录", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyMedalCreate, Description: "创建勋章定义", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyMedalManageRead, Description: "读取勋章定义和持有数量", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyMedalPurchaseSelf, Description: "使用自己的整数魔力值购买勋章", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyMedalReadSelf, Description: "查看自己的勋章、佩戴加成和勋章商店", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyMedalUpdate, Description: "更新勋章图片、发放方式和权益参数", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyMedalWearSelf, Description: "佩戴、取下并调整自己的勋章展示顺序", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyMemberGiftCreateSelf, Description: "向一名正常成员赠送自己的整数魔力值", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyMemberGiftPolicyIssue, Description: "签发立即生效的不可变成员赠送政策", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyMemberGiftPolicyRead, Description: "读取成员赠送政策时间线", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyMemberGiftReadSelf, Description: "查看自己的成员赠送规则与收发记录", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomyReadSelf, Description: "查看自己的魔力值账本、经验和等级进度", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomySeedingRewardPolicyIssue, Description: "签发未来生效的不可变做种奖励政策", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionEconomySeedingRewardPolicyRead, Description: "读取做种奖励政策时间线与预览", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionHNRAppealCreateSelf, Description: "为自己的逾期 H&R 义务提交一次申诉", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionHNRAssessmentManage, Description: "批准或驳回 H&R 申诉并签发本地豁免", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionHNRPolicyIssue, Description: "签发未来生效的全站 H&R 政策", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionHNRPolicyRead, Description: "读取 H&R 政策时间线与投递状态", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionHNRReadSelf, Description: "查看自己的 H&R 义务与达标进度", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionIntegrationMoviePilotManageSelf, Description: "创建、轮换或撤销自己的 MoviePilot API Key", Risk: RiskHigh,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionIntegrationMoviePilotReadSelf, Description: "查看自己的 MoviePilot API Key 状态", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionInvitationIssueSelf, Description: "签发自己的单次注册邀请码", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionInvitationReadSelf, Description: "查看自己的邀请名额与邀请记录", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionInvitationRevokeSelf, Description: "撤销自己尚未被领取的邀请码", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionNewcomerAssessmentExempt, Description: "人工豁免一条新人考核", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionNewcomerAssessmentRead, Description: "读取新人考核名单和进度", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionNewcomerAssessmentReadSelf, Description: "读取自己的新人考核进度", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionNewcomerPolicyIssue, Description: "签发未来生效的新人考核规则", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionNewcomerPolicyRead, Description: "读取新人考核规则和运行状态", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionNotificationArchiveSelf, Description: "归档自己的站内通知", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession, Grantable: true, Discoverable: true,
	},
	{
		Action: ActionNotificationFeedbackCreateSelf, Description: "向站点管理员提交反馈", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession, Grantable: true, Discoverable: true,
	},
	{
		Action: ActionNotificationReadSelf, Description: "查看自己的站内通知", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionNotificationReadStateWriteSelf, Description: "更新自己的通知已读状态", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionOperationsEmailTest, Description: "向指定地址发送一次邮件投递测试", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionOperationsMonitorRead, Description: "读取 Core 中的 Tracker 投影与 Worker 队列状态", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionProgressionContributionPolicyIssue, Description: "签发上传、发种与账号时长经验政策", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionProgressionContributionPolicyRead, Description: "读取上传、发种与账号时长经验政策", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionProgressionLevelPolicyIssue, Description: "签发未来生效的经验等级规则", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionProgressionLevelPolicyRead, Description: "查看经验等级规则时间线和用户分布", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionPromotionManageRead, Description: "读取优惠政策时间线与投递状态", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionPromotionSchedule, Description: "签发全站或单种子优惠政策", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionRatioAppealCreateSelf, Description: "提交自己当前长期分享率考核的一次申诉", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionRatioAssessmentManage, Description: "人工解除一条长期分享率考核", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionRatioAssessmentReadSelf, Description: "查看自己的长期分享率考核与恢复进度", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionRatioPolicyIssue, Description: "签发未来生效的全站长期分享率规则", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionRatioPolicyRead, Description: "读取长期分享率规则、考核和运行状态", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionRSSSettingsManageRead, Description: "读取 RSS 服务设置", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionRSSSettingsUpdate, Description: "修改 RSS 服务设置", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionRSSSubscriptionManageSelf, Description: "创建、修改或撤销自己的 RSS 订阅", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionRSSSubscriptionReadSelf, Description: "查看自己的 RSS 订阅", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionRSSSubscriptionToken, Description: "使用独立令牌读取一个固定 RSS 订阅及其种子附件", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceRSSToken,
	},
	{
		Action: ActionSessionCreateSelf, Description: "创建自己的 Web 会话", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceAnonymous,
	},
	{
		Action: ActionSessionReadSelf, Description: "读取自己的当前 Web 会话", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSessionRevokeSelf, Description: "撤销自己的当前 Web 会话", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSiteDisplayManageRead, Description: "读取站点与展示设置", Risk: RiskLow,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSiteDisplayUpdate, Description: "更新低风险站点与展示设置", Risk: RiskLow,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSiteRead, Description: "读取公开站点信息", Risk: RiskLow,
		Relationship: RelationshipNone, CredentialAudience: AudienceAnonymous,
	},
	{
		Action: ActionSiteRegistrationManageRead, Description: "读取站点注册准入策略", Risk: RiskLow,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSiteRegistrationUpdate, Description: "更新站点注册准入策略", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialBoardCreate, Description: "创建动态圈板块", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialBoardManageRead, Description: "读取动态圈板块管理视图", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialBoardUpdate, Description: "更新或停用动态圈板块", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialFollowWriteSelf, Description: "关注或取消关注其他成员", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialMediaCreateSelf, Description: "上传动态图片", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialPollVoteSelf, Description: "参与动态投票", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialPostCommentCreateSelf, Description: "在可见动态下发表评论", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialPostCreateRestrictedSelf, Description: "向仅限管理团队的动态圈板块发布动态", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialPostCreateSelf, Description: "向动态圈板块发布动态", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialPostDeleteSelf, Description: "删除自己的动态", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialPostLikeSelf, Description: "点赞或取消点赞动态", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialPostManageRead, Description: "读取动态圈内容管理视图", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialPostModerate, Description: "移动、置顶、加精、隐藏或恢复动态", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialPostRead, Description: "读取公开动态圈", Risk: RiskLow,
		Relationship: RelationshipNone, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialPostRepostSelf, Description: "转发或取消转发动态", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialPostUpdateSelf, Description: "编辑自己的动态", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialRedPacketClaimSelf, Description: "领取动态红包", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialReportRead, Description: "读取脱敏评论举报审核队列", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionSocialReportResolve, Description: "关闭举报案件或隐藏违规评论", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionStaffCredentialEnrollSelf, Description: "使用受控票据注册自己的后台 WebAuthn 凭据", Risk: RiskCritical,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionStaffSessionCreateSelf, Description: "通过 WebAuthn 创建自己的后台会话", Risk: RiskHigh,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionStaffSessionReadSelf, Description: "读取自己的当前后台会话", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceStaffSession,
		Grantable: true,
	},
	{
		Action: ActionStaffSessionRevokeSelf, Description: "撤销自己的当前后台会话", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceStaffSession,
		Grantable: true,
	},
	{
		Action: ActionTorrentBookmarkReadSelf, Description: "查看自己的种子收藏", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentBookmarkWriteSelf, Description: "添加或取消自己的种子收藏", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentCommentCreateSelf, Description: "在已发布种子下发表评论", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentContentChangeReview, Description: "审核已发布种子的内容资料修改", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentContentChangeSubmitSelf, Description: "提交自己已发布种子的内容资料修改", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentDownload, Description: "下载绑定本人 Tracker 凭据的已发布种子副本", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentLifecycleUpdate, Description: "下架或恢复已发布种子", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentManageRead, Description: "读取种子管理工作台", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentMetadataUpdateSelf, Description: "修改自己已发布种子的基础发布资料", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentPeerReadMember, Description: "查看已发布种子的隐私最小化实时用户列表", Risk: RiskLow,
		Relationship: RelationshipNone, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentPromotionPurchaseSelf, Description: "使用自己的整数魔力值为已发布种子购买限时优惠或置顶", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentPurchaseCreateSelf, Description: "使用整数魔力值购买指定种子的永久下载权限", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentPurchaseManageRead, Description: "查看全站种子购买与退款记录", Risk: RiskLow,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentPurchaseManageRefund, Description: "撤销种子购买权限并由站点返还整数魔力值", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentPurchaseManageUpdate, Description: "更新全站种子购买规则或单个种子的整数魔力值价格", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentPurchaseReadSelf, Description: "查看自己对指定种子的购买状态与价格", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentRead, Description: "读取公开种子摘要", Risk: RiskLow,
		Relationship: RelationshipNone, CredentialAudience: AudienceAnonymous,
	},
	{
		Action: ActionTorrentReportCreateSelf, Description: "举报一条他人已发布的种子", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentReportReview, Description: "审核种子举报并执行有界下架", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentReview, Description: "审核单个待发布种子", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentReviewVote, Description: "以有效种审组成员身份参与单个待审核种子的投票", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentScreenshotChangeReview, Description: "审核已发布种子的截图附件修改", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentScreenshotChangeSubmitSelf, Description: "提交自己已发布种子的截图附件修改", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentSubmissionReadSelf, Description: "查看自己的种子提交状态与审核反馈", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentSubmissionResubmitSelf, Description: "整改并重新提交自己的已驳回种子", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentSubmit, Description: "提交自己的种子进入审核", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentUploadPolicyIssue, Description: "签发新种子上传与截图限制版本", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentWithdrawRequestSelf, Description: "申请撤回自己已发布的种子", Risk: RiskHigh,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTorrentWithdrawReview, Description: "审核已发布种子的撤回申请", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTrackerPolicyIssue, Description: "签发 Tracker announce、scrape、客户端与请求频率政策", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTrackerPolicyRead, Description: "读取 Tracker 运行政策与签名发布状态", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTrackerSeedboxReadSelf, Description: "查看自己的盒子申报和审核结果", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTrackerSeedboxRegistryRead, Description: "查看盒子申报、用户绑定地址和审核记录", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTrackerSeedboxReportCreateSelf, Description: "申报自己的盒子主机地址等待审核", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTrackerSeedboxReportDecide, Description: "批准或驳回盒子申报并签发用户绑定规则", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionTrafficReadSelf, Description: "查看自己的最终流量汇总与结算记录", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionUserAccountAppealCreateRestricted, Description: "使用受限账户凭据查询本人限制并提交一次复核申请", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceAnonymous,
		Discoverable: true,
	},
	{
		Action: ActionUserAccountAppealDecide, Description: "批准或驳回账户访问限制复核申请", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionUserAccountAppealRead, Description: "读取账户访问限制复核队列", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionUserAccountRead, Description: "读取账户目录、完整邮箱、运营统计与当前有效限制", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionUserAccountRestrict, Description: "临时限制账户访问", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionUserAccountRestrictionRevoke, Description: "显式撤销账户访问限制", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionUserDownloadRestrictionAppealCreateSelf, Description: "为自己的旧站或人工下载限制提交一次复核申请", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionUserDownloadRestrictionReadSelf, Description: "查看自己的下载限制来源与旧站或人工限制申诉状态", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionUserDownloadRestrictionRestrict, Description: "签发或修改一个账户的人工下载限制", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionUserDownloadRestrictionRevoke, Description: "解除一个账户的人工下载限制", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionUserProfileReadMember, Description: "查看站内成员的公开资料", Risk: RiskLow,
		Relationship: RelationshipNone, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionUserVIPManage, Description: "签发、续期或撤销一个账户的 VIP 身份", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWikiPageCreate, Description: "创建并配置 Wiki 页面", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWikiPageManageRead, Description: "读取 Wiki 管理与版本视图", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWikiPageRead, Description: "查看公开 Wiki 页面", Risk: RiskLow,
		Relationship: RelationshipNone, CredentialAudience: AudienceAnonymous,
	},
	{
		Action: ActionWikiPageReadMember, Description: "查看仅站内成员可见的 Wiki 页面", Risk: RiskLow,
		Relationship: RelationshipNone, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWikiPageRestore, Description: "从历史修订恢复 Wiki 页面", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWikiPageUpdate, Description: "修改 Wiki 页面配置、正文与协作者", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWikiPageUpdateAssigned, Description: "修改自己创建或被指派协作的 Wiki 正文", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWorkgroupApplicationCreateSelf, Description: "申请加入允许自助申请的工作组", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWorkgroupApplicationDecide, Description: "审批工作组加入申请", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWorkgroupContributionPolicyIssue, Description: "签发未来自然月生效的工作组贡献目标", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWorkgroupContributionReminderIssue, Description: "依据已冻结的贡献周期快照向工作组成员发送人工提醒", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWorkgroupManageRead, Description: "查看工作组申请、成员与不可变变更记录", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWorkgroupMembershipManage, Description: "授予、暂停、恢复或结束工作组成员资格", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWorkgroupReadSelf, Description: "查看自己的工作组资格与申请状态", Risk: RiskLow,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWorkgroupTaskPublish, Description: "向一个固定工作组发布任务或活动并冻结成员名单", Risk: RiskHigh,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWorkgroupTaskReview, Description: "人工验收工作组成员提交的任务成果", Risk: RiskMedium,
		Relationship: RelationshipNone, CredentialAudience: AudienceStaffSession,
		Grantable: true, Discoverable: true,
	},
	{
		Action: ActionWorkgroupTaskSubmitSelf, Description: "提交自己被分配的工作组任务成果", Risk: RiskMedium,
		Relationship: RelationshipSelf, CredentialAudience: AudienceWebSession,
		Grantable: true, Discoverable: true,
	},
}

func Catalog() []PermissionDefinition {
	return append([]PermissionDefinition(nil), permissionCatalog...)
}

func Lookup(action Action) (PermissionDefinition, bool) {
	for _, definition := range permissionCatalog {
		if definition.Action == action {
			return definition, true
		}
	}
	return PermissionDefinition{}, false
}

func SiteScope() Scope {
	return Scope{Type: ScopeType(authzcontractv1.SiteScopeType), ID: authzcontractv1.SiteScopeID}
}
