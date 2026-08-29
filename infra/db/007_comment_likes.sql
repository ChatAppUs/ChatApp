-- 007_comment_likes.sql — comment likes (Facebook/X parity)
BEGIN;

CREATE TABLE IF NOT EXISTS comment_likes (
    comment_id UUID NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (comment_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_comment_likes_user ON comment_likes(user_id);

COMMIT;
