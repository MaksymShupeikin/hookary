import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/exceptions/exceptions.dart';
import '../auth/auth_controller.dart';
import 'project_models.dart';
import 'projects_api.dart';

final projectsApiProvider = Provider<ProjectsApi>((ref) {
  return ProjectsApi(ref.watch(apiClientProvider));
});

final projectsControllerProvider =
    NotifierProvider<ProjectsController, ProjectsState>(ProjectsController.new);

class ProjectsController extends Notifier<ProjectsState> {
  @override
  ProjectsState build() => const ProjectsState();

  ProjectsApi get _api => ref.read(projectsApiProvider);

  Future<void> load({bool refresh = false}) async {
    if (state.isLoading || (!refresh && state.hasLoaded)) return;
    state = state.copyWith(
      isLoading: true,
      clearError: true,
      clearNextCursor: refresh,
    );
    try {
      final result = await _api.list();
      state = ProjectsState(
        projects: result.projects,
        nextCursor: result.nextCursor,
        hasLoaded: true,
      );
    } on ApiException catch (error) {
      state = state.copyWith(
        isLoading: false,
        hasLoaded: true,
        error: error.message,
      );
    }
  }

  Future<void> loadMore() async {
    final cursor = state.nextCursor;
    if (cursor == null || state.isLoadingMore) return;
    state = state.copyWith(isLoadingMore: true, clearError: true);
    try {
      final result = await _api.list(cursor: cursor);
      state = state.copyWith(
        projects: [...state.projects, ...result.projects],
        nextCursor: result.nextCursor,
        clearNextCursor: result.nextCursor == null,
        isLoadingMore: false,
      );
    } on ApiException catch (error) {
      state = state.copyWith(isLoadingMore: false, error: error.message);
    }
  }

  Future<Project?> ensureProject(String id) async {
    final current = state.byId(id);
    if (current != null) return current;
    try {
      final project = await _api.get(id);
      state = state.copyWith(projects: [project, ...state.projects]);
      return project;
    } on ApiException catch (error) {
      state = state.copyWith(error: error.message);
      return null;
    }
  }

  Future<Project?> create(String title) async {
    final cleaned = title.trim();
    if (cleaned.isEmpty) {
      state = state.copyWith(error: 'Give the project a name.');
      return null;
    }
    state = state.copyWith(isMutating: true, clearError: true);
    try {
      final project = await _api.create(cleaned);
      state = state.copyWith(
        projects: [project, ...state.projects],
        isMutating: false,
      );
      return project;
    } on ApiException catch (error) {
      state = state.copyWith(isMutating: false, error: error.message);
      return null;
    }
  }

  Future<bool> delete(String id) async {
    state = state.copyWith(isMutating: true, clearError: true);
    try {
      await _api.delete(id);
      state = state.copyWith(
        projects: state.projects.where((project) => project.id != id).toList(),
        isMutating: false,
      );
      return true;
    } on ApiException catch (error) {
      state = state.copyWith(isMutating: false, error: error.message);
      return false;
    }
  }

  void replace(Project project) {
    final projects = [...state.projects];
    final index = projects.indexWhere((item) => item.id == project.id);
    if (index == -1) {
      projects.insert(0, project);
    } else {
      projects[index] = project;
    }
    state = state.copyWith(projects: projects);
  }
}
