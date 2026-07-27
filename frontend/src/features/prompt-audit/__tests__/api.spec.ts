import { beforeEach, describe, expect, it, vi } from 'vitest'
import { emptyEventFilters } from '../viewModel'

const client = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn(), delete: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))

import promptAuditAPI from '../api'

describe('Prompt Audit API', () => {
  beforeEach(() => Object.values(client).forEach((mock) => mock.mockReset()))

  it('uses the independent admin route namespace', async () => {
    client.get.mockResolvedValue({ data: { config_version: 1 } })
    await promptAuditAPI.getConfig()
    expect(client.get).toHaveBeenCalledWith('/admin/prompt-audit/config')

    client.get.mockResolvedValue({ data: { process_status: 'running' } })
    await promptAuditAPI.getRuntime()
    expect(client.get).toHaveBeenCalledWith('/admin/prompt-audit/runtime')
  })

  it('sends a temporary probe token only in the request and never invents response credentials', async () => {
    client.post.mockResolvedValue({ data: { ok: true, token_applied: true } })
    const result = await promptAuditAPI.probeEndpoint({
      id: 'guard-1', name: 'Guard', protocol: 'openai_compatible', base_url: 'http://127.0.0.1:8000', network_scope: 'loopback', model: 'guard',
      token: 'api-canary-secret', clear_token: false, timeout_ms: 1000, input_limit: 1000, enabled: true, has_token: false, token_status: 'missing',
    })
    expect(client.post).toHaveBeenCalledWith('/admin/prompt-audit/endpoints/probe', expect.objectContaining({ endpoint: expect.objectContaining({ token: 'api-canary-secret', network_scope: 'loopback' }) }))
    expect(JSON.stringify(result)).not.toContain('api-canary-secret')
  })

  it('passes a server preview token through the confirmed filter-delete contract', async () => {
    client.post.mockResolvedValue({ data: { deleted_events: 2, deleted_jobs: 2 } })
    const filters = emptyEventFilters()
    filters.start_at = '2026-07-15T00:00'
    filters.end_at = '2026-07-16T00:00'
    await promptAuditAPI.deleteEventsByFilter(filters, {
      matched_count: 2, filter_summary: {}, snapshot_max_id: 10, filter_hash: 'a'.repeat(64), confirmation_token: 'opaque-token', expires_at: '2026-07-16T00:05:00Z',
    })
    expect(client.post).toHaveBeenCalledWith('/admin/prompt-audit/events/delete-by-filter', expect.objectContaining({
      snapshot_max_id: 10, filter_hash: 'a'.repeat(64), confirmation_token: 'opaque-token', confirm: true,
    }))
  })

  it('reveals encrypted evidence only through the reason-bearing endpoint', async () => {
    client.post.mockResolvedValue({ data: { event_id: 7, full_prompt: 'sensitive', revealed_at: '2026-07-24T00:00:00Z' } })
    await promptAuditAPI.revealEventEvidence(7, 'manual incident review')
    expect(client.post).toHaveBeenCalledWith(
      '/admin/prompt-audit/events/7/evidence/reveal',
      { reason: 'manual incident review' },
    )
  })

  it('uses the job-governance contract for filtered listing and reason-bearing transitions', async () => {
    client.get.mockResolvedValue({ data: { items: [], failure_reasons: [], total: 0, page: 1, page_size: 20, pages: 0 } })
    await promptAuditAPI.listJobs({
      status: 'failed',
      error_code: 'timeout',
      keyword: 'request-7',
      start_at: '',
      end_at: '',
    }, 2, 50)
    expect(client.get).toHaveBeenCalledWith('/admin/prompt-audit/jobs', {
      params: {
        page: 2,
        page_size: 50,
        status: 'failed',
        error_code: 'timeout',
        keyword: 'request-7',
      },
    })

    client.post.mockResolvedValue({ data: { id: 7, status: 'queued' } })
    await promptAuditAPI.transitionJob(7, 'retry', 'operator verified endpoint recovery')
    expect(client.post).toHaveBeenCalledWith('/admin/prompt-audit/jobs/7/retry', {
      reason: 'operator verified endpoint recovery',
    })
  })

  it('uses the unified security-audit routes for policy, decision, action, and operations APIs', async () => {
    client.get.mockResolvedValue({ data: [] })
    client.post.mockResolvedValue({ data: {} })

    await promptAuditAPI.getSecurityOverview(48)
    expect(client.get).toHaveBeenLastCalledWith('/admin/security-audit/overview', {
      params: { window_hours: 48 },
    })

    await promptAuditAPI.listSecurityPolicyVersions('policy/special')
    expect(client.get).toHaveBeenLastCalledWith('/admin/security-audit/policies/policy%2Fspecial/versions')

    await promptAuditAPI.listSecurityPolicyTransitions('policy/special', 50)
    expect(client.get).toHaveBeenLastCalledWith('/admin/security-audit/policies/policy%2Fspecial/transitions', {
      params: { limit: 50 },
    })

    await promptAuditAPI.getSecurityPolicyShadowEvaluations('policy/special', 3, {
      window_hours: 72,
      limit: 25,
    })
    expect(client.get).toHaveBeenLastCalledWith(
      '/admin/security-audit/policies/policy%2Fspecial/versions/3/shadow-evaluations',
      { params: { window_hours: 72, limit: 25 } },
    )

    await promptAuditAPI.transitionSecurityPolicy('default', 3, 'activate', 'reviewed')
    expect(client.post).toHaveBeenLastCalledWith(
      '/admin/security-audit/policies/default/versions/3/activate',
      { reason: 'reviewed' },
    )

    await promptAuditAPI.replaySecurityPolicy('default', 3, { window_hours: 168, limit: 1000 })
    expect(client.post).toHaveBeenLastCalledWith(
      '/admin/security-audit/policies/default/versions/3/replay',
      { window_hours: 168, limit: 1000 },
    )

    await promptAuditAPI.revealSecurityEvidence(9, 'incident review')
    expect(client.post).toHaveBeenLastCalledWith(
      '/admin/security-audit/decisions/9/evidence/reveal',
      { reason: 'incident review' },
    )

    await promptAuditAPI.transitionSecurityAction(7, 'revert')
    expect(client.post).toHaveBeenLastCalledWith('/admin/security-audit/actions/7/revert')

    await promptAuditAPI.resetSecurityEndpointBreaker('guard/cn-1')
    expect(client.post).toHaveBeenLastCalledWith(
      '/admin/security-audit/endpoints/guard%2Fcn-1/reset-breaker',
    )

    await promptAuditAPI.expireSecurityException(6, 'temporary exception is no longer required')
    expect(client.post).toHaveBeenLastCalledWith(
      '/admin/security-audit/exceptions/6/expire',
      { reason: 'temporary exception is no longer required' },
    )

    await promptAuditAPI.updateSecurityNotificationStatus(4, 'dismissed')
    expect(client.post).toHaveBeenLastCalledWith(
      '/admin/security-audit/notifications/4/status',
      { status: 'dismissed' },
    )

    await promptAuditAPI.markAllSecurityNotificationsRead('admin')
    expect(client.post).toHaveBeenLastCalledWith(
      '/admin/security-audit/notifications/read-all',
      { audience: 'admin' },
    )
  })
})
