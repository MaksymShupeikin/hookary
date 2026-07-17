import 'package:flutter/material.dart';

/// On iOS and Android sign-in starts from our own button, so there is no
/// platform widget to render and the caller draws its own.
Widget? buildGoogleSignInButton() => null;
