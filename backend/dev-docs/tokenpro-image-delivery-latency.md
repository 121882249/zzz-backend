# Text-group image delivery latency (R82)

## Observed production behavior

On 2026-10-15 (Asia/Shanghai), the successful `画一只猫` conversation
`01a13dce-5027-7a41-b673-f101af2df3cb` used the original global Key,
group 69, `gpt-6.1-sol`, and the installed TokenPro image CLI.

| Request | CLI duration | File saved | Image message | Save-to-display |
| --- | ---: | --- | --- | ---: |
| Cat | 22.929 s | 12:25:38.111 | 12:26:08.281 | 30.170 s |
| Cat and dog | 26.461 s | 12:28:09.084 | 12:28:21.717 | 12.633 s |

The local file timestamps prove that these post-save delays were not the
Images API waiting to produce/validate the image. In the first request, the
image process completed while the agent was between short shell polls; the
successful output was not collected until 12:25:56.723. A text-model final
continuation then added about 11.6 seconds. The second request also performed
an unsupported `--help` probe and inspected the launcher before generation.

The gateway's dimension probe reads only image headers locally, and normal
usage recording is submitted to the worker pool. No additional remote image
verification was found in the inspected successful path. These observations
do not imply that upstream generation or all other clients are always fast.

## Scoped optimization

The existing authenticated text-group Skill compatibility instructions now
tell the model to use the documented CLI directly, wait up to 30 seconds per
execution/poll, and return the saved image immediately after confirmed CLI
success. Extra help/launcher probes, file inspections and verification are
unnecessary unless the user requested them. Incomplete or failed generation
must still be reported accurately, and mixed user tasks must not be discarded.

Only instructions change. The selected text model/reasoning effort, built-in
`openai` Provider, group/model routes, image parameters, authentication,
authorization, audit scanning, dimension-based billing and mandatory usage
recording remain unchanged. There is no synthetic success response or forced
completion for arbitrary shell output. The final text-model turn still exists;
this is guidance to reduce orchestration overhead, not a zero-latency guarantee.
