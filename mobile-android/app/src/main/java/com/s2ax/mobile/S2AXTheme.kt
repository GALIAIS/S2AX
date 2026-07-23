package com.s2ax.mobile

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

private val LightScheme = lightColorScheme(
    primary = Color(0xFF075F77),
    onPrimary = Color.White,
    primaryContainer = Color(0xFFC5EBF5),
    onPrimaryContainer = Color(0xFF002F3C),
    secondary = Color(0xFF4A6169),
    onSecondary = Color.White,
    secondaryContainer = Color(0xFFCDE6EC),
    onSecondaryContainer = Color(0xFF061F26),
    tertiary = Color(0xFF5A5E7D),
    background = Color(0xFFF9FAFA),
    onBackground = Color(0xFF191C1D),
    surface = Color(0xFFF9FAFA),
    onSurface = Color(0xFF191C1D),
    surfaceDim = Color(0xFFD8DDDE),
    surfaceBright = Color(0xFFF9FAFA),
    surfaceContainerLowest = Color(0xFFFFFFFF),
    surfaceContainerLow = Color(0xFFF3F6F7),
    surfaceContainer = Color(0xFFEEF2F3),
    surfaceContainerHigh = Color(0xFFE8ECEE),
    surfaceContainerHighest = Color(0xFFE1E6E8),
    surfaceVariant = Color(0xFFDCE5E8),
    onSurfaceVariant = Color(0xFF40484C),
    outline = Color(0xFF70787B),
    error = Color(0xFFB3261E),
    onError = Color.White,
    errorContainer = Color(0xFFFFDAD6),
    onErrorContainer = Color(0xFF410002),
)

private val DarkScheme = darkColorScheme(
    primary = Color(0xFF86D1ED),
    onPrimary = Color(0xFF003545),
    primaryContainer = Color(0xFF004D63),
    onPrimaryContainer = Color(0xFFB6EBFF),
    secondary = Color(0xFFB4CAD4),
    onSecondary = Color(0xFF1E333B),
    secondaryContainer = Color(0xFF354A53),
    onSecondaryContainer = Color(0xFFD0E7F1),
    tertiary = Color(0xFFC5C2EB),
    background = Color(0xFF101416),
    onBackground = Color(0xFFE1E3E4),
    surface = Color(0xFF101416),
    onSurface = Color(0xFFE1E3E4),
    surfaceDim = Color(0xFF101416),
    surfaceBright = Color(0xFF353B3D),
    surfaceContainerLowest = Color(0xFF0B0F10),
    surfaceContainerLow = Color(0xFF171C1E),
    surfaceContainer = Color(0xFF1B2123),
    surfaceContainerHigh = Color(0xFF252B2D),
    surfaceContainerHighest = Color(0xFF303638),
    surfaceVariant = Color(0xFF40484C),
    onSurfaceVariant = Color(0xFFC0C8CC),
    outline = Color(0xFF8A9296),
    error = Color(0xFFFFB4AB),
    onError = Color(0xFF690005),
    errorContainer = Color(0xFF93000A),
    onErrorContainer = Color(0xFFFFDAD6),
)

@Composable
fun S2AXTheme(content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = if (isSystemInDarkTheme()) DarkScheme else LightScheme, content = content)
}
