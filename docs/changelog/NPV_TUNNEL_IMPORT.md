# Npv Tunnel one-tap import

ELECTRUVPN BETA1 adds an **Open in Npv Tunnel** action to the Subscription page.

## Behavior
- Generates the real client share URI from the existing `/clients/:id/link` endpoint.
- Copies the URI to the device clipboard first.
- Android: attempts to launch Npv Tunnel using its published Android package (`com.napsternetlabs.napsternetv`). If the installed Npv Tunnel build does not auto-import clipboard content, the user can use `Configs -> + -> Import from Clipboard`.
- iOS/iPadOS: uses the native share sheet when available, with the config URI as the shared text. Npv Tunnel supports importing configurations from links and clipboard; no public Npv Tunnel custom URL scheme was found, so the panel does not invent an unsupported scheme.

The feature intentionally avoids embedding the subscription token in a third-party deep-link URL. The generated config is the same authenticated client share URI already used by the panel.


## Android one-tap flow

The Subscription page now provides an Android-specific **Add to Npv Tunnel** action. It copies the generated client URI to the Android clipboard first, then launches the Npv Tunnel package `com.napsternetlabs.napsternetv` using an Android intent. Npv Tunnel supports importing configurations from links and clipboard. On Android versions that do not auto-detect the copied URI, the user can use **Configs → + → Import from Clipboard**.

This is intentionally implemented without inventing an undocumented Npv Tunnel custom URL scheme.
