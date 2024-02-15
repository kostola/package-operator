package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"pkg.package-operator.run/cardboard/modules/kind"
	"pkg.package-operator.run/cardboard/modules/kubeclients"
	"pkg.package-operator.run/cardboard/run"

	corev1alpha1 "package-operator.run/apis/core/v1alpha1"
)

// Cluster focused targets.
type Cluster struct {
	*kind.Cluster
}

// NewCluster prepares a configured cluster object.
func NewCluster() *Cluster {
	return &Cluster{
		kind.NewCluster("pko",
			kind.WithClusterConfig(kindv1alpha4.Cluster{
				ContainerdConfigPatches: []string{
					// Replace `imageRegistry` with our local dev-registry.
					fmt.Sprintf(`[plugins."io.containerd.grpc.v1.cri".registry.mirrors."%s"]
endpoint = ["http://localhost:31320"]`, "quay.io"),
				},
				Nodes: []kindv1alpha4.Node{
					{
						Role: kindv1alpha4.ControlPlaneRole,
						ExtraPortMappings: []kindv1alpha4.PortMapping{
							// Open port to enable connectivity with local registry.
							{
								ContainerPort: 5001,
								HostPort:      5001,
								ListenAddress: "127.0.0.1",
								Protocol:      "TCP",
							},
						},
					},
				},
			}),
			kind.WithClientOptions{
				kubeclients.WithSchemeBuilder{corev1alpha1.AddToScheme},
			},
			kind.WithClusterInitializers{
				kind.ClusterLoadObjectsFromFiles{filepath.Join("config", "local-registry.yaml")},
			},
		),
	}
}

func NewHypershiftHostedCluster(name string, mgmtIPv4 string) *Cluster {
	return &Cluster{
		kind.NewCluster(name,
			kind.WithClusterConfig(kindv1alpha4.Cluster{
				ContainerdConfigPatches: []string{
					// Replace `imageRegistry` with our local dev-registry.
					fmt.Sprintf(`[plugins."io.containerd.grpc.v1.cri".registry.mirrors."%s"]
endpoint = ["http://%s:31320"]`, "quay.io", mgmtIPv4),
				},
				Nodes: []kindv1alpha4.Node{{Role: kindv1alpha4.ControlPlaneRole}},
			}),
		),
	}
}

// Creates the local development cluster.
func (c *Cluster) create(ctx context.Context) error {
	self := run.Meth(c, c.create)

	if err := mgr.SerialDeps(ctx, self, c); err != nil {
		return err
	}

	if err := os.MkdirAll(".cache/integration", 0o755); err != nil {
		return err
	}

	err := mgr.ParallelDeps(ctx, self,
		run.Fn2(pushImage, "package-operator-manager", "localhost:5001/package-operator"),
		run.Fn2(pushImage, "package-operator-webhook", "localhost:5001/package-operator"),
		run.Fn2(pushImage, "remote-phase-manager", "localhost:5001/package-operator"),
		run.Fn2(pushImage, "test-stub", "localhost:5001/package-operator"),
	)
	if err != nil {
		return err
	}

	if err := os.Setenv("PKO_REPOSITORY_HOST", "localhost:5001"); err != nil {
		return err
	}

	err = mgr.ParallelDeps(ctx, self,
		run.Fn2(pushPackage, "remote-phase", "localhost:5001/package-operator"),
		run.Fn2(pushPackage, "test-stub", "localhost:5001/package-operator"),
		run.Fn2(pushPackage, "test-stub-multi", "localhost:5001/package-operator"),
	)
	if err != nil {
		return err
	}

	err = mgr.ParallelDeps(ctx, self,
		run.Fn2(pushPackage, "package-operator", "localhost:5001/package-operator"),
	)
	if err != nil {
		return err
	}

	if err := os.Unsetenv("PKO_REPOSITORY_HOST"); err != nil {
		return err
	}

	return nil
}

// Destroys the local development cluster.
func (c *Cluster) destroy(ctx context.Context) error {
	return c.Destroy(ctx)
}
