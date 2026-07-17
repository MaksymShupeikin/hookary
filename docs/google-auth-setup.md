# Google Sign-In setup

Everything Hookary needs from Google, and what to do with it.

## Firebase is not needed

Register the clients **directly in Google Cloud Platform**. Firebase adds
nothing Hookary uses, and skipping it means one console instead of two.

This is the plugin's own supported path — `google_sign_in_android` documents it:

> To use Google Sign-In, you'll need to register your application, either using
> Firebase, or **directly with Google Cloud Platform**. […] If you are not using
> `google-services.json`, you need to pass the client ID of the *web*
> application as the `serverClientId`.

Which is exactly what `core/google_auth_client.dart` does. There is no
`firebase_auth` dependency: the Flutter app only obtains a Google ID token,
and the Go API verifies it and issues a session of its own. Native platform
files downloaded while registering the clients may be kept as configuration
references, but Hookary does not use Firebase Authentication.

A Firebase project *is* a GCP project with a second console on top, so using it
would work too. What must not happen is **splitting across two projects**: the
web client id is sent as `serverClientId` on Android and iOS, and Google will
not mint a token for an audience that belongs to a different project. All three
clients live in one project.

## What the server does with these

```
Flutter → Google → ID token → POST /v1/auth/google
                                    │
                          Go verifies signature, issuer,
                          audience, expiry, email_verified
                                    │
                        Hookary access + refresh token
```

The **audience** check is why the client ids below matter: an ID token is
addressed to a specific OAuth client, and the API only accepts tokens addressed
to one of ours. A token minted for any other app on the internet is rejected.

## Clients to create

Three OAuth 2.0 client ids, one per platform. They are **not** secrets — they
ship inside the app — so they live in `.env` and `--dart-define`, not in a
vault.

### 1. Web

Type: **Web application**.

Only its **client id** is used. The client secret is not needed by Flutter and
is not needed by the API to verify Google ID tokens: verification uses Google's
public signing keys. Do not add the web client secret to Dart, `.env`, or
`compose.yaml` for this login flow.

Authorized JavaScript origins — the browser sends these, and Google refuses any
origin not listed:

```
http://localhost
http://localhost:8000
https://app.hookary.com      # once the real domain exists
```

`flutter run -d chrome` picks a random port unless told otherwise. Pin it:

```sh
flutter run -d chrome --web-port=8000
```

No redirect URI is needed: Google Identity Services returns the token to the
page, it does not redirect.

Web signs people in two ways, and both need this one client:

- **One Tap** — an inline prompt, no popup and no click. Raised at startup for
  anyone with a Google session but no Hookary session.
- **The Google button** — the fallback when One Tap does not appear. Clicking it
  opens Google's account chooser in a popup. This cannot be avoided: GIS starts
  a sign-in only from its own rendered button, which is why `authenticate()`
  throws on web.

### 2. Android

Type: **Android**.

| Field | Value |
|---|---|
| Package name | `com.hookary.app` |
| SHA-1 | see `make android-fingerprints` |

Register **two** clients, or one client per fingerprint — debug and release
builds are signed with different keys, and Google matches on the signature. With
only the release fingerprint registered, sign-in fails on every development
build with a generic error.

### 3. iOS

Type: **iOS**.

| Field | Value |
|---|---|
| Bundle ID | `com.hookary.app` |

iOS needs two entries in `ios/Runner/Info.plist`, both derived from that one
client id — nothing else to fetch:

```xml
<!-- The iOS client id itself. -->
<key>GIDClientID</key>
<string>XXXXXXXX-YYYYYYY.apps.googleusercontent.com</string>

<!-- The same id reversed, registered as a URL scheme so Google can hand the
     result back to the app. -->
<key>CFBundleURLTypes</key>
<array>
  <dict>
    <key>CFBundleURLSchemes</key>
    <array>
      <string>com.googleusercontent.apps.XXXXXXXX-YYYYYYY</string>
    </array>
  </dict>
</array>
```

`GIDClientID` is read from the plist because the app passes `clientId: null` on
iOS; the URL scheme is a platform registration and has to be here regardless.

Before creating the iOS client, verify the Runner's actual bundle id in Xcode.
The client and the built application must both use `com.hookary.app`; a client
created for another bundle id cannot be repaired by editing its plist locally.

## Wiring the values in

### Backend — `.env`

```sh
GOOGLE_CLIENT_ID_WEB=xxx.apps.googleusercontent.com
GOOGLE_CLIENT_ID_IOS=yyy.apps.googleusercontent.com
GOOGLE_CLIENT_ID_ANDROID=zzz.apps.googleusercontent.com
```

Set every one you use. The API accepts a token addressed to any of them and
rejects everything else, so a missing id means that platform cannot sign in.

Without any of them the API still starts, and `/v1/auth/google` answers 503
`auth_not_configured`. In `HOOKARY_ENV=prod` it refuses to start instead — a
production API that cannot authenticate anyone should not pretend to be up.

Also generate the session signing secret:

```sh
openssl rand -base64 48   # -> JWT_SECRET
```

### Frontend — `--dart-define`

The normal entry point reads the web client id from the root `.env`, pins the
registered port, and passes only public values to Flutter:

```sh
make run-web
```

For a mobile device:

```sh
flutter devices
make run-mobile device=<device-id>
```

Only the **web** client id goes to Flutter, and it is used twice:

- on web, as the app's own `clientId`;
- on Android and iOS, as `serverClientId` — the audience the backend verifies.
  Without it those platforms return a token addressed to the native client, and
  the API rejects it.

The native client ids are configured in Google's console against the package
name and bundle id; the app never passes them.

## Android signing

`make android-fingerprints` prints both SHA-1s.

| Build | Keystore | Notes |
|---|---|---|
| debug | `~/.android/debug.keystore` | Created by the Android tooling. Shared by every Flutter project on the machine. |
| release | `~/.keystores/hookary-upload.jks` | Generated for Hookary. Alias `hookary-upload`, valid until 2053. |

The release keystore lives outside the repository on purpose: a keystore inside
the working tree eventually gets committed. `android/key.properties` points at
it and holds its password — that file is gitignored, and must stay that way.

**Back up the keystore and its password.** Without them you cannot ship an
update to an existing Play listing; the app would have to be published again
under a new identity. If you enroll in Play App Signing, this is the *upload*
key and Google can reset it — without Play App Signing, it is the only key and
losing it is unrecoverable.

If you re-key or use Play App Signing, the SHA-1 changes and the Android OAuth
client must be updated to match.

## Checking it works

```sh
make up
make migrate-up
make run-web
```

Sign in, then confirm the session survives a reload — that is the refresh cookie
doing its job. `docker compose logs api` shows `signed in with google` with the
user id.

## When it fails

| Symptom | Cause |
|---|---|
| 503 `auth_not_configured` | No `GOOGLE_CLIENT_ID_*` in the API's environment. |
| 401 `invalid_token`, logs say `audience ... is not ours` | The app's client id is not in the API's allowlist — usually `serverClientId` missing on mobile. |
| Web button does not render | Origin missing from Authorized JavaScript origins, or the wrong port. |
| One Tap never appears | Normal in several cases: no Google session in the browser, the user dismissed it before (GIS backs off on its own), or they signed out — `signOut` calls `disableAutoSelect`. The button still works. |
| Android sign-in fails with no detail | The build's SHA-1 is not registered. Debug and release differ. |
| Signed out on every reload (web) | The refresh cookie is not reaching the API: check `CORS_ALLOWED_ORIGINS` and that the app's origin is listed. |

## App Store login requirement

This Google flow is enough for web, Android, TestFlight development, and the
backend session model. Before App Store review, iOS also needs an equivalent
privacy-preserving login option under Apple's current
[App Review Guideline 4.8](https://developer.apple.com/app-store/review/guidelines/#login-services).
For Hookary that normally means Sign in with Apple. It should exchange an Apple
identity token for the same Hookary access/refresh session pair; it must not be
mixed into the separate YouTube authorization grant.
