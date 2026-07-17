import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../features/auth/auth_controller.dart';
import '../../features/auth/auth_models.dart';
import '../../features/auth/auth_page.dart';
import '../../features/jobs/job_page.dart';
import '../../features/projects/project_page.dart';
import '../../features/projects/projects_page.dart';

abstract final class Routes {
  static const splash = '/splash';
  static const signIn = '/sign-in';
  static const projects = '/';

  static String project(String projectId) => '/projects/$projectId';
  static String processing(String projectId) =>
      '/projects/$projectId/processing';
}

final routerProvider = Provider<GoRouter>((ref) {
  final auth = ValueNotifier<AuthState>(ref.read(authControllerProvider));

  ref.listen<AuthState>(authControllerProvider, (_, next) {
    auth.value = next;
  });
  ref.onDispose(auth.dispose);

  return GoRouter(
    initialLocation: Routes.splash,
    // Rebuilds the redirect when the session changes, which is what moves the
    // user off the splash screen and back to sign-in on logout.
    refreshListenable: auth,
    redirect: (context, state) {
      final session = auth.value;
      final location = state.matchedLocation;

      // Until the stored session has been checked, nobody goes anywhere:
      // guessing would flash the login screen at someone already signed in.
      if (!session.isResolved) {
        return location == Routes.splash ? null : Routes.splash;
      }

      if (!session.isSignedIn) {
        return location == Routes.signIn ? null : Routes.signIn;
      }

      // Signed in, but sitting on a screen that only exists for people who
      // are not.
      if (location == Routes.signIn || location == Routes.splash) {
        return Routes.projects;
      }
      return null;
    },
    routes: [
      GoRoute(path: Routes.splash, builder: (_, _) => const SplashPage()),
      GoRoute(path: Routes.signIn, builder: (_, _) => const AuthPage()),
      GoRoute(path: Routes.projects, builder: (_, _) => const ProjectsPage()),
      GoRoute(
        path: '/projects/:projectId/processing',
        builder: (_, state) =>
            JobPage(projectId: state.pathParameters['projectId']!),
      ),
      GoRoute(
        path: '/projects/:projectId',
        builder: (_, state) =>
            ProjectPage(projectId: state.pathParameters['projectId']!),
      ),
    ],
  );
});

/// Shown while the stored session is being checked.
class SplashPage extends StatelessWidget {
  const SplashPage({super.key});

  @override
  Widget build(BuildContext context) {
    return const Scaffold(body: Center(child: CircularProgressIndicator()));
  }
}
