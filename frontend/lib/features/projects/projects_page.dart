import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:intl/intl.dart';

import '../../core/router/app_router.dart';
import '../auth/auth_controller.dart';
import 'project_models.dart';
import 'projects_controller.dart';

class ProjectsPage extends ConsumerStatefulWidget {
  const ProjectsPage({super.key});

  @override
  ConsumerState<ProjectsPage> createState() => _ProjectsPageState();
}

class _ProjectsPageState extends ConsumerState<ProjectsPage> {
  @override
  void initState() {
    super.initState();
    Future.microtask(
      () => ref.read(projectsControllerProvider.notifier).load(),
    );
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(projectsControllerProvider);
    final user = ref.watch(authControllerProvider).user;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Hookary'),
        actions: [
          if (user != null)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 8),
              child: Center(child: Text(user.displayName)),
            ),
          IconButton(
            onPressed: () =>
                ref.read(authControllerProvider.notifier).signOut(),
            icon: const Icon(Icons.logout),
            tooltip: 'Sign out',
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: state.isMutating ? null : () => _createProject(context),
        icon: const Icon(Icons.add),
        label: const Text('New project'),
      ),
      body: RefreshIndicator(
        onRefresh: () =>
            ref.read(projectsControllerProvider.notifier).load(refresh: true),
        child: _body(state),
      ),
    );
  }

  Widget _body(ProjectsState state) {
    if (state.isLoading && state.projects.isEmpty) {
      return const Center(child: CircularProgressIndicator());
    }
    if (state.error != null && state.projects.isEmpty) {
      return _ScrollableMessage(
        icon: Icons.cloud_off,
        title: 'Could not load projects',
        message: state.error!,
        action: FilledButton(
          onPressed: () =>
              ref.read(projectsControllerProvider.notifier).load(refresh: true),
          child: const Text('Try again'),
        ),
      );
    }
    if (state.projects.isEmpty) {
      return const _ScrollableMessage(
        icon: Icons.video_library_outlined,
        title: 'No projects yet',
        message: 'Create one, then add a source video.',
      );
    }

    return ListView.separated(
      padding: const EdgeInsets.fromLTRB(20, 20, 20, 100),
      itemCount: state.projects.length + (state.nextCursor == null ? 0 : 1),
      separatorBuilder: (_, _) => const SizedBox(height: 12),
      itemBuilder: (context, index) {
        if (index == state.projects.length) {
          return Center(
            child: TextButton(
              onPressed: state.isLoadingMore
                  ? null
                  : () => ref
                        .read(projectsControllerProvider.notifier)
                        .loadMore(),
              child: state.isLoadingMore
                  ? const SizedBox.square(
                      dimension: 20,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Text('Load more'),
            ),
          );
        }
        final project = state.projects[index];
        return _ProjectCard(
          project: project,
          onOpen: () => context.go(Routes.project(project.id)),
          onDelete: () => _deleteProject(context, project),
        );
      },
    );
  }

  Future<void> _createProject(BuildContext context) async {
    final controller = TextEditingController();
    final title = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('New project'),
        content: TextField(
          controller: controller,
          autofocus: true,
          maxLength: 200,
          textInputAction: TextInputAction.done,
          decoration: const InputDecoration(labelText: 'Project name'),
          onSubmitted: (value) => Navigator.pop(context, value),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, controller.text),
            child: const Text('Create'),
          ),
        ],
      ),
    );
    controller.dispose();
    if (title == null || !context.mounted) return;

    final project = await ref
        .read(projectsControllerProvider.notifier)
        .create(title);
    if (project != null && context.mounted) {
      context.go(Routes.project(project.id));
    }
  }

  Future<void> _deleteProject(BuildContext context, Project project) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('Delete “${project.title}”?'),
        content: const Text(
          'The source video and every generated clip will be deleted.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Keep'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      await ref.read(projectsControllerProvider.notifier).delete(project.id);
    }
  }
}

class _ProjectCard extends StatelessWidget {
  const _ProjectCard({
    required this.project,
    required this.onOpen,
    required this.onDelete,
  });

  final Project project;
  final VoidCallback onOpen;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Card(
      clipBehavior: Clip.antiAlias,
      child: ListTile(
        onTap: onOpen,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: 20,
          vertical: 10,
        ),
        leading: CircleAvatar(
          backgroundColor: colors.primaryContainer,
          child: Icon(
            _statusIcon(project.status),
            color: colors.onPrimaryContainer,
          ),
        ),
        title: Text(project.title),
        subtitle: Text(
          '${_statusLabel(project.status)} · ${project.assetCount} source · '
          '${project.clipCount} clips · ${DateFormat.yMMMd().format(project.createdAt)}',
        ),
        trailing: PopupMenuButton<void>(
          itemBuilder: (_) => [
            PopupMenuItem(onTap: onDelete, child: const Text('Delete project')),
          ],
        ),
      ),
    );
  }

  IconData _statusIcon(ProjectStatus status) => switch (status) {
    ProjectStatus.empty => Icons.add_to_queue,
    ProjectStatus.processing => Icons.hourglass_top,
    ProjectStatus.ready => Icons.check,
    ProjectStatus.failed => Icons.error_outline,
  };

  String _statusLabel(ProjectStatus status) => switch (status) {
    ProjectStatus.empty => 'Empty',
    ProjectStatus.processing => 'Processing',
    ProjectStatus.ready => 'Ready',
    ProjectStatus.failed => 'Failed',
  };
}

class _ScrollableMessage extends StatelessWidget {
  const _ScrollableMessage({
    required this.icon,
    required this.title,
    required this.message,
    this.action,
  });

  final IconData icon;
  final String title;
  final String message;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    return ListView(
      physics: const AlwaysScrollableScrollPhysics(),
      children: [
        SizedBox(height: MediaQuery.sizeOf(context).height * 0.22),
        Icon(icon, size: 56),
        const SizedBox(height: 16),
        Text(
          title,
          textAlign: TextAlign.center,
          style: Theme.of(context).textTheme.titleLarge,
        ),
        const SizedBox(height: 8),
        Text(message, textAlign: TextAlign.center),
        if (action != null) ...[
          const SizedBox(height: 20),
          Center(child: action),
        ],
      ],
    );
  }
}
