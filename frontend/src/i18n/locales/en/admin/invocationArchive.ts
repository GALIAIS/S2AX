export default {
  invocationArchive: {
    title: 'Invocation Archive',
    description: 'Keeps bounded encrypted copies of selected gateway calls for administrator review. It is off by default; plaintext requires step-up verification for every direct view.',
    tabs: { records: 'Records', config: 'Policy', runtime: 'Runtime' },
    modes: { off: 'Off', request_only: 'Request only', full: 'Request and response' },
    scopes: { user: 'User', group: 'Group', api_key: 'API Key' },
    outcomes: { completed: 'Completed', client_error: 'Client error', server_error: 'Server error', websocket_error: 'WebSocket error' },
    capture: {
      captured: 'Encrypted capture', empty: 'Empty payload', not_read: 'Not read', omitted: 'Omitted by policy', encryption_failed: 'Encryption failed', truncated: 'Truncated',
    },
    records: {
      title: 'Archive records',
      description: 'Only archive metadata is shown here. Viewing any request or response body requires a separate reason and step-up verification.',
      search: 'Search', searchPlaceholder: 'User, API Key, group, model, or request ID', mode: 'Archive mode', outcome: 'Outcome', userId: 'User ID', groupId: 'Group ID', apiKeyId: 'API Key ID', from: 'From', to: 'To',
      time: 'Time', identity: 'Identity', route: 'Route / model', capture: 'Capture state', result: 'Result', request: 'Request', response: 'Response',
      empty: 'No matching archive records.', selectAll: 'Select all records on this page', selectRecord: 'Select archive record {id}', deleteSelected: 'Delete selected ({count})', deleteTitle: 'Permanently delete invocation archives?', deleteMessage: 'This permanently deletes {count} selected archive record(s). It cannot be undone; access-audit evidence remains retained.',
    },
    config: {
      title: 'Archive policy',
      description: 'Rule precedence is API Key, user, group, then default. An off rule explicitly excludes a subject from a broader policy.',
      version: 'Configuration v{version}', privacyNotice: 'Archiving occurs only when an enabled policy matches. Request headers, cookies, API Keys, and upstream credentials are never stored. Payloads are AES-GCM encrypted before reaching the database and automatically removed at the retention deadline.',
      defaultMode: 'Default archive mode', retentionDays: 'Retention days', requestLimit: 'Request limit (MiB)', responseLimit: 'Response limit (MiB)', directView: 'Allow administrators to directly view encrypted payloads', directViewHint: 'When off, only metadata is available. When on, every plaintext view still requires step-up verification and writes an access log.',
      unsaved: 'Unsaved changes', synced: 'Configuration synced',
    },
    rules: {
      title: 'Scope rules', description: 'Create exact rules with the subject selector. Saving validates that every subject still exists and is not deleted.', count: '{count} rule(s)', scope: 'Scope', subjectSearch: 'Search subject', subjectSearchPlaceholder: 'Name, email, or ID', subject: 'Subject', selectSubject: 'Select a subject', mode: 'Mode', empty: 'No overrides. Every call uses the default policy.',
    },
    runtime: {
      title: 'Archive runtime', description: 'Archive persistence runs after the gateway response. A saturated queue drops archive work and never blocks or affects the user call.', status: 'Service status', running: 'Running', stopped: 'Stopped', version: 'Config v{version}', queue: 'Async queue', queueHint: 'Current depth / capacity', persisted: 'Persisted', acceptedDropped: 'Accepted {accepted} · dropped {dropped}', purge: 'Expired records purged', failures: 'Persistence failures {count}', configError: 'Configuration load error', persistError: 'Latest persistence error',
    },
    detail: {
      title: 'Archive record #{id}', createdAt: 'Created', expiresAt: 'Expires', outcome: 'Outcome', identity: 'Identity', group: 'Group', route: 'Route', model: 'Model', requestId: 'Request ID', client: 'Client',
      payloads: 'Encrypted payloads', payloadsHint: 'Bodies are not loaded with metadata. Plaintext exists only in this dialog memory and is cleared immediately when it closes.', directViewDisabled: 'Direct viewing is disabled by the current policy. Enable and save it in Archive policy first; enabling it also requires step-up verification.', revealReason: 'Reason for viewing', revealReasonPlaceholder: 'Enter at least 3 characters explaining this review', reveal: 'Verify and reveal payloads', revealHint: 'This view records the administrator, reason, time, and client information. Binary payloads are displayed as raw Base64.',
      accesses: 'Direct-view access log', accessTime: 'Time', admin: 'Administrator', accessOutcome: 'Outcome', reason: 'Reason', noAccesses: 'There are no direct-view access entries yet.',
    },
    messages: { saved: 'Invocation archive policy saved.', deleted: 'Deleted {count} archive record(s).' },
    errors: {
      loadConfig: 'Unable to load the invocation archive policy.', loadRuntime: 'Unable to load invocation archive runtime.', loadRecords: 'Unable to load archive records.', loadSubjects: 'Unable to search rule subjects.', loadDetail: 'Unable to load archive record details.', saveConfig: 'Unable to save the invocation archive policy.', reveal: 'Unable to reveal the archive payload.', delete: 'Unable to delete archive records.',
      invocation_archive_rule_duplicate: 'This subject already has a rule for the selected scope.', invocation_archive_config_conflict: 'Another administrator changed the configuration. Refresh and try again.', invocation_archive_rule_subject_not_found: 'The selected subject no longer exists or has been deleted.', invocation_archive_direct_view_disabled: 'An administrator has not enabled direct invocation archive viewing.', invocation_archive_payload_expired: 'The archive payload has expired.', invocation_archive_payload_unavailable: 'The archive payload is unavailable.', invocation_archive_reveal_reason_invalid: 'The reveal reason must be 3–256 characters.',
    },
  },
}
