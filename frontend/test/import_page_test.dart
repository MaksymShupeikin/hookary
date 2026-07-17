import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hookary/features/import/import_page.dart';

void main() {
  testWidgets('YouTube is primary and local upload remains available', (
    tester,
  ) async {
    await tester.pumpWidget(
      const ProviderScope(
        child: MaterialApp(
          home: Scaffold(body: UploadPanel(projectId: 'project-id')),
        ),
      ),
    );

    expect(find.text('Import from YouTube'), findsOneWidget);
    expect(find.text('YouTube link'), findsOneWidget);
    expect(find.text('Download from YouTube'), findsOneWidget);
    expect(find.text('Choose a local video'), findsOneWidget);
  });
}
