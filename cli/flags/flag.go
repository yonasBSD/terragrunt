// Package flags provides tools that are used by all commands to create deprecation flags with strict controls.
package flags

import (
	"context"
	"flag"

	"github.com/gruntwork-io/terragrunt/internal/cli"
)

var _ = cli.Flag(new(Flag))

// Flag is a wrapper for `cli.Flag` that avoids displaying deprecated flags in help, but registers their flag names and environment variables.
type Flag struct {
	cli.Flag
	deprecatedFlags DeprecatedFlags
}

// NewFlag returns a new Flag instance.
func NewFlag(new cli.Flag, opts ...Option) *Flag {
	flag := &Flag{
		Flag: new,
	}

	for _, opt := range opts {
		opt(flag)
	}

	return flag
}

func NewMovedFlag(deprecatedFlag cli.Flag, newCommandName string, regControlsFn RegisterStrictControlsFunc) *Flag {
	return NewFlag(nil, WithDeprecatedMovedFlag(deprecatedFlag, newCommandName, regControlsFn))
}

// TakesValue implements `github.com/urfave/cli.DocGenerationFlag` required to generate help.
// TakesValue returns `true` for all flags except boolean ones that are `false` or `true` inverted.
func (newFlag *Flag) TakesValue() bool {
	if newFlag.Flag.Value() == nil {
		return false
	}

	val, ok := newFlag.Flag.Value().Get().(bool)

	if newFlag.Flag.Value().IsNegativeBoolFlag() {
		val = !val
	}

	return !ok || !val
}

// Value implements `cli.Flag` interface.
func (newFlag *Flag) Value() cli.FlagValue {
	for _, deprecatedFlag := range newFlag.deprecatedFlags {
		if deprecatedFlagValue := deprecatedFlag.Value(); deprecatedFlagValue.IsSet() && deprecatedFlag.newValueFn != nil {
			newValue := deprecatedFlag.newValueFn(deprecatedFlagValue)
			newFlag.Flag.Value().Getter(deprecatedFlagValue.GetName()).Set(newValue) //nolint:errcheck
		}
	}

	return newFlag.Flag.Value()
}

// Apply implements `cli.Flag` interface.
func (newFlag *Flag) Apply(set *flag.FlagSet) error {
	if err := newFlag.Flag.Apply(set); err != nil {
		return err
	}

	for _, deprecated := range newFlag.deprecatedFlags {
		if deprecated.Flag == newFlag.Flag {
			if err := cli.ApplyFlag(deprecated, set); err != nil {
				return err
			}

			continue
		}

		if err := deprecated.Flag.Apply(set); err != nil {
			return err
		}
	}

	return nil
}

// RunAction implements `cli.Flag` interface.
func (newFlag *Flag) RunAction(ctx *cli.Context) error {
	for _, deprecated := range newFlag.deprecatedFlags {
		if err := deprecated.Evaluate(ctx); err != nil {
			return err
		}

		if deprecated.Flag == nil || deprecated.Flag == newFlag.Flag || !deprecated.Value().IsSet() {
			continue
		}

		if err := deprecated.RunAction(ctx); err != nil {
			return err
		}
	}

	if deprecated, ok := newFlag.Flag.(interface {
		Evaluate(ctx context.Context) error
	}); ok {
		if err := deprecated.Evaluate(ctx); err != nil {
			return err
		}
	}

	return newFlag.Flag.RunAction(ctx)
}

// ParseEnvVars parses env vars values specified in the flag.
// The value will be assigned to the `Destination` field.
// The value can also be retrieved using `flag.Value().Get()`.
func (newFlag *Flag) ParseEnvVars() error {
	return newFlag.Apply(new(flag.FlagSet))
}
