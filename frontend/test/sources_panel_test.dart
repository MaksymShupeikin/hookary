import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hookary/features/import/import_models.dart';
import 'package:hookary/features/import/sources_controller.dart';
import 'package:hookary/features/import/sources_panel.dart';

const _projectId = '01JZ8WKQ4T6N9R2VBC3D5EFGHJ';

class _FixedSourcesController extends SourcesController {
  @override
  SourcesState build() {
    final now = DateTime.utc(2026, 7, 17, 12);
    return SourcesState(
      projectId: _projectId,
      hasLoaded: true,
      assets: [
        SourceAsset(
          id: '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
          projectId: _projectId,
          kind: SourceKind.publicUrl,
          title: 'Downloaded YouTube source',
          status: SourceStatus.ready,
          previewReady: true,
          transcriptReady: true,
          transcriptStatus: TranscriptStatus.succeeded,
          analysisStatus: TranscriptStatus.succeeded,
          durationMs: 19000,
          createdAt: now,
          updatedAt: now,
        ),
        SourceAsset(
          id: '01JZ8YKQ4T6N9R2VBC3D5EFGHJ',
          projectId: _projectId,
          kind: SourceKind.r2Upload,
          title: 'camera.mp4',
          status: SourceStatus.pending,
          previewReady: true,
          transcriptReady: false,
          transcriptStatus: TranscriptStatus.pending,
          analysisStatus: TranscriptStatus.pending,
          createdAt: now,
          updatedAt: now,
        ),
      ],
    );
  }
}

void main() {
  testWidgets('shows persisted YouTube and local project sources', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sourcesControllerProvider.overrideWith(_FixedSourcesController.new),
        ],
        child: const MaterialApp(
          home: Scaffold(body: ProjectSourcesPanel(projectId: _projectId)),
        ),
      ),
    );
    await tester.pump();

    expect(find.text('Downloaded YouTube source'), findsOneWidget);
    expect(find.text('camera.mp4'), findsOneWidget);
    expect(find.textContaining('YouTube · 0:19'), findsOneWidget);
    expect(find.textContaining('Local upload'), findsOneWidget);
    expect(find.text('Preview'), findsNWidgets(2));
  });
}
