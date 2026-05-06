#!/usr/bin/env sh
# Cluster-dependent smoke test. Requires a reachable Kubernetes cluster
# (e.g. kind) and the plugin installed.
#
# Asserts:
#   - `helm diffyml upgrade` against an installed release emits a non-empty
#     diff and exits 0 by default.
#   - `--exit-code` propagates rc=1 when differences exist.
#   - `-o json` produces valid JSON.
#   - `helm diffyml upgrade` against a NON-EXISTENT release succeeds with the
#     "from" side treated as empty (initial-install scenario).
set -eu

HERE="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)"
FIXTURE="$HERE/fixture-chart"
RELEASE="helm-diffyml-smoke-$$"
NAMESPACE="${HELM_DIFFYML_TEST_NS:-default}"

pass() { printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1" >&2; exit 1; }

cleanup() {
  helm uninstall "$RELEASE" -n "$NAMESPACE" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM HUP

echo "==> helm install $RELEASE (initial state for upgrade diff)"
helm install "$RELEASE" "$FIXTURE" -n "$NAMESPACE" --wait --timeout 60s >/dev/null \
  || fail "helm install failed"
pass "release installed"

echo "==> helm diffyml upgrade with values change"
out="$(helm diffyml upgrade "$RELEASE" "$FIXTURE" -f "$FIXTURE/values-changed.yaml" -n "$NAMESPACE")" \
  || fail "upgrade exited non-zero without --exit-code"
[ -n "$out" ] || fail "upgrade produced empty output despite known changes"
pass "upgrade emits non-empty diff"

echo "==> helm diffyml upgrade --exit-code (rc=1 on differences)"
set +e
helm diffyml upgrade "$RELEASE" "$FIXTURE" -f "$FIXTURE/values-changed.yaml" -n "$NAMESPACE" --exit-code >/dev/null
rc=$?
set -e
[ "$rc" = "1" ] || fail "expected rc=1 with --exit-code, got $rc"
pass "--exit-code propagates rc=1"

echo "==> helm diffyml upgrade -o json"
json="$(helm diffyml upgrade "$RELEASE" "$FIXTURE" -f "$FIXTURE/values-changed.yaml" -n "$NAMESPACE" -o json)" \
  || fail "upgrade -o json exited non-zero"
case "$(printf '%s' "$json" | tr -d ' \n\t' | cut -c1)" in
  '{'|'[') pass "json output is JSON-shaped" ;;
  *) fail "json output not JSON-shaped: $(printf '%s' "$json" | head -c 80)" ;;
esac

echo "==> helm diffyml upgrade against non-existent release (pure addition)"
out="$(helm diffyml upgrade "${RELEASE}-doesnotexist" "$FIXTURE" -n "$NAMESPACE" -f "$FIXTURE/values-changed.yaml")" \
  || fail "upgrade against missing release should not fail"
[ -n "$out" ] || fail "missing-release upgrade produced empty output"
pass "missing release treated as empty Source A"

echo "==> helm upgrade $RELEASE -f values-changed.yaml (creates revision 2)"
helm upgrade "$RELEASE" "$FIXTURE" -f "$FIXTURE/values-changed.yaml" -n "$NAMESPACE" --wait --timeout 60s >/dev/null \
  || fail "helm upgrade to revision 2 failed"
pass "release upgraded to revision 2"

echo "==> helm diffyml revision $RELEASE 1 2"
out="$(helm diffyml revision "$RELEASE" 1 2 -n "$NAMESPACE")" \
  || fail "revision 1->2 exited non-zero without --exit-code"
[ -n "$out" ] || fail "revision diff was empty despite known changes"
pass "revision 1->2 emits non-empty diff"

echo "==> helm diffyml revision $RELEASE 1 1 (identical)"
out="$(helm diffyml revision "$RELEASE" 1 1 -n "$NAMESPACE")" \
  || fail "revision 1->1 exited non-zero"
pass "revision 1->1 (identical) succeeds"

echo "==> helm diffyml revision $RELEASE 1 2 --exit-code (rc=1 on differences)"
set +e
helm diffyml revision "$RELEASE" 1 2 -n "$NAMESPACE" --exit-code >/dev/null
rc=$?
set -e
[ "$rc" = "1" ] || fail "expected rc=1 with --exit-code on revision diff, got $rc"
pass "revision --exit-code propagates rc=1"

echo "==> helm diffyml revision: nonexistent revision is hard error"
set +e
helm diffyml revision "$RELEASE" 1 99 -n "$NAMESPACE" >/dev/null 2>&1
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "expected non-zero exit for missing revision"
pass "missing revision exits non-zero"

echo "==> helm diffyml rollback $RELEASE 1 (explicit revision)"
out="$(helm diffyml rollback "$RELEASE" 1 -n "$NAMESPACE")" \
  || fail "rollback to revision 1 exited non-zero"
[ -n "$out" ] || fail "rollback to revision 1 produced empty output"
pass "rollback with explicit revision emits diff"

echo "==> helm diffyml rollback $RELEASE (implicit previous revision)"
out="$(helm diffyml rollback "$RELEASE" -n "$NAMESPACE")" \
  || fail "rollback (no revision arg) exited non-zero"
[ -n "$out" ] || fail "implicit-revision rollback produced empty output"
pass "implicit previous revision resolved via helm history"

echo "==> helm diffyml rollback --exit-code (rc=1 on differences)"
set +e
helm diffyml rollback "$RELEASE" 1 -n "$NAMESPACE" --exit-code >/dev/null
rc=$?
set -e
[ "$rc" = "1" ] || fail "expected rc=1 with --exit-code on rollback diff, got $rc"
pass "rollback --exit-code propagates rc=1"

echo "==> helm diffyml rollback against nonexistent release fails"
set +e
helm diffyml rollback "${RELEASE}-doesnotexist" 1 -n "$NAMESPACE" >/dev/null 2>&1
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "expected non-zero exit for missing release"
pass "rollback on missing release exits non-zero"

echo "==> helm diffyml upgrade --use-upgrade-dry-run (extant release)"
out="$(helm diffyml upgrade "$RELEASE" "$FIXTURE" -f "$FIXTURE/values.yaml" -n "$NAMESPACE" --use-upgrade-dry-run)" \
  || fail "upgrade --use-upgrade-dry-run exited non-zero"
[ -n "$out" ] || fail "upgrade --use-upgrade-dry-run produced empty output"
pass "upgrade --use-upgrade-dry-run emits diff via helm upgrade --dry-run"

echo "==> helm diffyml upgrade --use-upgrade-dry-run (non-existent release uses helm install)"
out="$(helm diffyml upgrade "${RELEASE}-newrel" "$FIXTURE" -f "$FIXTURE/values-changed.yaml" -n "$NAMESPACE" --use-upgrade-dry-run)" \
  || fail "missing-release --use-upgrade-dry-run exited non-zero"
[ -n "$out" ] || fail "missing-release --use-upgrade-dry-run produced empty output"
pass "missing release falls back to helm install --dry-run"

echo "==> HELM_DIFFYML_USE_UPGRADE_DRY_RUN=true env path"
out="$(HELM_DIFFYML_USE_UPGRADE_DRY_RUN=true helm diffyml upgrade "$RELEASE" "$FIXTURE" -f "$FIXTURE/values.yaml" -n "$NAMESPACE")" \
  || fail "env-flag upgrade exited non-zero"
[ -n "$out" ] || fail "env-flag upgrade produced empty output"
pass "env-var enables upgrade-dry-run end-to-end"

echo "==> three-way-merge: drift detection"
# Mutate the live Deployment out-of-band so the cluster diverges from the
# stored manifest. Two-way diff against the stored manifest won't see this
# drift; three-way against live state will.
kubectl scale deployment "${RELEASE}-web" -n "$NAMESPACE" --replicas=7 >/dev/null \
  || fail "kubectl scale to 7 failed"

# Two-way (default): re-rendering the chart with the original values yields
# replicas=2; helm get manifest also says replicas=2; so default upgrade
# diff is empty (no drift visible).
two_way="$(helm diffyml upgrade "$RELEASE" "$FIXTURE" -f "$FIXTURE/values.yaml" -n "$NAMESPACE")" \
  || fail "two-way upgrade exited non-zero"
case "$two_way" in
  *replicas*) fail "two-way diff should NOT show drift, got: $two_way" ;;
  *)          pass "two-way diff is empty (drift not visible — expected)" ;;
esac

# Three-way: live state has replicas=7, projected has replicas=2, so the
# diff should mention replicas going 7 → 2.
three_way="$(helm diffyml upgrade "$RELEASE" "$FIXTURE" -f "$FIXTURE/values.yaml" -n "$NAMESPACE" --three-way-merge)" \
  || fail "three-way upgrade exited non-zero"
case "$three_way" in
  *replicas*7*) pass "three-way diff surfaces drift (replicas 7→2)" ;;
  *)            fail "three-way diff should show the 7→2 drift, got: $three_way" ;;
esac

# Compose with --use-upgrade-dry-run for parity check (still detects drift).
combo="$(helm diffyml upgrade "$RELEASE" "$FIXTURE" -f "$FIXTURE/values.yaml" -n "$NAMESPACE" --three-way-merge --use-upgrade-dry-run)" \
  || fail "three-way + use-upgrade-dry-run exited non-zero"
[ -n "$combo" ] || fail "three-way + use-upgrade-dry-run produced empty output"
pass "three-way composes with --use-upgrade-dry-run"

echo
echo "All cluster smoke checks passed."
