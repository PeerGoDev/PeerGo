-- +goose Up

CREATE SCHEMA IF NOT EXISTS catalog;

CREATE TABLE catalog.categories (
    id text PRIMARY KEY,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 40),
    display_order integer NOT NULL CHECK (display_order >= 0),
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX categories_display_order_unique
    ON catalog.categories (display_order);

CREATE TABLE catalog.announcements (
    id text PRIMARY KEY,
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 160),
    summary text NOT NULL CHECK (char_length(summary) BETWEEN 1 AND 500),
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX announcements_latest_published_idx
    ON catalog.announcements (published_at DESC)
    WHERE published_at IS NOT NULL;

CREATE TABLE catalog.torrents (
    id text PRIMARY KEY,
    category_id text NOT NULL REFERENCES catalog.categories (id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 240),
    subtitle text NOT NULL DEFAULT '' CHECK (char_length(subtitle) <= 300),
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    promotion text NOT NULL DEFAULT 'none' CHECK (promotion IN ('none', 'free')),
    published_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX torrents_latest_published_idx
    ON catalog.torrents (published_at DESC, id DESC);

-- Swarm counters are an eventually consistent projection. Keeping them separate
-- prevents content writes from acquiring locks on the Tracker-owned data path.
CREATE TABLE catalog.torrent_swarm_stats (
    torrent_id text PRIMARY KEY REFERENCES catalog.torrents (id) ON DELETE CASCADE,
    seeders integer NOT NULL CHECK (seeders >= 0),
    leechers integer NOT NULL CHECK (leechers >= 0),
    completed integer NOT NULL CHECK (completed >= 0),
    observed_at timestamptz NOT NULL
);

-- +goose Down
DROP SCHEMA IF EXISTS catalog CASCADE;
