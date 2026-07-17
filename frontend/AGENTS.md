# Hookary frontend

Flutter for web, iOS, and Android from one codebase.

```
lib/
├── main.dart          MaterialApp lives here and nowhere else
├── core/              client plumbing, theme, router, shared widgets
└── features/<name>/   one flat directory per feature
```

There is no `lib/app/` and no `lib/shared/`. `core/` is the shared layer.

## A feature is four files

```
features/projects/
├── project_models.dart      plain classes, hand-written
├── projects_api.dart        every HTTP call for this feature
├── projects_controller.dart loading, error, and actions
├── projects_page.dart       widgets
```

Split a file when it reaches roughly 300–400 lines. Not before.

## No code generation

- **No Freezed.**
- **No `json_serializable`.**
- **No `build_runner`.**
- Riverpod providers are written by hand.

Models are plain classes with a `fromJson` constructor and a `toJson` method,
written out. Why: the generated code is larger than what it replaces, and it
puts a build step between every edit and the running app. The models here are
small enough that this is a bad trade.

## Rules

- **Widgets do not make requests.** They read a controller. HTTP lives in the
  feature's `*_api.dart`, always.
- **Controllers own loading, error, and empty states.** Every screen has all
  three; a page that only handles the happy path is unfinished.
- **Never read a video into a `Uint8List`.** Uploads are chunked and streamed.
  `core/upload_client.dart` has separate web and mobile implementations for
  exactly this reason.
- **The access token stays in memory.** Refresh tokens go in an `HttpOnly`
  cookie on web, and `flutter_secure_storage` on iOS/Android — never in
  `SharedPreferences`, never in a Dart field.
- **One video controller at a time.** Lists show thumbnails via
  `HookaryCachedImage`; a list that spins up a player per row will not survive
  contact with a real project.
- **Flutter never holds R2 credentials.** It uploads to presigned URLs the API
  mints, and nothing else.

## Libraries

| Job | Choice |
|---|---|
| State | `flutter_riverpod` |
| Routing | `go_router` |
| HTTP | `dio` |
| Auth | `google_sign_in` |
| Secrets | `flutter_secure_storage` |
| Files | `file_selector` |
| Video | `video_player` |
| Progress | `web_socket_channel` |
| Images | `cached_network_image` |
| Formatting | `intl` |

## Web renderer

CI builds both:

```sh
flutter build web --release
flutter build web --release --wasm
```

Production moves to `--wasm` only after Google Sign-In, video playback, and
multipart upload are confirmed working in every supported browser.

## Commands

```sh
make flutter-lint    # flutter analyze
make flutter-test
make web
make web-wasm
```
