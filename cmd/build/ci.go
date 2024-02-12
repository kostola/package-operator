package main

import (
	"context"
	"errors"

	"pkg.package-operator.run/cardboard/run"
)

// CI targets that should only be called within the CI/CD runners.
type CI struct{}

// Unit runs unittests in CI.
func (ci *CI) Unit(ctx context.Context, _ []string) error {
	return test.Unit(ctx, "")
}

// Integration runs integration tests in CI using a KinD cluster.
func (ci *CI) Integration(ctx context.Context, _ []string) error {
	return test.Integration(ctx, "")
}

// Lint runs linters in CI to check the codebase.
func (ci *CI) Lint(_ context.Context, _ []string) error {
	return lint.check()
}

// PostPush runs autofixes in CI and validates that the repo is clean afterwards.
func (ci *CI) PostPush(ctx context.Context, args []string) error {
	self := run.Meth1(ci, ci.PostPush, args)
	err := mgr.ParallelDeps(ctx, self,
		run.Meth(generate, generate.All),
		run.Meth(lint, lint.glciFix),
		run.Meth(lint, lint.goModTidyAll),
	)
	if err != nil {
		return err
	}

	return lint.validateGitClean()
}

// Release builds binaries and releases the CLI, PKO manager, PKO webhooks and test-stub images to the given registry.
func (ci *CI) Release(ctx context.Context, args []string) error {
	self := run.Meth1(ci, ci.Release, args)

	registry := "quay.io/package-operator" // TODO?

	if len(args) > 2 {
		return errors.New("target registry as a single arg or no args for official") //nolint:goerr113
	} else if len(args) == 1 {
		registry = args[1]
	}
	if registry == "" {
		return errors.New("registry may not be empty") //nolint:goerr113
	}

	return mgr.ParallelDeps(ctx, self,
		run.Fn2(pushImage, "cli", registry),
		run.Fn2(pushImage, "package-operator-manager", registry),
		run.Fn2(pushImage, "package-operator-webhook", registry),
		run.Fn2(pushImage, "remote-phase-manager", registry),
		run.Fn2(pushImage, "test-stub", registry),
	)
}
