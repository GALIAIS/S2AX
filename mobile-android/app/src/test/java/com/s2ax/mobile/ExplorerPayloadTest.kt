package com.s2ax.mobile

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ExplorerPayloadTest {
    @Test
    fun pagedModulesCarryTheirRequestedPageAndPageSize() {
        val module = MobileDataModule(
            id = "test",
            title = "Test",
            description = "Test",
            area = MobileDataArea.Service,
            path = "/test",
            paged = true,
            pageSize = 2,
        )

        assertEquals(mapOf("page" to "3", "page_size" to "2"), module.requestQuery(page = 3))
    }

    @Test
    fun workspaceRegistryKeepsRoutesStaticAndPermissionScoped() {
        val adminModules = MobileDataModules.visible(isAdmin = true)
        val userModules = MobileDataModules.visible(isAdmin = false)

        assertEquals(adminModules.size, adminModules.map { it.id }.toSet().size)
        assertTrue(adminModules.all { it.path.startsWith('/') && it.title.isNotBlank() && it.description.isNotBlank() })
        assertFalse(userModules.any { it.adminOnly })
    }

    @Test
    fun workspaceRegistryCoversCoreWebDataSurfaces() {
        val availablePaths = MobileDataModules.visible(isAdmin = true).map { it.path }.toSet()
        val expectedPaths = setOf(
            "/usage/dashboard/snapshot-v2",
            "/usage",
            "/account-allocations",
            "/user/currencies",
            "/payment/orders/my",
            "/admin/dashboard/snapshot-v2",
            "/admin/ops/dashboard/snapshot-v2",
            "/admin/audit-logs",
            "/admin/channels",
            "/admin/currencies",
            "/admin/risk-control/status",
            "/admin/settings",
            "/admin/system/version",
        )

        assertTrue(expectedPaths.all(availablePaths::contains))
    }
}
