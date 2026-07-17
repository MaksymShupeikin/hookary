import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hookary/features/import/import_controller.dart';
import 'package:hookary/features/import/import_models.dart';

void main() {
  test(
    'YouTube import rejects lookalike and unrelated hosts locally',
    () async {
      final container = ProviderContainer();
      addTearDown(container.dispose);
      final controller = container.read(importControllerProvider.notifier);

      await controller.importYouTube(
        'project-id',
        'https://youtube.com.attacker.example/watch?v=123',
      );

      final state = container.read(importControllerProvider);
      expect(state.phase, UploadPhase.failed);
      expect(state.error, 'Enter a public YouTube link.');
    },
  );
}
