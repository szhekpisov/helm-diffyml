# Changelog

All notable changes to `helm-diffyml` are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Each plugin tag pins a specific `diffyml` version (embedded as a Go library).

## [Unreleased]

### Added
- `helm diffyml upgrade --no-hooks` and `--include-tests` — control which
  helm hooks appear in the rendered diff. Default matches helm-diff:
  non-test hooks are included (so pre/post-install/upgrade hooks show up
  in the preview), test-event hooks are excluded. `--no-hooks` strips
  every hook; `--include-tests` adds the test pods back in. Hooks live
  in `release.Release.Hooks` separately from `Manifest`, and are merged
  in by the plugin per the flag combination.
- `helm diffyml upgrade --reuse-values` and `--reset-values` — mirror
  `helm upgrade`'s value-merging semantics. `--reuse-values` pulls the
  deployed release's values back in (with new `-f`/`--set` overriding on
  conflict); `--reset-values` ignores them entirely (chart defaults +
  CLI overrides). Passing both has `--reset-values` win, matching
  helm-diff. Composes with `--use-upgrade-dry-run` and `--three-way-merge`.
- `helm diffyml upgrade --three-way-merge` (env: `HELM_DIFFYML_THREE_WAY_MERGE`)
  — diff against live cluster state via three-way merge per resource.
  Catches out-of-band drift (`kubectl edit`, controller mutations,
  admission webhooks) that the default two-way `helm get manifest` path
  cannot see. Native Kubernetes types use strategic-merge-patch (so
  array fields like `containers` merge by `name` instead of being
  replaced wholesale); CRDs fall back to JSON merge. Resources tracked
  by the release's stored manifest but absent from the new render are
  surfaced as deletions. Composes with `--use-upgrade-dry-run`.

## [0.1.0]

### Added
- `helm diffyml upgrade RELEASE CHART` — diff between the current release and
  a re-rendered chart (the headline use case).
- `helm diffyml release REL_A REL_B` — diff between two live release manifests.
- `helm diffyml revision RELEASE REV_A REV_B` — diff between two revisions of
  one release (uses `helm get manifest --revision`).
- `helm diffyml rollback RELEASE [REVISION]` — preview a `helm rollback`. If
  the revision is omitted, the immediately previous one is selected via
  `helm history`.
- `helm diffyml upgrade --use-upgrade-dry-run` (env:
  `HELM_DIFFYML_USE_UPGRADE_DRY_RUN`) — high-fidelity source B via
  `helm upgrade --dry-run`, with `helm install --dry-run` fallback for
  releases that do not yet exist.
- `helm diffyml version` — print plugin version and embedded diffyml version.
- Single Go binary per OS/arch (Linux + macOS, amd64 + arm64). Diffyml is
  embedded as a Go library, so there is no separate diffyml install step.
- `install-binary.sh` install/update hook with SHA-256 verification against
  the release's `checksums.txt`.

## Plugin → diffyml support matrix

| helm-diffyml | diffyml |
|---|---|
| v0.1.0 | v1.6.0 |
