-- Preserve the complete list of local content-moderation rule matches.
ALTER TABLE content_moderation_logs
    ALTER COLUMN matched_keyword TYPE TEXT;
