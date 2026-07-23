package com.s2ax.mobile

import org.junit.Assert.assertEquals
import org.junit.Test

class ApiUrlTest {
    @Test
    fun normalizesRootAndApiPathOnce() {
        assertEquals("https://gateway.example.com/api/v1", ApiUrl.normalize("https://gateway.example.com/"))
        assertEquals("https://gateway.example.com/api/v1", ApiUrl.normalize("https://gateway.example.com/api/v1/"))
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsCleartextEndpoints() {
        ApiUrl.normalize("http://gateway.example.com")
    }
}
