package helmclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/api/meta"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/cli-runtime/pkg/resource"
	"sigs.k8s.io/yaml"
)

const lastAppliedAnnotation = "kubectl.kubernetes.io/last-applied-configuration"

// ThreeWayMerged produces (live, projected) YAML streams for the resources
// in the rendered chart. The live side is each resource's current cluster
// state; the projected side is the live state with the new chart applied via
// three-way JSON merge patch — i.e. exactly what helm upgrade would converge
// the cluster to.
//
// Resources that don't yet exist in the cluster contribute a pure addition
// (live = empty, projected = rendered manifest). The reverse case (resources
// removed from the chart but still in the cluster) is not handled here yet;
// helm-diff parity for that path is on the v0.3 backlog.
func (c *Client) ThreeWayMerged(releaseName, chartPath string, opts RenderOptions, useUpgradeDryRun bool) ([]byte, []byte, error) {
	rendered, err := c.renderForThreeWay(releaseName, chartPath, opts, useUpgradeDryRun)
	if err != nil {
		return nil, nil, err
	}

	resources, err := c.cfg.KubeClient.Build(bytes.NewReader(rendered), false)
	if err != nil {
		return nil, nil, fmt.Errorf("parse rendered chart: %w", err)
	}

	storedManifest, err := c.storedManifestByKey(releaseName)
	if err != nil {
		return nil, nil, err
	}

	var liveStream, projectedStream bytes.Buffer
	if err := resources.Visit(func(info *resource.Info, vErr error) error {
		if vErr != nil {
			return vErr
		}

		modifiedJSON, err := runtime.Encode(unstructuredJSONEncoder{}, info.Object)
		if err != nil {
			return fmt.Errorf("encode rendered %s: %w", resourceKey(info), err)
		}

		liveJSON, err := getLiveJSON(info)
		if err != nil {
			if !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
				return fmt.Errorf("get live %s: %w", resourceKey(info), err)
			}
			// Not found → pure addition.
			return appendYAML(&projectedStream, modifiedJSON, &liveStream, nil)
		}

		// Determine "original" (last-applied). Prefer the kubectl annotation;
		// fall back to the release-stored manifest for that key.
		originalJSON := lastAppliedFromLive(liveJSON)
		if len(originalJSON) == 0 {
			if stored, ok := storedManifest[resourceKey(info)]; ok {
				originalJSON, err = yaml.YAMLToJSON([]byte(stored))
				if err != nil {
					return fmt.Errorf("convert stored manifest for %s: %w", resourceKey(info), err)
				}
			}
		}
		// If still empty, fall back to using the modified manifest as original
		// — equivalent to helm-diff's behaviour when no last-applied is found.
		if len(originalJSON) == 0 {
			originalJSON = modifiedJSON
		}

		patch, err := jsonmergepatch.CreateThreeWayJSONMergePatch(originalJSON, modifiedJSON, liveJSON)
		if err != nil {
			return fmt.Errorf("compute three-way patch for %s: %w", resourceKey(info), err)
		}

		projectedJSON, err := jsonpatch.MergePatch(liveJSON, patch)
		if err != nil {
			return fmt.Errorf("apply three-way patch for %s: %w", resourceKey(info), err)
		}

		return appendYAML(&projectedStream, projectedJSON, &liveStream, liveJSON)
	}); err != nil {
		return nil, nil, err
	}

	return liveStream.Bytes(), projectedStream.Bytes(), nil
}

// renderForThreeWay produces the modified-side YAML — either via helm
// template (default) or helm upgrade --dry-run when the user opted in.
// Mirrors the source-B branching in cmd/upgrade.go so --three-way-merge
// composes with --use-upgrade-dry-run.
func (c *Client) renderForThreeWay(releaseName, chartPath string, opts RenderOptions, useUpgradeDryRun bool) ([]byte, error) {
	if !useUpgradeDryRun {
		return c.Template(releaseName, chartPath, opts)
	}
	// For three-way against a not-yet-installed release, fall back to install
	// --dry-run; otherwise upgrade --dry-run.
	manifest, err := c.GetManifest(releaseName, 0)
	if err != nil {
		return nil, err
	}
	if len(manifest) == 0 {
		return c.InstallDryRun(releaseName, chartPath, opts)
	}
	return c.UpgradeDryRun(releaseName, chartPath, opts)
}

// storedManifestByKey returns the helm-stored manifest, indexed by
// "kind/namespace/name". Used as the fallback "original" when a live object
// has no kubectl last-applied annotation (helm-managed resources frequently
// don't carry it).
func (c *Client) storedManifestByKey(releaseName string) (map[string]string, error) {
	manifest, err := c.GetManifest(releaseName, 0)
	if err != nil {
		return nil, err
	}
	if len(manifest) == 0 {
		return map[string]string{}, nil
	}
	out := make(map[string]string)
	docs := splitYAMLDocs(string(manifest))
	for _, doc := range docs {
		key, err := keyFromYAMLDoc(doc)
		if err != nil {
			continue // skip docs we can't index — they just won't have a fallback
		}
		out[key] = doc
	}
	return out, nil
}

func resourceKey(info *resource.Info) string {
	gvk := info.Object.GetObjectKind().GroupVersionKind()
	return strings.ToLower(gvk.Kind) + "/" + info.Namespace + "/" + info.Name
}

func keyFromYAMLDoc(doc string) (string, error) {
	var u unstructured.Unstructured
	if err := yaml.Unmarshal([]byte(doc), &u.Object); err != nil {
		return "", err
	}
	if u.GetKind() == "" || u.GetName() == "" {
		return "", errors.New("yaml doc missing kind or name")
	}
	return strings.ToLower(u.GetKind()) + "/" + u.GetNamespace() + "/" + u.GetName(), nil
}

func splitYAMLDocs(s string) []string {
	parts := strings.Split(s, "\n---\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(strings.TrimPrefix(p, "---\n"))
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

// getLiveJSON fetches the live cluster object via the resource.Helper and
// returns its JSON encoding (which is what the patch packages consume).
func getLiveJSON(info *resource.Info) ([]byte, error) {
	helper := resource.NewHelper(info.Client, info.Mapping)
	live, err := helper.Get(info.Namespace, info.Name)
	if err != nil {
		return nil, err
	}
	return runtime.Encode(unstructuredJSONEncoder{}, live)
}

// lastAppliedFromLive extracts the kubectl-style last-applied annotation if
// present. Helm-managed resources usually lack it but kubectl-managed ones
// have it.
func lastAppliedFromLive(liveJSON []byte) []byte {
	var u unstructured.Unstructured
	if err := json.Unmarshal(liveJSON, &u.Object); err != nil {
		return nil
	}
	v := u.GetAnnotations()[lastAppliedAnnotation]
	if v == "" {
		return nil
	}
	return []byte(v)
}

// appendYAML writes both live and projected JSON to their respective
// concatenated YAML streams. Either argument may be nil to emit an empty
// document for that side.
func appendYAML(projectedStream io.Writer, projectedJSON []byte, liveStream io.Writer, liveJSON []byte) error {
	if liveJSON != nil {
		if err := writeJSONAsYAMLDoc(liveStream, liveJSON); err != nil {
			return err
		}
	}
	if projectedJSON != nil {
		if err := writeJSONAsYAMLDoc(projectedStream, projectedJSON); err != nil {
			return err
		}
	}
	return nil
}

func writeJSONAsYAMLDoc(w io.Writer, in []byte) error {
	out, err := yaml.JSONToYAML(in)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, "---\n"); err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}

// unstructuredJSONEncoder serializes any runtime.Object to its JSON form by
// going through unstructured. Avoids needing the typed scheme to know about
// the GVK (works for CRDs and custom types).
type unstructuredJSONEncoder struct{}

func (unstructuredJSONEncoder) Encode(obj runtime.Object, w io.Writer) error {
	if u, ok := obj.(runtime.Unstructured); ok {
		return json.NewEncoder(w).Encode(u.UnstructuredContent())
	}
	// runtime.DefaultUnstructuredConverter via apimachinery knows how to
	// flatten structured types too.
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(m)
}

// Identifier is required by runtime.Encoder.
func (unstructuredJSONEncoder) Identifier() runtime.Identifier {
	return runtime.Identifier("helm-diffyml/unstructured-json")
}

// Compile-time assertion: we conform to the runtime.Encoder interface.
var _ runtime.Encoder = unstructuredJSONEncoder{}

// Sanity: ensure release.Release is referenced so the import isn't dropped
// when the file gets lints-only changes later.
var _ = (*release.Release)(nil)
