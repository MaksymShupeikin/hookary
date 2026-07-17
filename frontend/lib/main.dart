import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'core/router/app_router.dart';
import 'core/theme/app_theme.dart';
import 'features/auth/auth_controller.dart';

void main() {
  runApp(const ProviderScope(child: HookaryApp()));
}

/// MaterialApp lives here and nowhere else.
class HookaryApp extends ConsumerStatefulWidget {
  const HookaryApp({super.key});

  @override
  ConsumerState<HookaryApp> createState() => _HookaryAppState();
}

class _HookaryAppState extends ConsumerState<HookaryApp> {
  @override
  void initState() {
    super.initState();
    // Restoring the session is what moves the router off the splash screen, so
    // it starts as early as there is a ref to start it with.
    Future.microtask(
      () => ref.read(authControllerProvider.notifier).restoreSession(),
    );
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp.router(
      title: 'Hookary',
      theme: AppTheme.light,
      darkTheme: AppTheme.dark,
      themeMode: ThemeMode.dark,
      debugShowCheckedModeBanner: false,
      routerConfig: ref.watch(routerProvider),
    );
  }
}
