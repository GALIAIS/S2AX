package com.s2ax.mobile

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ExplorerPresentationTest {
    @Test
    fun `dashboard metric strip selects only known safe scalar metrics`() {
        val entries = listOf(
            "total_requests" to 1200,
            "total_tokens" to 5600,
            "access_token" to "at-this-must-never-be-present",
            "nested" to mapOf("requests" to 99),
        )

        assertEquals(
            listOf("total_requests", "total_tokens"),
            selectPreviewMetricEntries(entries).map { it.first },
        )
    }

    @Test
    fun `field aware preview keeps mobile values readable`() {
        assertEquals("1,236 ms", renderPreviewValue("p99_latency_ms", 1236))
        assertEquals("1.5 KB", renderPreviewValue("memory_bytes", 1536))
        assertEquals("2026-07-23 12:34:56", renderPreviewValue("created_at", "2026-07-23T12:34:56.123Z"))
        assertTrue(renderPreviewValue("message", "正常内容").contains("正常内容"))
    }
}
