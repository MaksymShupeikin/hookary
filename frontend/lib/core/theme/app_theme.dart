import 'package:flutter/material.dart';

/// The app's light and dark themes.
///
/// Editing video is a dark-room task and previews are full-bleed video, so dark
/// is the default and light is the alternate — not the other way round.
class AppTheme {
  const AppTheme._();

  static const Color _seed = Color(0xFF5B4BFF);

  static ThemeData get dark => _build(Brightness.dark);
  static ThemeData get light => _build(Brightness.light);

  static ThemeData _build(Brightness brightness) {
    final scheme = ColorScheme.fromSeed(
      seedColor: _seed,
      brightness: brightness,
    );

    return ThemeData(
      colorScheme: scheme,
      useMaterial3: true,
      visualDensity: VisualDensity.adaptivePlatformDensity,
    );
  }
}
