package com.s2ax.mobile

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class CurrencyAmountTest {
    @Test
    fun convertsHumanAmountToExactSmallestUnits() {
        assertEquals(123L, amountToUnits("1.23", 100))
        assertEquals(1L, amountToUnits("0.01", 100))
        assertEquals(500L, amountToUnits("5", 100))
    }

    @Test
    fun rejectsFractionalSmallestUnitsAndInvalidInput() {
        assertNull(amountToUnits("0.001", 100))
        assertNull(amountToUnits("not-a-number", 100))
    }
}
