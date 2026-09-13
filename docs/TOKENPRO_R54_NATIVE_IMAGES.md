# TokenPro R54: native Codex image delivery

This release is paired with the TokenPro desktop native-v1 configuration. It
does not install or start a local bridge, and does not recreate legacy state files.

## Routing contract

- Each selected model retains its own `tp-g<groupID>-<base64url(model)>` slug.
- The native Images request uses `x-tokenpro-image-route`; Responses text keeps
  its own group. Internal routing headers are consumed before upstream forwarding.
- Pure-image native dispatch is enabled only for an authenticated global key,
  an explicitly opted-in request, an image model, and a resolved OpenAI group
  whose `description` field, trimmed, equals `生图`. Group name is not used.
- Nonmatching pure-image groups retain the old transport path. Native preparation
  never replaces an image model with a text model. Plain Python API calls remain
  unmarked and retain their normal endpoint behavior.
- Text selections keep their selected text model. A separately selected image
  model has its own Images request, group authorization, scheduling and billing.
- Native dispatch occurs after authentication, group resolution, moderation,
  image permission, concurrency and billing eligibility checks. It generates no
  image bytes and incurs no inference usage; actual Images requests use the normal
  authenticated image pipeline and billing.

## Return contract and limitations

- Unique function `call_id` values correlate tool requests and results. Dispatch
  is stateless and stores no image bytes or cross-user request cache.
- A tool-result continuation does not regenerate or claim that an image exists.
  Acceptance requires a real Codex `imageGeneration` item, saved file and exact
  image-byte match on a new process's `thread/read`.
- Pure-image dispatch currently supports new text-to-image requests, not image
  editing or `previous_response_id`-only histories. Those unsupported inputs
  return explicit errors rather than silently dropping input images/history.
- The provider's first selected image is the default native image route. Selecting
  another image model without reapplying TokenPro configuration returns an explicit
  route mismatch instead of silently invoking or billing a different model.

## Web UI

The cosmic login page adds the existing approved `更多模型`/`More models` badge,
four-square gradient icon, matching widths and localized accessibility label.

## Deployment

Use a unique R54 image. Deploy the backend before releasing the native-v1 desktop
configuration, retain the previous compose file/image, then smoke-test opted-in
pure, text-only and mixed selections against the deployed backend. Never count a
model's textual success claim as successful image delivery.
