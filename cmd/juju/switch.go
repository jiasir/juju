package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"

	"launchpad.net/gnuflag"
	"launchpad.net/juju-core/cmd"
	"launchpad.net/juju-core/environs"
	"launchpad.net/juju-core/environs/config"
)

type SwitchCommand struct {
	cmd.CommandBase
	EnvName string
	List    bool
}

var switchDoc = `Show or change the default juju environment name.

If no command line parameters are passed, switch will output the current
environment as defined by the file $JUJU_HOME/current-environment.

If a command line parameter is passed in, that value will is stored in the
current environment file if it represents a valid environment name as
specified in the environments.yaml file.
`

func (c *SwitchCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "switch",
		Args:    "[environment name]",
		Purpose: "show or change the default juju environment name",
		Doc:     switchDoc,
		Aliases: []string{"env"},
	}
}

func (c *SwitchCommand) SetFlags(f *gnuflag.FlagSet) {
	f.BoolVar(&c.List, "l", false, "list the environment names")
	f.BoolVar(&c.List, "list", false, "")
}

func (c *SwitchCommand) Init(args []string) (err error) {
	c.EnvName, err = cmd.ZeroOrOneArgs(args)
	return
}

func validEnvironmentName(name string, names []string) bool {
	for _, n := range names {
		if name == n {
			return true
		}
	}
	return false
}

func (c *SwitchCommand) Run(ctx *cmd.Context) error {
	// Switch is an alternative way of dealing with environments than using
	// the JUJU_ENV environment setting, and as such, doesn't play too well.
	// If JUJU_ENV is set we should report that as the current environment,
	// and not allow switching when it is set.
	jujuEnv := os.Getenv("JUJU_ENV")
	if jujuEnv != "" {
		if c.EnvName == "" {
			fmt.Fprintf(ctx.Stdout, "Current environment: %q (from JUJU_ENV)\n", jujuEnv)
			return nil
		} else {
			return fmt.Errorf("Cannot switch when JUJU_ENV is overriding the environment (set to %q)", jujuEnv)
		}
	}

	// Passing through the empty string reads the default environments.yaml file.
	environments, err := environs.ReadEnvirons("")
	if err != nil {
		return errors.New("Couldn't read the environment.")
	}
	names := environments.Names()
	sort.Strings(names)

	currentEnv := readCurrentEnvironment()
	if currentEnv == "" {
		currentEnv = environments.Default
	}

	// In order to have only a set environment name quoted, make a small function
	env := func() string {
		if currentEnv == "" {
			return "<not specified>"
		}
		return fmt.Sprintf("%q", currentEnv)
	}

	if c.EnvName == "" || c.EnvName == currentEnv {
		fmt.Fprintf(ctx.Stdout, "Current environment: %s\n", env())
	} else {
		// Check to make sure that the specified environment
		if !validEnvironmentName(c.EnvName, names) {
			return fmt.Errorf("%q is not a name of an existing defined environment", c.EnvName)
		}
		currentEnvironment := filepath.Join(config.JujuHome(), CurrentEnvironmentFile)
		err := ioutil.WriteFile(currentEnvironment, []byte(c.EnvName), 0644)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "Unable to write to the environment file: %q", currentEnvironment)
			return err
		}
		fmt.Fprintf(ctx.Stdout, "Changed default environment from %s to %q\n", env(), c.EnvName)
	}
	if c.List {
		fmt.Fprintf(ctx.Stdout, "\nEnvironments:\n")
		for _, name := range names {
			fmt.Fprintf(ctx.Stdout, "\t%s\n", name)
		}
	}

	return nil
}
