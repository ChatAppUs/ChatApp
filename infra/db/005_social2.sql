-- 005_social2.sql — pins, forwards, saved messages, story interactions,
-- threads, post editing (competitor-parity batch). Reposts already exist
-- via posts.repost_of (002_features.sql).
BEGIN;

ALTER TABLE posts
    ADD COLUMN IF NOT EXISTS thread_parent_id UUID REFERENCES posts(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS edited_at        TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_posts_thread ON posts (thread_parent_id) WHERE thread_parent_id IS NOT NULL;

-- Pinned messages (Telegram/FB Messenger)
CREATE TABLE IF NOT EXISTS message_pins (
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    message_id      UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    pinned_by       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pinned_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, message_id)
);

-- Forward attribution + story-reply reference on messages
ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS forwarded_from UUID REFERENCES messages(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS story_id       UUID REFERENCES posts(id) ON DELETE SET NULL;

-- Saved Messages (Telegram self-chat): one per user, single member
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS is_saved BOOLEAN NOT NULL DEFAULT FALSE;
CREATE UNIQUE INDEX IF NOT EXISTS idx_conversations_saved
    ON conversations (created_by) WHERE is_saved;

-- Story engagement
CREATE TABLE IF NOT EXISTS story_views (
    story_id  UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    viewer_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    viewed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (story_id, viewer_id)
);
CREATE TABLE IF NOT EXISTS story_reactions (
    story_id   UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    emoji      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (story_id, user_id)
);

COMMIT;
