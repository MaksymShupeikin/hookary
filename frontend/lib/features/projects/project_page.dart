import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/router/app_router.dart';
import '../clips/clips_panel.dart';
import '../import/import_page.dart';
import '../import/sources_panel.dart';
import 'project_models.dart';
import 'projects_controller.dart';

class ProjectPage extends ConsumerStatefulWidget {
  const ProjectPage({required this.projectId, super.key});

  final String projectId;

  @override
  ConsumerState<ProjectPage> createState() => _ProjectPageState();
}

class _ProjectPageState extends ConsumerState<ProjectPage> {
  @override
  void initState() {
    super.initState();
    Future.microtask(
      () => ref
          .read(projectsControllerProvider.notifier)
          .ensureProject(widget.projectId),
    );
  }

  @override
  Widget build(BuildContext context) {
    final projects = ref.watch(projectsControllerProvider);
    final project = projects.byId(widget.projectId);

    return Scaffold(
      appBar: AppBar(
        leading: IconButton(
          onPressed: () => context.go('/'),
          icon: const Icon(Icons.arrow_back),
        ),
        title: Text(project?.title ?? 'Project'),
      ),
      body: project == null ? _missing(projects) : _content(project),
    );
  }

  Widget _missing(ProjectsState state) {
    if (state.error != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Text(state.error!, textAlign: TextAlign.center),
        ),
      );
    }
    return const Center(child: CircularProgressIndicator());
  }

  Widget _content(Project project) {
    return ListView(
      padding: const EdgeInsets.all(24),
      children: [
        Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 860),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  project.title,
                  style: Theme.of(context).textTheme.headlineMedium,
                ),
                const SizedBox(height: 8),
                Text(
                  '${project.assetCount} source · ${project.clipCount} clips',
                ),
                const SizedBox(height: 24),
                ProjectSourcesPanel(projectId: project.id),
                const SizedBox(height: 16),
                SuggestedClipsPanel(projectId: project.id),
                const SizedBox(height: 16),
                UploadPanel(projectId: project.id),
                if (project.assetCount > 0 ||
                    project.status != ProjectStatus.empty) ...[
                  const SizedBox(height: 16),
                  OutlinedButton.icon(
                    onPressed: () => context.go(Routes.processing(project.id)),
                    icon: const Icon(Icons.account_tree_outlined),
                    label: const Text('View processing'),
                  ),
                ],
              ],
            ),
          ),
        ),
      ],
    );
  }
}
