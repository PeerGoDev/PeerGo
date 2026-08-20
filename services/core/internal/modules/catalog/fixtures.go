package catalog

import "time"

// NewDemoRepository provides synthetic local data so the walking skeleton can
// run without copying production users, passkeys, torrents, or IP addresses.
func NewDemoRepository(now time.Time) *MemoryRepository {
	movies := Category{ID: "movies", Name: "电影"}
	tv := Category{ID: "tv", Name: "剧集"}
	anime := Category{ID: "anime", Name: "动漫"}
	documentary := Category{ID: "documentary", Name: "纪录片"}
	music := Category{ID: "music", Name: "音乐"}

	return NewMemoryRepository(MemoryData{
		Site: SiteInfo{
			Name:                   "PeerGo",
			Description:            "面向真实协作与长期治理的私有分享社区。",
			OnlineUsers:            222,
			DefaultTorrentView:     TorrentViewList,
			ShowLatestAnnouncement: true,
		},
		Announcements: []Announcement{{
			AnnouncementSummary: AnnouncementSummary{
				ID:          "welcome-to-peergo",
				Title:       "欢迎来到 PeerGo",
				Summary:     "请在发布和下载前阅读站点规则，共同维护长期、稳定的分享环境。",
				PublishedAt: now.Add(-2 * time.Hour),
			},
			Body:       "PeerGo 希望建立一个重视内容质量、长期做种与友善交流的分享社区。\n\n发布资源前请核对文件、标题与分类；下载完成后请尽量保持做种。讨论时请围绕内容本身友善交流，不公开他人的个人信息。\n\n如遇到资源信息有误、文件异常或其他需要站务协助的情况，请通过页面提供的举报或反馈入口联系管理团队。",
			BodyFormat: AnnouncementBodyPlainText,
			Version:    1,
			UpdatedAt:  now.Add(-2 * time.Hour),
		}},
		Categories: []Category{movies, tv, anime, documentary, music},
		Torrents: []Torrent{
			{
				ID: 1, Name: "Cosmos Restored 2026 2160p WEB-DL HDR",
				Subtitle: "宇宙影像修复计划 · 双语字幕 · 纪录片系列", Category: documentary,
				SizeBytes: 18_742_348_800, Promotion: PromotionFree, UploadedAt: now.Add(-35 * time.Minute),
				Swarm: SwarmStats{Seeders: 86, Leechers: 7, Completed: 141, ObservedAt: now.Add(-40 * time.Second)},
			},
			{
				ID: 2, Name: "Night Train S02 1080p WEB-DL H.265 AAC",
				Subtitle: "夜行列车 第二季全 8 集 · 简繁字幕", Category: tv,
				SizeBytes: 9_841_624_064, Promotion: PromotionNone, UploadedAt: now.Add(-2 * time.Hour),
				Swarm: SwarmStats{Seeders: 41, Leechers: 12, Completed: 76, ObservedAt: now.Add(-90 * time.Second)},
			},
			{
				ID: 3, Name: "Paper Cranes 2025 BluRay 1080p AVC DTS-HD MA",
				Subtitle: "纸鹤 · 原盘保留 · 国英双语", Category: movies,
				SizeBytes: 31_385_128_960, Promotion: PromotionFree, UploadedAt: now.Add(-5 * time.Hour),
				Swarm: SwarmStats{Seeders: 29, Leechers: 4, Completed: 103, ObservedAt: now.Add(-8 * time.Minute)},
			},
			{
				ID: 4, Name: "Harbor Lights 1968 Restored 1080p BluRay",
				Subtitle: "港湾灯火 · 经典修复 · 导演评论音轨", Category: movies,
				SizeBytes: 21_904_277_504, Promotion: PromotionNone, UploadedAt: now.Add(-9 * time.Hour),
				Swarm: SwarmStats{Seeders: 17, Leechers: 1, Completed: 55, ObservedAt: now.Add(-2 * time.Minute)},
			},
			{
				ID: 5, Name: "Tiny Orchestra Live 2026 FLAC 24bit 96kHz",
				Subtitle: "室内乐现场录音 · 24bit Hi-Res", Category: music,
				SizeBytes: 3_482_533_888, Promotion: PromotionNone, UploadedAt: now.Add(-12 * time.Hour),
				Swarm: SwarmStats{Seeders: 12, Leechers: 2, Completed: 31, ObservedAt: now.Add(-70 * time.Second)},
			},
			{
				ID: 6, Name: "Sky Garden Complete Collection 1080p",
				Subtitle: "天空花园 · 全集 · 粤语与普通话音轨", Category: anime,
				SizeBytes: 27_531_231_232, Promotion: PromotionFree, UploadedAt: now.Add(-18 * time.Hour),
				Swarm: SwarmStats{Seeders: 64, Leechers: 9, Completed: 210, ObservedAt: now.Add(-3 * time.Minute)},
			},
		},
	})
}
