package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"sigs.k8s.io/yaml"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"pkg.package-operator.run/cardboard/run"
)

const hostedClName = "pko-hs-hc"

// Dev focused commands using local development environment.
type Dev struct{}

// PreCommit runs linters and code-gens for pre-commit.
func (dev *Dev) PreCommit(ctx context.Context, args []string) error {
	self := run.Meth1(dev, dev.PreCommit, args)
	return mgr.ParallelDeps(ctx, self,
		run.Meth(generate, generate.All),
		run.Meth(lint, lint.glciFix),
		run.Meth(lint, lint.goModTidyAll),
	)
}

// Generate code, api docs, install files.
func (dev *Dev) Generate(ctx context.Context, args []string) error {
	self := run.Meth1(dev, dev.Generate, args)

	return mgr.ParallelDeps(
		ctx, self,
		run.Meth(generate, generate.code),
		run.Meth(generate, generate.docs),
		run.Meth(generate, generate.installYamlFile),
		run.Meth(generate, generate.selfBootstrapJob),
		run.Meth(generate, generate.selfBootstrapJobLocal),
	)
}

// Unit runs local unittests.
func (dev *Dev) Unit(ctx context.Context, args []string) error {
	var filter string
	switch len(args) {
	case 0:
		// nothing
	case 1:
		filter = args[0]
	default:
		return errors.New("only supports a single argument") //nolint:goerr113
	}
	return test.Unit(ctx, filter)
}

// Integration runs local integration tests in a KinD cluster.
func (dev *Dev) Integration(ctx context.Context, args []string) error {
	var filter string
	switch len(args) {
	case 0:
		// nothing
	case 1:
		filter = args[0]
	default:
		return errors.New("only supports a single argument") //nolint:goerr113
	}
	return test.Integration(ctx, false, filter)
}

// Lint runs local linters to check the codebase.
func (dev *Dev) Lint(_ context.Context, _ []string) error {
	return lint.glciCheck()
}

// LintFix tries to fix linter issues.
func (dev *Dev) LintFix(_ context.Context, _ []string) error {
	return lint.glciFix()
}

// Create the local development cluster.
func (dev *Dev) Create(ctx context.Context, _ []string) error {
	return cluster.create(ctx)
}

// Create the local development cluster and load CRDs.
func (dev *Dev) CreateWithCrds(ctx context.Context, args []string) error {
	self := run.Meth1(dev, dev.CreateWithCrds, args)
	if err := mgr.ParallelDeps(ctx, self,
		run.Meth1(dev, dev.Create, []string{}),
		run.Meth(generate, generate.code),
	); err != nil {
		return err
	}

	// get cluster clients
	clients, err := cluster.Clients()
	if err != nil {
		return err
	}

	// load CRDs
	entries, err := os.ReadDir(filepath.Join("config", "crds"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			entryPath := filepath.Join("config", "crds", entry.Name())
			if err = clients.CreateAndWaitFromFiles(ctx, []string{entryPath}); err != nil {
				return err
			}
		}
	}

	return nil
}

// Destroy the local development cluster.
func (dev *Dev) Destroy(ctx context.Context, _ []string) error {
	return cluster.destroy(ctx)
}

// Create the local Hypershift development environment.
func (dev *Dev) HypershiftCreate(ctx context.Context, args []string) error {
	self := run.Meth1(dev, dev.HypershiftCreate, args)
	if err := mgr.SerialDeps(ctx, self, run.Meth1(dev, dev.CreateWithCrds, []string{})); err != nil {
		return err
	}

	// get mgmt cluster clients
	clClients, err := cluster.Clients()
	if err != nil {
		return fmt.Errorf("can't get client for mgmt cluster %s: %w", cluster.Name(), err)
	}

	// install hosted cluster CRD into mgmt cluster
	hcCrdPath := filepath.Join("integration", "package-operator", "testdata", "hostedclusters.crd.yaml")
	if err = clClients.CreateAndWaitFromFiles(ctx, []string{hcCrdPath}); err != nil {
		return fmt.Errorf("can't apply HostedCluster CRD to mgmt cluster %s: %w", cluster.Name(), err)
	}

	// create package-operator-remote-phase-manager ClusterRole in mgmt cluster
	rpmCrPath := filepath.Join("config", "packages", "package-operator", "rbac",
		"package-operator-remote-phase-manager.ClusterRole.yaml")
	if err = clClients.CreateAndWaitFromFiles(ctx, []string{rpmCrPath}); err != nil {
		return fmt.Errorf("can't create remote phase manager ClusterRole in mgmt cluster %s: %w", cluster.Name(), err)
	}

	// create hosted cluster
	hostedCl := NewHypershiftHostedCluster(hostedClName, "pko-control-plane")
	if err = hostedCl.create(ctx); err != nil {
		return fmt.Errorf("can't create hosted cluster %s: %w", hostedCl.Name(), err)
	}

	// get kubeconfig of hosted cluster and replace hostname with cluster IP
	hostedClKubeconfig, err := hostedCl.Kubeconfig(true)
	if err != nil {
		return fmt.Errorf("can't get Kubeconfig of hosted cluster %s: %w", hostedCl.Name(), err)
	}

	// create namespace
	namespaceName := fmt.Sprintf("default-%s", hostedClName)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}}
	if err = clClients.CreateAndWaitForReadiness(ctx, namespace); err != nil {
		return fmt.Errorf("can't create hosted cluster namespace in mgmt cluster %s: %w", cluster.Name(), err)
	}

	// create secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service-network-admin-kubeconfig",
			Namespace: namespaceName,
		},
		Data: map[string][]byte{
			"kubeconfig": []byte(hostedClKubeconfig),
		},
	}
	if err = clClients.CreateAndWaitForReadiness(ctx, secret); err != nil {
		return fmt.Errorf("can't create kubeconfig secret in mgmt cluster %s: %w", cluster.Name(), err)
	}

	// create hosted cluster
	yamlDefinition := fmt.Sprintf(`
apiVersion: hypershift.openshift.io/v1beta1
kind: HostedCluster
metadata:
  name: %s
  namespace: default
`, hostedClName)
	hostedClResource := &unstructured.Unstructured{}
	if err = yaml.Unmarshal([]byte(yamlDefinition), &hostedClResource); err != nil {
		return fmt.Errorf("can't unmarshal HostedCluster yaml definition: %w", err)
	}
	if err = clClients.CreateAndWaitForReadiness(ctx, hostedClResource); err != nil {
		return fmt.Errorf("can't create HostedCluster in mgmt cluster %s: %w", cluster.Name(), err)
	}

	return nil
}

// Destroy the local Hypershift development environment.
func (dev *Dev) HypershiftDestroy(ctx context.Context, args []string) error {
	hostedCl := NewHypershiftHostedCluster(hostedClName, "pko-control-plane")
	if err := hostedCl.destroy(ctx); err != nil {
		return err
	}

	self := run.Meth1(dev, dev.HypershiftDestroy, args)
	return mgr.SerialDeps(ctx, self, run.Meth1(dev, dev.Destroy, []string{}))
}
