# ELECTRUVPN BETA1 — Inbound / Client UX Fix

## Changes
- Added guarded inbound wizard navigation with required-field and JSON validation.
- Added server draft validation before the review step.
- Prevented duplicate inbound/client submissions while requests are in flight.
- Added client label and non-negative quota validation.
- Improved mobile modal sizing, scroll behavior, touch target sizes, Safe Area spacing, and responsive action layout.
- Made client creation controls stack on narrow screens.

## Notes
The wizard currently uses the existing four-step progress UI; the source markup does not isolate every existing configuration field into separate step panels, so fields remain in the existing form body. Draft validation and navigation are now guarded, but a deeper markup refactor is still needed to make each step display only its own fields.
