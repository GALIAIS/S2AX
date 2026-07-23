package com.s2ax.mobile

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ExplorerPrivacyTest {
    @Test
    fun `sensitive fields are never included in generic previews`() {
        assertTrue(isSensitiveField("access_token"))
        assertTrue(isSensitiveField("apiKey"))
        assertTrue(isSensitiveField("webhook_secret"))
        assertTrue(isSensitiveField("session_token"))
        assertFalse(isSensitiveField("total_tokens"))
        assertFalse(isSensitiveField("token_count"))
        assertFalse(isSensitiveField("model"))
    }

    @Test
    fun `credential shaped values are redacted before rendering`() {
        assertEquals("已隐藏", redactPreviewText("at-abcdefghijklmnopqrstuvwxyz0123456789"))
        assertEquals("已隐藏", redactPreviewText("Bearer abcdefghijklmnop"))
        assertEquals("已隐藏", redactPreviewText("https://example.test/callback?token=super-secret"))
        assertEquals("普通文本", redactPreviewText("  普通文本  "))
    }
}
