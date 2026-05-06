package helmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/kubectl/pkg/scheme"
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
// removed from the chart but still in the cluster) is not handled here yet.
func (c *Client) ThreeWayMerged(releaseName, chartPath string, opts RenderOptions, useUpgradeDryRun bool) ([]byte, []byte, error) {
	rendered, err := c.renderForThreeWay(releaseName, chartPath, opts, useUpgradeDryRun)
	if err != nil {
		return nil, nil, err
	}

	objs, err := splitYAMLToUnstructured(rendered)
	if err != nil {
		return nil, nil, fmt.Errorf("parse rendered chart: %w", err)
	}

	storedManifest, err := c.storedManifestByKey(releaseName)
	if err != nil {
		return nil, nil, err
	}

	// Lazy: only build the dynamic client when there's at least one object
	// to look up. Empty charts therefore don't require a reachable cluster.
	var (
		dyn       dynamic.Interface
		mapper    meta.RESTMapper
		dynBuilt  bool
	)

	debug := os.Getenv("HELM_DIFFYML_DEBUG_3WAY") != ""
	if debug {
		fmt.Fprintf(os.Stderr, "--- HELM_DIFFYML_DEBUG_3WAY: rendered chart (%d bytes) parsed into %d objects ---\n", len(rendered), len(objs))
	}

	var liveStream, projectedStream bytes.Buffer
	for _, obj := range objs {
		modifiedJSON, err := obj.MarshalJSON()
		if err != nil {
			return nil, nil, fmt.Errorf("encode rendered %s: %w", objKey(obj), err)
		}

		if !dynBuilt {
			d, m, derr := c.dynamicLookup()
			if derr != nil {
				return nil, nil, fmt.Errorf("build dynamic client: %w", derr)
			}
			dyn, mapper, dynBuilt = d, m, true
		}
		liveObj, err := getLive(context.TODO(), dyn, mapper, obj, c.namespaceFor(opts))
		if err != nil {
			if !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
				return nil, nil, fmt.Errorf("get live %s: %w", objKey(obj), err)
			}
			if debug {
				fmt.Fprintf(os.Stderr, "  %s: not found in cluster (pure addition)\n", objKey(obj))
			}
			if err := writeJSONAsYAMLDoc(&projectedStream, modifiedJSON); err != nil {
				return nil, nil, err
			}
			continue
		}
		liveJSON, err := liveObj.MarshalJSON()
		if err != nil {
			return nil, nil, fmt.Errorf("encode live %s: %w", objKey(obj), err)
		}

		originalJSON := lastAppliedFromLive(liveJSON)
		if len(originalJSON) == 0 {
			if stored, ok := storedManifest[objKey(obj)]; ok {
				originalJSON, err = yaml.YAMLToJSON([]byte(stored))
				if err != nil {
					return nil, nil, fmt.Errorf("convert stored manifest for %s: %w", objKey(obj), err)
				}
			}
		}
		if len(originalJSON) == 0 {
			originalJSON = modifiedJSON
		}

		patch, projectedJSON, err := computeThreeWay(obj.GroupVersionKind(), originalJSON, modifiedJSON, liveJSON)
		if err != nil {
			return nil, nil, fmt.Errorf("three-way patch for %s: %w", objKey(obj), err)
		}

		if debug {
			fmt.Fprintf(os.Stderr, "  %s: patch=%s\n", objKey(obj), shortPatch(patch))
		}

		if err := writeJSONAsYAMLDoc(&liveStream, liveJSON); err != nil {
			return nil, nil, err
		}
		if err := writeJSONAsYAMLDoc(&projectedStream, projectedJSON); err != nil {
			return nil, nil, err
		}
	}

	if debug {
		fmt.Fprintf(os.Stderr, "--- HELM_DIFFYML_DEBUG_3WAY: live stream (%d bytes) ---\n", liveStream.Len())
		_, _ = os.Stderr.Write(liveStream.Bytes())
		fmt.Fprintf(os.Stderr, "\n--- HELM_DIFFYML_DEBUG_3WAY: projected stream (%d bytes) ---\n", projectedStream.Len())
		_, _ = os.Stderr.Write(projectedStream.Bytes())
		fmt.Fprintln(os.Stderr, "\n--- end debug ---")
	}

	return liveStream.Bytes(), projectedStream.Bytes(), nil
}

// computeThreeWay produces (patch, projected) for one resource. Native
// Kubernetes types resolve through k8s.io/kubectl/pkg/scheme so we use
// strategic-merge-patch (which honours `patchStrategy: merge` and
// `patchMergeKey` annotations on fields like `containers` so arrays merge
// by key instead of being replaced wholesale). Unregistered types (CRDs,
// resources from API groups the scheme doesn't know about) fall through
// to JSON merge patch, matching helm-diff's behaviour.
func computeThreeWay(gvk schema.GroupVersionKind, original, modified, live []byte) ([]byte, []byte, error) {
	if dataStruct, err := scheme.Scheme.New(gvk); err == nil {
		if lookup, lerr := strategicpatch.NewPatchMetaFromStruct(dataStruct); lerr == nil {
			// overwrite=true matches helm upgrade's default reconcile
			// semantics: chart values override out-of-band drift on
			// conflict. With overwrite=false the strategic patcher
			// refuses any divergence between live and modified that
			// wasn't intentionally chart-driven, which forces every
			// helm-managed resource down the JSON merge fallback.
			if patch, perr := strategicpatch.CreateThreeWayMergePatch(original, modified, live, lookup, true); perr == nil {
				if projected, aerr := strategicpatch.StrategicMergePatch(live, patch, dataStruct); aerr == nil {
					return patch, projected, nil
				}
			}
		}
	}
	// CRDs / types unknown to k8s.io/kubectl/pkg/scheme fall through to
	// JSON merge, matching helm-diff's behaviour.
	patch, err := jsonmergepatch.CreateThreeWayJSONMergePatch(original, modified, live)
	if err != nil {
		return nil, nil, fmt.Errorf("compute json-merge patch: %w", err)
	}
	projected, err := jsonpatch.MergePatch(live, patch)
	if err != nil {
		return nil, nil, fmt.Errorf("apply json-merge patch: %w", err)
	}
	return patch, projected, nil
}

// renderForThreeWay produces the modified-side YAML — either via helm
// template (default) or helm upgrade --dry-run when the user opted in.
func (c *Client) renderForThreeWay(releaseName, chartPath string, opts RenderOptions, useUpgradeDryRun bool) ([]byte, error) {
	if !useUpgradeDryRun {
		return c.Template(releaseName, chartPath, opts)
	}
	manifest, err := c.GetManifest(releaseName, 0)
	if err != nil {
		return nil, err
	}
	if len(manifest) == 0 {
		return c.InstallDryRun(releaseName, chartPath, opts)
	}
	return c.UpgradeDryRun(releaseName, chartPath, opts)
}

// dynamicLookup returns a dynamic.Interface and a RESTMapper built from the
// same RESTClientGetter the helm action.Configuration uses.
func (c *Client) dynamicLookup() (dynamic.Interface, meta.RESTMapper, error) {
	getter := c.settings.RESTClientGetter()
	rc, err := getter.ToRESTConfig()
	if err != nil {
		return nil, nil, err
	}
	mapper, err := getter.ToRESTMapper()
	if err != nil {
		return nil, nil, err
	}
	dyn, err := dynamic.NewForConfig(rc)
	if err != nil {
		return nil, nil, err
	}
	return dyn, mapper, nil
}

// getLive fetches the live cluster object matching obj's GVK + namespace + name.
// fallbackNamespace is used when the rendered manifest doesn't carry an
// explicit metadata.namespace (which is the common case — helm injects it
// from the install context).
func getLive(ctx context.Context, dyn dynamic.Interface, mapper meta.RESTMapper, obj *unstructured.Unstructured, fallbackNamespace string) (*unstructured.Unstructured, error) {
	gvk := obj.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}
	name := obj.GetName()
	namespace := obj.GetNamespace()
	if namespace == "" {
		namespace = fallbackNamespace
	}
	var ri dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ri = dyn.Resource(mapping.Resource).Namespace(namespace)
	} else {
		ri = dyn.Resource(mapping.Resource)
	}
	return ri.Get(ctx, name, metav1.GetOptions{})
}

// splitYAMLToUnstructured parses a multi-doc YAML stream into typed
// unstructured objects. Skips empty documents.
func splitYAMLToUnstructured(in []byte) ([]*unstructured.Unstructured, error) {
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(in), 4096)
	var out []*unstructured.Unstructured
	for {
		raw := map[string]interface{}{}
		if err := dec.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if len(raw) == 0 {
			continue
		}
		out = append(out, &unstructured.Unstructured{Object: raw})
	}
	return out, nil
}

// storedManifestByKey returns the helm-stored manifest, indexed by
// "kind/namespace/name". Used as the fallback "original" when a live object
// has no kubectl last-applied annotation.
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
			continue
		}
		out[key] = doc
	}
	return out, nil
}

func objKey(obj *unstructured.Unstructured) string {
	gvk := obj.GroupVersionKind()
	return strings.ToLower(gvk.Kind) + "/" + obj.GetNamespace() + "/" + obj.GetName()
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

// lastAppliedFromLive extracts the kubectl-style last-applied annotation if
// present.
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

func shortPatch(p []byte) string {
	if len(p) <= 200 {
		return string(p)
	}
	return string(p[:200]) + "...(truncated)"
}

