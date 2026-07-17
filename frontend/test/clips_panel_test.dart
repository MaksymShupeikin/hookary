import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hookary/features/clips/clip_models.dart';
import 'package:hookary/features/clips/clips_controller.dart';
import 'package:hookary/features/clips/clips_panel.dart';

const _projectId = '01JZ8WKQ4T6N9R2VBC3D5EFGHJ';

class _FixedClipsController extends ClipsController {
  @override
  ClipsState build() {
    return const ClipsState(
      projectId: _projectId,
      hasLoaded: true,
      clips: [
        SuggestedClip(
          id: '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
          projectId: _projectId,
          assetId: '01JZ8YKQ4T6N9R2VBC3D5EFGHJ',
          title: 'A strong standalone idea',
          hook: 'Most creators miss this.',
          reason: 'Clear hook and complete payoff.',
          score: 0.91,
          contextRisk: 'low',
          startMs: 12000,
          endMs: 49000,
          status: 'candidate',
        ),
      ],
    );
  }
}

void main() {
  testWidgets('shows ranked clip suggestions with timing and rationale', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          clipsControllerProvider.overrideWith(_FixedClipsController.new),
        ],
        child: const MaterialApp(
          home: Scaffold(
            body: SingleChildScrollView(
              child: SuggestedClipsPanel(projectId: _projectId),
            ),
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.text('Suggested moments'), findsOneWidget);
    expect(find.text('A strong standalone idea'), findsOneWidget);
    expect(find.text('“Most creators miss this.”'), findsOneWidget);
    expect(find.textContaining('0:12–0:49'), findsOneWidget);
    expect(find.text('91% match'), findsOneWidget);
    expect(find.text('Preview clip'), findsOneWidget);
    expect(find.text('Render selected (0)'), findsOneWidget);

    await tester.tap(find.byType(Checkbox));
    await tester.pump();

    expect(find.text('Render selected (1)'), findsOneWidget);
  });

  testWidgets('shows only the selected suggestion preview loader', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          clipsControllerProvider.overrideWith(_PreviewingClipsController.new),
        ],
        child: const MaterialApp(
          home: Scaffold(
            body: SingleChildScrollView(
              child: SuggestedClipsPanel(projectId: _projectId),
            ),
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.text('Close preview'), findsOneWidget);
    expect(find.text('Source segment preview'), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
  });

  testWidgets('shows download action for a rendered suggestion', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          clipsControllerProvider.overrideWith(_RenderedClipsController.new),
        ],
        child: const MaterialApp(
          home: Scaffold(
            body: SingleChildScrollView(
              child: SuggestedClipsPanel(projectId: _projectId),
            ),
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.text('Ready'), findsOneWidget);
    expect(find.text('Download MP4'), findsOneWidget);
    expect(tester.widget<Checkbox>(find.byType(Checkbox)).onChanged, isNotNull);
  });
}

class _PreviewingClipsController extends ClipsController {
  @override
  ClipsState build() {
    return const ClipsState(
      projectId: _projectId,
      hasLoaded: true,
      previewClipId: '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
      previewLoading: true,
      clips: [
        SuggestedClip(
          id: '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
          projectId: _projectId,
          assetId: '01JZ8YKQ4T6N9R2VBC3D5EFGHJ',
          title: 'A strong standalone idea',
          score: 0.91,
          startMs: 12000,
          endMs: 49000,
          status: 'candidate',
        ),
      ],
    );
  }
}

class _RenderedClipsController extends ClipsController {
  @override
  ClipsState build() {
    return const ClipsState(
      projectId: _projectId,
      hasLoaded: true,
      clips: [
        SuggestedClip(
          id: '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
          projectId: _projectId,
          assetId: '01JZ8YKQ4T6N9R2VBC3D5EFGHJ',
          title: 'Rendered moment',
          score: 0.91,
          startMs: 12000,
          endMs: 49000,
          status: 'rendered',
          renderAvailable: true,
        ),
      ],
    );
  }
}
