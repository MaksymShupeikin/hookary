import 'package:flutter/material.dart';
import 'package:google_sign_in_web/web_only.dart' as web;

/// Google's own rendered button.
///
/// Web is the one platform where sign-in cannot be started from an ordinary
/// button: Google Identity Services requires this widget, and calling
/// authenticate() there throws. Clicking it raises a sign-in event on
/// GoogleAuthClient.events, which is where the result is picked up — the
/// button itself returns nothing.
Widget? buildGoogleSignInButton() => web.renderButton(
  configuration: web.GSIButtonConfiguration(
    theme: web.GSIButtonTheme.filledBlack,
    size: web.GSIButtonSize.large,
    text: web.GSIButtonText.continueWith,
    shape: web.GSIButtonShape.pill,
  ),
);
