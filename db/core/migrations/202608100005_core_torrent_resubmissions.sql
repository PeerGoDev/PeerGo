-- +goose Up

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'torrent.submission.resubmit.self',
    '整改并重新提交自己的已驳回种子',
    'medium',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'torrent.submission.resubmit.self');

-- A resubmission is an immutable response to exactly one immutable rejection.
-- The torrent row remains the current aggregate, while this table preserves
-- the uploader-visible metadata snapshot and correction note used to reopen
-- review. Original metainfo identity and file evidence are never accepted here.
CREATE TABLE review.torrent_resubmissions (
    id uuid PRIMARY KEY,
    torrent_id bigint NOT NULL
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    responds_to_decision_id uuid NOT NULL UNIQUE
        REFERENCES review.torrent_decisions (id) ON DELETE RESTRICT,
    expected_torrent_version bigint NOT NULL CHECK (expected_torrent_version > 0),
    resulting_torrent_version bigint NOT NULL CHECK (
        resulting_torrent_version = expected_torrent_version + 1
    ),
    category_id text NOT NULL
        REFERENCES catalog.categories (id) ON DELETE RESTRICT,
    title text NOT NULL
        CHECK (char_length(btrim(title)) BETWEEN 1 AND 240),
    subtitle text NOT NULL DEFAULT ''
        CHECK (char_length(subtitle) <= 300),
    correction_note text NOT NULL
        CHECK (char_length(btrim(correction_note)) BETWEEN 10 AND 1000),
    authorization_decision_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (torrent_id, expected_torrent_version),
    UNIQUE (torrent_id, resulting_torrent_version)
);

CREATE INDEX torrent_resubmissions_history_idx
    ON review.torrent_resubmissions (torrent_id, occurred_at DESC, id DESC);

-- +goose StatementBegin
CREATE FUNCTION review.reject_torrent_resubmission_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'torrent resubmissions are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_resubmissions_immutable
BEFORE UPDATE OR DELETE ON review.torrent_resubmissions
FOR EACH ROW EXECUTE FUNCTION review.reject_torrent_resubmission_mutation();

-- +goose Down

DROP TRIGGER IF EXISTS torrent_resubmissions_immutable
    ON review.torrent_resubmissions;
DROP TABLE IF EXISTS review.torrent_resubmissions;
DROP FUNCTION IF EXISTS review.reject_torrent_resubmission_mutation();

DELETE FROM authz.role_permissions
WHERE role_id = 'member' AND action = 'torrent.submission.resubmit.self';
DELETE FROM authz.permissions
WHERE action = 'torrent.submission.resubmit.self';
