package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1alpha1 "package-operator.run/apis/core/v1alpha1"
	"package-operator.run/package-operator/internal/controllers"
	"package-operator.run/package-operator/internal/packages"
)

const (
	packageOperatorClusterPackageName   = "package-operator"
	packageOperatorPackageCheckInterval = 2 * time.Second
)

func runBootstrap(log logr.Logger, scheme *runtime.Scheme, opts opts) error {
	folderLoader := packages.NewFolderLoader(scheme)

	ctx := logr.NewContext(context.Background(), log.WithName("bootstrap"))
	res, err := folderLoader.Load(ctx, "/package", packages.FolderLoaderTemplateContext{})
	if err != nil {
		return err
	}

	c, err := client.New(ctrl.GetConfigOrDie(), client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	// Install CRDs or the manager wont start
	crdGK := schema.GroupKind{Group: "apiextensions.k8s.io", Kind: "CustomResourceDefinition"}
	for _, phase := range res.TemplateSpec.Phases {
		for _, obj := range phase.Objects {
			gk := obj.Object.GetObjectKind().GroupVersionKind().GroupKind()
			if gk != crdGK {
				continue
			}

			crd := &obj.Object

			// Set cache label
			labels := crd.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
			labels[controllers.DynamicCacheLabel] = "True"
			crd.SetLabels(labels)

			if err := c.Create(ctx, crd); err != nil && !errors.IsAlreadyExists(err) {
				return err
			}
		}
	}

	packageOperatorPackage := &corev1alpha1.ClusterPackage{}
	err = c.Get(ctx, client.ObjectKey{
		Name: "package-operator",
	}, packageOperatorPackage)
	if err == nil && meta.IsStatusConditionTrue(packageOperatorPackage.Status.Conditions, corev1alpha1.PackageUnpacked) {
		// Package Operator is already installed
		log.Info("Package Operator already installed, updating via in-cluster Package Operator")
		packageOperatorPackage.Spec.Image = opts.selfBootstrap
		return c.Update(ctx, packageOperatorPackage)
	}

	if err != nil && !errors.IsNotFound(err) && !meta.IsNoMatchError(err) {
		return err
	}

	log.Info("Package Operator NOT installed, self-bootstrapping")

	// Force Adoption of objects during initial bootstrap to take ownership of
	// CRDs, Namespace, ServiceAccount and ClusterRoleBinding.
	if err := os.Setenv("PKO_FORCE_ADOPTION", "1"); err != nil {
		return err
	}

	// Create PackageOperator ClusterPackage
	packageOperatorPackage = &corev1alpha1.ClusterPackage{
		ObjectMeta: metav1.ObjectMeta{
			Name: packageOperatorClusterPackageName,
		},
		Spec: corev1alpha1.PackageSpec{
			Image: opts.selfBootstrap,
		},
	}
	if err := c.Create(ctx, packageOperatorPackage); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	// Wait till PKO is ready.
	go func() {
		err := wait.PollImmediateUntilWithContext(
			ctx, packageOperatorPackageCheckInterval,
			func(ctx context.Context) (done bool, err error) {
				packageOperatorPackage := &corev1alpha1.ClusterPackage{}
				err = c.Get(ctx, client.ObjectKey{Name: packageOperatorClusterPackageName}, packageOperatorPackage)
				if err != nil {
					return done, err
				}

				if meta.IsStatusConditionTrue(packageOperatorPackage.Status.Conditions, corev1alpha1.PackageAvailable) {
					return true, nil
				}
				return false, nil
			})
		if err != nil {
			panic(err)
		}

		log.Info("Package Operator bootstrapped successfully!")
		os.Exit(0)
	}()

	// Run Manager until it has bootstrapped itself.
	return runManager(log, scheme, opts)
}
