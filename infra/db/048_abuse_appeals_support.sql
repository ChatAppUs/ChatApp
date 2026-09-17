-- gap pack 13: abuse reporting, content appeals, support tickets, age verification
-- closes ChatApp_Competitor_Comparison.md gaps:
--   "moderation queues, policy enforcement ... copyright workflows, age/safety controls, spam prevention"

-- Content reports (user-facing abuse reporting)
CREATE TABLE IF NOT EXISTS content_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id UUID NOT NULL REFERENCES users(id),
    target_type TEXT NOT NULL CHECK (target_type IN ('post','comment','user','message','reel','live')),
    target_id UUID NOT NULL,
    reason TEXT NOT NULL,
    detail TEXT DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','reviewed','resolved','dismissed')),
    resolution TEXT DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reviewed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_content_reports_reporter ON content_reports(reporter_id, created_at);
CREATE INDEX IF NOT EXISTS idx_content_reports_target ON content_reports(target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_content_reports_status ON content_reports(status);

-- Content appeals (user appeals moderation decisions)
CREATE TABLE IF NOT EXISTS content_appeals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    action_id UUID NOT NULL,
    reason TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','resolved')),
    resolution TEXT DEFAULT '' CHECK (resolution IN ('','upheld','overturned')),
    reviewer_note TEXT DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reviewed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_content_appeals_user ON content_appeals(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_content_appeals_status ON content_appeals(status);

-- Support tickets
CREATE TABLE IF NOT EXISTS support_tickets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    category TEXT NOT NULL DEFAULT 'other' CHECK (category IN ('account','billing','bug','abuse','copyright','other')),
    subject TEXT NOT NULL,
    body TEXT NOT NULL,
    priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low','normal','high')),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','resolved','closed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_support_tickets_user ON support_tickets(user_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_support_tickets_status ON support_tickets(status, priority, created_at);

-- Age verification: add birth_date and age_verified column to users
ALTER TABLE users ADD COLUMN IF NOT EXISTS birth_date DATE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS age_verified BOOLEAN NOT NULL DEFAULT FALSE;

-- Post rights for copyright/remix policy (gap pack 13)
ALTER TABLE posts ADD COLUMN IF NOT EXISTS rights_owner TEXT DEFAULT '';
ALTER TABLE posts ADD COLUMN IF NOT EXISTS remix_policy TEXT DEFAULT 'allow' CHECK (remix_policy IN ('allow','ask','block'));

-- Stitch and duet metadata on posts
ALTER TABLE posts ADD COLUMN IF NOT EXISTS stitch_source_id UUID;
ALTER TABLE posts ADD COLUMN IF NOT EXISTS stitch_start FLOAT8 DEFAULT 0;
ALTER TABLE posts ADD COLUMN IF NOT EXISTS stitch_end FLOAT8 DEFAULT 0;
ALTER TABLE posts ADD COLUMN IF NOT EXISTS parent_reel_id UUID;
ALTER TABLE posts ADD COLUMN IF NOT EXISTS duet_side TEXT DEFAULT 'right' CHECK (duet_side IN ('left','right'));

-- Moderation actions log (referenced by appeals)
CREATE TABLE IF NOT EXISTS moderation_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_user_id UUID NOT NULL REFERENCES users(id),
    moderator_id UUID,
    action_type TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id UUID NOT NULL,
    reason TEXT DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_mod_actions_user ON moderation_actions(target_user_id, created_at);