package cmd

import (
	"slices"
	"strings"

	"github.com/f1bonacc1/process-compose/src/loader"
	"github.com/spf13/cobra"
)

// completionEntry formats a process name and its optional description as a
// cobra completion candidate.
func completionEntry(name, description string) string {
	if desc, _, _ := strings.Cut(description, "\n"); desc != "" {
		// cobra expects "name\tdescription". We replace tabs with spaces in the
		// description. cobra doesn't currently care about tabs beyond the
		// first, but spaces are shorter and zsh converts such tabs into `:`s.
		return name + "\t" + strings.ReplaceAll(desc, "\t", " ")
	}
	return name
}

// completeProcessNamesFromConfig returns a cobra completion function that
// completes process names by loading the config file(s), without a running
// server. Used by commands that start their own server (run, up).
//
// For processes that have configured descriptions, the names come concatenated
// with the first line of the descriptions in the cobra-appropriate
// "name\tdescription" format.
//
// When single is true (for `run`, which takes one PROCESS), it stops offering
// names once PROCESS is present and falls back to the shell's default file
// completion, since `run` passes the arguments after `--` to PROCESS.
func completeProcessNamesFromConfig(single bool) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if single && len(args) != 0 {
			// We can't tell whether `--` was typed: during completion,
			// cmd.ArgsLenAtDash() is always >= 0 (spf13/cobra#1877).
			return nil, cobra.ShellCompDirectiveDefault
		}
		// cobra parses flags twice during completion, and array flags append on
		// the second pass, so `-f a.yaml` arrives as [a.yaml a.yaml]. Loading a
		// file twice breaks `extends:` ("... extends itself").
		seen := make(map[string]bool)
		opts.FileNames = slices.DeleteFunc(opts.FileNames, func(name string) bool {
			dup := seen[name]
			seen[name] = true
			return dup
		})
		// Avoid aborting or printing errors for e.g. unparseable YAML config
		opts.IsInternalLoader = true
		project, err := loader.Load(opts)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names, err := project.GetLexicographicProcessNames()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		// Don't offer names that are already on the command line.
		names = slices.DeleteFunc(names, func(name string) bool { return slices.Contains(args, name) })
		comps := make([]string, len(names))
		for i, name := range names {
			comps[i] = completionEntry(name, project.Processes[name].Description)
		}
		return comps, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeProcessNamesFromServer returns a cobra completion function that
// completes process names from a running process-compose server. Used by the
// `process` subcommands (start/stop/restart), which act on a live project; this
// also surfaces dynamically scaled replica names.
func completeProcessNamesFromServer(single bool) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if single && len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		// Names only, no descriptions. TODO: consider adding process
		// descriptions to the server's `/processes` response.
		names, err := getClient().GetLexicographicProcessNames()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		// Don't offer names that are already on the command line.
		names = slices.DeleteFunc(names, func(name string) bool { return slices.Contains(args, name) })
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}
