# Bounded production release

The release contract is at most ten minutes from merging the prepared commit to both production nodes serving it. Each node has a one-to-two-minute decision window: commit after direct health, TLS, peer-sync and UDP/TCP DNS checks, or recover using the qualified image on the same data volumes. A timeout is a failed release, not permission to skip verification or discard data.

Branch CI completes the existing tests and security checks, builds the final runtime image, and publishes it as `verified-<source SHA>` before merge. Merge the tested commit by fast-forward so the release selects that exact source. Deployment resolves the prepared image to an immutable digest once and uses that digest for both nodes. An absent or mismatched prepared image fails before replacing either node. Deployment does not rebuild the application.

Recovery rehearsals and full database integrity scans run only in the isolated development environment. They are not CI or production deployment steps. The production path preserves code/Compose checkpoints, complete recovery images, generation identity, and node-local recovery authority. It never restores an old database over newer accepted history. It contains no prolonged canary or soak waits.

The daily schedule still rebuilds against current upstream bases, runs the complete CI/security gates, and deploys the resulting prepared image. Manual security-only auditing remains available. Branch tests do not repeat solely because the same commit is opened as a pull request or fast-forwarded to main.

This decision supersedes the former fifteen-minute Blue canary, five-minute Red soak, deployment-time image rebuild, and production clone rehearsal described in historical release notes. Actual run timings and recovery outcomes must establish compliance; these limits are not claims that an unmeasured release succeeded.
