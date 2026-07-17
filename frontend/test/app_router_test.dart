import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hookary/core/router/app_router.dart';
import 'package:hookary/features/auth/auth_controller.dart';
import 'package:hookary/features/auth/auth_models.dart';
import 'package:hookary/features/auth/auth_page.dart';
import 'package:hookary/features/projects/project_models.dart';
import 'package:hookary/features/projects/projects_controller.dart';
import 'package:hookary/features/projects/projects_page.dart';

/// An AuthController pinned to one state, so the redirect can be tested without
/// a network or a Google client.
class _FixedAuthController extends AuthController {
  _FixedAuthController(this._fixed);

  final AuthState _fixed;

  @override
  AuthState build() => _fixed;
}

class _FixedProjectsController extends ProjectsController {
  @override
  ProjectsState build() => const ProjectsState(hasLoaded: true);
}

Future<void> pumpApp(WidgetTester tester, AuthState state) async {
  final container = ProviderContainer(
    overrides: [
      authControllerProvider.overrideWith(() => _FixedAuthController(state)),
      projectsControllerProvider.overrideWith(_FixedProjectsController.new),
    ],
  );
  addTearDown(container.dispose);

  await tester.pumpWidget(
    UncontrolledProviderScope(
      container: container,
      child: Consumer(
        builder: (context, ref, _) =>
            MaterialApp.router(routerConfig: ref.watch(routerProvider)),
      ),
    ),
  );
  // pump, not pumpAndSettle: the splash spinner never stops, so settling would
  // wait forever.
  await tester.pump();
  await tester.pump();
}

void main() {
  testWidgets('waits on splash until the stored session is checked', (
    tester,
  ) async {
    await pumpApp(tester, const AuthState());

    // Guessing here would flash the login screen at someone already signed in.
    expect(find.byType(SplashPage), findsOneWidget);
    expect(find.byType(AuthPage), findsNothing);
    expect(find.byType(ProjectsPage), findsNothing);
  });

  testWidgets('sends a signed-out visitor to sign-in', (tester) async {
    await pumpApp(tester, const AuthState(status: AuthStatus.signedOut));

    expect(find.byType(AuthPage), findsOneWidget);
    expect(find.byType(ProjectsPage), findsNothing);
  });

  testWidgets('sends a signed-in user to projects', (tester) async {
    await pumpApp(
      tester,
      const AuthState(
        status: AuthStatus.signedIn,
        user: AuthUser(
          id: '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
          email: 'a@example.com',
          displayName: 'Ada',
        ),
      ),
    );

    expect(find.byType(ProjectsPage), findsOneWidget);
    expect(find.byType(AuthPage), findsNothing);
  });

  testWidgets('a signed-in user cannot sit on the sign-in screen', (
    tester,
  ) async {
    final container = ProviderContainer(
      overrides: [
        authControllerProvider.overrideWith(
          () => _FixedAuthController(
            const AuthState(
              status: AuthStatus.signedIn,
              user: AuthUser(
                id: '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
                email: 'a@example.com',
                displayName: 'Ada',
              ),
            ),
          ),
        ),
        projectsControllerProvider.overrideWith(_FixedProjectsController.new),
      ],
    );
    addTearDown(container.dispose);

    final router = container.read(routerProvider);
    router.go(Routes.signIn);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp.router(routerConfig: router),
      ),
    );
    await tester.pump();
    await tester.pump();

    expect(find.byType(ProjectsPage), findsOneWidget);
  });
}
