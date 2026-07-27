-- record_hash previously appeared in the policy schema even though the unified
-- action worker did not write to the request-path hash denylist. Remove it from
-- stored policies and fail any non-terminal legacy actions instead of reporting
-- a false success.

UPDATE security_audit_policy_versions
SET config = jsonb_set(
    jsonb_set(
        jsonb_set(
            jsonb_set(
                config,
                '{actions,low}',
                COALESCE((
                    SELECT jsonb_agg(entry.value ORDER BY entry.ordinality)
                    FROM jsonb_array_elements(COALESCE(config #> '{actions,low}', '[]'::jsonb))
                         WITH ORDINALITY AS entry(value, ordinality)
                    WHERE entry.value <> to_jsonb('record_hash'::text)
                ), '[]'::jsonb),
                TRUE
            ),
            '{actions,medium}',
            COALESCE((
                SELECT jsonb_agg(entry.value ORDER BY entry.ordinality)
                FROM jsonb_array_elements(COALESCE(config #> '{actions,medium}', '[]'::jsonb))
                     WITH ORDINALITY AS entry(value, ordinality)
                WHERE entry.value <> to_jsonb('record_hash'::text)
            ), '[]'::jsonb),
            TRUE
        ),
        '{actions,high}',
        COALESCE((
            SELECT jsonb_agg(entry.value ORDER BY entry.ordinality)
            FROM jsonb_array_elements(COALESCE(config #> '{actions,high}', '[]'::jsonb))
                 WITH ORDINALITY AS entry(value, ordinality)
            WHERE entry.value <> to_jsonb('record_hash'::text)
        ), '[]'::jsonb),
        TRUE
    ),
    '{actions,critical}',
    COALESCE((
        SELECT jsonb_agg(entry.value ORDER BY entry.ordinality)
        FROM jsonb_array_elements(COALESCE(config #> '{actions,critical}', '[]'::jsonb))
             WITH ORDINALITY AS entry(value, ordinality)
        WHERE entry.value <> to_jsonb('record_hash'::text)
    ), '[]'::jsonb),
    TRUE
)
WHERE COALESCE(config #> '{actions}', '{}'::jsonb) @> '{"low":["record_hash"]}'::jsonb
   OR COALESCE(config #> '{actions}', '{}'::jsonb) @> '{"medium":["record_hash"]}'::jsonb
   OR COALESCE(config #> '{actions}', '{}'::jsonb) @> '{"high":["record_hash"]}'::jsonb
   OR COALESCE(config #> '{actions}', '{}'::jsonb) @> '{"critical":["record_hash"]}'::jsonb;

UPDATE security_audit_actions
SET status='failed',
    processed_at=NOW(),
    lease_owner='',
    lease_expires_at=NULL,
    error_code='unsupported_action',
    error_message='record_hash does not have a request-path denylist executor',
    updated_at=NOW()
WHERE action_type='record_hash'
  AND status IN ('pending', 'processing', 'retry');

UPDATE security_audit_outbox AS outbox
SET status='failed',
    lease_owner='',
    lease_expires_at=NULL,
    last_error='record_hash does not have a request-path denylist executor',
    updated_at=NOW()
FROM security_audit_actions AS action
WHERE outbox.action_id=action.id
  AND action.action_type='record_hash'
  AND outbox.status IN ('pending', 'processing', 'retry');
