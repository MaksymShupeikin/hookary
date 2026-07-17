import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/widgets/google_signin_button_stub.dart'
    if (dart.library.js_interop) '../../core/widgets/google_signin_button_web.dart';
import 'auth_controller.dart';

class AuthPage extends ConsumerWidget {
  const AuthPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(authControllerProvider);
    final theme = Theme.of(context);

    return Scaffold(
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 360),
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  'Hookary',
                  textAlign: TextAlign.center,
                  style: theme.textTheme.displaySmall,
                ),
                const SizedBox(height: 8),
                Text(
                  'Turn a long video into short vertical clips.',
                  textAlign: TextAlign.center,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(height: 40),
                if (state.isBusy)
                  const Center(child: CircularProgressIndicator())
                else
                  const _SignInButton(),
                if (state.error != null) ...[
                  const SizedBox(height: 24),
                  _ErrorMessage(message: state.error!),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Google's rendered button on web, ours everywhere else.
///
/// Web has no choice in this: Google Identity Services only starts a sign-in
/// from its own widget, and calling authenticate() there throws.
class _SignInButton extends ConsumerWidget {
  const _SignInButton();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final googleButton = buildGoogleSignInButton();
    if (googleButton != null) {
      return Center(child: googleButton);
    }

    return FilledButton.icon(
      onPressed: () =>
          ref.read(authControllerProvider.notifier).signInWithGoogle(),
      icon: const Icon(Icons.login),
      label: const Text('Continue with Google'),
      style: FilledButton.styleFrom(
        padding: const EdgeInsets.symmetric(vertical: 16),
      ),
    );
  }
}

class _ErrorMessage extends StatelessWidget {
  const _ErrorMessage({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.errorContainer,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        children: [
          Icon(
            Icons.error_outline,
            color: theme.colorScheme.onErrorContainer,
            size: 20,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onErrorContainer,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
