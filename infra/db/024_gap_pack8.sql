-- Gap pack 8 (2026-08-31): TikTok-style duet/stitch reel remixes.
-- remix_mode qualifies a remix (posts.remix_of): 'duet' renders the source
-- reel side-by-side with the response, 'stitch' plays the source clip first
-- followed by the response. NULL = plain remix (attribution only).

ALTER TABLE posts
  ADD COLUMN IF NOT EXISTS remix_mode TEXT;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'posts_remix_mode_check') THEN
    ALTER TABLE posts
      ADD CONSTRAINT posts_remix_mode_check CHECK (remix_mode IN ('duet', 'stitch'));
  END IF;
END $$;

-- A remix layout is meaningless without a source reel.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'posts_remix_mode_requires_source') THEN
    ALTER TABLE posts
      ADD CONSTRAINT posts_remix_mode_requires_source
      CHECK (remix_mode IS NULL OR remix_of IS NOT NULL);
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_posts_remix_of ON posts (remix_of) WHERE remix_of IS NOT NULL;
