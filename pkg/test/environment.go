/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package test

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
<<<<<<< HEAD
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	"knative.dev/pkg/system"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/aws/karpenter-core/pkg/apis/v1beta1"
	"github.com/aws/karpenter-core/pkg/utils/env"
	"github.com/aws/karpenter-core/pkg/utils/functional"
=======
	"knative.dev/pkg/ptr"

	"github.com/patrickmn/go-cache"

	coreapis "github.com/aws/karpenter-core/pkg/apis"
	"github.com/aws/karpenter-core/pkg/apis/v1alpha5"
	corev1beta1 "github.com/aws/karpenter-core/pkg/apis/v1beta1"
	"github.com/aws/karpenter-core/pkg/operator/scheme"
	"github.com/aws/karpenter/pkg/apis"
	awscache "github.com/aws/karpenter/pkg/cache"
	"github.com/aws/karpenter/pkg/fake"
	"github.com/aws/karpenter/pkg/providers/amifamily"
	"github.com/aws/karpenter/pkg/providers/instance"
	"github.com/aws/karpenter/pkg/providers/instanceprofile"
	"github.com/aws/karpenter/pkg/providers/instancetype"
	"github.com/aws/karpenter/pkg/providers/launchtemplate"
	"github.com/aws/karpenter/pkg/providers/pricing"
	"github.com/aws/karpenter/pkg/providers/securitygroup"
	"github.com/aws/karpenter/pkg/providers/subnet"
	"github.com/aws/karpenter/pkg/providers/version"

	coretest "github.com/aws/karpenter-core/pkg/test"

	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
>>>>>>> 1db74f402628818c1f6ead391cc039d2834e7e13
)

func init() {
	lo.Must0(apis.AddToScheme(scheme.Scheme))
	v1alpha5.NormalizedLabels = lo.Assign(v1alpha5.NormalizedLabels, map[string]string{"topology.ebs.csi.aws.com/zone": corev1.LabelTopologyZone})
	corev1beta1.NormalizedLabels = lo.Assign(corev1beta1.NormalizedLabels, map[string]string{"topology.ebs.csi.aws.com/zone": corev1.LabelTopologyZone})
	coreapis.Settings = append(coreapis.Settings, apis.Settings...)
}

type Environment struct {
	envtest.Environment

	Client              client.Client
	KubernetesInterface kubernetes.Interface
	Version             *version.Version
	Done                chan struct{}
	Cancel              context.CancelFunc
}

type EnvironmentOptions struct {
	crds          []*v1.CustomResourceDefinition
	fieldIndexers []func(cache.Cache) error
}

// WithCRDs registers the specified CRDs to the apiserver for use in testing
func WithCRDs(crds ...*v1.CustomResourceDefinition) functional.Option[EnvironmentOptions] {
	return func(o EnvironmentOptions) EnvironmentOptions {
		o.crds = append(o.crds, crds...)
		return o
	}
}

// WithFieldIndexers expects a function that indexes fields against the cache such as cache.IndexField(...)
func WithFieldIndexers(fieldIndexers ...func(cache.Cache) error) functional.Option[EnvironmentOptions] {
	return func(o EnvironmentOptions) EnvironmentOptions {
		o.fieldIndexers = append(o.fieldIndexers, fieldIndexers...)
		return o
	}
}

func NodeClaimFieldIndexer(ctx context.Context) func(cache.Cache) error {
	return func(c cache.Cache) error {
		return c.IndexField(ctx, &v1beta1.NodeClaim{}, "status.providerID", func(obj client.Object) []string {
			return []string{obj.(*v1beta1.NodeClaim).Status.ProviderID}
		})
	}
}

func NewEnvironment(scheme *runtime.Scheme, options ...functional.Option[EnvironmentOptions]) *Environment {
	opts := functional.ResolveOptions(options...)
	ctx, cancel := context.WithCancel(context.Background())

	os.Setenv(system.NamespaceEnvKey, "default")
	version := version.MustParseSemantic(strings.Replace(env.WithDefaultString("K8S_VERSION", "1.28.x"), ".x", ".0", -1))
	environment := envtest.Environment{Scheme: scheme, CRDs: opts.crds}
	if version.Minor() >= 21 {
		// PodAffinityNamespaceSelector is used for label selectors in pod affinities.  If the feature-gate is turned off,
		// the api-server just clears out the label selector so we never see it.  If we turn it on, the label selectors
		// are passed to us and we handle them. This feature is alpha in v1.21, beta in v1.22 and will be GA in 1.24. See
		// https://github.com/kubernetes/enhancements/issues/2249 for more info.
		environment.ControlPlane.GetAPIServer().Configure().Set("feature-gates", "PodAffinityNamespaceSelector=true")
	}
	if version.Minor() >= 24 {
		// MinDomainsInPodTopologySpread enforces a minimum number of eligible node domains for pod scheduling
		// See https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/#spread-constraint-definition
		// Ref: https://github.com/aws/karpenter-core/pull/330
		environment.ControlPlane.GetAPIServer().Configure().Set("feature-gates", "MinDomainsInPodTopologySpread=true")
	}

	_ = lo.Must(environment.Start())

	// We use a modified client if we need field indexers
	var c client.Client
	if len(opts.fieldIndexers) > 0 {
		cache := lo.Must(cache.New(environment.Config, cache.Options{Scheme: scheme}))
		for _, index := range opts.fieldIndexers {
			lo.Must0(index(cache))
		}
		lo.Must0(cache.IndexField(ctx, &corev1.Pod{}, "spec.nodeName", func(o client.Object) []string {
			pod := o.(*corev1.Pod)
			return []string{pod.Spec.NodeName}
		}))
		c = &CacheSyncingClient{
			Client: lo.Must(client.New(environment.Config, client.Options{Scheme: scheme, Cache: &client.CacheOptions{Reader: cache}})),
		}
		go func() {
			lo.Must0(cache.Start(ctx))
		}()
		if !cache.WaitForCacheSync(ctx) {
			log.Fatalf("cache failed to sync")
		}
	} else {
		c = lo.Must(client.New(environment.Config, client.Options{Scheme: scheme}))
	}
	return &Environment{
		Environment:         environment,
		Client:              c,
		KubernetesInterface: kubernetes.NewForConfigOrDie(environment.Config),
		Version:             version,
		Done:                make(chan struct{}),
		Cancel:              cancel,
	}
}

func (e *Environment) Stop() error {
	close(e.Done)
	e.Cancel()
	return e.Environment.Stop()
}
